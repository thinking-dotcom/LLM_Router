package format

import (
	"net/url"
	"strings"

	"llm-router-go/types"
)

// OutboundRequest 复用于 types.OutboundRequest（共享于 format 与 transrules）。
// 见 types.OutboundRequest 定义。

// BuildOutboundRequest creates the upstream request from a provider and an
// OpenAI-shaped request. It resolves the model alias, the endpoint URL, and
// transforms the body into the provider's native format.
func BuildOutboundRequest(provider types.Provider, openaiReq map[string]any, modelOverride string) types.OutboundRequest {
	ptype := string(provider.Type)
	stream, _ := openaiReq["stream"].(bool)

	clientModel := strOr(openaiReq["model"], "")
	if modelOverride != "" {
		clientModel = modelOverride
	}
	model := clientModel
	if alias, ok := provider.Aliases[clientModel]; ok {
		model = alias
	} else {
		// 路由匹配别名时用的是 EqualFold（大小写不敏感），这里转换也必须一致，
		// 否则 "GPT4" 能路由到服务商却不做别名替换，把原样名字发给上游。
		for k, v := range provider.Aliases {
			if strings.EqualFold(k, clientModel) {
				model = v
				break
			}
		}
	}

	// Clone request with resolved model.
	req := make(map[string]any)
	for k, v := range openaiReq {
		req[k] = v
	}
	req["model"] = model

	// 流式请求默认向上游请求 usage：多数 OpenAI 兼容上游（含 gemini-openai 兼容层）
	// 只有显式传入 stream_options.include_usage 才会在流式响应里返回 token 用量，
	// 否则统计页面的 Token 数据会一直为 0。
	// 客户端若自带 stream_options 但缺 include_usage（如空对象），同样注入，
	// 否则「存在 stream_options 就不注入」会漏掉 usage；显式 include_usage:false 保留尊重。
	if stream && (ptype == "openai" || ptype == "gemini-openai") {
		so, ok := req["stream_options"].(map[string]any)
		if !ok {
			req["stream_options"] = map[string]any{"include_usage": true}
		} else if _, has := so["include_usage"]; !has {
			// 复制后再改，避免写入与 openaiReq 共享的 map（浅拷贝别名）
			cloned := make(map[string]any, len(so)+1)
			for k, v := range so {
				cloned[k] = v
			}
			cloned["include_usage"] = true
			req["stream_options"] = cloned
		}
	}

	// Google Gemini 的 OpenAI 兼容端点用 protobuf 严格校验 JSON，不接受 OpenAI
	// 特有/扩展字段：中间商缓存字段（prompt_cache_key）与 JSON Schema 扩展键
	// （additionalProperties / exclusiveMinimum 等）会被 400 拒绝。这里先剥离，
	// 否则 opencode 等客户端经原生 gemini / gemini-openai 路由必失败。
	switch ptype {
	case "gemini":
		// 原生 Gemini 出站走 ToGemini 白名单重建，未知顶层字段本来就不会透出，
		// 这里只需清洗工具 JSON Schema。注意不能删除 reasoning_effort / n：
		// ToGemini 要读它们映射 thinkingConfig / candidateCount，此前在此处
		// 一并剥离导致原生 Gemini 路由的推理深度与 candidateCount 永远失效。
		sanitizeToolsJSONSchema(req)
	case "gemini-openai":
		// OpenAI 兼容端点 protobuf 严格校验，顶层 OpenAI 扩展字段同样要剥离。
		stripGeminiUnsupported(req)
	}

	u := ResolveEndpoint(ptype, provider, model)
	headers := map[string]string{"content-type": "application/json"}
	var body any

	switch ptype {
	case "gemini":
		if isGeminiInteractions(NormalizeBaseURL(provider.BaseURL)) {
			// Interactions API：模型在 body 的 model 字段，端点即 base 的
			// /interactions；流式用 stream:true + alt=sse（与 generateContent
			// 的 SSE 约定一致）。
			if stream {
				u += "?key=" + url.QueryEscape(provider.APIKey) + "&alt=sse"
			} else {
				u += "?key=" + url.QueryEscape(provider.APIKey)
			}
			body = ToGeminiInteractions(req)
		} else if stream {
			// alt=sse 只对 streamGenerateContent 有效；generateContent 不是流式方法，
			// 直接拼 alt=sse 会导致上游返回整体 JSON 或报错，客户端收到空流。
			u = strings.Replace(u, ":generateContent", ":streamGenerateContent", 1)
			u += "?alt=sse&key=" + url.QueryEscape(provider.APIKey)
			// 流式与非流式 body 相同（仅 URL 不同）；此前漏赋值导致
			// 流式请求体为 nil，json.Marshal 后把 `null` 发给了上游。
			body = ToGemini(req)
		} else {
			u += "?key=" + url.QueryEscape(provider.APIKey)
			body = ToGemini(req)
		}
	case "anthropic":
		if provider.APIKey != "" {
			headers["x-api-key"] = provider.APIKey
		}
		headers["anthropic-version"] = "2023-06-01"
		body = ToAnthropic(req)
	case "response":
		if provider.APIKey != "" {
			headers["authorization"] = "Bearer " + provider.APIKey
		}
		body = ChatToResponses(req)
	default: // openai, gemini-openai
		if provider.APIKey != "" {
			headers["authorization"] = "Bearer " + provider.APIKey
		}
		stripClientExtensions(req)
		// 归一化层可能残留 Anthropic/Gemini 特有顶层参数（anthropic 入站的
		// top_k / metadata），真实 OpenAI 上游会报 Unrecognized request argument。
		// gemini-openai 已由 stripGeminiUnsupported 覆盖。
		delete(req, "top_k")
		delete(req, "metadata")
		// Anthropic extended thinking 内容块（type=thinking）OpenAI Chat API 不识别，
		// 跨型进 OpenAI 兼容上游前剔除，避免整请求 400。
		stripThinkingContentParts(req)
		body = req
	}

	return types.OutboundRequest{URL: u, Headers: headers, Body: body}
}

// BuildExactOutbound 构建「同型直通」的出站请求：不做格式转换，仅按目标格式
// 设置 URL 与鉴权头，并把解析后的模型写回请求体，从而完整保留客户端的原生字段
// （Anthropic 的 thinking / Gemini 的 safetySettings、fileData 等 OpenAI 形状
// 无法承载的内容）。仅用于 inbound.Format 与 provider.Type 完全一致的同型路径；
// 其余跨型路径仍走 BuildOutboundRequest 的归一化转换。
//
// 注意：请求侧是 JSON 往返（Unmarshal→Marshal 以写回解析后的 model），并非字节级
// 直通；响应侧才由 handleRawStream / handleRawNonStream 逐字节/原样转发。
func BuildExactOutbound(provider types.Provider, rawBody map[string]any, model string, stream bool) types.OutboundRequest {
	ptype := string(provider.Type)
	// 浅拷贝原始请求体，改写模型字段时不污染解析出的 bodyMap。
	req := make(map[string]any, len(rawBody))
	for k, v := range rawBody {
		req[k] = v
	}
	u := ResolveEndpoint(ptype, provider, model)
	headers := map[string]string{"content-type": "application/json"}
	switch ptype {
	case "gemini":
		if isGeminiInteractions(u) {
			// Interactions 同型：模型在 body 的 model 字段，直通保留原生字段。
			if stream {
				u += "?key=" + url.QueryEscape(provider.APIKey) + "&alt=sse"
			} else {
				u += "?key=" + url.QueryEscape(provider.APIKey)
			}
			if _, ok := req["model"]; !ok {
				req["model"] = model
			}
		} else if stream {
			u = strings.Replace(u, ":generateContent", ":streamGenerateContent", 1)
			u += "?alt=sse&key=" + url.QueryEscape(provider.APIKey)
		} else {
			u += "?key=" + url.QueryEscape(provider.APIKey)
		}
		// Gemini 原生 body 通常没有顶层 model 字段（模型在 URL 路径中）；
		// 若客户端带了则同步为解析后的模型，避免与 URL 不一致。
		if _, ok := req["model"]; ok {
			req["model"] = model
		}
	case "anthropic":
		if provider.APIKey != "" {
			headers["x-api-key"] = provider.APIKey
		}
		headers["anthropic-version"] = "2023-06-01"
		req["model"] = model
		// 兜底剥离：同型直通已被官方域名门槛（allowSameTypePassthrough）把关，
		// 这里对仍可能直通的场景剥离 context_management（2025-10「清空 thinking
		// 上下文」新字段，第三方 Anthropic 兼容层不认识会 400 [1210]）。对真
		// Anthropic 上游仅损失缓存精细控制，不损失对话正确性。
		delete(req, "context_management")
	case "response":
		// Responses 同型直通（/v1/responses → response 上游）：保留
		// previous_response_id / store / include 等原生字段，仅写回解析后的模型。
		if provider.APIKey != "" {
			headers["authorization"] = "Bearer " + provider.APIKey
		}
		req["model"] = model
	}
	return types.OutboundRequest{URL: u, Headers: headers, Body: req}
}

// ResolveEndpoint builds the upstream API URL for a provider.
// All types first go through NormalizeBaseURL to strip redundant paths,
// then re-append the correct path for the selected type.
func ResolveEndpoint(ptype string, provider types.Provider, model string) string {
	base := NormalizeBaseURL(provider.BaseURL)
	switch ptype {
	case "openai":
		return base + "/v1/chat/completions"
	case "gemini-openai":
		return base + "/v1beta/openai/chat/completions"
	case "response":
		return base + "/v1/responses"
	case "anthropic":
		return base + "/v1/messages"
	case "gemini":
		if isGeminiInteractions(base) {
			// Interactions API：端点即 base 本身（模型在 body 的 model 字段，
			// 不拼 :generateContent 也不会拼模型路径）。
			return base
		}
		return base + "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	default: // gemini 未知格式兜底
		return base + "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	}
}

// stripClientExtensions 删除归一化请求中的全部 IR 扩展键（_ext_* 命名空间及
// 旧版 _client_ / _gemini_ 前缀）。这些键仅用于路由器内部在跨型出站时择一
// 取用：_ext_gemini_* 只被原生 Gemini 出站分支（ToGemini）读取、_ext_anthropic_*
// 只被 Anthropic 出站分支读取，本函数仅被 openai/gemini-openai 分支调用，
// OpenAI 兼容上游不接受未知字段，原样透传会被 400 拒绝。
func stripClientExtensions(req map[string]any) {
	types.IRDropAll(req)
}

// stripThinkingContentParts 从归一化消息中剔除 Anthropic extended thinking
// 内容块（type=thinking / redacted_thinking）。OpenAI Chat API 不识别这两个
// 内容类型，跨型（anthropic → openai/gemini-openai）时若不剔除会整请求 400。
// 仅本剔除函数面向 OpenAI 兼容出站调用；Anthropic 同型直通走原始请求体，
// Anthropic/Responses 跨型出站经各自的转换器重建，均不经过这里。
func stripThinkingContentParts(req map[string]any) {
	msgs, ok := req["messages"].([]any)
	if !ok {
		return
	}
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		parts, ok := mm["content"].([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(parts))
		changed := false
		for _, p := range parts {
			if pm, ok := p.(map[string]any); ok {
				if t, _ := pm["type"].(string); t == "thinking" || t == "redacted_thinking" {
					changed = true
					continue
				}
			}
			kept = append(kept, p)
		}
		if changed {
			if len(kept) == 0 {
				mm["content"] = ""
			} else {
				mm["content"] = kept
			}
		}
	}
}

// stripGeminiUnsupported 移除 Google Gemini 不接受的 OpenAI 扩展字段：
// 顶层 prompt_cache_key（中间商缓存键）与工具 JSON Schema 里的 protobuf
// 无法解析的键。真实 OpenAI API 接受这些字段，但 Google 校验严格，会直接 400。
func stripGeminiUnsupported(req map[string]any) {
	for _, k := range []string{
		"prompt_cache_key", "reasoning_effort", "parallel_tool_calls",
		"service_tier", "user", "logprobs", "top_logprobs", "logit_bias",
		"seed", "n", "stream_options", "functions", "function_call",
	} {
		delete(req, k)
	}
	// 剥离内部扩展字段
	for k := range req {
		if strings.HasPrefix(k, "_gemini_") || strings.HasPrefix(k, "_client_") {
			delete(req, k)
		}
	}
	sanitizeToolsJSONSchema(req)
}

// sanitizeToolsJSONSchema 清洗 tools[].function.parameters 中 Google protobuf
// 校验不支持的 JSON Schema 扩展键。原生 gemini 与 gemini-openai 都需要。
func sanitizeToolsJSONSchema(req map[string]any) {
	if tools, ok := req["tools"].([]any); ok {
		for _, t := range tools {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			fn, ok := tm["function"].(map[string]any)
			if !ok {
				continue
			}
			if params, ok := fn["parameters"].(map[string]any); ok {
				sanitizeJSONSchema(params)
			}
		}
	}
}

// sanitizeJSONSchema 递归删除 Google protobuf 校验不支持的 JSON Schema 扩展键。
// 仅删除会导致上游 400 的键，保留大多数常规约束（type/description/enum/required 等）。
func sanitizeJSONSchema(m map[string]any) {
	// 以 $ 开头的都是 JSON Schema 元关键词（$schema/$id/$ref/$defs/$comment 等），
	// Google 的 protobuf 校验一概不认，且对参数结构校验无影响，统一删除。
	for k := range m {
		if strings.HasPrefix(k, "$") {
			delete(m, k)
			continue
		}
	}
	// 具体不支持的扩展键
	for _, k := range []string{
		"additionalProperties", "exclusiveMinimum", "exclusiveMaximum",
		"patternProperties", "minProperties", "maxProperties", "propertyNames",
		"minItems", "maxItems", "uniqueItems", "pattern",
	} {
		delete(m, k)
	}
	// 递归遍历嵌套结构
	if props, ok := m["properties"].(map[string]any); ok {
		for _, v := range props {
			if sm, ok := v.(map[string]any); ok {
				sanitizeJSONSchema(sm)
			}
		}
	}
	if items, ok := m["items"].(map[string]any); ok {
		sanitizeJSONSchema(items)
	} else if itemsList, ok := m["items"].([]any); ok {
		for _, v := range itemsList {
			if sm, ok := v.(map[string]any); ok {
				sanitizeJSONSchema(sm)
			}
		}
	}
	// $defs / definitions 是 schema 对象映射
	for _, key := range []string{"$defs", "definitions"} {
		if defs, ok := m[key].(map[string]any); ok {
			for _, v := range defs {
				if sm, ok := v.(map[string]any); ok {
					sanitizeJSONSchema(sm)
				}
			}
		}
	}
	for _, key := range []string{"anyOf", "allOf", "oneOf", "not"} {
		if list, ok := m[key].([]any); ok {
			for _, v := range list {
				if sm, ok := v.(map[string]any); ok {
					sanitizeJSONSchema(sm)
				}
			}
		}
	}
	if additional, ok := m["additionalProperties"].(map[string]any); ok {
		sanitizeJSONSchema(additional) // 会在本轮 handle 后无效果，但无害
	}
}
