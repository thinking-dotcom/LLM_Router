package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"llm-router-go/types"
)

// 通配别名：handler 内按别名（含 gemini-* 等通配模式）选定服务商，
// 并把请求模型改写为别名目标；上游收到改写后的真实模型名 → 200。
func TestWildcardAliasRewrite(t *testing.T) {
	var upstreamPath string
	var upstreamBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{}}`))
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3997, Host: "127.0.0.1",
		DefaultProvider: "p-1",
		Providers: []types.Provider{{
			ID: "p-1", Name: "go", Type: types.ProviderOpenAI,
			BaseURL: mock.URL + "/v1beta", APIKey: "k",
			Aliases: map[string]string{"gemini-*": "deepseek-v4-flash"},
			Enabled: true,
		}},
	}

	body := `{"contents":[{"parts":[{"text":"hi"}],"role":"user"}]}`
	req := httptest.NewRequest(http.MethodPost, "/router/v1beta/models/gemini-3.1-flash-lite-preview:generateContent", strings.NewReader(body))
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

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s, log = %+v", rec.Code, rec.Body.String(), logged)
	}
	if !strings.Contains(upstreamPath, "deepseek-v4-flash") && !strings.Contains(upstreamBody, "deepseek-v4-flash") {
		t.Errorf("upstream path %q / body %q, want rewritten model deepseek-v4-flash", upstreamPath, upstreamBody)
	}
}
