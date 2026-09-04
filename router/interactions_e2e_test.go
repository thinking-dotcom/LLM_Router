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

// 端到端：OpenAI 客户端 → Gemini Interactions 上游（baseUrl 指向 /interactions）。
// 出站请求体应为 Interactions 形状（input/steps + generation_config），响应按
// steps 时间线还原为 OpenAI 形状。
func TestOpenAIClientToGeminiInteractionsUpstream(t *testing.T) {
	var upstreamBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/interactions", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		if !strings.Contains(r.URL.Path, "/interactions") {
			t.Errorf("upstream path = %q, want /interactions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"v1_x","object":"interaction","model":"gemini-3.7-flash","status":"completed","steps":[{"type":"thought"},{"type":"model_output","content":[{"type":"text","text":"Hello!"}]}],"usage":{"total_input_tokens":10,"total_output_tokens":5,"total_tokens":15}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 4001, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pg", Name: "G", Type: types.ProviderGemini,
			BaseURL: mock.URL + "/v1beta/interactions", APIKey: "gk",
			Models: []string{"gemini-3.7-flash"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewBufferString(`{"model":"gemini-3.7-flash","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 出站请求体：Interactions 形状（input steps，模型在 body）
	var ub map[string]any
	if err := json.Unmarshal([]byte(upstreamBody), &ub); err != nil {
		t.Fatalf("upstream body not JSON: %v", err)
	}
	if ub["model"] != "gemini-3.7-flash" {
		t.Errorf("upstream body model = %v", ub["model"])
	}
	if _, has := ub["input"]; !has {
		t.Errorf("upstream body missing input: %s", upstreamBody)
	}
	if _, has := ub["contents"]; has {
		t.Errorf("upstream body must not use legacy contents: %s", upstreamBody)
	}

	// 响应：OpenAI 形状且携带文本
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resp not JSON: %v", err)
	}
	chs, _ := resp["choices"].([]any)
	if len(chs) != 1 {
		t.Fatalf("choices = %v", resp["choices"])
	}
	c0, _ := chs[0].(map[string]any)
	msg, _ := c0["message"].(map[string]any)
	if msg["content"] != "Hello!" {
		t.Errorf("content = %v", msg["content"])
	}
	u, _ := resp["usage"].(map[string]any)
	if v, _ := u["prompt_tokens"].(float64); v != 10 {
		t.Errorf("prompt_tokens = %v", u)
	}
}

// 端到端：OpenAI 客户端 → Gemini Interactions 流式上游。
// 出站 body 带 stream:true，SSE 事件（step.delta / interaction.completed）
// 还原为 OpenAI SSE 增量。
func TestOpenAIClientToGeminiInteractionsStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/interactions", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var ub map[string]any
		if json.Unmarshal(b, &ub) == nil {
			if ub["stream"] != true {
				t.Errorf("upstream stream = %v, want true", ub["stream"])
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: interaction.created\ndata: "+`{"interaction":{"id":"v1_s","status":"in_progress","model":"gemini-3.7-flash"},"event_type":"interaction.created"}`+"\n\n")
		io.WriteString(w, "event: step.start\ndata: "+`{"index":0,"step":{"type":"model_output"},"event_type":"step.start"}`+"\n\n")
		io.WriteString(w, "event: step.delta\ndata: "+`{"index":0,"delta":{"type":"text","text":"Hi "},"event_type":"step.delta"}`+"\n\n")
		io.WriteString(w, "event: step.delta\ndata: "+`{"index":0,"delta":{"type":"text","text":"there"},"event_type":"step.delta"}`+"\n\n")
		io.WriteString(w, "event: interaction.completed\ndata: "+`{"interaction":{"id":"v1_s","status":"completed","usage":{"total_input_tokens":8,"total_output_tokens":5}},"event_type":"interaction.completed"}`+"\n\n")
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 4002, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pg", Name: "G", Type: types.ProviderGemini,
			BaseURL: mock.URL + "/v1beta/interactions", APIKey: "gk",
			Models: []string{"gemini-3.7-flash"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewBufferString(`{"model":"gemini-3.7-flash","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"content":"Hi "`) || !strings.Contains(body, `"content":"there"`) {
		t.Errorf("stream body missing text deltas:\n%s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Errorf("stream body missing finish_reason stop:\n%s", body)
	}
}

// 端到端：Interactions API 客户端（POST /router/v1beta/interactions，2026 GA
// 标准入站）→ openai 上游。入站归一化为 OpenAI 形状，响应还原为 Interaction 对象。
func TestGeminiInteractionsClientToOpenAIUpstream(t *testing.T) {
	var upstreamBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ch","object":"chat.completion","model":"gemini-3.7-flash","choices":[{"index":0,"message":{"role":"assistant","content":"42"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 4004, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "po", Name: "O", Type: types.ProviderOpenAI,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"gemini-3.7-flash"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/router/v1beta/interactions",
		bytes.NewBufferString(`{"model":"gemini-3.7-flash","input":"what is 40+2?"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 出站：OpenAI 形状（messages）
	var ub map[string]any
	if err := json.Unmarshal([]byte(upstreamBody), &ub); err != nil {
		t.Fatalf("upstream body not JSON: %v", err)
	}
	msgs, _ := ub["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("upstream missing messages: %s", upstreamBody)
	}
	// 响应还原为 Interaction 对象（不要求 model 同形，含 steps）
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resp not JSON: %v", err)
	}
	if resp["object"] != "interaction" {
		t.Errorf("response object = %v, want interaction", resp["object"])
	}
	steps, _ := resp["steps"].([]any)
	if len(steps) == 0 {
		t.Fatalf("response missing steps: %s", rec.Body.String())
	}
	s0, _ := steps[0].(map[string]any)
	if s0["type"] != "model_output" {
		t.Errorf("steps[0] = %v", s0)
	}
}

// 端到端：Interactions 客户端流式（POST /router/v1beta/interactions + stream:true）
// → openai 流式上游 → 还原为 Interactions SSE 事件流。
func TestGeminiInteractionsClientStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: "+`{"object":"chat.completion.chunk","model":"gemini-3.7-flash","choices":[{"index":0,"delta":{"role":"assistant"}}]}`+"\n\n")
		io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"Hello "}}]}`+"\n\n")
		io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"world"}}]}`+"\n\n")
		io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 4005, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "po", Name: "O", Type: types.ProviderOpenAI,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"gemini-3.7-flash"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1beta/interactions",
		bytes.NewBufferString(`{"model":"gemini-3.7-flash","input":"hi","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "interaction.created") {
		t.Errorf("missing interaction.created:\n%s", body)
	}
	if !strings.Contains(body, `"type":"text","text":"Hello "`) && !strings.Contains(body, `"text":"Hello "`) {
		t.Errorf("missing text step delta:\n%s", body)
	}
	if !strings.Contains(body, "interaction.completed") {
		t.Errorf("missing interaction.completed:\n%s", body)
	}
}

func TestGeminiInteractionsDisablesSameTypePassthrough(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/interactions", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		tbody := string(b)
		if strings.Contains(tbody, "contents") {
			t.Errorf("interactions upstream received legacy contents body: %s", tbody)
		}
		if !strings.Contains(tbody, "input") {
			t.Errorf("interactions upstream should receive input-shaped body: %s", tbody)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"v1_x","object":"interaction","model":"gemini-3.7-flash","status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"ok"}]}]}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 4003, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "pg", Name: "G", Type: types.ProviderGemini,
			BaseURL: mock.URL + "/v1beta/interactions", APIKey: "gk",
			Models: []string{"gemini-3.7-flash"}, Enabled: true,
		}},
	}

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.7-flash:generateContent",
		bytes.NewBufferString(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(model string) func() { return func() {} })
	writeBack(rec, ret)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 客户端为 generateContent 形状，响应应还原为 candidates
	if !strings.Contains(rec.Body.String(), "candidates") {
		t.Errorf("client should receive gemini legacy-shaped response (candidates): %s", rec.Body.String())
	}
}

func TestModelsURLGeminiInteractions(t *testing.T) {
	p := types.Provider{Type: types.ProviderGemini, BaseURL: "https://generativelanguage.googleapis.com/v1beta/interactions", APIKey: "gk"}
	got := modelsURL(p)
	if !strings.HasPrefix(got, "https://generativelanguage.googleapis.com/v1beta/models") {
		t.Errorf("modelsURL = %q, want root /v1beta/models (no duplicated path)", got)
	}
	if strings.Count(strings.Split(got, "?")[0], "/v1beta") != 1 {
		t.Errorf("modelsURL duplicated /v1beta: %q", got)
	}
	if !strings.Contains(got, "key=gk") {
		t.Errorf("modelsURL missing key=: %q", got)
	}
}

func TestAllowSameTypePassthroughGeminiInteractions(t *testing.T) {
	p := types.Provider{Type: types.ProviderGemini, BaseURL: "https://generativelanguage.googleapis.com/v1beta/interactions"}
	if allowSameTypePassthrough("gemini", p) {
		t.Error("interactions base must disable same-type passthrough")
	}
	p2 := types.Provider{Type: types.ProviderGemini, BaseURL: "https://generativelanguage.googleapis.com"}
	if !allowSameTypePassthrough("gemini", p2) {
		t.Error("legacy gemini official domain should allow passthrough")
	}
}
