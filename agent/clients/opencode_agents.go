package clients

import (
	"llm-router-go/agent/core"

	"path/filepath"
	"strings"
)

// opencode 的 AGENTS.md（全局编码规范，Markdown）：直接读写真实文件
// ~/.config/opencode/AGENTS.md（无模板中转；模板仅作真实文件不存在时的首次初始值）。
// 纯文本校验 UTF-8 合法性后原样写入，覆盖前不备份。

// opencodeAgentsTemplateFile 返回 AGENTS.md 参考模版路径。
func opencodeAgentsTemplateFile() string {
	return filepath.Join(core.JSONModelsDir("opencode"), "AGENTS.md")
}

// opencodeAgentsRealFile 返回 AGENTS.md 真实路径（client-paths.json：opencode/agents）。
func opencodeAgentsRealFile() string {
	return core.ClientPath("opencode", "agents")
}

// GetOpencodeAgents 读取真实 AGENTS.md；不存在时用参考模版作首次编辑初始值。
func GetOpencodeAgents() (content string, exists bool, filePath string, errMsg string) {
	return core.GetRealText(opencodeAgentsRealFile(), opencodeAgentsTemplateFile())
}

// SaveOpencodeAgents 保存 AGENTS.md：直写真实路径 ~/.config/opencode/AGENTS.md。
func SaveOpencodeAgents(content string) string {
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
	if err := writeOpencodeDeploy(opencodeAgentsRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
