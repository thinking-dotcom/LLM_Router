package agent

import (
	"os"
	"path/filepath"
)

// kimi 参考模版文件（agent/Json_modles/kimi/）：
//   - config.toml -> 主配置（真实部署到 ~/.kimi-code/config.toml）
//   - AGENTS.md    -> 全局 Agent 指令（真实部署到 ~/.kimi-code/AGENTS.md）
//
// 模版文件是「编辑源」：加载时从它读取，保存时把内容写回它并清空覆写到真实路径。
// MCP 声明在独立文件 ~/.kimi-code/mcp.json（kimiMcpRealFile），不属主配置。

// kimiTemplateFile 返回 kimi 主配置参考模版路径。
func kimiTemplateFile() string { return filepath.Join(jsonModelsDir("kimi"), "config.toml") }

// kimiRealFile 返回 kimi 真实主配置路径（client-paths.json：kimi/config）。
func kimiRealFile() string { return clientPath("kimi", "config") }

// kimiAgentsTemplateFile 返回 kimi 全局指令参考模版路径（agent/Json_modles/kimi/AGENTS.md）。
func kimiAgentsTemplateFile() string { return filepath.Join(jsonModelsDir("kimi"), "AGENTS.md") }

// kimiAgentsRealFile 返回 kimi 真实全局指令路径（client-paths.json：kimi/agents）。
func kimiAgentsRealFile() string { return clientPath("kimi", "agents") }

// kimiMcpRealFile 返回 kimi 真实 MCP 声明路径（client-paths.json：kimi/mcp）。
func kimiMcpRealFile() string { return clientPath("kimi", "mcp") }

// kimiSkillsDir 返回 kimi 用户级 skills 目录（client-paths.json：kimi/skills）。
func kimiSkillsDir() string { return clientPath("kimi", "skills") }

// writeKimiDeploy 原子覆写目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。
func writeKimiDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(fp, out, 0o600)
}
