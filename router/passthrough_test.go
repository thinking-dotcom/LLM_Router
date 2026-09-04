package router

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"llm-router-go/types"
)

// roundTripperFunc 把函数适配为 http.RoundTripper（测试中重定向上游请求用）。
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// 跨型转换丢弃字段检测：客户端显式给出的、目标不认识的字段应被报告；
// 结构性折叠字段（重命名后仍保留）与全量保留的情况不应误报。
func TestDroppedRequestFields(t *testing.T) {
	// 典型请求与转换后 body 一致 → 无丢弃
	typical := map[string]any{
		"model": "x", "messages": []any{}, "temperature": 0.7,
		"max_tokens": 100, "stream": false,
	}
	out := map[string]any{
		"model": "x", "messages": []any{}, "temperature": 0.7,
		"max_tokens": 100, "stream": false,
	}
	if d := droppedRequestFields(typical, out); len(d) != 0 {
		t.Errorf("typical request should drop nothing, got %v", d)
	}

	// 转换后目标不认识的字段 → 应报告
	rich := map[string]any{
		"model": "x", "messages": []any{}, "seed": 42,
		"logprobs": true, "n": 2, "user": "u", "response_format": "json_object",
	}
	converted := map[string]any{"model": "x", "messages": []any{}}
	want := []string{"logprobs", "n", "response_format", "seed", "user"}
	if d := droppedRequestFields(rich, converted); !reflect.DeepEqual(d, want) {
		t.Errorf("dropped = %v, want %v", d, want)
	}

	// 结构性折叠字段（stop→stop_sequences、top_k→topK）不应误报
	folded := map[string]any{"model": "x", "messages": []any{}, "stop": "END", "top_k": 40}
	foldedOut := map[string]any{"model": "x", "messages": []any{}, "stop_sequences": []any{"END"}, "topK": 40}
	if d := droppedRequestFields(folded, foldedOut); len(d) != 0 {
		t.Errorf("folded fields should not be reported, got %v", d)
	}
}

// 同型直通判定：body 格式与上游格式一致才直通；OpenAI body + anthropic/gemini
// 上游（前缀声明即使同型）必须走归一化转换，不能直通。
func TestSameTypePassthroughDetection(t *testing.T) {
	cases := []struct {
		inbound string
		ptype   types.ProviderType
		want    bool
	}{
		{"anthropic", types.ProviderAnthropic, true},
		{"gemini", types.ProviderGemini, true},
		{"anthropic", types.ProviderOpenAI, false},
		{"anthropic", types.ProviderGemini, false},
		{"gemini", types.ProviderAnthropic, false},
		{"openai", types.ProviderOpenAI, false}, // openai→openai 已有独立直通分支
		{"openai", types.ProviderGemini, false},
	}
	for _, c := range cases {
		if got := sameTypePassthrough(c.inbound, c.ptype); got != c.want {
			t.Errorf("sameTypePassthrough(%q, %q) = %v, want %v", c.inbound, c.ptype, got, c.want)
		}
	}
}

// 端到端：/router/v1/messages + anthropic 原生请求体（含 thinking）打 anthropic 上游
// → 上游收到的必须是原样 body（thinking 保留、model 正确），响应也原样转发
// （thinking 块不被 OpenAI 形状吞掉），并正确摄取 usage。
func TestAnthropicSameTypePassthrough(t *testing.T) {
	var upstreamBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet",`+
			`"content":[{"type":"thinking","thinking":"let me think"},{"type":"text","text":"hi"}],`+
			`"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3995, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pa", Name: "A", Type: types.ProviderAnthropic,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"claude-3-5-sonnet"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/v1/messages",
		bytes.NewBufferString(`{"model":"claude-3-5-sonnet","max_tokens":100,"thinking":{"type":"enabled","budget_tokens":1000},`+
			`"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var logged types.LogEntry
	ret := HandleLLM(rec, req, cfg, func(e types.LogEntry) { logged = e }, func(model string) func() { return func() {} })
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
	bodyStr := string(bodyBytes)

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, bodyStr)
	}
	// 上游收到原样 body：thinking 保留 + model 正确
	var ub map[string]any
	if err := json.Unmarshal([]byte(upstreamBody), &ub); err != nil {
		t.Fatalf("upstream body not JSON: %v", err)
	}
	if _, ok := ub["thinking"].(map[string]any); !ok {
		t.Errorf("upstream missing native thinking field: %s", upstreamBody)
	}
	if m, _ := ub["model"].(string); m != "claude-3-5-sonnet" {
		t.Errorf("upstream model = %q, want claude-3-5-sonnet", m)
	}
	// 客户端收到原样响应：thinking 块不被转换吞掉
	if !strings.Contains(bodyStr, `"type":"thinking"`) {
		t.Errorf("client response lost native thinking block: %s", bodyStr)
	}
	if logged.Usage == nil || logged.Usage.PromptTokens == nil || *logged.Usage.PromptTokens != 10 ||
		logged.Usage.CompletionTokens == nil || *logged.Usage.CompletionTokens != 5 {
		t.Errorf("logged usage = %+v, want input=10 output=5", logged.Usage)
	}
}

// 端到端：/router/v1beta/models/{model}:generateContent + gemini 原生请求体
// （safetySettings / fileData）打 gemini 上游 → 原生字段原样转发、URL 用解析后模型、
// 响应原样转发（usageMetadata 保留）并正确摄取 usage。
func TestGeminiSameTypePassthrough(t *testing.T) {
	var upstreamBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/models/gemini-2.5-flash:generateContent", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],`+
			`"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	// allowSameTypePassthrough 的官方域名门槛按 baseUrl host 判定，httptest 的
	// 127.0.0.1 不在白名单内（该门槛加入前本测试一直以 127.0.0.1 直通）。
	// 这里用白名单域名 + 自定义 Transport 把请求重定向回 mock 服务，
	// 端到端验证真实的同型直通路径。
	targetHost := strings.TrimPrefix(mock.URL, "http://")
	origClient := upstreamHTTPClient
	upstreamHTTPClient = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		u := *r.URL
		u.Host = targetHost
		r.URL = &u
		return http.DefaultTransport.RoundTrip(r)
	})}
	defer func() { upstreamHTTPClient = origClient }()

	cfg := &types.Config{
		Port: 3994, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pg", Name: "G", Type: types.ProviderGemini,
			BaseURL: "http://generativelanguage.googleapis.com", APIKey: "gk", Models: []string{"gemini-2.5-flash"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/v1beta/models/gemini-2.5-flash:generateContent",
		bytes.NewBufferString(`{"safetySettings":[{"category":"HARM_CATEGORY_HARASSMENT","threshold":"BLOCK_NONE"}],`+
			`"contents":[{"role":"user","parts":[{"text":"hi"}]}],`+
			`"generationConfig":{"thinkingConfig":{"thinkingLevel":"LOW"}}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var logged types.LogEntry
	ret := HandleLLM(rec, req, cfg, func(e types.LogEntry) { logged = e }, func(model string) func() { return func() {} })
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
	bodyStr := string(bodyBytes)

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, bodyStr)
	}
	if !strings.Contains(upstreamBody, "safetySettings") {
		t.Errorf("upstream lost native safetySettings: %s", upstreamBody)
	}
	if !strings.Contains(bodyStr, "usageMetadata") {
		t.Errorf("client response lost native usageMetadata: %s", bodyStr)
	}
	if logged.Usage == nil || logged.Usage.PromptTokens == nil || *logged.Usage.PromptTokens != 3 ||
		logged.Usage.CompletionTokens == nil || *logged.Usage.CompletionTokens != 2 {
		t.Errorf("logged usage = %+v, want input=3 output=2", logged.Usage)
	}
}

// 端到端：/router/v1/messages + stream:true → 原生 Anthropic SSE 逐字节转发
// （thinking_delta 增量保留），并摄取 message_start/message_delta 中的 usage。
func TestAnthropicSameTypePassthroughStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_start\n")
		io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet","content":[],"usage":{"input_tokens":10,"output_tokens":0}}}`+"\n\n")
		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`+"\n\n")
		io.WriteString(w, "event: message_delta\n")
		io.WriteString(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3993, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pa", Name: "A", Type: types.ProviderAnthropic,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"claude-3-5-sonnet"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/v1/messages",
		bytes.NewBufferString(`{"model":"claude-3-5-sonnet","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var logged types.LogEntry
	ret := HandleLLM(rec, req, cfg, func(e types.LogEntry) { logged = e }, func(model string) func() { return func() {} })
	if ret != nil {
		t.Fatal("streaming same-type passthrough should write directly to w, got a response to forward")
	}

	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "event: message_start") {
		t.Errorf("client stream lost message_start event: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "thinking_delta") {
		t.Errorf("client stream lost thinking_delta increment: %s", bodyStr)
	}
	if logged.Usage == nil || logged.Usage.PromptTokens == nil || *logged.Usage.PromptTokens != 10 ||
		logged.Usage.CompletionTokens == nil || *logged.Usage.CompletionTokens != 5 {
		t.Errorf("logged usage = %+v, want input=10 output=5", logged.Usage)
	}
}
