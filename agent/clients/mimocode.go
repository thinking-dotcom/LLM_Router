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

// MimoCode（小米 MiMo Code）适配，配置 ~/.config/mimocode/mimocode.jsonc，
// JSONC 可带注释。接入方式与 OpenCode 同族：
//
// - provider：顶层 provider 对象按 provider id 存放服务商，options.baseURL /
//   apiKey 指向本路由器；顶层 model / small_model 以「服务商/模型」引用
// - skills：注入到配置目录 ~/.config/mimocode/skills
// - mcp：合并进 mimocode.jsonc 顶层的 mcp 键（local 用 command 数组 /
//   remote 用 url，带 enabled 开关），字节级定点替换，保留文件其余内容
// - 主配置编辑：直接读写真实 mimocode.jsonc（JSONC 宽容读取，保存前校验），
//   模板仅作首次初始值
//
// 本文件仅提供常量、真实配置路径定位、MCP 合并与目标注册，不依赖
// config/format/router/core。

// mimoRealFile 返回 ~/.config/mimocode/mimocode.jsonc（主配置）。
func mimoRealFile() string {
	return core.ClientPath("mimocode", "config")
}

// mimoMcpRealFile 返回主配置路径（MiMo Code 的 mcp 声明在 mimocode.jsonc 顶层 mcp 键）。
func mimoMcpRealFile() string {
	return core.ClientPath("mimocode", "mcp")
}

// mimoSkillsDir 返回技能注入目录（配置目录下 skills 子目录）。
func mimoSkillsDir() string {
	return core.ClientPath("mimocode", "skills")
}

// mimoTemplateFile 返回参考模版路径（仅真实文件缺失时作首次初始值）。
func mimoTemplateFile() string {
	return filepath.Join(core.JSONModelsDir("mimocode"), "mimocode.jsonc")
}

// mimoServerToAny 把 core.MCPServer 转成 MiMo Code 的 mcp 配置对象：
//
//	stdio → { "type": "local",  "command": ["cmd", ...args], "enabled": true }
//	sse/http → { "type": "remote", "url": "...", "enabled": true }
func mimoServerToAny(s core.MCPServer) map[string]any {
	switch strings.ToLower(s.Type) {
	case "stdio":
		cmd := []any{s.Command}
		for _, a := range s.Args {
			cmd = append(cmd, a)
		}
		return map[string]any{"type": "local", "command": cmd, "enabled": true}
	default: // sse / http
		return map[string]any{"type": "remote", "url": s.URL, "enabled": true}
	}
}

// mergeMimoMcp 把 servers 合并进 mimocode.jsonc 顶层的 mcp 键，文件其余内容
// （键序 / 缩进 / 数字格式）原样保留（type local/remote + command 数组 /
// url + enabled）。文件不存在或为空时按空对象初始化；文件为严格 JSON 时
// 字节级定点替换（upsertJSONObjectKey），JSONC 含注释时回退整文档解析
// 合并后重排（首次注入前备份，避免活文件覆盖丢失）。
func mergeMimoMcp(mcpFile string, servers []core.MCPServer) error {
	// 读-改-写全程持锁：与 SaveTargetConfig 等并发时避免后写覆盖先写
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	if mcpFile == "" {
		return nil
	}
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
	mergeFn := func(existing json.RawMessage) (json.RawMessage, error) {
		// 替换语义：值整体替换为本次所选服务（旧条目清掉），与 MCP 页所配一致
		obj := map[string]any{}
		for _, s := range servers {
			obj[s.Name] = mimoServerToAny(s)
		}
		return json.Marshal(obj)
	}
	newDoc, err := core.UpsertJSONObjectKey(doc, "mcp", mergeFn)
	if err != nil {
		// JSONC（含注释/尾逗号）：jsoncToJSON 后整文档合并重排
		strict, jerr := core.JSONCToJSON(doc)
		if jerr != nil {
			return fmt.Errorf("解析 %s 失败，已拒绝覆写以免丢失现有配置: %w", mcpFile, err)
		}
		var root map[string]any
		if err := json.Unmarshal(strict, &root); err != nil {
			return fmt.Errorf("解析 %s 失败，已拒绝覆写以免丢失现有配置: %w", mcpFile, err)
		}
		rawMcp, hasMcp := root["mcp"]
		existing, mcpOK := rawMcp.(map[string]any)
		if hasMcp && !mcpOK {
			// 顶层 mcp 已存在但类型不符（字符串/数组等）：直接覆盖会丢用户原值
			return fmt.Errorf("解析 %s 失败，mcp 顶层键类型不匹配，已拒绝覆写以免丢失现有配置", mcpFile)
		}
		if existing == nil {
			existing = map[string]any{}
		}
		for _, s := range servers {
			existing[s.Name] = mimoServerToAny(s)
		}
		root["mcp"] = existing
		rendered, merr := json.MarshalIndent(root, "", "  ")
		if merr != nil {
			return merr
		}
		newDoc = append(rendered, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(mcpFile), 0o755); err != nil {
		return err
	}
	// mimocode.jsonc 可能含 apiKey 等凭据，落盘权限收紧为 0600
	return core.WriteFileAtomic(mcpFile, newDoc, 0o600)
}

// mimoTarget 表示 skills + mcp + 编辑配置的目标（MiMo Code）。
// skills 注入到 ~/.config/mimocode/skills；MCP 声明在主配置 mimocode.jsonc
// 顶层 mcp 键（local/remote + enabled）；主配置直接读写真实 mimocode.jsonc
// （JSONC 宽容），模板仅作首次初始值。
type mimoTarget struct {
	id         string
	name       string
	apiFormat  string
	skillsDir  string
	mcpFile    string
	configFile string
	presetFile string
}

func (m *mimoTarget) Info() core.TargetInfo {
	return core.TargetInfo{ID: m.id, Name: m.name, APIFormat: m.apiFormat, ConfigFormat: "jsonc",
		SkillsSupported: true, McpSupported: true, SkillsDir: m.skillsDir, McpFile: m.mcpFile,
		ConfigFile: m.configFile, PresetFile: m.presetFile}
}

func (m *mimoTarget) InjectSkills(_ string, skills []core.Skill) error {
	return core.InjectSkills(m.skillsDir, skills)
}

func (m *mimoTarget) UndoSkills() error { return core.UndoSkills(m.skillsDir) }

func (m *mimoTarget) InjectMcp(_ string, servers []core.MCPServer) error {
	return mergeMimoMcp(m.mcpFile, servers)
}
