package agent

import (
	"os"
	"path/filepath"
)

// CodeBuddy CLI 参考模版文件（agent/Json_modles/codebuddy）：
//   - settings.json -> 主配置（真实部署到 ~/.codebuddy/settings.json）
//   - models.json   -> 端点模型清单（真实部署到 ~/.codebuddy/models.json）
//   - mcp.json      -> MCP 声明参考示例（不自动部署：真实文件缺失时 mcp.json
//                      以空 mcpServers 起始，前端源码页签直编）
//
// 主配置直接读写真实文件（无模板中转），模板仅作首次初始值；models.json /
// mcp.json 由前端并入同一编辑页，经下方 Get/Save 直读直写真实路径。

// codebuddySettingsTemplateFile 返回主配置参考模版路径。
func codebuddySettingsTemplateFile() string {
	return filepath.Join(jsonModelsDir("codebuddy"), "settings.json")
}

// codebuddyModelsTemplateFile 返回 models.json 参考模版路径。
func codebuddyModelsTemplateFile() string {
	return filepath.Join(jsonModelsDir("codebuddy"), "models.json")
}

// 三个真实部署路径（client-paths.json：codebuddy/*，~ 自动展开）。
func codebuddySettingsRealFile() string { return clientPath("codebuddy", "config") }
func codebuddyModelsRealFile() string   { return clientPath("codebuddy", "models") }
func codebuddyMcpRealFile() string      { return clientPath("codebuddy", "mcp") }

// writeCodebuddyDeploy 原子覆写到目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。
func writeCodebuddyDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(fp, out, 0o600)
}

// GetCodebuddyModels 读取真实 models.json（~/.codebuddy/models.json）；真实文件
// 缺失时用参考模版内容作首次编辑初始值（exists=false，filePath 为真实路径）。
func GetCodebuddyModels() (content string, exists bool, filePath string, errMsg string) {
	fp := codebuddyModelsRealFile()
	if fp == "" {
		return "", false, "", "未配置 codebuddy 的 models 路径"
	}
	return getRealText(fp, codebuddyModelsTemplateFile())
}

// SaveCodebuddyModels 校验并保存 codebuddy 端点模型清单：直写真实路径
// ~/.codebuddy/models.json（无模板中转）。按原文落盘（保留键序/精度），持锁串行化。
func SaveCodebuddyModels(content string) string {
	out, errStr := normalizeJSONSave(content)
	if errStr != "" {
		return errStr
	}
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	if err := writeCodebuddyDeploy(codebuddyModelsRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}

// GetCodebuddyMcp 读取真实 mcp.json（~/.codebuddy/mcp.json）；真实文件缺失时
// 返回空内容（exists=false，filePath 为真实路径，前端回退默认空对象）。
func GetCodebuddyMcp() (content string, exists bool, filePath string, errMsg string) {
	fp := codebuddyMcpRealFile()
	if fp == "" {
		return "", false, "", "未配置 codebuddy 的 mcp 路径"
	}
	return getRealText(fp, "")
}

// SaveCodebuddyMcp 校验并保存 codebuddy MCP 声明：直写真实路径 ~/.codebuddy/mcp.json。
// 按原文落盘（保留键序/精度），持锁串行化。
func SaveCodebuddyMcp(content string) string {
	out, errStr := normalizeJSONSave(content)
	if errStr != "" {
		return errStr
	}
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	if err := writeCodebuddyDeploy(codebuddyMcpRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
