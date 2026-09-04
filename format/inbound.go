package format

import "strings"

// strOr returns the string value or default
func strOr(v any, def string) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return def
	}
	return s
}

// copyFloat copies a float value from src to dst if present
func copyFloat(src, dst map[string]any, key string) {
	if v, ok := src[key].(float64); ok {
		dst[key] = v
	}
}

// sliceOrEmpty returns a []any or empty slice
func sliceOrEmpty(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return []any{}
}

// strSlice converts []any to []string
func strSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Inbound represents a parsed inbound request
type Inbound struct {
	Format    string
	OpenaiReq map[string]any
	Stream    bool
}

// ParseInbound detects the request format and converts to OpenAI shape
func ParseInbound(pathname string, body any) *Inbound {
	// OpenAI Chat Completions 兼容路径（含通用别名）
	if pathname == "/v1/chat/completions" || pathname == "/chat/completions" {
		m, _ := body.(map[string]any)
		stream := false
		if m != nil {
			stream, _ = m["stream"].(bool)
		}
		return &Inbound{Format: "openai", OpenaiReq: m, Stream: stream}
	}
	if pathname == "/v1/messages" {
		m, _ := body.(map[string]any)
		stream := false
		if m != nil {
			stream, _ = m["stream"].(bool)
		}
		return &Inbound{Format: "anthropic", OpenaiReq: OpenaiFromAnthropic(m), Stream: stream}
	}
	// OpenAI Responses 协议接入（如新版 Codex wire_api=responses）
	if pathname == "/v1/responses" || pathname == "/responses" {
		m, _ := body.(map[string]any)
		stream := false
		if m != nil {
			stream, _ = m["stream"].(bool)
		}
		return &Inbound{Format: "responses", OpenaiReq: OpenaiFromResponses(m), Stream: stream}
	}
	// Gemini Interactions API 接入（2026 GA 标准，POST /v1beta/interactions）。
	// 客户端把 base_url 指到 /router 后 SDK 按新规范请求 /v1beta/interactions。
	for _, p := range []string{"/v1beta/interactions", "/v1alpha/interactions"} {
		if pathname == p {
			m, _ := body.(map[string]any)
			stream := false
			if m != nil {
				stream, _ = m["stream"].(bool)
			}
			return &Inbound{Format: "gemini-interactions", OpenaiReq: OpenaiFromGeminiInteractions(m), Stream: stream}
		}
	}
	// /v1alpha/：部分客户端（含官方 SDK 的实验特性）使用 v1alpha
	for _, prefix := range []string{"/v1beta/", "/v1beta/v1beta/", "/v1alpha/", "/v1alpha/v1alpha/"} {
		if !strings.HasPrefix(pathname, prefix) {
			continue
		}
		rest := pathname[len(prefix):]
		if !strings.HasPrefix(rest, "models/") {
			continue
		}
		rest = rest[len("models/"):]
		idx := strings.Index(rest, ":")
		if idx == -1 {
			continue
		}
		modelName := rest[:idx]
		action := rest[idx+1:]
		if action != "streamGenerateContent" && action != "generateContent" {
			continue
		}
		req := OpenaiFromGemini(body)
		if m, ok := req["model"].(string); m == "" || !ok {
			req["model"] = modelName
		}
		if m, ok := req["model"].(string); ok && strings.HasPrefix(m, "models/") {
			req["model"] = m[len("models/"):]
		}
		isStream := action == "streamGenerateContent"
		req["stream"] = isStream
		return &Inbound{Format: "gemini", OpenaiReq: req, Stream: isStream}
	}
	return nil
}
