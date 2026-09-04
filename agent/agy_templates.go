package agent

import (
	"os"
	"path/filepath"
)

// Antigravity CLI 参考模版文件（agent/Json_modles/agy）：
//   - settings.json -> 主配置（真实部署到 ~/.gemini/antigravity-cli/settings.json）
//
// 模版文件是「编辑源」：加载时从它读取，保存时把内容写回真实路径。
// MCP 声明在全局 ~/.gemini/config/mcp_config.json（agyMcpRealFile，顶层 mcpServers 键），
// skills 注入 ~/.gemini/config/skills，均不属主配置。

// agySettingsTemplateFile 返回 agy 主配置参考模版路径。
func agySettingsTemplateFile() string { return filepath.Join(jsonModelsDir("agy"), "settings.json") }

// agySettingsRealFile 返回 agy 真实主配置路径（client-paths.json：agy/config）。
func agySettingsRealFile() string { return clientPath("agy", "config") }

// agyMcpRealFile 返回 agy 真实 MCP 声明路径（client-paths.json：agy/mcp）。
func agyMcpRealFile() string { return clientPath("agy", "mcp") }

// agySkillsDir 返回 agy 全局 skills 目录（client-paths.json：agy/skills）。
func agySkillsDir() string { return clientPath("agy", "skills") }

// writeAgyDeploy 原子覆写目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。
func writeAgyDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(fp, out, 0o600)
}
