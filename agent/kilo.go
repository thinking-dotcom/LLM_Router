package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Kilo（kilo. 配置 ~/.config/kilo/kilo.jsonc，JSONC 可带注释）适配。
//
// 接入方式与 OpenCode 同族：主配置顶层 provider 对象按 provider id 存放
// 服务商（@ai-sdk/openai-compatible，options.baseURL/apiKey + models 表），
// 顶层 model / small_model / subagent_model 选默认模型（"服务商/模型" 格式），
// 顶层 permission 为工具权限（allow/ask/deny），顶层 mcp 为 MCP 服务器声明。
//
// - skills：注入到配置目录 ~/.config/kilo/skills（Kilo 从配置顶层 skills 或
//   配置目录的 skill/{name}/SKILL.md 发现技能）
// - mcp：合并进 kilo.jsonc 顶层的 mcp 键（Kilo 格式：local 用 command 数组 /
//   remote 用 url，带 enabled 开关），字节级定点替换，保留文件其余内容
// - 主配置编辑：直接读写真实 kilo.jsonc（JSONC 宽容读取，保存前校验），
//   模板仅作首次初始值
//
// 本文件仅提供常量、真实配置路径定位、MCP 合并与主配置读写，不依赖
// config/format/router/core。

// kiloRealFile 返回 ~/.config/kilo/kilo.jsonc（主配置）。
func kiloRealFile() string {
	return clientPath("kilo", "config")
}

// kiloMcpRealFile 返回主配置路径（Kilo 的 mcp 声明在 kilo.jsonc 顶层 mcp 键）。
func kiloMcpRealFile() string {
	return clientPath("kilo", "mcp")
}

// kiloSkillsDir 返回技能注入目录（配置目录下 skills 子目录）。
func kiloSkillsDir() string {
	return clientPath("kilo", "skills")
}

// kiloTemplateFile 返回参考模版路径（仅真实文件缺失时作首次初始值）。
func kiloTemplateFile() string {
	return filepath.Join(jsonModelsDir("kilo"), "kilo.jsonc")
}

// kiloAgentsTemplateFile 返回 AGENTS.md 参考模版路径（仅真实文件缺失时作首次初始值）。
func kiloAgentsTemplateFile() string {
	return filepath.Join(jsonModelsDir("kilo"), "AGENTS.md")
}

// kiloAgentsRealFile 返回 AGENTS.md 真实路径（client-paths.json：kilo/agents）。
func kiloAgentsRealFile() string {
	return clientPath("kilo", "agents")
}

// GetKiloAgents 读取真实 AGENTS.md；不存在时用参考模版作首次编辑初始值。
func GetKiloAgents() (content string, exists bool, filePath string, errMsg string) {
	return getRealText(kiloAgentsRealFile(), kiloAgentsTemplateFile())
}

// SaveKiloAgents 保存 AGENTS.md：直写真实路径 ~/.config/kilo/AGENTS.md。
func SaveKiloAgents(content string) string {
	if err := ValidateText(content); err != nil {
		return "文本格式错误：" + err.Error()
	}
	out := []byte(content)
	if strings.TrimSpace(content) == "" {
		out = nil
	} else if !strings.HasSuffix(content, "\n") {
		out = append(out, '\n')
	}
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	if err := writeOpencodeDeploy(kiloAgentsRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}

// kiloServerToAny 把 MCPServer 转成 Kilo 的 mcp 配置对象：
//
//	stdio → { "type": "local",  "command": ["cmd", ...args], "enabled": true }
//	sse/http → { "type": "remote", "url": "...", "enabled": true }
func kiloServerToAny(s MCPServer) map[string]any {
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

// mergeKiloMcp 把 servers 合并进 kilo.jsonc 顶层的 mcp 键，文件其余内容
// （键序 / 缩进 / 数字格式）原样保留。文件不存在或为空时按空对象初始化；
// 文件为严格 JSON 时字节级定点替换（upsertJSONObjectKey），JSONC 含注释时
// 回退整文档解析合并后重排（首次注入前备份，避免活文件覆盖丢失）。
func mergeKiloMcp(mcpFile string, servers []MCPServer) error {
	// 读-改-写全程持锁：与 SaveTargetConfig 等并发时避免后写覆盖先写
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	if mcpFile == "" {
		return nil
	}
	b, rerr := os.ReadFile(mcpFile)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rerr
	}
	// 首次注入前备份原始状态（已有备份则保留首次），供手动回退
	if _, err := backupIfAbsent(mcpFile); err != nil {
		return err
	}
	doc := stripBOM(b)
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}")
	}
	mergeFn := func(existing json.RawMessage) (json.RawMessage, error) {
		// 合并而非替换：保留目标已有 mcp 服务器（避免破坏用户原有配置）
		obj := map[string]any{}
		if len(bytes.TrimSpace(existing)) > 0 {
			if err := json.Unmarshal(existing, &obj); err != nil {
				obj = map[string]any{}
			}
		}
		for _, s := range servers {
			obj[s.Name] = kiloServerToAny(s)
		}
		return json.Marshal(obj)
	}
	newDoc, err := upsertJSONObjectKey(doc, "mcp", mergeFn)
	if err != nil {
		// JSONC（含注释/尾逗号）：jsoncToJSON 后整文档合并重排
		strict, jerr := jsoncToJSON(doc)
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
			existing[s.Name] = kiloServerToAny(s)
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
	// kilo.jsonc 可能含 apiKey 等凭据，落盘权限收紧为 0600
	return writeFileAtomic(mcpFile, newDoc, 0o600)
}

// kiloTarget 表示 skills + mcp + 编辑配置的目标（Kilo）。
// skills 注入到 ~/.config/kilo/skills；MCP 声明在主场配置 kilo.jsonc 顶层
// mcp 键（Kilo 格式：local/remote + enabled）；主配置直接读写真实
// kilo.jsonc（JSONC 宽容），模板仅作首次初始值。
type kiloTarget struct {
	id         string
	name       string
	apiFormat  string
	skillsDir  string
	mcpFile    string
	configFile string
	presetFile string
}

func (k *kiloTarget) Info() TargetInfo {
	return TargetInfo{ID: k.id, Name: k.name, APIFormat: k.apiFormat, ConfigFormat: "jsonc",
		SkillsSupported: true, McpSupported: true, SkillsDir: k.skillsDir, McpFile: k.mcpFile,
		ConfigFile: k.configFile, PresetFile: k.presetFile}
}
func (k *kiloTarget) InjectSkills(_ string, skills []Skill) error {
	return injectSkills(k.skillsDir, skills)
}
func (k *kiloTarget) UndoSkills() error { return undoSkills(k.skillsDir) }
func (k *kiloTarget) InjectMcp(_ string, servers []MCPServer) error {
	return mergeKiloMcp(k.mcpFile, servers)
}
