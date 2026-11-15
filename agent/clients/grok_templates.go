package clients

import (
	"llm-router-go/agent/core"

	"os"
	"path/filepath"
)

// grok 参考模版文件（agent/Json_modles/grok/config.toml）：
// 模版文件是「编辑源」：加载时从它读取，保存时把内容写回它并清空覆写到真实路径 ~/.grok/config.toml。
// 字段对齐真实 Grok Build 架构：[models].default 与 [model.<key>] 块。

// grokTemplatesDir 返回 grok 参考模版文件所在目录（agent/Json_modles/grok）。
// 优先级：LLMROUTER_JSON_MODLES_DIR 环境变量 > 从当前工作目录逐级向上查找 > 可执行文件同目录。
func grokTemplatesDir() string {
	if d := os.Getenv("LLMROUTER_JSON_MODLES_DIR"); d != "" {
		return filepath.Join(d, "grok")
	}
	if wd, err := os.Getwd(); err == nil {
		c := wd
		for i := 0; i < 9; i++ {
			cand := filepath.Join(c, "agent", "Json_modles", "grok")
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
		dir := filepath.Join(filepath.Dir(exe), "agent", "Json_modles", "grok")
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
		return dir
	}
	return filepath.Join("agent", "Json_modles", "grok")
}

// 模版文件路径（编辑源）与真实部署路径（client-paths.json：grok-build/*）。
func grokTemplateFile() string { return filepath.Join(grokTemplatesDir(), "config.toml") }
func grokRealFile() string     { return core.ClientPath("grok-build", "config") }

// grok 全局规则文件（Markdown）：加载/保存均以参考模版文件 agent/Json_modles/grok/AGENTS.md 为准，
// 保存时再清空覆写真实路径 ~/.grok/AGENTS.md。纯文本校验 UTF-8 合法性后原样写入，覆盖前不备份。
func grokAgentsTemplateFile() string { return filepath.Join(grokTemplatesDir(), "AGENTS.md") }
func grokAgentsRealFile() string     { return core.ClientPath("grok-build", "agents") }

// writeGrokDeploy 原子覆写目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。
func writeGrokDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return core.WriteFileAtomic(fp, out, 0o600)
}
