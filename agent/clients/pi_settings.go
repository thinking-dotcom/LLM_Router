package clients

import "llm-router-go/agent/core"

// pi 全局设置配置文件路径（client-paths.json：pi/settings，~ 自动展开）。
// 与 pi 主配置（models.json）、AGENTS.md 分开存放，由前端合并进 pi 编辑页统一维护。
func piSettingsFile() string {
	return core.ClientPath("pi", "settings")
}

// GetPiSettings 读取真实 settings.json（~/.pi/agent/settings.json）；真实文件
// 不存在时用参考模版内容作首次编辑初始值（exists=false，filePath 为真实路径）。
func GetPiSettings() (content string, exists bool, filePath string, errMsg string) {
	return core.GetRealText(piSettingsFile(), piSettingsTemplateFile())
}

// SavePiSettings 校验并保存 pi 全局设置：直写真实路径 ~/.pi/agent/settings.json
// （无模板中转）。按原文落盘（保留键序/精度），持锁串行化。
func SavePiSettings(content string) string {
	out, errStr := core.NormalizeJSONSave(content)
	if errStr != "" {
		return errStr
	}
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	if err := writePiDeploy(piSettingsFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
