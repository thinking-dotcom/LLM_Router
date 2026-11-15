package clients

import (
	"llm-router-go/agent/core"

	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Deep Code CLI（npm 包 @vegamo/deepcode-cli，bin: deepcode）接入契约。
// 字段口径依据官方文档 docs/configuration.md + docs/permission.md（lessweb/deepcode-cli）：
// 前端面板受管键 = env.{BASE_URL,API_KEY} / model / thinkingEnabled / reasoningEffort /
// temperature / contextWindow / autoCompactWindow / multimodal / debugLogEnabled /
// telemetryEnabled / notify / webSearchTool / filesApiEnabled / filesApiTimeoutMs /
// permissions.{allow,deny,ask,defaultMode}；未受管的顶层键（mcpServers / enabledSkills /
// statusline / permissions.addWorkingDirs / Files API 的其它阈值）原样保留。
//
// 以下字段均取自 0.4.0 包内代码（cli.js 的 settings.ts / mcp 合并逻辑）核对：
//
//   - 主配置 ~/.deepcode/settings.json（严格 JSON；VSCode 插件共享同一份）。用户级
//     只认这个文件；项目级另有 <项目>/.deepcode/settings.json（由 CLI 自己维护）。
//   - 模型/接入地址/密钥分两处：
//     · 顶层 model（CLI 自身 /model 命令写的就是它；读优先级高于 env.MODEL）
//     · env.{BASE_URL,API_KEY}（BASE_URL 没有顶层键，只有 env 这一处）
//     BASE_URL 指向 OpenAI 兼容地址即可（CLI 自行拼 /chat/completions），
//     默认 https://api.deepseek.com；填本路由器接入地址即完成接入。
//   - thinkingEnabled(bool) + reasoningEffort(仅 low|high|max，其余值回落 max)
//     为顶层键；请求侧落成 thinking:{type:enabled|disabled} + reasoning_effort
//     （经 OpenAI SDK 的 extra_body 合并进请求体），所以路由器日志「原始推理」列
//     会显示 reasoning_effort=low|high|max。
//   - MCP：settings.json 顶层 mcpServers，**仅 stdio**（command + 可选 args/env，
//     无 type 字段）。CLI 的 mergeMcpServers 以「缺少 command」判废并静默跳过，
//     因此远程（sse/http）服务这里也不写入，避免留下 CLI 永不加载的垃圾条目。
//   - skills：~/.deepcode/skills（用户级原生位置；另有 ~/.agents/skills 互操作目录，
//     本项目只注入原生位置）。
//
// 主配置直接读写真实 settings.json（无模板中转，JSON 校验后原文直存），
// agent/Json_modles/deepcode/settings.json 仅作首次初始值（presetFile）。

// deepcodeTarget 表示 Deep Code CLI：skills + mcp + 编辑配置。
type deepcodeTarget struct {
	id         string
	name       string
	apiFormat  string
	skillsDir  string
	mcpFile    string
	configFile string
	presetFile string
}

func (d *deepcodeTarget) Info() core.TargetInfo {
	return core.TargetInfo{ID: d.id, Name: d.name, APIFormat: d.apiFormat, ConfigFormat: "json",
		SkillsSupported: true, McpSupported: true, SkillsDir: d.skillsDir, McpFile: d.mcpFile,
		ConfigFile: d.configFile, PresetFile: d.presetFile}
}
func (d *deepcodeTarget) InjectSkills(_ string, skills []core.Skill) error {
	return core.InjectSkills(d.skillsDir, skills)
}
func (d *deepcodeTarget) UndoSkills() error { return core.UndoSkills(d.skillsDir) }
func (d *deepcodeTarget) InjectMcp(_ string, servers []core.MCPServer) error {
	return mergeDeepcodeMcp(d.mcpFile, servers)
}

// ---------- 路径 helper（与 agent/Json_modles/client-paths.json 的 deepcode/* 一致） ----------

// deepcodeRealFile 真实主配置 ~/.deepcode/settings.json。
func deepcodeRealFile() string { return core.ClientPath("deepcode", "config") }

// deepcodeMcpRealFile MCP 声明文件（与主配置同一文件，顶层 mcpServers 键）。
func deepcodeMcpRealFile() string { return core.ClientPath("deepcode", "mcp") }

// deepcodeSkillsDir 技能注入目录 ~/.deepcode/skills。
func deepcodeSkillsDir() string { return core.ClientPath("deepcode", "skills") }

// deepcodePresetFile 参考模板（agent/Json_modles/deepcode/settings.json）。
func deepcodePresetFile() string {
	return filepath.Join(core.JSONModelsDir("deepcode"), "settings.json")
}

// mergeDeepcodeMcp 把 servers 合并进 settings.json 的 mcpServers 键，其余键（env /
// model / thinkingEnabled / permissions / 用户手写字段）原样保留：字节级定点替换
// （upsertJSONObjectKey）只改 mcpServers 的值，不重排用户配置与缩进。文件不存在或
// 内容为空时按空对象初始化；文件非严格 JSON 时拒绝覆写（settings.json 是 CLI 与
// VSCode 插件共用的活文件，盲目重写会把用户配置整体抹掉）。首次注入前备份 .bak。
//
// 仅写入 stdio 服务（见文件头：CLI 只认带 command 的条目）。
func mergeDeepcodeMcp(mcpFile string, servers []core.MCPServer) error {
	if mcpFile == "" {
		return nil
	}
	// 读-改-写全程持锁：与 SaveTargetConfig 等并发时避免后写覆盖先写
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	b, rerr := os.ReadFile(mcpFile)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rerr
	}
	// 首次注入前备份原始状态（已有备份则保留首次），供手动回退
	if _, err := core.BackupIfAbsent(mcpFile); err != nil {
		return err
	}
	doc := core.StripBOM(b)
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}")
	}
	newDoc, err := core.UpsertJSONObjectKey(doc, "mcpServers", func(existing json.RawMessage) (json.RawMessage, error) {
		// 替换语义：值整体替换为本次所选服务（旧条目清掉），与 MCP 页所配一致
		obj := map[string]any{}
		for _, s := range servers {
			if entry, ok := deepcodeServerToAny(s); ok {
				obj[s.Name] = entry
			}
		}
		return json.Marshal(obj)
	})
	if err != nil {
		return fmt.Errorf("解析 %s 失败，已拒绝覆写以免丢失现有配置: %w", mcpFile, err)
	}
	if err := os.MkdirAll(filepath.Dir(mcpFile), 0o755); err != nil {
		return err
	}
	// mcpServers 可能携带用户凭据（env），落盘权限收紧为 0600
	return core.WriteFileAtomic(mcpFile, newDoc, 0o600)
}

// deepcodeServerToAny 把单个 MCP 服务转成 Deep Code CLI 的 mcpServers 条目：
// 仅 stdio（command + 可选 args）。第二返回值 false 表示该类型 CLI 不支持
// （远程 sse/http），调用方跳过、不写入文件。
func deepcodeServerToAny(s core.MCPServer) (map[string]any, bool) {
	switch strings.ToLower(strings.TrimSpace(s.Type)) {
	case "", "stdio":
	default: // sse / http：CLI 的 mergeMcpServers 只认带 command 的条目
		return nil, false
	}
	if strings.TrimSpace(s.Command) == "" {
		return nil, false
	}
	m := map[string]any{"command": s.Command}
	if len(s.Args) > 0 {
		m["args"] = s.Args
	}
	return m, true
}
