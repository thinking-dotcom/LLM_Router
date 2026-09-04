package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"llm-router-go/types"
)

// thirdparty_access_test.go verifies that third-party SDK clients can start a
// conversation through the router's access prefixes (/oai, /aai, /gai) and
// that request forwarding to an upstream of each wire format is correct.

var openaiChunks = []string{
	`{"id":"c1","object":"chat.completion.chunk","model":"m-openai","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
	`{"id":"c1","object":"chat.completion.chunk","model":"m-openai","choices":[{"index":0,"delta":{"content":"Hello from openai"}}]}`,
	`{"id":"c1","object":"chat.completion.chunk","model":"m-openai","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	`{"id":"c1","object":"chat.completion.chunk","model":"m-openai","choices":[],"usage":{"prompt_tokens":6,"completion_tokens":4,"total_tokens":10}}`,
}

func newMockUpstream(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()

	// Parse the JSON stream flag from either OpenAI or Anthropic shape.
	jsonBool := func(body []byte, key string) bool {
		var m map[string]any
		if json.Unmarshal(body, &m) == nil {
			if b, ok := m[key].(bool); ok {
				return b
			}
		}
		return false
	}

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		if jsonBool(body, "stream") {
			f := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			for _, c := range openaiChunks {
				fmt.Fprintf(w, "data: %s\n\n", c)
				f.Flush()
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": "cmpl1", "object": "chat.completion", "model": "m-openai",
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "Hello from openai"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 6, "completion_tokens": 4, "total_tokens": 10},
		})
	})

	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		if jsonBool(body, "stream") {
			f := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			events := []string{
				`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg1","type":"message","role":"assistant","model":"m-anthropic","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`,
				`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from anthropic"}}`,
				`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
				`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":3}}`,
				`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
			}
			for _, e := range events {
				fmt.Fprintf(w, "%s\n\n", e)
				f.Flush()
			}
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": "msg1", "type": "message", "role": "assistant", "model": "m-anthropic",
			"content":     []any{map[string]any{"type": "text", "text": "Hello from anthropic"}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 5, "output_tokens": 3},
		})
	})

	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if jsonBool(body, "stream") {
			f := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			events := []string{
				`{"type":"response.output_text.delta","delta":"Hello ","output_index":0,"item_id":"i1","sequence_number":1}`,
				`{"type":"response.output_text.delta","delta":"from responses","output_index":0,"item_id":"i1","sequence_number":2}`,
				`{"type":"response.completed","response":{"id":"r1","object":"response","status":"completed","usage":{"input_tokens":5,"output_tokens":4,"total_tokens":9}}}`,
			}
			for _, e := range events {
				fmt.Fprintf(w, "data: %s\n\n", e)
				f.Flush()
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": "r1", "object": "response", "status": "completed", "model": "m-response",
			"output": []any{map[string]any{
				"type": "message", "id": "msg1", "role": "assistant", "status": "completed",
				"content": []any{map[string]any{"type": "output_text", "text": "Hello from responses", "annotations": []any{}}},
			}},
			"usage": map[string]any{"input_tokens": 5, "output_tokens": 4, "total_tokens": 9},
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ":streamGenerateContent") {
			f := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			chunks := []string{
				`{"candidates":[{"content":{"parts":[{"text":"Hello "}]}}]}`,
				`{"candidates":[{"content":{"parts":[{"text":"from gemini"}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":3}}`,
			}
			for _, c := range chunks {
				fmt.Fprintf(w, "data: %s\n\n", c)
				f.Flush()
			}
			fmt.Fprint(w, "data: {}\n\n")
			return
		}
		if strings.Contains(r.URL.Path, ":generateContent") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"candidates":    []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "Hello from gemini"}}}, "finishReason": "STOP"}},
				"usageMetadata": map[string]any{"promptTokenCount": 4, "candidatesTokenCount": 3},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []any{
				map[string]any{"id": "m-openai", "object": "model"},
				map[string]any{"id": "m-anthropic", "object": "model"},
				map[string]any{"id": "m-gemini", "object": "model"},
			},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// startRouter builds a config pointing at the mock upstream and starts the real
// router Server, returning its base URL. The config is assigned directly to
// s.cfg so the test is independent of the shared LLMROUTER_CONFIG path cache
// (other tests in the package mutate that environment variable).
func startThirdPartyRouter(t *testing.T, mockURL string) string {
	s := NewServer()
	providers := []types.Provider{
		{ID: "p1", Name: "oai", Type: types.ProviderOpenAI, BaseURL: mockURL, Models: []string{"m-openai"}, Enabled: true},
		{ID: "p2", Name: "aai", Type: types.ProviderAnthropic, BaseURL: mockURL, Models: []string{"m-anthropic"}, Enabled: true},
		{ID: "p3", Name: "gai", Type: types.ProviderGemini, BaseURL: mockURL, Models: []string{"m-gemini"}, Enabled: true},
		{ID: "p4", Name: "rsp", Type: types.ProviderResponse, BaseURL: mockURL, Models: []string{"m-response"}, Enabled: true},
	}
	s.mu.Lock()
	s.cfg = types.Config{Port: pickFreePort(t), Host: "127.0.0.1", AuthToken: "", DefaultProvider: "", Providers: providers}
	s.mu.Unlock()
	if err := s.Start(); err != nil {
		t.Fatalf("router start: %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	return "http://127.0.0.1:" + strconv.Itoa(s.Port())
}

func pickFreePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func doPost(base, path, body string) (int, string, http.Header) {
	resp, err := http.Post(base+path, "application/json", bytes.NewBufferString(body))
	if err != nil {
		return 0, "", nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func doGet(base, path string) (int, string, http.Header) {
	resp, err := http.Get(base + path)
	if err != nil {
		return 0, "", nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

// TestThirdPartyAccess exercises the three wire formats through both the short
// access prefixes and the canonical paths, exactly as client SDKs would.
func TestThirdPartyAccess(t *testing.T) {
	mock := newMockUpstream(t)
	base := startThirdPartyRouter(t, mock.URL)

	t.Run("OpenAI chat via /oai and /v1", func(t *testing.T) {
		for _, p := range []string{"/oai/v1/chat/completions", "/v1/chat/completions", "/oai/chat/completions"} {
			code, body, _ := doPost(base, p, `{"model":"m-openai","messages":[{"role":"user","content":"hi"}],"stream":false}`)
			if code != 200 {
				t.Fatalf("%s nonstream status=%d body=%s", p, code, body)
			}
			if !strings.Contains(body, "Hello from openai") {
				t.Errorf("%s nonstream missing content: %s", p, body)
			}
			if !strings.Contains(body, `"prompt_tokens":6`) {
				t.Errorf("%s nonstream missing usage: %s", p, body)
			}
		}
		// stream
		code, body, _ := doPost(base, "/oai/v1/chat/completions", `{"model":"m-openai","messages":[{"role":"user","content":"hi"}],"stream":true}`)
		if code != 200 {
			t.Fatalf("stream status=%d body=%s", code, body)
		}
		if !strings.Contains(body, "Hello from openai") || !strings.Contains(body, "[DONE]") {
			t.Errorf("openai stream missing chunks: %s", body)
		}
	})

	t.Run("Anthropic Messages via /aai and /v1", func(t *testing.T) {
		body := `{"model":"m-anthropic","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
		for _, p := range []string{"/aai/v1/messages", "/v1/messages"} {
			code, resp, _ := doPost(base, p, body)
			if code != 200 {
				t.Fatalf("%s nonstream status=%d body=%s", p, code, resp)
			}
			if !strings.Contains(resp, "Hello from anthropic") {
				t.Errorf("%s nonstream missing content: %s", p, resp)
			}
		}
		streamBody := `{"model":"m-anthropic","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
		code, resp, _ := doPost(base, "/aai/v1/messages", streamBody)
		if code != 200 {
			t.Fatalf("anthropic stream status=%d body=%s", code, resp)
		}
		if !strings.Contains(resp, "text_delta") || !strings.Contains(resp, "Hello from anthropic") {
			t.Errorf("anthropic stream missing delta: %s", resp)
		}
		if !strings.Contains(resp, "message_stop") {
			t.Errorf("anthropic stream missing message_stop: %s", resp)
		}
	})

	t.Run("Gemini generateContent via /gai and /v1beta", func(t *testing.T) {
		np, resp, _ := doPost(base, "/gai/v1beta/models/m-gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}]}`)
		if np != 200 {
			t.Fatalf("gemini nonstream status=%d body=%s", np, resp)
		}
		if !strings.Contains(resp, "Hello from gemini") {
			t.Errorf("gemini nonstream missing content: %s", resp)
		}
		sp, resp, _ := doPost(base, "/gai/v1beta/models/m-gemini:streamGenerateContent", `{"contents":[{"parts":[{"text":"hi"}]}]}`)
		if sp != 200 {
			t.Fatalf("gemini stream status=%d body=%s", sp, resp)
		}
		if !strings.Contains(resp, "Hello ") || !strings.Contains(resp, "gemini") {
			t.Errorf("gemini stream missing chunks: %s", resp)
		}
	})

	t.Run("Model list and CORS", func(t *testing.T) {
		for _, p := range []string{"/v1/models", "/oai/v1/models", "/oai/models", "/models"} {
			code, body, hdr := doGet(base, p)
			if code != 200 || !strings.Contains(body, "m-openai") || !strings.Contains(body, "m-anthropic") {
				t.Errorf("%s status=%d body=%s", p, code, body)
			}
			// 无 Origin 的非浏览器请求不下发 ACAO（不再无条件回 *，防任意网页耗 Key）
			if got := hdr.Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("unexpected ACAO %q on %s: %v", got, p, hdr)
			}
		}
		// 本机回环 Origin（任意端口）应被回显，本机开发前端照常可用
		req, _ := http.NewRequest("GET", base+"/v1/models", nil)
		req.Header.Set("Origin", "http://127.0.0.1:5173")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("loopback origin GET: %v", err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:5173" {
			t.Errorf("loopback origin should be echoed, got %q", got)
		}
		// 外部网站 Origin 不下发 ACAO
		req2, _ := http.NewRequest("GET", base+"/v1/models", nil)
		req2.Header.Set("Origin", "https://evil.example")
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			t.Fatalf("evil origin GET: %v", err)
		}
		resp2.Body.Close()
		if got := resp2.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("external origin must not get ACAO, got %q", got)
		}
		// OPTIONS preflight is answered without requiring auth/config
		req, _ = http.NewRequest("OPTIONS", base+"/v1/chat/completions", nil)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("OPTIONS: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != 204 {
			t.Errorf("OPTIONS status=%d", resp.StatusCode)
		}
	})

	// 把完整端点当 base_url 的 SDK 会拼出重复路径段，应被折叠后正常处理
	t.Run("Doubled paths from misconfigured base_url", func(t *testing.T) {
		for _, p := range []string{"/oai/chat/completions/chat/completions", "/chat/completions/chat/completions"} {
			code, body, _ := doPost(base, p, `{"model":"m-openai","messages":[{"role":"user","content":"hi"}],"stream":false}`)
			if code != 200 || !strings.Contains(body, "Hello from openai") {
				t.Errorf("%s status=%d body=%s", p, code, body)
			}
		}
		// 完整 chat 端点作 base 时 SDK 拉的模型列表
		for _, p := range []string{"/oai/chat/completions/models", "/chat/completions/models", "/v1/chat/completions/models"} {
			code, body, _ := doGet(base, p)
			if code != 200 || !strings.Contains(body, "m-openai") {
				t.Errorf("%s status=%d body=%s", p, code, body)
			}
		}
	})

	// API 格式配置为 response（OpenAI Responses）的服务商：客户端仍按 OpenAI
	// chat completions 走 baseURL，路由器负责 chat→responses 双向转换。
	t.Run("Responses provider via /oai", func(t *testing.T) {
		// 非流式
		code, body, _ := doPost(base, "/oai/chat/completions", `{"model":"m-response","messages":[{"role":"user","content":"hi"}],"stream":false}`)
		if code != 200 || !strings.Contains(body, "Hello from responses") {
			t.Fatalf("response nonstream status=%d body=%s", code, body)
		}
		// 流式（Responses 的 delta 会被拆成多个 content 块，这里查分块内容）
		code, body, _ = doPost(base, "/oai/chat/completions", `{"model":"m-response","messages":[{"role":"user","content":"hi"}],"stream":true}`)
		if code != 200 || !strings.Contains(body, "from responses") || !strings.Contains(body, "[DONE]") {
			t.Fatalf("response stream status=%d body=%s", code, body)
		}
		// 模型列表（type=response 的服务商同样聚合在内）
		code, body, _ = doGet(base, "/oai/models")
		if code != 200 || !strings.Contains(body, "m-response") {
			t.Fatalf("response models status=%d body=%s", code, body)
		}
	})
}
