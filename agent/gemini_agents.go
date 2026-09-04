package agent

import (
	"strings"
)

// Gemini CLI 的 GEMINI.md（全局规则，Markdown）：直接读写真实文件 ~/.gemini/GEMINI.md
//（无模板中转；模板仅作真实文件不存在时的首次初始值）。
// 纯文本校验 UTF-8 合法性后原样写入，覆盖前不备份。

// GetGeminiAgents 读取真实 GEMINI.md；不存在时用参考模版作首次编辑初始值。
func GetGeminiAgents() (content string, exists bool, filePath string, errMsg string) {
	return getRealText(geminiAgentsRealFile(), geminiAgentsTemplateFile())
}

// SaveGeminiAgents 保存 GEMINI.md：直写真实路径 ~/.gemini/GEMINI.md。
func SaveGeminiAgents(content string) string {
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
	if err := writeGeminiDeploy(geminiAgentsRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
