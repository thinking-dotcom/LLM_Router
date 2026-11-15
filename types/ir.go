package types

import "strings"

// ============================================================================
// IR（Intermediate Representation）规范
//
// 本项目的格式互转统一经过一个「中立 IR」层。IR 的物理载体是 OpenAI Chat
// Completions 形状的 map[string]any（所有接入格式都归一到该形状，出站时再
// 装配成目标格式），再加上一个规范化的扩展键命名空间 `_ext_*` 作为
// extensions 兜底槽：
//
//   - 平台专有字段在入站归一时写入扩展键（Anthropic 的 thinking / citations、
//     Gemini 的 safetySettings / thinkingConfig、Responses 的 reasoning 等）；
//   - 出站时目标格式支持则从扩展键还原，不支持才剥离并记入降级日志；
//   - 从而跨型转换不再丢字段，多跳往返（A→B→A）可保真。
//
// 键命名规则：
//
//	_ext_<家族>_<字段>        家族取 anthropic / gemini / responses / client
//	_client_* / _gemini_*     v3 之前的旧版扩展键，读取时自动兼容映射到 _ext_*
//
// 统一推理档位（reasoning_effort）是 IR 内建的跨家族归一字段：Anthropic 的
// thinking.budget_tokens、Gemini 的 thinkingConfig.thinkingLevel、Responses 的
// reasoning.effort 三者经它互相折算（近似），原始值保留在对应扩展键中。
// ============================================================================

// ExtensionPrefix 是所有 IR 扩展键的统一前缀。
const ExtensionPrefix = "_ext_"

// IR 扩展键常量（统一命名空间，家族前缀 + 字段语义）。
const (
	// ---- Anthropic 请求侧 ----
	ExtAnthropicThinking     = "_ext_anthropic_thinking"      // thinking 配置（{type, budget_tokens} 等）
	ExtAnthropicTopK         = "_ext_anthropic_top_k"         // 原生 top_k 采样参数
	ExtAnthropicCacheCtrl    = "_ext_anthropic_cache_control" // 顶层/块级 cache_control 配置
	ExtAnthropicServerTools  = "_ext_anthropic_server_tools"  // 服务端工具（web_search 等，原样保真）
	ExtAnthropicOutputConfig = "_ext_anthropic_output_config" // output_config（effort / format 结构化输出）
	ExtAnthropicInferenceGeo = "_ext_anthropic_inference_geo" // inference_geo（推理地域）
	ExtAnthropicServiceTier  = "_ext_anthropic_service_tier"  // service_tier（auto / standard_only）

	// ---- Anthropic 响应侧 ----
	ExtAnthropicStopSeq        = "_ext_anthropic_stop_sequence"     // 命中的停止序列
	ExtAnthropicCitations      = "_ext_anthropic_citations"         // text 块的 citations 引用
	ExtAnthropicThinkingBlock  = "_ext_anthropic_thinking_block"    // thinking 块（明文，多轮回传/响应）
	ExtAnthropicRedactedThink  = "_ext_anthropic_redacted_thinking" // 脱敏思考块（不透明 data，原样回传）
	ExtAnthropicThinkingDelta  = "_ext_anthropic_thinking_delta"    // 流式 thinking 增量文本
	ExtAnthropicSignatureDelta = "_ext_anthropic_signature_delta"   // 流式思考块签名增量
	ExtAnthropicCitationsDelta = "_ext_anthropic_citations_delta"   // 流式 citations 增量（text 块在线引用）

	// ---- Gemini 请求侧（由旧 _gemini_* 规范迁移） ----
	ExtGeminiSafetySettings = "_ext_gemini_safety_settings"        // safetySettings（顶层）
	ExtGeminiCachedContent  = "_ext_gemini_cached_content"         // cachedContent（顶层）
	ExtGeminiThinkingConfig = "_ext_gemini_thinking_config"        // thinkingConfig（generationConfig 内）
	ExtGeminiResponseSchema = "_ext_gemini_response_schema"        // responseSchema（generationConfig 内）
	ExtGeminiToolConfig     = "_ext_gemini_tool_config"            // toolConfig（工具选择）
	ExtGeminiToolsExtra     = "_ext_gemini_tools_extra"            // 非 function 工具（google_search 等）
	ExtGeminiAllowedFns     = "_ext_gemini_allowed_function_names" // toolConfig.allowedFunctionNames
	ExtGeminiThoughtSigs    = "_ext_gemini_thought_signatures"     // thoughtSignature（按消息序号跨型往返）

	// ---- Responses 请求侧 ----
	ExtResponsesReasoning          = "_ext_responses_reasoning"            // reasoning（effort/summary/mode/context）
	ExtResponsesTextFormat         = "_ext_responses_text_format"          // text.format（json_schema/json_object）
	ExtResponsesPromptCacheOptions = "_ext_responses_prompt_cache_options" // prompt_cache_options（ttl/mode）

	// ---- 客户端推理档位（跨家族，由旧 _client_* 规范迁移） ----
	ExtClientReasoningEffort = "_ext_client_reasoning_effort" // 显式 reasoning 档位（OpenAI 兼容 / Responses 用）
	ExtClientAnthropicBudget = "_ext_client_anthropic_budget" // 显式思考预算（Anthropic 用）
)

// legacyExtKeys 旧版扩展键 → 新版扩展键的映射（读取时兼容）。
var legacyExtKeys = map[string]string{
	"_client_reasoning_effort":     ExtClientReasoningEffort,
	"_client_anthropic_budget":     ExtClientAnthropicBudget,
	"_gemini_safetySettings":       ExtGeminiSafetySettings,
	"_gemini_cachedContent":        ExtGeminiCachedContent,
	"_gemini_thinkingConfig":       ExtGeminiThinkingConfig,
	"_gemini_responseSchema":       ExtGeminiResponseSchema,
	"_gemini_tools_extra":          ExtGeminiToolsExtra,
	"_gemini_allowedFunctionNames": ExtGeminiAllowedFns,
}

// IRIsExtKey 判断键是否属于 IR 扩展键（统一前缀 + 旧版命名空间）。
// 用于出站到 OpenAI 兼容上游前剥离内部键（上游不认识下划线扩展字段）。
func IRIsExtKey(k string) bool {
	return strings.HasPrefix(k, ExtensionPrefix) ||
		strings.HasPrefix(k, "_client_") ||
		strings.HasPrefix(k, "_gemini_")
}

// IRGet 读取扩展键，兼容旧版命名空间：先读新键，再读旧键。
func IRGet(m map[string]any, key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	if v, ok := m[key]; ok {
		return v, true
	}
	for old, new := range legacyExtKeys {
		if new == key {
			if v, ok := m[old]; ok {
				return v, true
			}
		}
	}
	return nil, false
}

// IRSet 写入扩展键（统一新版命名），同时清除同义旧键避免重复。
func IRSet(m map[string]any, key string, v any) {
	if m == nil {
		return
	}
	for old, new := range legacyExtKeys {
		if new == key {
			delete(m, old)
		}
	}
	m[key] = v
}

// IRDropAll 删除 map 中的全部扩展键（含旧版命名空间）。
func IRDropAll(m map[string]any) {
	if m == nil {
		return
	}
	for k := range m {
		if IRIsExtKey(k) {
			delete(m, k)
		}
	}
}

// IRKeys 列出 map 中全部扩展键（统一命名后）。
func IRKeys(m map[string]any) []string {
	if m == nil {
		return nil
	}
	var keys []string
	for k := range m {
		if IRIsExtKey(k) {
			keys = append(keys, k)
		}
	}
	return keys
}
