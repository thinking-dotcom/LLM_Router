package agent

import (
	"os"
	"path/filepath"
)

// Gemini CLI 参考模版文件（agent/Json_modles/gemini）：
//   - settings.json -> 主配置（真实部署到 ~/.gemini/settings.json）
//   - .env          -> 环境变量（真实部署到 ~/.gemini/.env）
//   - GEMINI.md     -> 全局规则（真实部署到 ~/.gemini/GEMINI.md）
//
// 模版文件是「编辑源」：加载时从它读取，保存时把内容写回它并清空覆写到真实路径。

// jsonModelsDir 返回 agent/Json_modles/<sub> 目录。
// 优先级：LLMROUTER_JSON_MODLES_DIR 环境变量 > 从当前工作目录逐级向上查找 > 可执行文件同目录。
func jsonModelsDir(sub string) string {
	if d := os.Getenv("LLMROUTER_JSON_MODLES_DIR"); d != "" {
		return filepath.Join(d, sub)
	}
	if wd, err := os.Getwd(); err == nil {
		c := wd
		for i := 0; i < 9; i++ {
			cand := filepath.Join(c, "agent", "Json_modles", sub)
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
		dir := filepath.Join(filepath.Dir(exe), "agent", "Json_modles", sub)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
		return dir
	}
	return filepath.Join("agent", "Json_modles", sub)
}

// geminiTemplatesDir 返回 gemini 参考模版文件所在目录（agent/Json_modles/gemini）。
func geminiTemplatesDir() string { return jsonModelsDir("gemini") }

// 模版文件路径（编辑源）。
func geminiSettingsTemplateFile() string { return filepath.Join(geminiTemplatesDir(), "settings.json") }
func geminiDotEnvTemplateFile() string   { return filepath.Join(geminiTemplatesDir(), ".env") }
func geminiAgentsTemplateFile() string   { return filepath.Join(geminiTemplatesDir(), "GEMINI.md") }

// 真实部署路径（client-paths.json：gemini/*，~ 自动展开）。
func geminiSettingsRealFile() string { return clientPath("gemini", "config") }
func geminiDotEnvRealFile() string   { return clientPath("gemini", "env") }
func geminiAgentsRealFile() string   { return clientPath("gemini", "agents") }

// writeGeminiDeploy 原子覆写到目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。
func writeGeminiDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(fp, out, 0o600)
}
