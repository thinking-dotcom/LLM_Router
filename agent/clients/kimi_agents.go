package clients

import (
	"llm-router-go/agent/core"

	"strings"
)

// kimi 的 AGENTS.md（全局 Kimi 专属 Agent 指令）：直接读写真实文件
// ~/.kimi-code/AGENTS.md（无模板中转；模板仅作真实文件不存在时的首次初始值）。
// 纯文本校验 UTF-8 合法性后原样写入，覆盖前不备份。

// GetKimiAgents 读取真实 AGENTS.md；不存在时用参考模版作首次编辑初始值。
func GetKimiAgents() (content string, exists bool, filePath string, errMsg string) {
	return core.GetRealText(kimiAgentsRealFile(), kimiAgentsTemplateFile())
}

// SaveKimiAgents 保存 AGENTS.md：直写真实路径 ~/.kimi-code/AGENTS.md。
func SaveKimiAgents(content string) string {
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
	if err := writeKimiDeploy(kimiAgentsRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
