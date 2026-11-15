package clients

import "llm-router-go/agent/core"

// pi 的 MCP 服务配置文件路径（client-paths.json：pi/mcp，~ 自动展开）。
// 与 pi 主配置、全局设置分开存放，由前端并入 pi 编辑页统一维护。
// 参考模版 agent/Json_modles/pi/mcp.json 仅作真实文件不存在时的首次初始值。

// GetPiMcp 读取真实 mcp.json（~/.pi/agent/mcp.json）；真实文件不存在时用参考
// 模版内容作首次编辑初始值（exists=false，filePath 为真实路径）。
func GetPiMcp() (content string, exists bool, filePath string, errMsg string) {
	return core.GetRealText(piMcpRealFile(), piMcpTemplateFile())
}

// SavePiMcp 校验并保存 pi MCP 配置：直写真实路径 ~/.pi/agent/mcp.json（无模板中转）。
// 按原文落盘（保留键序/精度），持锁串行化。
func SavePiMcp(content string) string {
	out, errStr := core.NormalizeJSONSave(content)
	if errStr != "" {
		return errStr
	}
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	if err := writePiDeploy(piMcpRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
