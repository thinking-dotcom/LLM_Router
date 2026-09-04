package agent

import (
	"strings"
)

// pi 的 AGENTS.md（项目指令，Markdown）：直接读写真实文件 ~/.pi/agent/AGENTS.md
//（无模板中转；模板仅作真实文件不存在时的首次初始值）。
// 纯文本原样写入，不做 JSON 校验，覆盖前不备份。

// GetPiAgents 读取真实 AGENTS.md；不存在时用参考模版作首次编辑初始值。
func GetPiAgents() (content string, exists bool, filePath string, errMsg string) {
	return getRealText(piAgentsRealFile(), piAgentsTemplateFile())
}

// SavePiAgents 保存 AGENTS.md：直写真实路径 ~/.pi/agent/AGENTS.md。
func SavePiAgents(content string) string {
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
	if err := writePiDeploy(piAgentsRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
