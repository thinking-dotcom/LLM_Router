package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"llm-router-go/types"
)

// writeBack 把 HandleLLM 返回的非流式响应写回 recorder（与 core/server.go handleRequest 一致）。
func writeBack(rec *httptest.ResponseRecorder, ret *http.Response) {
	if ret == nil {
		return
	}
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

// 端到端：/router/v1beta/models/{model}:streamGenerateContent → 原生 Gemini SSE 逐字节转发
// （多块、usageMetadata 取末块累计值），并断言 URL 带 alt=sse&key=。
func TestGeminiSameTypePassthroughStream(t *testing.T) {
	var gotAlt, gotKey string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/models/gemini-2.5-flash:streamGenerateContent", func(w http.ResponseWriter, r *http.Request) {
		gotAlt = r.URL.Query().Get("alt")
		gotKey = r.URL.Query().Get("key")
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello "}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":0}}`+"\n\n")
		io.WriteString(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"world"}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":3}}`+"\n\n")
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3992, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pg", Name: "G", Type: types.ProviderGemini,
			BaseURL: mock.URL, APIKey: "gk", Models: []string{"gemini-2.5-flash"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/v1beta/models/gemini-2.5-flash:streamGenerateContent",
		bytes.NewBufferString(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	var logged types.LogEntry
	ret := HandleLLM(rec, req, cfg, func(e types.LogEntry) { logged = e }, func(model string) func() { return func() {} })
	if ret != nil {
		t.Fatal("streaming same-type passthrough should write directly to w, got a response to forward")
	}

	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "Hello ") || !strings.Contains(bodyStr, "world") {
		t.Errorf("client stream lost native chunks: %s", bodyStr)
	}
	if gotAlt != "sse" || gotKey != "gk" {
		t.Errorf("URL query alt=%q key=%q, want alt=sse key=gk", gotAlt, gotKey)
	}
	if logged.Usage == nil || logged.Usage.PromptTokens == nil || *logged.Usage.PromptTokens != 4 ||
		logged.Usage.CompletionTokens == nil || *logged.Usage.CompletionTokens != 3 {
		t.Errorf("logged usage = %+v, want input=4 output=3 (accumulated across chunks)", logged.Usage)
	}
}

// 同型直通下的别名路由：anthropic 请求用别名 → 上游 body.model 必须是解析后的真实模型
// （非别名）；gemini 别名 → 上游 URL 路径用解析后模型（mock 注册在解析后路径上，
// 若路由器用别名路径会 404）。
func TestSameTypePassthroughAliasResolvesModel(t *testing.T) {
	t.Run("anthropic alias", func(t *testing.T) {
		var upstreamBody string
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			upstreamBody = string(b)
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`)
		})
		mock := httptest.NewServer(mux)
		defer mock.Close()

		cfg := &types.Config{
			Port: 3991, Host: "127.0.0.1",
			Providers: []types.Provider{{
				ID: "pa", Name: "A", Type: types.ProviderAnthropic,
				BaseURL: mock.URL, APIKey: "k",
				Models:  []string{"claude-3-5-sonnet"},
				Aliases: map[string]string{"myclaude": "claude-3-5-sonnet"}, Enabled: true,
			}},
		}

		req := httptest.NewRequest(http.MethodPost, "/router/v1/messages",
			bytes.NewBufferString(`{"model":"myclaude","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
		writeBack(rec, ret)
		if rec.Code != 200 {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var ub map[string]any
		if err := json.Unmarshal([]byte(upstreamBody), &ub); err != nil {
			t.Fatalf("upstream body not JSON: %v", err)
		}
		if m, _ := ub["model"].(string); m != "claude-3-5-sonnet" {
			t.Errorf("upstream model = %q, want resolved claude-3-5-sonnet (not alias myclaude)", m)
		}
	})

	t.Run("gemini alias", func(t *testing.T) {
		var gotPath string
		mux := http.NewServeMux()
		mux.HandleFunc("/v1beta/models/gemini-2.5-flash:generateContent", func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`)
		})
		mock := httptest.NewServer(mux)
		defer mock.Close()

		cfg := &types.Config{
			Port: 3990, Host: "127.0.0.1",
			Providers: []types.Provider{{
				ID: "pg", Name: "G", Type: types.ProviderGemini,
				BaseURL: mock.URL, APIKey: "gk",
				Models:  []string{"gemini-2.5-flash"},
				Aliases: map[string]string{"g2": "gemini-2.5-flash"}, Enabled: true,
			}},
		}

		req := httptest.NewRequest(http.MethodPost, "/router/v1beta/models/g2:generateContent",
			bytes.NewBufferString(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
		writeBack(rec, ret)
		if rec.Code != 200 {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		if gotPath != "/v1beta/models/gemini-2.5-flash:generateContent" {
			t.Errorf("gemini upstream path = %q, want resolved model path (mock would 404 otherwise)", gotPath)
		}
	})
}

// 同型直通错误：上游 400 + 原生 Anthropic 错误体 → 客户端原样收到原生结构
// （不会转成 OpenAI 形状，即不含 OpenAI 错误模板里的 param 字段）。
func TestSameTypeErrorPassthroughNative(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad request from anthropic"}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3989, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pa", Name: "A", Type: types.ProviderAnthropic,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"claude-3-5-sonnet"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/v1/messages",
		bytes.NewBufferString(`{"model":"claude-3-5-sonnet","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	bodyStr := rec.Body.String()
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, bodyStr)
	}
	if !strings.Contains(bodyStr, `"type":"error"`) || !strings.Contains(bodyStr, "invalid_request_error") {
		t.Errorf("client should receive native anthropic error shape, got: %s", bodyStr)
	}
	if strings.Contains(bodyStr, `"param"`) {
		t.Errorf("native error must not be rewritten to OpenAI shape (contains param): %s", bodyStr)
	}
}

// 同型 gemini 错误透传：上游 400 + 原生 Gemini 错误体 → 客户端原样收到原生结构
// （含 gemini 的 status 字段、不含 OpenAI 形状的 param），与跨型转 OpenAI 相对。
func TestGeminiSameTypeErrorPassthroughNative(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/models/gemini-2.5-flash:generateContent", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"code":400,"message":"bad gemini","status":"INVALID_ARGUMENT"}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3984, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pg", Name: "G", Type: types.ProviderGemini,
			BaseURL: mock.URL, APIKey: "gk", Models: []string{"gemini-2.5-flash"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/v1beta/models/gemini-2.5-flash:generateContent",
		bytes.NewBufferString(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],`+
			`"generationConfig":{"thinkingConfig":{"thinkingLevel":"LOW"}}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	bodyStr := rec.Body.String()
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, bodyStr)
	}
	if !strings.Contains(bodyStr, `"status":"INVALID_ARGUMENT"`) {
		t.Errorf("client should receive native gemini error shape, got: %s", bodyStr)
	}
	if strings.Contains(bodyStr, `"param"`) {
		t.Errorf("native error must not be rewritten to OpenAI shape (contains param): %s", bodyStr)
	}
}

// 跨型（前缀声明 anthropic 但 body 实为 OpenAI 形状）错误：上游 400 仍转成 OpenAI 形状
// （含 param 字段，与同型透传相对）。
func TestCrossTypeErrorStillOpenAIShape(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"up fail"}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3988, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pa", Name: "A", Type: types.ProviderAnthropic,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"claude-3"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/chat/completions",
		bytes.NewBufferString(`{"model":"claude-3","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	bodyStr := rec.Body.String()
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, bodyStr)
	}
	if !strings.Contains(bodyStr, `"param"`) {
		t.Errorf("cross-type error should be OpenAI shape (contains param), got: %s", bodyStr)
	}
}

// 不变量 1 负向：/router/v1/messages 路径声明 + OpenAI 形状 body 打 anthropic 上游时，上游收到的必须是
// 转换后的 anthropic 形状（content 块数组），而不是原样 OpenAI body（content 字符串）。
func TestCrossTypePrefixStillConverts(t *testing.T) {
	var upstreamBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3987, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pa", Name: "A", Type: types.ProviderAnthropic,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"claude-3"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/chat/completions",
		bytes.NewBufferString(`{"model":"claude-3","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 结构断言：content 必须是块数组（Go json.Marshal 键按字母序，不能断言键序）
	var ub map[string]any
	if err := json.Unmarshal([]byte(upstreamBody), &ub); err != nil {
		t.Fatalf("upstream body not JSON: %v", err)
	}
	msgs, _ := ub["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("upstream missing messages: %s", upstreamBody)
	}
	m0, _ := msgs[0].(map[string]any)
	content, _ := m0["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("upstream message content must be a block array (converted anthropic shape), got: %s", upstreamBody)
	}
	b0, _ := content[0].(map[string]any)
	if b0["type"] != "text" || b0["text"] != "hi" {
		t.Errorf("upstream content block = %v, want {type:text, text:hi}", b0)
	}
	// 负向断言：直通透传时 content 是字符串 "hi"，绝不会出现 content 块数组
	if strings.Contains(upstreamBody, `"content":"hi"`) {
		t.Errorf("upstream received raw OpenAI body (content string) instead of converted blocks: %s", upstreamBody)
	}
}

// openai→openai 流式直通不回归：客户端自带 stream_options 时直通原始 body，
// SSE 逐字节转发并扫描 usage（断言 usage 块与 [DONE] 均到达客户端）。
func TestOpenaiPassthroughStreamUnchanged(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}],"model":"m-openai"}`+"\n\n")
		io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1}}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3986, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "po", Name: "O", Type: types.ProviderOpenAI,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"m-openai"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewBufferString(`{"model":"m-openai","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	var logged types.LogEntry
	ret := HandleLLM(rec, req, cfg, func(e types.LogEntry) { logged = e }, func(model string) func() { return func() {} })
	if ret != nil {
		t.Fatal("openai passthrough stream should write directly to w, got a response to forward")
	}
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, `"role":"assistant"`) || !strings.Contains(bodyStr, "data: [DONE]") {
		t.Errorf("openai passthrough stream incomplete: %s", bodyStr)
	}
	if logged.Usage == nil || logged.Usage.PromptTokens == nil || *logged.Usage.PromptTokens != 3 ||
		logged.Usage.CompletionTokens == nil || *logged.Usage.CompletionTokens != 1 {
		t.Errorf("logged usage = %+v, want input=3 output=1", logged.Usage)
	}
}

// 直通流的中断语义：已部分传输后上游 context.Canceled（客户端断连）→ 视为干净结束、
// 已扫描的 usage 保留；非 context 的上游读取错误 → 返回错误（供上层记 502）。
type scriptedReadCloser struct {
	data []byte
	err  error
}

func (s *scriptedReadCloser) Read(p []byte) (int, error) {
	if len(s.data) > 0 {
		n := copy(p, s.data)
		s.data = s.data[n:]
		return n, nil
	}
	return 0, s.err
}
func (s *scriptedReadCloser) Close() error { return nil }

func TestPassthroughRawStreamDisconnectAndError(t *testing.T) {
	usageLine := `data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1}}` + "\n\n"

	// 客户端断连（context.Canceled 且已部分传输）：视为干净结束，不报错；usage 保留
	rec := httptest.NewRecorder()
	lastUsage, err := passthroughRawStream(rec, &scriptedReadCloser{data: []byte(usageLine), err: context.Canceled}, true, rec, extractUsage)
	if err != nil {
		t.Errorf("client-disconnect after partial transfer should not be an error, got: %v", err)
	}
	if !strings.Contains(rec.Body.String(), `"prompt_tokens":3`) {
		t.Errorf("partial stream bytes not forwarded: %s", rec.Body.String())
	}
	if lastUsage == nil || lastUsage.PromptTokens == nil || *lastUsage.PromptTokens != 3 {
		t.Errorf("usage = %+v, want prompt=3 from partial stream", lastUsage)
	}

	// 非 context 的上游读取错误（部分传输后）→ 返回错误，便于上层记 502
	rec2 := httptest.NewRecorder()
	_, err = passthroughRawStream(rec2, &scriptedReadCloser{data: []byte(usageLine), err: errors.New("upstream reset")}, true, rec2, extractUsage)
	if err == nil {
		t.Error("non-context read error after partial transfer should be reported")
	}
}

// usage 扫描器多事件行为：anthropic 在 message_start/message_delta 间累加；
// gemini 跨 chunk 覆盖到末值；空事件不产生 usage。
func TestUsageScanners(t *testing.T) {
	t.Run("anthropic", func(t *testing.T) {
		scan := anthropicStreamScanner()
		u := scan(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": float64(10)}}})
		if u == nil || u.PromptTokens == nil || *u.PromptTokens != 10 {
			t.Fatalf("after message_start usage = %+v, want input=10", u)
		}
		u = scan(map[string]any{"type": "message_delta", "usage": map[string]any{"output_tokens": float64(5)}})
		if u == nil || u.CompletionTokens == nil || *u.CompletionTokens != 5 || *u.PromptTokens != 10 {
			t.Fatalf("after message_delta usage = %+v, want input=10 output=5", u)
		}
		u = scan(map[string]any{"type": "content_block_delta"})
		if u == nil || *u.PromptTokens != 10 || *u.CompletionTokens != 5 {
			t.Fatalf("after unrelated event usage = %+v, want unchanged 10/5", u)
		}
	})

	t.Run("gemini", func(t *testing.T) {
		scan := geminiStreamScanner()
		if u := scan(map[string]any{}); u != nil {
			t.Fatalf("empty event should yield nil usage, got %+v", u)
		}
		u := scan(map[string]any{"usageMetadata": map[string]any{"promptTokenCount": float64(4), "candidatesTokenCount": float64(0)}})
		if u == nil || u.PromptTokens == nil || *u.PromptTokens != 4 {
			t.Fatalf("chunk1 usage = %+v, want input=4", u)
		}
		u = scan(map[string]any{"usageMetadata": map[string]any{"promptTokenCount": float64(4), "candidatesTokenCount": float64(3)}})
		if u == nil || u.CompletionTokens == nil || *u.CompletionTokens != 3 || *u.PromptTokens != 4 {
			t.Fatalf("chunk2 usage = %+v, want input=4 output=3", u)
		}
	})
}

// 丢弃日志门控：同型直通与 openai→openai 直通不产生 [ROUTER] 丢弃日志；
// 跨型归一化转换且真实丢弃字段（seed）时才产生。
func TestDropLogGating(t *testing.T) {
	var buf bytes.Buffer
	// 抓的是进程级全局 log 输出：本测试必须保持串行，不得加 t.Parallel()，
	// 否则可能与其它测试或异步日志互相污染。
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ch","object":"chat.completion","model":"m-openai","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3985, Host: "127.0.0.1",
		Providers: []types.Provider{
			{ID: "pa", Name: "A", Type: types.ProviderAnthropic, BaseURL: mock.URL, APIKey: "k", Models: []string{"claude-3-5-sonnet"}, Enabled: true},
			{ID: "po", Name: "O", Type: types.ProviderOpenAI, BaseURL: mock.URL, APIKey: "k", Models: []string{"m-openai"}, Enabled: true},
		},
	}

	// 1) 同型 anthropic 直通（显式携带 thinking，路由器不注入）+ seed 原样保留 → 无丢弃日志
	buf.Reset()
	req := httptest.NewRequest(http.MethodPost, "/router/v1/messages",
		bytes.NewBufferString(`{"model":"claude-3-5-sonnet","seed":42,"max_tokens":100,`+
			`"thinking":{"type":"enabled","budget_tokens":1024},`+
			`"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	if s := buf.String(); strings.Contains(s, "[ROUTER] 跨型转换丢弃") {
		t.Errorf("same-type passthrough must not emit drop log, got: %s", s)
	}

	// 2) openai→openai 直通（body 全量保留）→ 无丢弃日志
	buf.Reset()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewBufferString(`{"model":"m-openai","seed":42,"messages":[{"role":"user","content":"hi"}]}`))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	ret2 := HandleLLM(rec2, req2, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec2, ret2)
	if s := buf.String(); strings.Contains(s, "[ROUTER] 跨型转换丢弃") {
		t.Errorf("openai passthrough must not emit drop log, got: %s", s)
	}

	// 3) 跨型（/router/v1/messages + OpenAI body）转换 + seed 被 ToAnthropic 丢弃 → 应产生丢弃日志
	buf.Reset()
	req3 := httptest.NewRequest(http.MethodPost, "/router/chat/completions",
		bytes.NewBufferString(`{"model":"claude-3-5-sonnet","seed":42,"messages":[{"role":"user","content":"hi"}]}`))
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	ret3 := HandleLLM(rec3, req3, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec3, ret3)
	if s := buf.String(); !strings.Contains(s, "[ROUTER] 跨型转换丢弃") || !strings.Contains(s, "seed") {
		t.Errorf("cross-type conversion should emit drop log for seed, got: %s", s)
	}

	// 4) 跨型 + 结构性折叠字段（stop→stop_sequences、top_k→topK）→ 仍不应产生丢弃日志
	buf.Reset()
	req4 := httptest.NewRequest(http.MethodPost, "/router/chat/completions",
		bytes.NewBufferString(`{"model":"claude-3-5-sonnet","stop":"END","top_k":40,"messages":[{"role":"user","content":"hi"}]}`))
	req4.Header.Set("Content-Type", "application/json")
	rec4 := httptest.NewRecorder()
	ret4 := HandleLLM(rec4, req4, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec4, ret4)
	if s := buf.String(); strings.Contains(s, "[ROUTER] 跨型转换丢弃") {
		t.Errorf("folded fields (stop/top_k) must not be reported as dropped, got: %s", s)
	}
}

// 同型直通 body 形似守卫：路径声明 anthropic/gemini，但 body 实为 OpenAI 形状时
// 不得原样透传（会 400），必须降级走归一化转换。
func TestBodyCompatibilityGuard(t *testing.T) {
	// 单元级：OpenAI 专有字段 → 判定为不兼容（跨型）
	openaiAnthropic := map[string]any{
		"model": "claude-3", "max_completion_tokens": 100,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	if isBodyCompatibleWithFormat("anthropic", openaiAnthropic) {
		t.Error("anthropic + OpenAI-only field (max_completion_tokens) must not passthrough")
	}
	// 合法 anthropic body（max_tokens/thinking）→ 兼容（直通）
	legitAnthropic := map[string]any{
		"model": "claude-3", "max_tokens": 50, "thinking": map[string]any{"type": "enabled"},
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	if !isBodyCompatibleWithFormat("anthropic", legitAnthropic) {
		t.Error("legit anthropic body should passthrough")
	}
	// gemini：只有 messages 无 contents → 不兼容；有 contents → 兼容
	if isBodyCompatibleWithFormat("gemini", map[string]any{"messages": []any{}}) {
		t.Error("gemini body with only messages must not passthrough")
	}
	if !isBodyCompatibleWithFormat("gemini", map[string]any{"contents": []any{}}) {
		t.Error("legit gemini body (contents) should passthrough")
	}
}

// 端到端：/router/v1/messages + OpenAI 形状 body（max_completion_tokens）打 anthropic
// 上游 → 必须走归一化（上游收到含 max_tokens 的 anthropic 形状），而非原样透传。
func TestAnthropicOpenAIBodyConvertedNotPassthrough(t *testing.T) {
	var upstreamBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3983, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pa", Name: "A", Type: types.ProviderAnthropic,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"claude-3"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/v1/messages",
		bytes.NewBufferString(`{"model":"claude-3","max_completion_tokens":100,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(upstreamBody, "max_completion_tokens") {
		t.Errorf("upstream must not receive OpenAI-only field after normalize: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, `"max_tokens"`) || !strings.Contains(upstreamBody, `"type":"text"`) {
		t.Errorf("upstream should receive converted anthropic shape (max_tokens + content blocks), got: %s", upstreamBody)
	}
}

// openai→openai 流式直通守卫：客户端 stream_options 未带 include_usage=true 时
// 不得直通（应重建并注入 include_usage，使上游返回 usage 供统计）。
func TestOpenaiPassthroughRequiresIncludeUsage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var j map[string]any
		json.Unmarshal(b, &j)
		so, _ := j["stream_options"].(map[string]any)
		inc, _ := so["include_usage"].(bool)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}],"model":"m-openai"}`+"\n\n")
		if inc {
			io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1}}`+"\n\n")
		}
		io.WriteString(w, "data: [DONE]\n\n")
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3982, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "po", Name: "O", Type: types.ProviderOpenAI,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"m-openai"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewBufferString(`{"model":"m-openai","stream":true,"stream_options":{},"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	var logged types.LogEntry
	ret := HandleLLM(rec, req, cfg, func(e types.LogEntry) { logged = e }, func(model string) func() { return func() {} })
	if ret != nil {
		t.Fatal("streaming request should write directly to w")
	}
	// 空 stream_options 不应直通，而应重建并注入 include_usage=true → 上游返回 usage
	if logged.Usage == nil || logged.Usage.PromptTokens == nil || *logged.Usage.PromptTokens != 3 {
		t.Errorf("stream_options without include_usage must trigger rebuild+inject; usage=%+v", logged.Usage)
	}
}
