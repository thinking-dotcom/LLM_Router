package clients

import (
	"llm-router-go/agent/core"

	"os"
	"path/filepath"
	"strings"
)

// codex 参考模版文件（agent/Json_modles/codex）：
//   - config.toml -> Codex CLI 主配置（TOML，真实部署到 ~/.codex/config.toml）
//   - AGENTS.md   -> 全局指令    （真实部署到 ~/.codex/AGENTS.md）
//
// 模版文件是「编辑源」：加载时从它读取，保存时把内容写回它并清空覆写到真实路径。

// codexTemplatesDir 返回 codex 参考模版文件所在目录（agent/Json_modles/codex）。
func codexTemplatesDir() string { return core.JSONModelsDir("codex") }

// 模版文件路径（编辑源）与真实部署路径（client-paths.json：codex/*）。
func codexTemplateFile() string       { return filepath.Join(codexTemplatesDir(), "config.toml") }
func codexAgentsTemplateFile() string { return filepath.Join(codexTemplatesDir(), "AGENTS.md") }
func codexRealFile() string           { return core.ClientPath("codex", "config") }
func codexAgentsRealFile() string     { return core.ClientPath("codex", "agents") }

// writeCodexDeploy 原子覆写目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。
func writeCodexDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return core.WriteFileAtomic(fp, out, 0o600)
}

// GetCodexAgents 读取真实 AGENTS.md；不存在时用参考模版作首次编辑初始值。
func GetCodexAgents() (content string, exists bool, filePath string, errMsg string) {
	return core.GetRealText(codexAgentsRealFile(), codexAgentsTemplateFile())
}

// SaveCodexAgents 保存 AGENTS.md：直写真实路径 ~/.codex/AGENTS.md。
func SaveCodexAgents(content string) string {
	if err := core.ValidateText(content); err != nil {
		return "文本格式错误：" + err.Error()
	}
	out := []byte(content)
	if strings.TrimSpace(content) == "" {
		out = nil
	} else if !strings.HasSuffix(content, "\n") {
		out = append(out, '\n')
	}
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	if err := writeCodexDeploy(codexAgentsRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
