package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// cline 参考模版文件（agent/Json_modles/cline/）：
// Cline 独立版桌面端的配置集中在 ~/.cline/data/settings/（cline_mcp_settings.json、
// models.json、providers.json），全局规则在 ~/.agents/AGENTS.md。
// 配置项与源码直接读写真实文件（双向同步，无模板中转）；参考模版仅当真实
// 文件不存在时作为首次编辑初始值。MCP 注入直接改动真实路径。

// clineTemplatesDir 返回 cline 参考模版文件所在目录（agent/Json_modles/cline）。
func clineTemplatesDir() string { return jsonModelsDir("cline") }

// clineTemplateFile 返回 Cline MCP 配置首次初始值模板路径。
func clineTemplateFile() string { return filepath.Join(clineTemplatesDir(), "cline_mcp_settings.json") }

// clineModelsTemplateFile / clineProvidersTemplateFile / clineAgentsTemplateFile
// 返回 models.json / providers.json / AGENTS.md 参考模版路径。
func clineModelsTemplateFile() string    { return filepath.Join(clineTemplatesDir(), "models.json") }
func clineProvidersTemplateFile() string { return filepath.Join(clineTemplatesDir(), "providers.json") }
func clineAgentsTemplateFile() string    { return filepath.Join(clineTemplatesDir(), "AGENTS.md") }

// clineMcpRealFile 返回 Cline 真实 MCP 配置路径（client-paths.json：cline/config）。
// 可用 CLINE_MCP_SETTINGS 环境变量覆盖（如 VSCode 扩展版的 globalStorage 路径）。
func clineMcpRealFile() string {
	if p := os.Getenv("CLINE_MCP_SETTINGS"); p != "" {
		return p
	}
	return clientPath("cline", "config")
}

// clineModelsRealFile / clineProvidersRealFile 返回 models.json / providers.json 真实路径。
func clineModelsRealFile() string    { return clientPath("cline", "models") }
func clineProvidersRealFile() string { return clientPath("cline", "providers") }

// clineAgentsRealFile 返回 AGENTS.md 真实路径（client-paths.json：cline/agents）。
func clineAgentsRealFile() string { return clientPath("cline", "agents") }

// clineGlobalStateRealFile 返回 Cline CLI 运行时状态文件路径
// （client-paths.json：cline/globalstate，~/.cline/data/globalState.json）。
// 该文件由 Cline 自行维护（活文件），不做模板化部署：编辑页签直接读写真实文件本身。
func clineGlobalStateRealFile() string { return clientPath("cline", "globalstate") }

// writeClineDeploy 原子覆写目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。含 apiKey 等敏感凭据，落盘权限收紧为 0600。
func writeClineDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(fp, out, 0o600)
}

// getClineText 读取文本类配置：直接读真实文件，真实文件不存在时用参考模版
// 内容作首次编辑初始值（exists=false，filePath 为真实路径）。
func getClineText(fp, presetFile string) (content string, exists bool, filePath string, errMsg string) {
	return getRealText(fp, presetFile)
}

// saveClineJSON 校验 JSON 后直写真实文件（无模板中转：表单/源码与真实文件
// 直接双向同步）。校验通过后按原文落盘（仅剥 BOM、统一结尾换行），不经 any
// 反序列化重排，保留用户的键序、数字精度与缩进；空内容写空对象。
// 持锁串行化（与 MCP 注入等读-改-写路径互斥）。
func saveClineJSON(realFile, content string) string {
	out, errStr := normalizeJSONSave(content)
	if errStr != "" {
		return errStr
	}
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	if err := writeClineDeploy(realFile, out); err != nil {
		return err.Error()
	}
	return ""
}

// GetClineModels 读取真实 models.json（~/.cline/data/settings/models.json）；
// 真实文件不存在时用参考模版内容作首次编辑初始值。
func GetClineModels() (content string, exists bool, filePath string, errMsg string) {
	return getClineText(clineModelsRealFile(), clineModelsTemplateFile())
}

// SaveClineModels 保存 models.json：直写真实路径 ~/.cline/data/settings/models.json。
func SaveClineModels(content string) string {
	return saveClineJSON(clineModelsRealFile(), content)
}

// GetClineProviders 读取真实 providers.json；真实文件不存在时用参考模版作首次初始值。
func GetClineProviders() (content string, exists bool, filePath string, errMsg string) {
	return getClineText(clineProvidersRealFile(), clineProvidersTemplateFile())
}

// SaveClineProviders 保存 providers.json：直写真实路径 ~/.cline/data/settings/providers.json。
func SaveClineProviders(content string) string {
	return saveClineJSON(clineProvidersRealFile(), content)
}

// GetClineAgents 读取真实 AGENTS.md（~/.agents/AGENTS.md，Cline 官方跨工具全局规则）；
// 真实文件不存在时用参考模版作首次编辑初始值。
func GetClineAgents() (content string, exists bool, filePath string, errMsg string) {
	return getClineText(clineAgentsRealFile(), clineAgentsTemplateFile())
}

// SaveClineAgents 保存 AGENTS.md：直写真实路径 ~/.agents/AGENTS.md。
// 纯文本校验 UTF-8 合法性后原样写入，补结尾换行。
func SaveClineAgents(content string) string {
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
	if err := writeClineDeploy(clineAgentsRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}

// GetClineGlobalState 读取 globalState.json 内容（直接读真实文件）。
func GetClineGlobalState() (content string, exists bool, filePath string, errMsg string) {
	return getClineText(clineGlobalStateRealFile(), "")
}

// SaveClineGlobalState 保存 globalState.json：JSON 校验后原子覆写真实文件。
func SaveClineGlobalState(content string) string {
	return saveClineJSON(clineGlobalStateRealFile(), content)
}
