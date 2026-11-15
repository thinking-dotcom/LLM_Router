package clients

import (
	"llm-router-go/agent/core"

	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Copilot CLI（GitHub Copilot）接入。
//
// Copilot CLI 通过用户级环境变量（BYOK）配置模型提供方端点，模型同样以
// 环境变量提供（BYOK 校验只认环境变量 / --model，settings.json 的 model 键
// 不参与）；settings.json 另行持久化 model 与用户偏好：
//
//	COPILOT_PROVIDER_BASE_URL → 必需：提供方 API 端点 base URL
//	                             （接入本路由器时填 接入地址 + /router，OpenAI 兼容）
//	COPILOT_PROVIDER_TYPE     → 可选：openai（默认）/ azure / anthropic
//	COPILOT_PROVIDER_API_KEY  → 可选：提供方 API Key（无鉴权端点可省略）
//	COPILOT_MODEL             → BYOK 必需：官方明确三选一（COPILOT_MODEL /
//	                             COPILOT_PROVIDER_MODEL_ID / --model）
//	settings.json 的 model 键 → 模型持久化（非 BYOK 校验依据，便于回显与
//	                             非 BYOK 场景使用）
//
// 端点与模型环境变量由桌面端「配置选项」页签经 SetUserEnv / DelUserEnv
// 写入/清除（注册表 HKCU\Environment，广播 WM_SETTINGCHANGE）；settings.json
// 的面板键（model / effortLevel / allowedUrls / deniedUrls / permissions.* /
// sandbox.* / ide.* / 通用偏好）经 GetCopilotSettings / SaveCopilotSettings
// 批量读写，model 另有 Get/SaveCopilotModel 单键入口；工具审批
// （~/.copilot/permissions-config.json 的 tool_approvals + allowed_directories）
// 与「作用位置」候选（GetCopilotPermLocCandidates）同样在本文件。MCP 配置在
// ~/.copilot/mcp-config.json（Claude 风格 mcpServers 对象，与通用 MCP 注入
// mergeMcpServers 完全兼容），主配置编辑窗口指向 settings.json。
//
// 本文件仅提供常量、真实配置路径定位与上述配置文件读写，不依赖 config/format/router/core。

// Copilot BYOK 端点与模型环境变量名（写入/清除面板直接引用）。
const (
	CopilotProviderBaseURL = "COPILOT_PROVIDER_BASE_URL"
	CopilotProviderType    = "COPILOT_PROVIDER_TYPE"
	CopilotProviderAPIKey  = "COPILOT_PROVIDER_API_KEY"
	CopilotModelEnv        = "COPILOT_MODEL"
)

// copilotProviderTypeDefault 是默认提供方类型：OpenAI 兼容端点（本路由器接入方式）。
const copilotProviderTypeDefault = "openai"

// copilotSettingsRealFile 返回 ~/.copilot/settings.json（主配置：model 等用户设置）。
func copilotSettingsRealFile() string {
	return core.ClientPath("copilot", "config")
}

// copilotMcpRealFile 返回 ~/.copilot/mcp-config.json（MCP 服务器配置）。
func copilotMcpRealFile() string {
	return core.ClientPath("copilot", "mcp")
}

// copilotPermRealFile 返回 ~/.copilot/permissions-config.json（按项目位置保存的
// 工具审批：tool_approvals + allowed_directories）。
func copilotPermRealFile() string {
	return core.ClientPath("copilot", "permissions")
}

// copilotCliConfigRealFile 返回 ~/.copilot/config.json（CLI 自动管理，含
// trustedFolders——用户运行过 copilot 的信任目录清单）。
func copilotCliConfigRealFile() string {
	return core.ClientPath("copilot", "cliconfig")
}

// GetCopilotPermLocCandidates 返回「作用位置」候选目录：~/.copilot/config.json
// 的 trustedFolders（CLI 自动记录的信任/运行目录）∪ permissions-config.json
// locations 已有键（两来源均 JSONC 宽容读取，缺失或解析失败时跳过该来源），
// 去重排序。供权限配置面板自动识别实际项目路径。
func GetCopilotPermLocCandidates() (folders []string, filePath string, errMsg string) {
	seen := map[string]bool{}
	out := []string{}
	add := func(list []string) {
		for _, s := range list {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	if b, err := os.ReadFile(copilotCliConfigRealFile()); err == nil {
		if doc, jerr := core.JSONCToJSON(b); jerr == nil {
			var obj map[string]any
			if json.Unmarshal(doc, &obj) == nil {
				if tf, ok := obj["trustedFolders"].([]any); ok {
					add(anyToStrs(tf))
				}
			}
		}
	}
	if b, err := os.ReadFile(copilotPermRealFile()); err == nil {
		if doc, jerr := core.JSONCToJSON(b); jerr == nil {
			var obj map[string]any
			if json.Unmarshal(doc, &obj) == nil {
				if locs, ok := obj["locations"].(map[string]any); ok {
					keys := make([]string, 0, len(locs))
					for k := range locs {
						keys = append(keys, k)
					}
					add(keys)
				}
			}
		}
	}
	sort.Strings(out)
	return out, copilotCliConfigRealFile(), ""
}

// Copilot CLI settings.json 中由「配置选项」页签管理的其它键（用户级合法键；
// allow/deny/ask 规则仅在 MDM 受管层，用户级不读取）。沙箱键为嵌套路径。
const (
	CopilotKeyEffort       = "effortLevel"                  // 推理深度 low/medium/high/xhigh
	CopilotKeyAllowURL     = "allowedUrls"                  // URL 白名单（数组）
	CopilotKeyDenyURL      = "deniedUrls"                   // URL 黑名单（数组）
	CopilotKeyBypassPerm   = "disableBypassPermissionsMode" // 提权限制 disable/allow-auto-only
	CopilotKeyBypassParent = "permissions"                  // 嵌套父键
	CopilotKeySandbox      = "sandbox"                      // OS 沙箱
	CopilotKeyIde          = "ide"                          // IDE 集成（嵌套父键）
	// 面板用的沙箱字段名（settings.json 中按嵌套路径读写）：
	// sandbox.enabled / sandbox.allowBypass / sandbox.userPolicy.network.allowLocalNetwork
	CopilotFieldSandboxEna    = "sandboxEnabled"
	CopilotFieldSandboxBypass = "sandboxAllowBypass"
	CopilotFieldSandboxLocal  = "sandboxAllowLocal"
	// 面板用的 IDE 字段名：ide.autoConnect / ide.openDiffOnEdit
	CopilotFieldIdeConn = "ideAutoConnect"
	CopilotFieldIdeDiff = "ideOpenDiff"
)

// copilotGeneralBoolFields 「通用设置」面板管理的顶层布尔键（三态串 ”/true/false）。
var copilotGeneralBoolFields = []string{
	"stream", "compactPaste", "notifications", "beep", "autoUpdate",
	"memory", "askUser", "includeCoAuthoredBy", "continueOnAutoMode",
	"experimental", "mouse", "renderMarkdown", "updateTerminalTitle", "terminalProgress",
}

// copilotGeneralEnumFields 「通用设置」面板管理的顶层枚举键 → 合法值集合。
var copilotGeneralEnumFields = map[string]map[string]bool{
	"theme":              {"default": true, "github": true, "dim": true, "high-contrast": true, "colorblind": true},
	"banner":             {"always": true, "once": true, "never": true},
	"keepAlive":          {"off": true, "on": true, "busy": true},
	"autoUpdatesChannel": {"stable": true, "prerelease": true},
}

// copilotIdeFields 面板管理的 IDE 字段 → settings.json 嵌套路径（逐段）。
var copilotIdeFields = map[string][]string{
	CopilotFieldIdeConn: {CopilotKeyIde, "autoConnect"},
	CopilotFieldIdeDiff: {CopilotKeyIde, "openDiffOnEdit"},
}

// copilotEffortLevels 合法推理深度档位（settings.json effortLevel）。
var copilotEffortLevels = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true}

// copilotBypassModes 合法的 allow-all 提权限制档位。
var copilotBypassModes = map[string]bool{"disable": true, "allow-auto-only": true}

// copilotSandboxFields 面板管理的沙箱字段 → settings.json 嵌套路径（逐段）。
var copilotSandboxFields = map[string][]string{
	CopilotFieldSandboxEna:    {"sandbox", "enabled"},
	CopilotFieldSandboxBypass: {"sandbox", "allowBypass"},
	CopilotFieldSandboxLocal:  {"sandbox", "userPolicy", "network", "allowLocalNetwork"},
}

// copilotSettingsDefaults 返回「配置选项」页签管理的 settings.json 键的默认值。
// allowedUrls / deniedUrls 类型为 []string{}；沙箱字段为 "" / "true" / "false" 三态串。
func copilotSettingsDefaults() map[string]any {
	out := map[string]any{
		"model":                   "",
		CopilotKeyEffort:          "",
		CopilotKeyAllowURL:        []string{},
		CopilotKeyDenyURL:         []string{},
		CopilotKeyBypassPerm:      "",
		CopilotFieldSandboxEna:    "",
		CopilotFieldSandboxBypass: "",
		CopilotFieldSandboxLocal:  "",
		CopilotFieldIdeConn:       "",
		CopilotFieldIdeDiff:       "",
	}
	for _, k := range copilotGeneralBoolFields {
		out[k] = ""
	}
	for k := range copilotGeneralEnumFields {
		out[k] = ""
	}
	return out
}

// GetCopilotSettings 读取 settings.json 中面板管理的全部键（model / effortLevel /
// allowedUrls / deniedUrls / permissions.disableBypassPermissionsMode）。
// JSONC 宽容读取；settings.json 不存在时返回默认值与 exists=false。
func GetCopilotSettings() (out map[string]any, exists bool, filePath string, errMsg string) {
	file := copilotSettingsRealFile()
	out = copilotSettingsDefaults()
	b, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return out, false, file, ""
		}
		return nil, false, file, err.Error()
	}
	doc, jerr := core.JSONCToJSON(b)
	if jerr != nil || len(bytes.TrimSpace(doc)) == 0 {
		return nil, false, file, "settings.json 解析失败（可切换 JSON 源码页签手动修复）"
	}
	var obj map[string]any
	if err := json.Unmarshal(doc, &obj); err != nil {
		return nil, false, file, "settings.json 解析失败：" + err.Error()
	}
	if v, ok := obj["model"].(string); ok {
		out["model"] = v
	}
	if v, ok := obj[CopilotKeyEffort].(string); ok {
		out[CopilotKeyEffort] = v
	}
	if v, ok := obj[CopilotKeyAllowURL].([]any); ok {
		out[CopilotKeyAllowURL] = anyToStrs(v)
	}
	if v, ok := obj[CopilotKeyDenyURL].([]any); ok {
		out[CopilotKeyDenyURL] = anyToStrs(v)
	}
	if pm, ok := obj[CopilotKeyBypassParent].(map[string]any); ok {
		if v, ok := pm[CopilotKeyBypassPerm].(string); ok {
			out[CopilotKeyBypassPerm] = v
		}
	}
	// 沙箱嵌套：sandbox.enabled / sandbox.allowBypass / sandbox.userPolicy.network.allowLocalNetwork
	if sb, ok := obj[CopilotKeySandbox].(map[string]any); ok {
		if v, ok := sb["enabled"].(bool); ok {
			out[CopilotFieldSandboxEna] = boolTri(v)
		}
		if v, ok := sb["allowBypass"].(bool); ok {
			out[CopilotFieldSandboxBypass] = boolTri(v)
		}
		if up, ok := sb["userPolicy"].(map[string]any); ok {
			if net, ok := up["network"].(map[string]any); ok {
				if v, ok := net["allowLocalNetwork"].(bool); ok {
					out[CopilotFieldSandboxLocal] = boolTri(v)
				}
			}
		}
	}
	// 通用布尔三态键（存在才回显，缺失保持 ""）
	for _, k := range copilotGeneralBoolFields {
		if v, ok := obj[k].(bool); ok {
			out[k] = boolTri(v)
		}
	}
	// 通用枚举键
	for k := range copilotGeneralEnumFields {
		if v, ok := obj[k].(string); ok {
			out[k] = v
		}
	}
	// IDE 嵌套：ide.autoConnect / ide.openDiffOnEdit
	if ide, ok := obj[CopilotKeyIde].(map[string]any); ok {
		if v, ok := ide["autoConnect"].(bool); ok {
			out[CopilotFieldIdeConn] = boolTri(v)
		}
		if v, ok := ide["openDiffOnEdit"].(bool); ok {
			out[CopilotFieldIdeDiff] = boolTri(v)
		}
	}
	return out, true, file, ""
}

// boolTri 把布尔值转成面板用的三态字符串（"true"/"false"）。
func boolTri(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// SaveCopilotSettings 把面板管理的键写入 settings.json（严格 JSON 定点替换，
// 保留文件其余内容与格式）。map 中缺失的键保持不动；空串 / 空数组 / 非法值
// 移除对应键；permissions.disableBypassPermissionsMode 写入嵌套 permissions 对象
// （移除后若 permissions 为空对象则删除顶层键）。文件不是严格 JSON 时对两类情况
// 分别处理：仅含 JSONC 注释扩展时先剥离注释再定点写入（保留键序/缩进/数字格式，
// 用户手写的注释会丢失，但面板从此可用）；连剥离后仍解析失败（语法损坏）才
// 拒绝覆写，避免把用户手写的 settings.json 整体重排或抹掉。首次写入前备份。
// 返回错误信息，成功返回空串。
func SaveCopilotSettings(m map[string]any) string {
	file := copilotSettingsRealFile()
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	b, rerr := os.ReadFile(file)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rerr.Error()
	}
	if _, err := core.BackupIfAbsent(file); err != nil {
		return err.Error()
	}
	doc := b
	var err error
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}")
	} else if !json.Valid(doc) {
		// 严格 JSON 解析失败的两种情形分开对待：
		//   1) 仅含 JSONC 注释 —— 剥离注释后即合法，继续写入（本函数所有
		//      upsert/delete 都要求严格 JSON，不剥离会导致面板永远写不进去）；
		//   2) 剥离后仍非法（尾逗号、语法损坏）—— 保持原拒绝行为，提示改源码页签。
		stripped, jerr := core.JSONCToJSON(doc)
		if jerr != nil || !json.Valid(stripped) {
			return "settings.json 不是严格 JSON（含注释或语法错误），请切换 JSON 源码页签编辑"
		}
		doc = stripped
	}

	// 顶层字符串键：model / effortLevel（空串移除）
	for _, k := range []string{"model", CopilotKeyEffort} {
		v, present := m[k]
		if !present {
			continue
		}
		s, _ := v.(string)
		if s == "" {
			doc, _, err = core.DeleteJSONObjectKey(doc, k)
		} else if k == CopilotKeyEffort && !copilotEffortLevels[s] {
			return "effortLevel 仅支持 low / medium / high / xhigh"
		} else {
			doc, err = core.UpsertJSONObjectKey(doc, k, func(_ json.RawMessage) (json.RawMessage, error) {
				return json.Marshal(s)
			})
		}
		if err != nil {
			return copilotRejectMsg(file, err)
		}
	}

	// 顶层数组键：allowedUrls / deniedUrls（空数组移除）
	for _, k := range []string{CopilotKeyAllowURL, CopilotKeyDenyURL} {
		v, present := m[k]
		if !present {
			continue
		}
		arr := anyToStrSlice(v)
		if len(arr) == 0 {
			doc, _, err = core.DeleteJSONObjectKey(doc, k)
		} else {
			doc, err = core.UpsertJSONObjectKey(doc, k, func(_ json.RawMessage) (json.RawMessage, error) {
				return json.Marshal(arr)
			})
		}
		if err != nil {
			return copilotRejectMsg(file, err)
		}
	}

	// 嵌套键：permissions.disableBypassPermissionsMode
	if v, present := m[CopilotKeyBypassPerm]; present {
		s, _ := v.(string)
		if s == "" {
			doc, err = deleteCopilotBypassPerm(doc)
		} else if !copilotBypassModes[s] {
			return "disableBypassPermissionsMode 仅支持 disable / allow-auto-only"
		} else {
			doc, err = core.UpsertJSONObjectKey(doc, CopilotKeyBypassParent, func(existing json.RawMessage) (json.RawMessage, error) {
				obj := map[string]any{}
				if len(bytes.TrimSpace(existing)) > 0 {
					if jerr := json.Unmarshal(existing, &obj); jerr != nil {
						obj = map[string]any{}
					}
				}
				obj[CopilotKeyBypassPerm] = s
				return json.Marshal(obj)
			})
		}
		if err != nil {
			return copilotRejectMsg(file, err)
		}
	}

	// 嵌套键：sandbox.* 沙箱权限（enabled / allowBypass / userPolicy.network.allowLocalNetwork）
	if copilotHasSandbox(m) {
		doc, err = applyCopilotNestedBoolToDoc(doc, CopilotKeySandbox, copilotSandboxFields, m)
		if err != nil {
			return copilotRejectMsg(file, err)
		}
	}

	// 通用设置：顶层布尔三态（''=删键回官方默认）
	for _, k := range copilotGeneralBoolFields {
		v, present := m[k]
		if !present {
			continue
		}
		s, _ := v.(string)
		if s == "" {
			doc, _, err = core.DeleteJSONObjectKey(doc, k)
		} else if s != "true" && s != "false" {
			return k + " 仅支持 true / false"
		} else {
			doc, err = core.UpsertJSONObjectKey(doc, k, func(_ json.RawMessage) (json.RawMessage, error) {
				return json.Marshal(s == "true")
			})
		}
		if err != nil {
			return copilotRejectMsg(file, err)
		}
	}

	// 通用设置：顶层枚举键（''=删键，其余校验合法值）
	for k, legal := range copilotGeneralEnumFields {
		v, present := m[k]
		if !present {
			continue
		}
		s, _ := v.(string)
		if s == "" {
			doc, _, err = core.DeleteJSONObjectKey(doc, k)
		} else if !legal[s] {
			return k + " 值不合法：" + s
		} else {
			doc, err = core.UpsertJSONObjectKey(doc, k, func(_ json.RawMessage) (json.RawMessage, error) {
				return json.Marshal(s)
			})
		}
		if err != nil {
			return copilotRejectMsg(file, err)
		}
	}

	// 嵌套键：ide.autoConnect / ide.openDiffOnEdit
	if copilotHasAny(m, copilotIdeFields) {
		doc, err = applyCopilotNestedBoolToDoc(doc, CopilotKeyIde, copilotIdeFields, m)
		if err != nil {
			return copilotRejectMsg(file, err)
		}
	}

	if bytes.Equal(doc, b) {
		return "" // 无实际变更，幂等
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err.Error()
	}
	// settings.json 可能含令牌等敏感信息，落盘权限收紧为 0600
	if err := core.WriteFileAtomic(file, doc, 0o600); err != nil {
		return err.Error()
	}
	return ""
}

// copilotRejectMsg 统一构造「拒绝覆写」错误信息。
func copilotRejectMsg(file string, err error) string {
	return "解析 " + file + " 失败，已拒绝覆写以免丢失现有配置: " + err.Error()
}

// deleteEmptyJSONKey 若顶层 key 的值为空对象 {} 则删除该键（其余内容原样保留），
// 否则原样返回。用于清理经定点操作后变空的嵌套父对象。
func deleteEmptyJSONKey(doc []byte, key string) ([]byte, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(doc, &probe); err != nil {
		return doc, nil
	}
	raw, ok := probe[key]
	if !ok || !bytes.Equal(bytes.TrimSpace(raw), []byte("{}")) {
		return doc, nil
	}
	out, _, err := core.DeleteJSONObjectKey(doc, key)
	return out, err
}

// deleteCopilotBypassPerm 从 permissions 嵌套对象中移除 disableBypassPermissionsMode
// 键；若 permissions 因此变为空对象则整体删除顶层 permissions 键。
func deleteCopilotBypassPerm(doc []byte) ([]byte, error) {
	newDoc, err := core.UpsertJSONObjectKey(doc, CopilotKeyBypassParent, func(existing json.RawMessage) (json.RawMessage, error) {
		obj := map[string]any{}
		if len(bytes.TrimSpace(existing)) > 0 {
			_ = json.Unmarshal(existing, &obj)
		}
		delete(obj, CopilotKeyBypassPerm)
		if len(obj) == 0 {
			return []byte("{}"), nil
		}
		return json.Marshal(obj)
	})
	if err != nil {
		return nil, err
	}
	return deleteEmptyJSONKey(newDoc, CopilotKeyBypassParent)
}

// copilotHasSandbox 判断面板设置是否携带沙箱字段。
func copilotHasSandbox(m map[string]any) bool {
	return copilotHasAny(m, copilotSandboxFields)
}

// copilotHasAny 判断面板设置是否携带 fields 中任一字段。
func copilotHasAny(m map[string]any, fields map[string][]string) bool {
	for f := range fields {
		if _, ok := m[f]; ok {
			return true
		}
	}
	return false
}

// applyCopilotNestedBoolToDoc 把面板三态串布尔字段按嵌套路径写入 settings.json
// 的 parent 对象：空串移除对应键，否则写入 true/false；移除后自底向上清理
// 空的中间对象；parent 整体变空时删除顶层 parent 键。
func applyCopilotNestedBoolToDoc(doc []byte, parent string, fields map[string][]string, m map[string]any) ([]byte, error) {
	newDoc, err := core.UpsertJSONObjectKey(doc, parent, func(existing json.RawMessage) (json.RawMessage, error) {
		obj := map[string]any{}
		if len(bytes.TrimSpace(existing)) > 0 {
			if je := json.Unmarshal(existing, &obj); je != nil {
				obj = map[string]any{}
			}
		}
		for field, path := range fields {
			if v, ok := m[field]; ok {
				// upsert 的顶层键已是 parent，路径去掉首段避免双重嵌套
				applySandboxField(obj, path[1:], v)
			}
		}
		if len(obj) == 0 {
			return []byte("{}"), nil
		}
		return json.Marshal(obj)
	})
	if err != nil {
		return nil, err
	}
	return deleteEmptyJSONKey(newDoc, parent)
}

// applySandboxField 在 sandbox 对象树中按 path 设置/移除「三态串」布尔字段；
// "" 移除叶子键（并自底向上清理变空的中间对象），"true"/"false" 写入布尔值。
func applySandboxField(obj map[string]any, path []string, v any) {
	s, _ := v.(string)
	chain := make([]map[string]any, 0, len(path)-1)
	cur := obj
	for _, p := range path[:len(path)-1] {
		chain = append(chain, cur)
		next, ok := cur[p].(map[string]any)
		if !ok {
			if s == "" {
				return // 目标键本就不存在，无需创建路径
			}
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	leaf := path[len(path)-1]
	if s == "" {
		delete(cur, leaf)
	} else {
		cur[leaf] = s == "true"
	}
	// 自底向上删除变空的中间对象
	for i := len(chain) - 1; i >= 0; i-- {
		if len(cur) == 0 {
			delete(chain[i], path[i])
		}
		cur = chain[i]
	}
}

// anyToStrs 把 []any 转成 []string（保留字符串元素顺序，忽略非字符串项）。
func anyToStrs(v []any) []string {
	out := make([]string, 0, len(v))
	for _, it := range v {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// anyToStrSlice 把 []string / []any 统一转为 []string（顺序保持，忽略非字符串项）；
// 其它类型返回 nil。Wails JSON 反序列化数组为 []any，读取侧则为 []string。
func anyToStrSlice(v any) []string {
	if s, ok := v.([]string); ok {
		return s
	}
	if a, ok := v.([]any); ok {
		return anyToStrs(a)
	}
	return nil
}

// GetCopilotPermissions 读取 ~/.copilot/permissions-config.json（按项目位置保存的
// 工具审批：tool_approvals + allowed_directories）。文件不存在返回空串与 exists=false。
func GetCopilotPermissions() (content string, exists bool, filePath string, errMsg string) {
	return core.GetRealText(copilotPermRealFile(), "")
}

// SaveCopilotPermissions 校验并保存 ~/.copilot/permissions-config.json（严格 JSON：
// 顶层必须是对象，含注释等 JSONC 会被拒绝——与其它真实配置文件策略一致，避免
// 整体重排或破坏。首次保存前备份）。
func SaveCopilotPermissions(content string) string {
	file := copilotPermRealFile()
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	payload, msg := core.NormalizeJSONSave(content)
	if msg != "" {
		return msg
	}
	var probe any
	if err := json.Unmarshal(payload, &probe); err != nil {
		return "JSON 格式错误：" + err.Error()
	}
	if _, ok := probe.(map[string]any); !ok {
		return "JSON 顶层必须是对象（{...}）"
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err.Error()
	}
	if _, err := core.BackupIfAbsent(file); err != nil {
		return err.Error()
	}
	// 工具审批可能含路径等敏感信息，落盘权限收紧为 0600
	if err := core.WriteFileAtomic(file, payload, 0o600); err != nil {
		return err.Error()
	}
	return ""
}

// GetCopilotModel 读取 ~/.copilot/settings.json 的 model 键（JSONC 宽容读取：
// 允许 // 与 /* */ 注释与尾逗号）。model 键缺失时 exists=false 且 error 为 nil。
func GetCopilotModel() (model string, exists bool, filePath string, errMsg string) {
	file := copilotSettingsRealFile()
	b, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, file, ""
		}
		return "", false, file, err.Error()
	}
	doc, jerr := core.JSONCToJSON(b)
	if jerr != nil || len(bytes.TrimSpace(doc)) == 0 {
		return "", false, file, "settings.json 解析失败（可切换 JSON 源码页签手动修复）"
	}
	var obj map[string]any
	if err := json.Unmarshal(doc, &obj); err != nil {
		return "", false, file, "settings.json 解析失败：" + err.Error()
	}
	if v, ok := obj["model"].(string); ok {
		return v, true, file, ""
	}
	return "", false, file, ""
}

// SaveCopilotModel 写入 ~/.copilot/settings.json 的 model 键（严格 JSON 定点
// 替换，键序 / 缩进 / 数字格式 / 行尾原样保留）；model 为空时移除该键。
// 文件不存在或为空时按空对象初始化；文件不是严格 JSON（含注释等）时拒绝
// 覆写，避免把用户手写的 settings.json 整体重排或抹掉。首次写入前备份。
func SaveCopilotModel(model string) string {
	file := copilotSettingsRealFile()
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	b, rerr := os.ReadFile(file)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rerr.Error()
	}
	if _, err := core.BackupIfAbsent(file); err != nil {
		return err.Error()
	}
	doc := b
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}")
	}
	var newDoc []byte
	var err error
	if model == "" {
		newDoc, _, err = core.DeleteJSONObjectKey(doc, "model")
	} else {
		newDoc, err = core.UpsertJSONObjectKey(doc, "model", func(_ json.RawMessage) (json.RawMessage, error) {
			return json.Marshal(model)
		})
	}
	if err != nil {
		return "解析 " + file + " 失败，已拒绝覆写以免丢失现有配置: " + err.Error()
	}
	if bytes.Equal(newDoc, doc) {
		return "" // 键值未变化，幂等
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err.Error()
	}
	// settings.json 可能含令牌等敏感信息，落盘权限收紧为 0600
	if err := core.WriteFileAtomic(file, newDoc, 0o600); err != nil {
		return err.Error()
	}
	return ""
}
