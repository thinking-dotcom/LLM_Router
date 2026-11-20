package router

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"llm-router-go/types"
)

// sameTypePassthrough 判断是否启用「同型直通」：当客户端实际请求体格式
// （inbound.Format，由剥离前缀后的路径决定）与上游服务商 API 格式完全一致时，
// 不再经 OpenAI 形状中转，直接按原生格式转发请求/响应，从而完整保留 thinking、
// safetySettings、fileData 等 OpenAI 形状无法承载的原生字段。
//
// 注意判定用的是 inbound.Format（body 的真实解析格式）而非接入前缀：前缀只声明
// 接入语言，若客户端用 /aai 前缀但 body 实际是 OpenAI 形状（路径被拼成
// /chat/completions），必须继续走归一化转换，不能直通，否则上游会收到自己
// 不认识格式的请求体。
func sameTypePassthrough(inboundFormat string, ptype types.ProviderType) bool {
	switch inboundFormat {
	case "anthropic":
		return ptype == types.ProviderAnthropic
	case "gemini":
		return ptype == types.ProviderGemini
	case "gemini-interactions":
		// /v1beta/interactions 接入 + gemini 上游（base 含 /interactions 时）
		// 是同型组合；上游是否为 interactions 由 BuildOutboundRequest 按 base
		// 检测，响应侧按客户端格式还原 Interaction 对象。
		return ptype == types.ProviderGemini
	case "responses":
		// /v1/responses 接入 + response 上游是最典型的同型组合，此前遗漏导致
		// previous_response_id / store 等原生字段在归一化往返中被静默丢弃，
		// Codex 类客户端的多轮续接每轮都变成全新对话。
		return ptype == types.ProviderResponse
	}
	return false
}

// allowSameTypePassthrough 对「同型直通」增加官方域名信任门槛：
//
//	anthropic → 仅当 baseUrl 指向 Anthropic 官方域（api.anthropic.com 等）
//	gemini    → 仅当 baseUrl 指向 Google 官方地址（generativelanguage.googleapis.com）
//
// 理由：Claude Code 客户端请求体携带大量 Anthropic 新协议字段
// （context_management、块级 cache_control、thinking 等）。第三方 Anthropic
// 兼容层（GLM / z.ai 等）按旧规范实现，对这些字段做严格
// 校验会直接 400（GLM 报 [1210] invalid parameter）。这类上游不应直通，必须
// 走归一化转换——格式实现的白名单重建会天然剥离兼容层不认识的字段，换取
// 可用性；官方域名上游仍保留直通的原生字段保真能力。
func allowSameTypePassthrough(inboundFormat string, p types.Provider) bool {
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Host)
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	switch inboundFormat {
	case "anthropic":
		// host 精确/后缀匹配：此前用 strings.Contains 子串匹配，
		// "anthropic.com.evil.example"、"api.anthropic.com.proxy.io" 都能绕过
		return host == "anthropic.com" ||
			host == "api.anthropic.com" ||
			strings.HasSuffix(host, ".api.anthropic.com")
	case "gemini":
		// Google 官方域名信任门槛。Interactions API（base 含 /interactions）的
		// 请求形状与 generateContent 完全不同（input/steps 而非 contents），
		// 同型直通会把 clients 的 generateContent body 原样发到 /interactions
		// 导致上游 400：凡 interactions base 一律禁用直通、走归一化转换。
		if strings.Contains(strings.ToLower(p.BaseURL), "/interactions") {
			return false
		}
		return host == "generativelanguage.googleapis.com" ||
			strings.HasSuffix(host, ".generativelanguage.googleapis.com")
	case "gemini-interactions":
		// Interactions 客户端同型直通：目标上游必须是 interactions base（base 含
		// /interactions），否则 body 形状不匹配，走归一化转换。
		if !strings.Contains(strings.ToLower(p.BaseURL), "/interactions") {
			return false
		}
		return host == "generativelanguage.googleapis.com" ||
			strings.HasSuffix(host, ".generativelanguage.googleapis.com")
	case "responses":
		return host == "api.openai.com" ||
			strings.HasSuffix(host, ".api.openai.com")
	}
	return false
}

// isBodyCompatibleWithFormat 在 sameTypePassthrough 通过后再做一次形似校验：
// 同型直通按路径格式判定，但个别客户端把 base_url 指到 /v1/messages（或 /aai）却
// 发 OpenAI 形状 body。若被原样透传到 anthropic/gemini 上游会因字段不识别直接 400。
// 改为正向特征判别：anthropic 必须含 messages 且首条消息 content 为块数组且 tool_use/tool_result 形状，
// gemini 必须含 contents，否则一律走归一化，避免把错误形状原样上送。
func isBodyCompatibleWithFormat(inboundFormat string, body map[string]any) bool {
	if body == nil {
		return true
	}
	switch inboundFormat {
	case "anthropic":
		// 正向：必须有 messages，且至少一条消息的 content 符合 Anthropic 块形状
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) == 0 {
			return false
		}
		// Anthropic 必填 max_tokens，缺失直通必 400。
		// 兼容 int/float64：JSON 解码为 float64，单元测试字面量为 int。
		mt, ok := body["max_tokens"].(float64)
		if !ok {
			if mi, ok2 := body["max_tokens"].(int); ok2 {
				mt, ok = float64(mi), true
			}
		}
		if !ok || mt <= 0 {
			return false
		}
		// OpenAI 专有字段出现即不兼容
		for _, k := range []string{
			"max_completion_tokens", "stream_options", "n", "logprobs",
			"response_format", "frequency_penalty", "presence_penalty",
			"functions", "function_call", "reasoning_effort",
		} {
			if _, ok := body[k]; ok {
				return false
			}
		}
		// OpenAI 形状的 tools（{type:"function",function:{...}}）与字符串
		// tool_choice（Anthropic 要求 {"type":"auto"} 等对象形状）不兼容
		if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
			if tm, ok := tools[0].(map[string]any); ok {
				if _, ok := tm["function"]; ok {
					return false
				}
				if t, _ := tm["type"].(string); t == "function" {
					return false
				}
			}
		}
		if _, ok := body["tool_choice"].(string); ok {
			return false
		}
		// 额外校验：content 块中不应出现 OpenAI 的 image_url 形状（Anthropic 用 image/document）
		for _, m := range msgs {
			if mm, ok := m.(map[string]any); ok {
				if arr, ok := mm["content"].([]any); ok {
					for _, b := range arr {
						if bm, ok := b.(map[string]any); ok {
							if t, _ := bm["type"].(string); t == "image_url" {
								return false
							}
						}
					}
				}
			}
		}
		return true
	case "gemini-interactions":
		// Interactions body 必须含 input（或 steps / agent）；不带则不是这个形状
		if _, ok := body["input"]; ok {
			return true
		}
		if _, ok := body["steps"]; ok {
			return true
		}
		return false
	case "gemini":
		if _, ok := body["contents"]; !ok {
			return false
		}
		// 额外：Gemini 原生不应出现 messages 顶层
		if _, has := body["messages"]; has {
			return false
		}
		return true
	}
	return true
}

// usageFromCounts 仅在任一 token 计数为正时构造 Usage（0/缺省不写），
// 与 extractUsage 的输出规则保持一致。
func usageFromCounts(pt, ct int) *types.Usage {
	if pt <= 0 && ct <= 0 {
		return nil
	}
	var p, c *int
	if pt > 0 {
		v := pt
		p = &v
	}
	if ct > 0 {
		v := ct
		c = &v
	}
	return &types.Usage{PromptTokens: p, CompletionTokens: c}
}

// anthropicStreamScanner 返回有状态的 Anthropic SSE 用量扫描器：
// 输入 token 在 message_start.message.usage.input_tokens，
// 输出 token 在 message_delta.usage.output_tokens。
func anthropicStreamScanner() func(map[string]any) *types.Usage {
	var pt, ct int
	return func(evt map[string]any) *types.Usage {
		switch evt["type"] {
		case "message_start":
			if msg, ok := evt["message"].(map[string]any); ok {
				if u, ok := msg["usage"].(map[string]any); ok {
					if v, ok := u["input_tokens"].(float64); ok {
						pt = int(v)
					}
				}
			}
		case "message_delta":
			if u, ok := evt["usage"].(map[string]any); ok {
				if v, ok := u["output_tokens"].(float64); ok {
					ct = int(v)
				}
			}
		}
		return usageFromCounts(pt, ct)
	}
}

// geminiStreamScanner 返回有状态的 Gemini alt=sse 用量扫描器：
// 每个 chunk 可能携带 usageMetadata.promptTokenCount / candidatesTokenCount。
func geminiStreamScanner() func(map[string]any) *types.Usage {
	var pt, ct int
	return func(evt map[string]any) *types.Usage {
		if um, ok := evt["usageMetadata"].(map[string]any); ok {
			if v, ok := um["promptTokenCount"].(float64); ok {
				pt = int(v)
			}
			if v, ok := um["candidatesTokenCount"].(float64); ok {
				ct = int(v)
			}
		}
		return usageFromCounts(pt, ct)
	}
}

// responsesStreamScanner 返回有状态的 Responses SSE 用量扫描器：
// usage 在 response.completed / response.incomplete 事件的
// response.usage.input_tokens / output_tokens。
func responsesStreamScanner() func(map[string]any) *types.Usage {
	var pt, ct int
	return func(evt map[string]any) *types.Usage {
		switch evt["type"] {
		case "response.completed", "response.incomplete":
			if resp, ok := evt["response"].(map[string]any); ok {
				if u, ok := resp["usage"].(map[string]any); ok {
					if v, ok := u["input_tokens"].(float64); ok {
						pt = int(v)
					}
					if v, ok := u["output_tokens"].(float64); ok {
						ct = int(v)
					}
				}
			}
		}
		return usageFromCounts(pt, ct)
	}
}

// anthropicResponseUsage 从非流式 Anthropic 响应中提取用量。
func anthropicResponseUsage(body map[string]any) *types.Usage {
	u, ok := body["usage"].(map[string]any)
	if !ok {
		return nil
	}
	pt, _ := numInt(u, "input_tokens")
	ct, _ := numInt(u, "output_tokens")
	return usageFromCounts(pt, ct)
}

// responsesResponseUsage 从非流式 Responses 响应中提取用量。
func responsesResponseUsage(body map[string]any) *types.Usage {
	u, ok := body["usage"].(map[string]any)
	if !ok {
		return nil
	}
	pt, _ := numInt(u, "input_tokens")
	ct, _ := numInt(u, "output_tokens")
	return usageFromCounts(pt, ct)
}

// geminiResponseUsage 从非流式 Gemini 响应中提取用量。
func geminiResponseUsage(body map[string]any) *types.Usage {
	um, ok := body["usageMetadata"].(map[string]any)
	if !ok {
		return nil
	}
	pt, _ := numInt(um, "promptTokenCount")
	ct, _ := numInt(um, "candidatesTokenCount")
	return usageFromCounts(pt, ct)
}

// geminiInteractionsUsageObject 兼容 Interactions 的用量位置：流事件在
// interaction.usage，非流响应在顶层 usage；两者字段均为 total_*_tokens。
func geminiInteractionsUsageObject(evt map[string]any) map[string]any {
	if u, ok := evt["usage"].(map[string]any); ok {
		return u
	}
	if it, ok := evt["interaction"].(map[string]any); ok {
		if u, ok := it["usage"].(map[string]any); ok {
			return u
		}
	}
	return nil
}

func geminiInteractionsStreamScanner() func(map[string]any) *types.Usage {
	var pt, ct int
	return func(evt map[string]any) *types.Usage {
		if u := geminiInteractionsUsageObject(evt); u != nil {
			if v, ok := numInt(u, "total_input_tokens"); ok {
				pt = v
			}
			if v, ok := numInt(u, "total_output_tokens"); ok {
				ct = v
			}
		}
		return usageFromCounts(pt, ct)
	}
}

func geminiInteractionsResponseUsage(body map[string]any) *types.Usage {
	u := geminiInteractionsUsageObject(body)
	if u == nil {
		return nil
	}
	pt, _ := numInt(u, "total_input_tokens")
	ct, _ := numInt(u, "total_output_tokens")
	return usageFromCounts(pt, ct)
}

// rawUsageScanner / rawResponseUsage 按「接入格式 + 上游类型」选用量摄取器。
// 不能只看 provider.Type：gemini-interactions 同型直通的上游也是 gemini，
// 但响应里没有 usageMetadata，只有 interaction.usage.total_*。
func rawUsageScanner(inboundFormat string, ptype types.ProviderType) func(map[string]any) *types.Usage {
	switch {
	case inboundFormat == "gemini-interactions":
		return geminiInteractionsStreamScanner()
	case ptype == types.ProviderAnthropic:
		return anthropicStreamScanner()
	case ptype == types.ProviderResponse:
		return responsesStreamScanner()
	default:
		return geminiStreamScanner()
	}
}

func rawResponseUsage(inboundFormat string, ptype types.ProviderType, body map[string]any) *types.Usage {
	switch {
	case inboundFormat == "gemini-interactions":
		return geminiInteractionsResponseUsage(body)
	case ptype == types.ProviderAnthropic:
		return anthropicResponseUsage(body)
	case ptype == types.ProviderResponse:
		return responsesResponseUsage(body)
	default:
		return geminiResponseUsage(body)
	}
}

// handleRawStream 处理同型直通的流式响应：把上游 Anthropic/Gemini 原生 SSE
// 逐字节转发给同格式客户端（保留事件名、thinking 增量、usage 位置等），
// 同时用协议对应扫描器摄取 Token 用量供统计。
func handleRawStream(w http.ResponseWriter, r *http.Request, upstreamResp *http.Response, provider *types.Provider, inboundFormat string, entry *types.LogEntry, start time.Time, logger func(types.LogEntry)) *http.Response {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)

	flusher, canFlush := w.(http.Flusher)
	scan := rawUsageScanner(inboundFormat, provider.Type)
	lastUsage, readErr := passthroughRawStream(w, upstreamResp.Body, canFlush, flusher, scan)

	// 中断的流只算失败请求，不记 usage，避免 Token 统计被虚增。
	entry.Status = 200
	if readErr != nil {
		// 客户端主动断连时 context 被取消，流已部分传输，保持 200
		// （与其它流式分支的处理一致）。
		if r.Context().Err() != nil {
			// 客户端主动断连，保持 200
		} else {
			entry.Status = 502
			entry.Resp = truncateRunes("stream read failed: "+readErr.Error(), 2000)
		}
	} else if lastUsage != nil {
		entry.Usage = lastUsage
	}
	entry.Ms = int(time.Since(start).Milliseconds())
	logger(*entry)
	return nil
}

// handleRawNonStream 处理同型直通的非流式响应：把上游 Anthropic/Gemini 原生
// JSON 原样转发给客户端（保留 thinking 块、usageMetadata 等原生字段），
// 并按协议提取用量供统计。
func handleRawNonStream(r *http.Request, upstreamResp *http.Response, provider *types.Provider, inboundFormat string, entry *types.LogEntry, start time.Time, logger func(types.LogEntry)) *http.Response {
	bodyBytes, err := io.ReadAll(io.LimitReader(upstreamResp.Body, 50<<20))
	if err != nil {
		if r.Context().Err() != nil {
			entry.Status = 499
			entry.Resp = truncateRunes("client disconnected: "+r.Context().Err().Error(), 2000)
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(*entry)
			return ErrorResponse(499, "client disconnected", "client_error")
		}
		entry.Status = 502
		entry.Resp = truncateRunes("failed to read upstream response: "+err.Error(), 2000)
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(*entry)
		return ErrorResponse(502, "failed to read upstream response: "+err.Error(), "upstream_error")
	}
	if len(bodyBytes) >= 50<<20 {
		entry.Status = 502
		entry.Resp = "upstream response too large (>50MB)"
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(*entry)
		return ErrorResponse(502, "upstream response too large (>50MB)", "upstream_error")
	}

	var parsed map[string]any
	if json.Unmarshal(bodyBytes, &parsed) == nil {
		entry.Usage = rawResponseUsage(inboundFormat, provider.Type, parsed)
	}
	entry.Status = upstreamResp.StatusCode
	entry.Resp = truncateRunes(string(bodyBytes), 2000)
	entry.Ms = int(time.Since(start).Milliseconds())
	logger(*entry)

	contentType := upstreamResp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	return &http.Response{
		StatusCode:    upstreamResp.StatusCode,
		Status:        upstreamResp.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{contentType}},
		Body:          io.NopCloser(bytes.NewReader(bodyBytes)),
		ContentLength: int64(len(bodyBytes)),
	}
}
