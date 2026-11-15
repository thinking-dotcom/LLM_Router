package clients

import (
	"llm-router-go/agent/core"

	"strings"
)

// grok 的 AGENTS.md（全局规则，Markdown）：直接读写真实文件 ~/.grok/AGENTS.md
//（无模板中转；模板仅作真实文件不存在时的首次初始值）。
// 纯文本校验 UTF-8 合法性后原样写入，覆盖前不备份。

// GetGrokAgents 读取真实 AGENTS.md；不存在时用参考模版作首次编辑初始值。
func GetGrokAgents() (content string, exists bool, filePath string, errMsg string) {
	return core.GetRealText(grokAgentsRealFile(), grokAgentsTemplateFile())
}

// SaveGrokAgents 保存 AGENTS.md：直写真实路径 ~/.grok/AGENTS.md。
func SaveGrokAgents(content string) string {
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
	if err := writeGrokDeploy(grokAgentsRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
