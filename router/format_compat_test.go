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

// 端到端（IR 全矩阵）：gemini 接入（generateContent 路径）→ openai 上游已登记
// 转换规则 → 200 正常转换；上游 OpenAI 形状响应还原为 Gemini candidates 形状。
// 规则矩阵已全开（transrules 全矩阵 4 接入 × 9 上游），此组合不再 400。
func TestGeminiClientToOpenAIUpstream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ch","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3997, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "po", Name: "O", Type: types.ProviderOpenAI,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"gemini-x"}, Enabled: true,
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-x:generateContent",
		bytes.NewBufferString(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ret := HandleLLM(rec, req, cfg, func(e types.LogEntry) {}, func(model string) func() { return func() {} })
	if ret != nil {
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
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200 (full-matrix rule exists); body=%s", resp.StatusCode, bodyBytes)
	}
	body := string(bodyBytes)
	if !strings.Contains(body, "candidates") {
		t.Errorf("expected gemini-shaped response (candidates), got: %s", body)
	}
}

// 端到端跨型（用户场景）：Anthropic 客户端（/v1/messages）指定 OpenAI 兼容上游。
// 请求经归一化转换为 OpenAI 形状上送，上游响应还原为 Anthropic 块数组返回。
func TestAnthropicClientToOpenAIUpstream(t *testing.T) {
	var upstreamBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ch","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3999, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "po", Name: "O", Type: types.ProviderOpenAI,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"m"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/v1/messages",
		bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 上游收到 OpenAI 形状：messages 数组 + content 字符串
	var ub map[string]any
	if err := json.Unmarshal([]byte(upstreamBody), &ub); err != nil {
		t.Fatalf("upstream body not JSON: %v", err)
	}
	msgs, _ := ub["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("upstream missing messages: %s", upstreamBody)
	}
	m0, _ := msgs[0].(map[string]any)
	if _, isStr := m0["content"].(string); !isStr {
		t.Errorf("upstream content must be OpenAI string shape, got: %s", upstreamBody)
	}
	// 客户端收到 Anthropic 形状：content 块数组
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resp not JSON: %v", err)
	}
	content, _ := resp["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("client response content must be anthropic block array, got: %s", rec.Body.String())
	}
}

// 统一接入判定：/router 前缀下按标准路径判别协议；裸根地址同理。
func TestDeclaredFormatFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/router/chat/completions", "openai"},
		{"/router/v1/chat/completions", "openai"},
		{"/router/v1/messages", "anthropic"},
		{"/router/v1beta/models/x:generateContent", "gemini"},
		{"/router/v1beta/models/x:streamGenerateContent", "gemini"},
		{"/v1/chat/completions", "openai"},
		{"/v1/messages", "anthropic"},
		{"/v1beta/models/x:streamGenerateContent", "gemini"},
		{"/router", "openai"},
	}
	for _, c := range cases {
		if got := declaredInboundFormat(c.path); got != c.want {
			t.Errorf("declaredInboundFormat(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// 端到端：统一接入 /router 下协议由路径声明——POST /router/v1/messages
// 即便请求体拼成 OpenAI 格式，也按 Anthropic 接入转换后打 anthropic 上游。
func TestPrefixDeclaredNotBlockedByBodyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"content":[{"type":"text","text":"hi"}]}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3996, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pa", Name: "A", Type: types.ProviderAnthropic,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"claude-3"}, Enabled: true,
		}},
	}
	// 路径 /router/v1/messages 声明 Anthropic 接入 → 上游 anthropic 匹配 → 应放行。
	req := httptest.NewRequest(http.MethodPost, "/router/v1/messages",
		bytes.NewBufferString(`{"model":"claude-3","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ret := HandleLLM(rec, req, cfg, func(e types.LogEntry) {}, func(model string) func() { return func() {} })
	if ret != nil {
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
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (path /router/v1/messages declares anthropic); body=%s", rec.Code, rec.Body.String())
	}
}
