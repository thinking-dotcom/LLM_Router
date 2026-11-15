package router

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"llm-router-go/types"
)

// 回归：兜底模型命中的请求不能走「原始 body 直通」。
// 直通会把客户端原始模型名（两个服务商都解析不到）原样发给上游，兜底等于失效。
//
// 注意：兜底解析只在「没有默认服务商、且启用的服务商不止一个」（routing.go 步骤 4/5）
// 时才会触发；单服务商配置会被默认路由接走，走不到 fallback。
func TestFallbackModelIsSentUpstream(t *testing.T) {
	var gotModel string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ch","object":"chat.completion","model":"`+gotModel+
			`","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3997, Host: "127.0.0.1",
		Providers: []types.Provider{
			{ID: "p1", Name: "P1", Type: types.ProviderOpenAI, BaseURL: mock.URL, APIKey: "k",
				Models: []string{"m1"}, Enabled: true},
			{ID: "p2", Name: "P2", Type: types.ProviderOpenAI, BaseURL: mock.URL, APIKey: "k",
				Models: []string{"m2"}, Enabled: true},
		},
		FallbackModels: []string{"m2"},
		// 关掉全局推理注入：开着注入会强制走转换节点（forceConvert），直通分支根本不执行，
		// 这条回归就测不到兜底被直通绕过的问题。
		ReasoningDepth: "none",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewBufferString(`{"model":"unknown-model","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(string) func() { return func() {} })
	if ret != nil && ret.Body != nil {
		ret.Body.Close()
	}

	if gotModel != "m2" {
		t.Fatalf("上游收到的 model = %q, want 兜底模型 \"m2\"（直通分支把原始模型名发了出去）", gotModel)
	}
}
