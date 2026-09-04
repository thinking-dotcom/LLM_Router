package router

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"llm-router-go/types"
)

// 验证 opencode 类客户端（带 prompt_cache_key / JSON Schema 扩展字段）经
// gemini-openai 路由时，上游收到的 body 已被剥离 Google 不认识的字段。
func TestGeminiOpenAIRouteStripsUnsupportedFields(t *testing.T) {
	var upstreamReqBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/openai/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamReqBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test", "object": "chat.completion", "model": "gemini-2.5-flash",
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "hi"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 5, "completion_tokens": 1},
		})
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3998, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pg", Name: "G", Type: types.ProviderGeminiOpenAI,
			BaseURL: mock.URL, APIKey: "test-key", Models: []string{"gemini-2.5-flash"}, Enabled: true,
		}},
	}

	reqBody := `{"model":"gemini-2.5-flash","prompt_cache_key":"cache-abc",` +
		`"messages":[{"role":"user","content":"hi"}],` +
		`"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object","additionalProperties":false,"exclusiveMinimum":1,"properties":{"a":{"type":"string"}}}}}]}`

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var logged types.LogEntry
	ret := HandleLLM(rec, req, cfg, func(e types.LogEntry) { logged = e }, func(model string) func() { return func() {} })
	if ret != nil {
		// 与 core/server.go handleRequest 一致：非流式响应由调用方写回
		for k, vals := range ret.Header {
			for _, v := range vals {
				rec.Header().Add(k, v)
			}
		}
		rec.WriteHeader(ret.StatusCode)
		if ret.Body != nil {
			io.Copy(rec, ret.Body)
			ret.Body.Close()
		}
	}

	resp := rec.Result()
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bodyStr := string(bodyBytes)

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, body = %s, log = %+v", resp.StatusCode, bodyStr, logged)
	}
	if !strings.Contains(bodyStr, `"content":"hi"`) {
		t.Errorf("unexpected response body: %s", bodyStr)
	}

	if strings.Contains(upstreamReqBody, "prompt_cache_key") {
		t.Errorf("upstream still received prompt_cache_key: %s", upstreamReqBody)
	}
	if strings.Contains(upstreamReqBody, "additionalProperties") {
		t.Errorf("upstream still received additionalProperties: %s", upstreamReqBody)
	}
	if strings.Contains(upstreamReqBody, "exclusiveMinimum") {
		t.Errorf("upstream still received exclusiveMinimum: %s", upstreamReqBody)
	}
}
