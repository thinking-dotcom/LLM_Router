package clients

import (
	"llm-router-go/agent/core"

	"os"
	"path/filepath"
)

// pi 参考模版文件（agent/Json_modles/pi）：
//   - models.json  -> 面板① 提供商与模型（真实部署到 ~/.pi/agent/models.json）
//   - settings.json-> 面板② 全局设置    （真实部署到 ~/.pi/agent/settings.json）
//   - AGENTS.md    -> 页签③ 项目指令    （真实部署到 ~/.pi/agent/AGENTS.md）
//
// 模版文件是「编辑源」：加载时从它读取，保存时把内容写回它并清空覆写到真实路径。

// piTemplatesDir 返回 pi 参考模版文件所在目录（agent/Json_modles/pi）。
// 优先级：LLMROUTER_JSON_MODLES_DIR 环境变量 > 从当前工作目录逐级向上查找 > 可执行文件同目录。
func piTemplatesDir() string {
	if d := os.Getenv("LLMROUTER_JSON_MODLES_DIR"); d != "" {
		return filepath.Join(d, "pi")
	}
	if wd, err := os.Getwd(); err == nil {
		c := wd
		for i := 0; i < 9; i++ {
			cand := filepath.Join(c, "agent", "Json_modles", "pi")
			if st, err := os.Stat(cand); err == nil && st.IsDir() {
				return cand
			}
			p := filepath.Dir(c)
			if p == c {
				break
			}
			c = p
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "agent", "Json_modles", "pi")
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
		return dir
	}
	return filepath.Join("agent", "Json_modles", "pi")
}

// 四个模版文件路径（编辑源）。
func piModelTemplateFile() string    { return filepath.Join(piTemplatesDir(), "models.json") }
func piSettingsTemplateFile() string { return filepath.Join(piTemplatesDir(), "settings.json") }
func piMcpTemplateFile() string      { return filepath.Join(piTemplatesDir(), "mcp.json") }
func piAgentsTemplateFile() string   { return filepath.Join(piTemplatesDir(), "AGENTS.md") }

// 四个真实部署路径（client-paths.json：pi/*，~ 自动展开）。
func piModelRealFile() string    { return core.ClientPath("pi", "config") }
func piSettingsRealFile() string { return core.ClientPath("pi", "settings") }
func piMcpRealFile() string      { return core.ClientPath("pi", "mcp") }
func piAgentsRealFile() string   { return core.ClientPath("pi", "agents") }

// writePiDeploy 原子覆写到目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。
func writePiDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return core.WriteFileAtomic(fp, out, 0o600)
}
