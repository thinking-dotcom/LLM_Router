package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Copilot CLI（GitHub Copilot）接入。
//
// Copilot CLI 通过用户级环境变量（BYOK）配置模型提供方端点，并在
// ~/.copilot/settings.json 的 model 键持久化模型选择：
//
//	COPILOT_PROVIDER_BASE_URL → 必需：提供方 API 端点 base URL
//	                             （接入本路由器时填 接入地址 + /router，OpenAI 兼容）
//	COPILOT_PROVIDER_TYPE     → 可选：openai（默认）/ azure / anthropic
//	COPILOT_PROVIDER_API_KEY  → 可选：提供方 API Key（无鉴权端点可省略）
//	settings.json 的 model 键 → 模型标识（等价 COPILOT_MODEL 环境变量，
//	                             官方持久化写法，优先级低于环境变量）
//
// 端点三个环境变量由桌面端「环境变量 BYOK」面板经 SetUserEnv / DelUserEnv
// 写入/清除（注册表 HKCU\Environment，广播 WM_SETTINGCHANGE）；model 键经
// GetCopilotModel / SaveCopilotModel 读写 settings.json。MCP 配置在
// ~/.copilot/mcp-config.json（Claude 风格 mcpServers 对象，与通用 MCP 注入
// mergeMcpServers 完全兼容），主配置编辑窗口指向 settings.json。
//
// 本文件仅提供常量、真实配置路径定位与 model 键读写，不依赖 config/format/router/core。

// Copilot BYOK 端点环境变量名（写入/清除面板直接引用）。
const (
	CopilotProviderBaseURL = "COPILOT_PROVIDER_BASE_URL"
	CopilotProviderType    = "COPILOT_PROVIDER_TYPE"
	CopilotProviderAPIKey  = "COPILOT_PROVIDER_API_KEY"
)

// copilotProviderTypeDefault 是默认提供方类型：OpenAI 兼容端点（本路由器接入方式）。
const copilotProviderTypeDefault = "openai"

// copilotSettingsRealFile 返回 ~/.copilot/settings.json（主配置：model 等用户设置）。
func copilotSettingsRealFile() string {
	return clientPath("copilot", "config")
}

// copilotMcpRealFile 返回 ~/.copilot/mcp-config.json（MCP 服务器配置）。
func copilotMcpRealFile() string {
	return clientPath("copilot", "mcp")
}

// copilotPermRealFile 返回 ~/.copilot/permissions-config.json（按项目位置保存的
// 工具审批：tool_approvals + allowed_directories）。
func copilotPermRealFile() string {
	return clientPath("copilot", "permissions")
}

// Copilot CLI settings.json 中由「接入配置」面板管理的其它键（用户级合法键；
// allow/deny/ask 规则仅在 MDM 受管层，用户级不读取）。沙箱键为嵌套路径。
const (
	CopilotKeyEffort       = "effortLevel"                  // 推理深度 low/medium/high/xhigh
	CopilotKeyAllowURL     = "allowedUrls"                  // URL 白名单（数组）
	CopilotKeyDenyURL      = "deniedUrls"                   // URL 黑名单（数组）
	CopilotKeyBypassPerm   = "disableBypassPermissionsMode" // 提权限制 disable/allow-auto-only
	CopilotKeyBypassParent = "permissions"                  // 嵌套父键
	CopilotKeySandbox      = "sandbox"                      // OS 沙箱
	// 面板用的沙箱字段名（settings.json 中按嵌套路径读写）：
	// sandbox.enabled / sandbox.allowBypass / sandbox.userPolicy.network.allowLocalNetwork
	CopilotFieldSandboxEna    = "sandboxEnabled"
	CopilotFieldSandboxBypass = "sandboxAllowBypass"
	CopilotFieldSandboxLocal  = "sandboxAllowLocal"
)

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

// copilotSettingsDefaults 返回「接入配置」面板管理的 settings.json 键的默认值。
// allowedUrls / deniedUrls 类型为 []string{}；沙箱字段为 "" / "true" / "false" 三态串。
func copilotSettingsDefaults() map[string]any {
	return map[string]any{
		"model":                   "",
		CopilotKeyEffort:          "",
		CopilotKeyAllowURL:        []string{},
		CopilotKeyDenyURL:         []string{},
		CopilotKeyBypassPerm:      "",
		CopilotFieldSandboxEna:    "",
		CopilotFieldSandboxBypass: "",
		CopilotFieldSandboxLocal:  "",
	}
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
	doc, jerr := jsoncToJSON(b)
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
// （移除后若 permissions 为空对象则删除顶层键）。文件不是严格 JSON（含注释等）
// 时拒绝覆写；首次写入前备份。返回错误信息，成功返回空串。
func SaveCopilotSettings(m map[string]any) string {
	file := copilotSettingsRealFile()
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	b, rerr := os.ReadFile(file)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rerr.Error()
	}
	if _, err := backupIfAbsent(file); err != nil {
		return err.Error()
	}
	doc := b
	var err error
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}")
	}

	// 顶层字符串键：model / effortLevel（空串移除）
	for _, k := range []string{"model", CopilotKeyEffort} {
		v, present := m[k]
		if !present {
			continue
		}
		s, _ := v.(string)
		if s == "" {
			doc, _, err = deleteJSONObjectKey(doc, k)
		} else if k == CopilotKeyEffort && !copilotEffortLevels[s] {
			return "effortLevel 仅支持 low / medium / high / xhigh"
		} else {
			doc, err = upsertJSONObjectKey(doc, k, func(_ json.RawMessage) (json.RawMessage, error) {
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
			doc, _, err = deleteJSONObjectKey(doc, k)
		} else {
			doc, err = upsertJSONObjectKey(doc, k, func(_ json.RawMessage) (json.RawMessage, error) {
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
			doc, err = upsertJSONObjectKey(doc, CopilotKeyBypassParent, func(existing json.RawMessage) (json.RawMessage, error) {
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
		doc, err = applyCopilotSandboxToDoc(doc, m)
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
	if err := writeFileAtomic(file, doc, 0o600); err != nil {
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
	out, _, err := deleteJSONObjectKey(doc, key)
	return out, err
}

// deleteCopilotBypassPerm 从 permissions 嵌套对象中移除 disableBypassPermissionsMode
// 键；若 permissions 因此变为空对象则整体删除顶层 permissions 键。
func deleteCopilotBypassPerm(doc []byte) ([]byte, error) {
	newDoc, err := upsertJSONObjectKey(doc, CopilotKeyBypassParent, func(existing json.RawMessage) (json.RawMessage, error) {
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
	for f := range copilotSandboxFields {
		if _, ok := m[f]; ok {
			return true
		}
	}
	return false
}

// applyCopilotSandboxToDoc 把面板沙箱字段（三态串）按嵌套路径写入 settings.json
// 的 sandbox 对象：空串移除对应键，否则写入 true/false；移除后自底向上清理
// 空的中间对象；sandbox 整体变空时删除顶层 sandbox 键。
func applyCopilotSandboxToDoc(doc []byte, m map[string]any) ([]byte, error) {
	newDoc, err := upsertJSONObjectKey(doc, CopilotKeySandbox, func(existing json.RawMessage) (json.RawMessage, error) {
		obj := map[string]any{}
		if len(bytes.TrimSpace(existing)) > 0 {
			if je := json.Unmarshal(existing, &obj); je != nil {
				obj = map[string]any{}
			}
		}
		for field, path := range copilotSandboxFields {
			if v, ok := m[field]; ok {
				// upsert 的顶层键已是 sandbox，路径去掉首段避免双重嵌套
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
	return deleteEmptyJSONKey(newDoc, CopilotKeySandbox)
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
	return getRealText(copilotPermRealFile(), "")
}

// SaveCopilotPermissions 校验并保存 ~/.copilot/permissions-config.json（严格 JSON：
// 顶层必须是对象，含注释等 JSONC 会被拒绝——与其它真实配置文件策略一致，避免
// 整体重排或破坏。首次保存前备份）。
func SaveCopilotPermissions(content string) string {
	file := copilotPermRealFile()
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	payload, msg := normalizeJSONSave(content)
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
	if _, err := backupIfAbsent(file); err != nil {
		return err.Error()
	}
	// 工具审批可能含路径等敏感信息，落盘权限收紧为 0600
	if err := writeFileAtomic(file, payload, 0o600); err != nil {
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
	doc, jerr := jsoncToJSON(b)
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
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	b, rerr := os.ReadFile(file)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rerr.Error()
	}
	if _, err := backupIfAbsent(file); err != nil {
		return err.Error()
	}
	doc := b
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}")
	}
	var newDoc []byte
	var err error
	if model == "" {
		newDoc, _, err = deleteJSONObjectKey(doc, "model")
	} else {
		newDoc, err = upsertJSONObjectKey(doc, "model", func(_ json.RawMessage) (json.RawMessage, error) {
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
	if err := writeFileAtomic(file, newDoc, 0o600); err != nil {
		return err.Error()
	}
	return ""
}

// deleteJSONObjectKey 删除顶层 JSON 对象中的指定键（含其值），其余内容
// （键序 / 缩进 / 行尾）原样保留，返回 (新文档, 是否删除, 错误)。
// 非最后一项：切除 [键起点, 下一键起点) 区间（该区间含值、尾逗号与缩进，
// 前驱键的逗号得以保留）；最后一项：切除 [键起点, 值结束)，并去掉前驱键的
// 尾逗号。仅支持严格 JSON（与 upsertJSONObjectKey 一致：文件含注释时解析
// 报错，调用方应拒绝覆写）。
func deleteJSONObjectKey(doc []byte, key string) ([]byte, bool, error) {
	if len(bytes.TrimSpace(doc)) == 0 {
		return doc, false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	tk, err := dec.Token()
	if err != nil {
		return nil, false, err
	}
	dl, ok := tk.(json.Delim)
	if !ok || dl != '{' {
		return nil, false, errors.New("顶层必须是 JSON 对象")
	}
	type kvEnt struct {
		keyStart int
		valEnd   int
	}
	var kvs []kvEnt
	found := -1
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, false, err
		}
		k, _ := kt.(string)
		keyStart := int(dec.InputOffset()) - len(k) - 2 // 键名起始（含两端引号）
		keyEnd := int(dec.InputOffset())
		colon := bytes.IndexByte(doc[keyEnd:], ':')
		if colon < 0 {
			return nil, false, errors.New("缺少冒号")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, false, err
		}
		valEnd := int(dec.InputOffset())
		if len(kvs) > 0 && keyStart <= kvs[len(kvs)-1].valEnd {
			return nil, false, errors.New("键区间重叠，文档结构异常")
		}
		if k == key && found < 0 {
			found = len(kvs)
		}
		kvs = append(kvs, kvEnt{keyStart: keyStart, valEnd: valEnd})
	}
	if found < 0 {
		return doc, false, nil
	}
	if found < len(kvs)-1 {
		// 非最后一项：前驱逗号保留，本项（值+尾逗号+缩进）整体切除
		var out bytes.Buffer
		out.Write(doc[:kvs[found].keyStart])
		out.Write(doc[kvs[found+1].keyStart:])
		return out.Bytes(), true, nil
	}
	// 最后一项：切除 [键起点, 值结束)，并去掉前驱键的尾逗号
	bs, be := kvs[found].keyStart, kvs[found].valEnd
	end := bs
	for end > 0 && isJSONSpace(doc[end-1]) {
		end--
	}
	if end > 0 && doc[end-1] == ',' {
		end--
	}
	if len(kvs) == 1 {
		return []byte("{}"), true, nil // 唯一键被删后对象为空
	}
	var out bytes.Buffer
	out.Write(doc[:end])
	out.Write(doc[be:])
	return out.Bytes(), true, nil
}

// jsoncToJSON 把 JSONC（JSON with Comments）文本剥离 // 行注释、/* */ 块注释
// 与字符串字面量外的尾逗号，返回严格 JSON 字节。字符串内的注释符号与换行
// 不做处理；块注释跨行时保留换行以维持行号。剥离结果仍非合法 JSON 时返回
// 错误。空文档按空对象处理。
func jsoncToJSON(doc []byte) ([]byte, error) {
	var out bytes.Buffer
	inStr, esc := false, false
	i := 0
	for i < len(doc) {
		c := doc[i]
		if inStr {
			out.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte(c)
			i++
		case c == '/' && i+1 < len(doc) && doc[i+1] == '/':
			for i < len(doc) && doc[i] != '\n' {
				i++
			}
			// 保留换行（若到文件尾无换行则自然结束），维持后面内容的行号
			if i < len(doc) {
				out.WriteByte('\n')
				i++
			}
		case c == '/' && i+1 < len(doc) && doc[i+1] == '*':
			i += 2
			for i+1 < len(doc) && !(doc[i] == '*' && doc[i+1] == '/') {
				if doc[i] == '\n' {
					out.WriteByte('\n') // 块注释跨行：保留换行保持行号
				}
				i++
			}
			if i+1 < len(doc) {
				i += 2
			} else {
				i = len(doc)
			}
		case c == ',':
			// 尾逗号：跳过其后空白/注释，若遇 } 或 ] 则丢弃该逗号
			j := i + 1
			for j < len(doc) {
				// 跳过空白
				for j < len(doc) && isJSONSpace(doc[j]) {
					j++
				}
				// 跨过行注释与块注释后再判断闭合符
				if j+1 < len(doc) && doc[j] == '/' && doc[j+1] == '/' {
					for j < len(doc) && doc[j] != '\n' {
						j++
					}
					continue
				}
				if j+1 < len(doc) && doc[j] == '/' && doc[j+1] == '*' {
					j += 2
					for j+1 < len(doc) && !(doc[j] == '*' && doc[j+1] == '/') {
						j++
					}
					if j+1 < len(doc) {
						j += 2
					} else {
						j = len(doc)
					}
					continue
				}
				break
			}
			if j < len(doc) && (doc[j] == '}' || doc[j] == ']') {
				i = j
			} else {
				out.WriteByte(c)
				i++
			}
		default:
			out.WriteByte(c)
			i++
		}
	}
	if inStr {
		return nil, errors.New("JSONC 字符串未闭合")
	}
	if len(bytes.TrimSpace(out.Bytes())) == 0 {
		return []byte("{}"), nil
	}
	if !json.Valid(out.Bytes()) {
		return nil, errors.New("JSONC 剥离后仍不是合法 JSON")
	}
	return out.Bytes(), nil
}
