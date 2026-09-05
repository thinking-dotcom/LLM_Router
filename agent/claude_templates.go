package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// claude 参考模版文件（agent/Json_modles/claude）：
//   - settings.json -> Claude Code 主配置（真实部署到 ~/.claude/settings.json）
//
// 模版文件是「编辑源」：加载时从它读取，保存时把内容写回它并清空覆写到真实路径。
// MCP 注入走真实路径，注入/撤销后回写模版保持同步（templateSync）。

// claudeTemplatesDir 返回 claude 参考模版文件所在目录（agent/Json_modles/claude）。
func claudeTemplatesDir() string { return jsonModelsDir("claude") }

// claudeCodeTemplateFile 返回 Claude Code 主配置参考模版路径。
func claudeCodeTemplateFile() string { return filepath.Join(claudeTemplatesDir(), "settings.json") }

// claudeSettingsRealFile 返回 Claude Code 真实配置路径（client-paths.json：claude-code/config）。
func claudeSettingsRealFile() string {
	return clientPath("claude-code", "config")
}

// claudeMarkdownRealFile 返回 Claude 全局记忆/指令文件路径（client-paths.json：claude-code/md）。
func claudeMarkdownRealFile() string { return clientPath("claude-code", "md") }

// GetClaudeMarkdown 读取 ~/.claude/CLAUDE.md；不存在时返回空内容（无参考模版）。
func GetClaudeMarkdown() (content string, exists bool, filePath string, errMsg string) {
	return getRealText(claudeMarkdownRealFile(), "")
}

// SaveClaudeMarkdown 保存 ~/.claude/CLAUDE.md（纯文本 UTF-8 校验后原样写入）。
func SaveClaudeMarkdown(content string) string {
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
	if err := writeClaudeDeploy(claudeMarkdownRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}

// writeClaudeDeploy 原子覆写目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。
func writeClaudeDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(fp, out, 0o600)
}
