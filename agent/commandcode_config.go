package agent

import (
	"os"
	"path/filepath"
)

// commandcode 参考模版文件（agent/Json_modles/commandcode/）：
// Command Code（commandcode.ai，CLI 名 cmdc / command-code）用户配置集中在 ~/.commandcode/：
//   - config.json    个人偏好：provider / model / theme / compactMode / reasoningEffort
//                    （按模型 id 键控的推理深度：low / medium / high / xhigh / max）等
//   - settings.json  项目行为：model / effort / reasoningEffort / permissions
//                    （allow / ask / deny / defaultMode / additionalDirectories / disableBypass）等
//   - providers.json BYOK 自定义服务商：name / api（openai-completions | anthropic-messages）/
//                    baseURL / apiKey（$VAR 引用或 false）/ models
//   - mcp.json       user 作用域 MCP 服务：{"mcpServers": {...}}（Claude 风格对象）
// 模版文件是「编辑源」：加载时从它读取，保存时把内容写回它并清空覆写到真实路径；
// MCP 注入走真实 mcp.json，注入后回写主配置模版保持同步（templateSync）。

// commandcodeTemplatesDir 返回 commandcode 参考模版目录（agent/Json_modles/commandcode）。
func commandcodeTemplatesDir() string { return jsonModelsDir("commandcode") }

// commandcodeTemplateFile 返回主配置 config.json 参考模版路径（编辑源）。
func commandcodeTemplateFile() string {
	return filepath.Join(commandcodeTemplatesDir(), "config.json")
}

// commandcodeSettingsTemplateFile / commandcodeProvidersTemplateFile
// 返回 settings.json / providers.json 参考模版路径。
func commandcodeSettingsTemplateFile() string {
	return filepath.Join(commandcodeTemplatesDir(), "settings.json")
}
func commandcodeProvidersTemplateFile() string {
	return filepath.Join(commandcodeTemplatesDir(), "providers.json")
}

// commandcodeRealFile / commandcodeSettingsRealFile / commandcodeProvidersRealFile /
// commandcodeMcpRealFile 返回真实部署路径（client-paths.json：commandcode/*，~ 自动展开）。
func commandcodeRealFile() string          { return clientPath("commandcode", "config") }
func commandcodeSettingsRealFile() string  { return clientPath("commandcode", "settings") }
func commandcodeProvidersRealFile() string { return clientPath("commandcode", "providers") }
func commandcodeMcpRealFile() string       { return clientPath("commandcode", "mcp") }

// writeCommandcodeDeploy 原子覆写到目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。config.json 官方以 0600 权限落盘，这里统一 0600。
func writeCommandcodeDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(fp, out, 0o600)
}

// getCommandcodeText 读取真实配置文件文本；真实文件不存在时用参考模版内容作
// 首次编辑初始值（exists=false，filePath 为真实路径）。
func getCommandcodeText(fp, presetFile string) (content string, exists bool, filePath string, errMsg string) {
	return getRealText(fp, presetFile)
}

// saveCommandcodeJSON 校验 JSON 后直写真实文件（无模板中转）。
// 按原文落盘（保留键序/精度），持锁串行化；空内容写空对象。
func saveCommandcodeJSON(realFile, content string) string {
	out, errStr := normalizeJSONSave(content)
	if errStr != "" {
		return errStr
	}
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	if err := writeCommandcodeDeploy(realFile, out); err != nil {
		return err.Error()
	}
	return ""
}

// GetCommandcodeSettings 读取真实 settings.json；不存在时用参考模版作首次初始值。
func GetCommandcodeSettings() (content string, exists bool, filePath string, errMsg string) {
	return getCommandcodeText(commandcodeSettingsRealFile(), commandcodeSettingsTemplateFile())
}

// SaveCommandcodeSettings 保存 settings.json：直写 ~/.commandcode/settings.json。
func SaveCommandcodeSettings(content string) string {
	return saveCommandcodeJSON(commandcodeSettingsRealFile(), content)
}

// GetCommandcodeProviders 读取真实 providers.json；不存在时用参考模版作首次初始值。
func GetCommandcodeProviders() (content string, exists bool, filePath string, errMsg string) {
	return getCommandcodeText(commandcodeProvidersRealFile(), commandcodeProvidersTemplateFile())
}

// SaveCommandcodeProviders 保存 providers.json：直写 ~/.commandcode/providers.json。
func SaveCommandcodeProviders(content string) string {
	return saveCommandcodeJSON(commandcodeProvidersRealFile(), content)
}
