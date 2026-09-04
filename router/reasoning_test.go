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

// ---------------------------------------------------------------------------
// 单元测试：推理深度参数检测
// ---------------------------------------------------------------------------

func TestHasReasoningDepth(t *testing.T) {
	cases := []struct {
		name    string
		format  string
		body    map[string]any
		norm    map[string]any
		present bool
	}{
		{"openai 显式档位", "openai",
			map[string]any{"reasoning_effort": "low"}, map[string]any{}, true},
		{"openai 空档位不算", "openai",
			map[string]any{"reasoning_effort": ""}, map[string]any{}, false},
		{"openai Responses 风格", "openai",
			map[string]any{"reasoning": map[string]any{"effort": "high"}}, map[string]any{}, true},
		{"anthropic thinking", "anthropic",
			map[string]any{"thinking": map[string]any{"type": "enabled"}}, map[string]any{}, true},
		{"gemini thinkingConfig", "gemini",
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingBudget": 1024},
			}}, map[string]any{}, true},
		{"归一化档位 none 也算显式配置", "openai",
			map[string]any{}, map[string]any{"reasoning_effort": "none"}, true},
		{"归一化 anthropic 预算标记", "anthropic",
			map[string]any{}, map[string]any{"_client_anthropic_budget": 2048}, true},
		{"归一化 gemini 思考配置标记", "gemini",
			map[string]any{}, map[string]any{"_gemini_thinkingConfig": map[string]any{}}, true},
		{"完全未携带", "openai",
			map[string]any{"model": "m", "messages": []any{}}, map[string]any{}, false},
	}
	for _, c := range cases {
		if got := HasReasoningDepth(c.format, c.body, c.norm); got != c.present {
			t.Errorf("%s: HasReasoningDepth = %v, want %v", c.name, got, c.present)
		}
	}
}

// ---------------------------------------------------------------------------
// 单元测试：三种格式预设方案注入
// ---------------------------------------------------------------------------

func TestInjectDefaultReasoning(t *testing.T) {
	// OpenAI 方案：空档位按默认 high 处理
	body := map[string]any{"model": "m"}
	norm := map[string]any{}
	if !InjectDefaultReasoning("openai", "", body, norm) {
		t.Fatal("openai 注入应成功")
	}
	if body["reasoning_effort"] != defaultReasoningTier || norm["reasoning_effort"] != defaultReasoningTier {
		t.Errorf("openai 注入结果 body=%v norm=%v", body, norm)
	}

	// OpenAI 方案：max 封顶为 high（body 与归一化形状都不出现 max）
	body = map[string]any{"model": "m"}
	norm = map[string]any{}
	if !InjectDefaultReasoning("openai", ReasoningMax, body, norm) {
		t.Fatal("openai max 档注入应成功")
	}
	if body["reasoning_effort"] != "high" || norm["reasoning_effort"] != "high" {
		t.Errorf("openai max 应封顶为 high: body=%v norm=%v", body, norm)
	}

	// none 档位：不注入
	body = map[string]any{"model": "m"}
	norm = map[string]any{}
	if InjectDefaultReasoning("openai", ReasoningNone, body, norm) {
		t.Error("none 档位不应注入")
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Error("none 档位不应写入 reasoning_effort")
	}

	// Gemini 方案：generationConfig.thinkingConfig.thinkingLevel（默认 high → HIGH）
	body = map[string]any{"model": "m"}
	norm = map[string]any{}
	if !InjectDefaultReasoning("gemini", "", body, norm) {
		t.Fatal("gemini 注入应成功")
	}
	gc, _ := body["generationConfig"].(map[string]any)
	tc, _ := gc["thinkingConfig"].(map[string]any)
	if tc == nil || tc["thinkingLevel"] != "HIGH" {
		t.Errorf("gemini 注入结果 %v", body)
	}

	// Gemini 方案：low → LOW
	body = map[string]any{"model": "m"}
	norm = map[string]any{}
	if !InjectDefaultReasoning("gemini", ReasoningLow, body, norm) {
		t.Fatal("gemini low 注入应成功")
	}
	gc, _ = body["generationConfig"].(map[string]any)
	tc, _ = gc["thinkingConfig"].(map[string]any)
	if tc == nil || tc["thinkingLevel"] != "LOW" {
		t.Errorf("gemini low 注入结果 %v", body)
	}

	// Anthropic 方案：max_tokens 充裕 → 直接用档位预算（high = 24576）
	body = map[string]any{"max_tokens": float64(40000)}
	norm = map[string]any{}
	if !InjectDefaultReasoning("anthropic", "", body, norm) {
		t.Fatal("anthropic 注入应成功")
	}
	th, _ := body["thinking"].(map[string]any)
	if th == nil || th["budget_tokens"] != anthropicBudgetOf(ReasoningHigh) {
		t.Errorf("anthropic 注入预算 = %v, want %d", th["budget_tokens"], anthropicBudgetOf(ReasoningHigh))
	}

	// Anthropic 方案：max 档位 → 预算上限 32768
	body = map[string]any{}
	norm = map[string]any{}
	if !InjectDefaultReasoning("anthropic", ReasoningMax, body, norm) {
		t.Fatal("anthropic max 注入应成功")
	}
	th, _ = body["thinking"].(map[string]any)
	if th["budget_tokens"] != anthropicBudgetOf(ReasoningMax) {
		t.Errorf("anthropic max 预算 = %v, want %d", th["budget_tokens"], anthropicBudgetOf(ReasoningMax))
	}

	// Anthropic 方案：max_tokens 偏小 → 收敛到剩余空间
	body = map[string]any{"max_tokens": float64(3000)}
	norm = map[string]any{}
	if !InjectDefaultReasoning("anthropic", "", body, norm) {
		t.Fatal("anthropic 注入应成功")
	}
	th, _ = body["thinking"].(map[string]any)
	if th["budget_tokens"] != 3000-1024 {
		t.Errorf("anthropic 收敛预算 = %v, want %d", th["budget_tokens"], 3000-1024)
	}

	// Anthropic 方案：max_tokens 放不下有效预算 → 放弃注入
	body = map[string]any{"max_tokens": float64(1500)}
	norm = map[string]any{}
	if InjectDefaultReasoning("anthropic", "", body, norm) {
		t.Error("max_tokens 过小时应放弃注入")
	}
	if _, ok := body["thinking"]; ok {
		t.Error("放弃注入时不应写入 thinking")
	}
}

// ---------------------------------------------------------------------------
// 端到端：
//   A) 无推理深度参数 → 追加预设并强制走转换节点，上游收到映射后的参数；
//   B) 已携带推理深度参数 + 同型上游 → 原样直通，路由器不覆盖客户端取值。
// ---------------------------------------------------------------------------

func TestAutoReasoningE2E(t *testing.T) {
	const okResp = `{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"test-model",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":1,"completion_tokens":1}}`

	var upstreamBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, okResp)
	})
	mock := httptest.NewServer(mux)
	defer mock.Close()

	cfg := &types.Config{
		Port: 3992, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "po", Name: "O", Type: types.ProviderOpenAI,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"test-model"}, Enabled: true,
		}},
	}

	post := func(payload string) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
			bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ret := HandleLLM(rec, req, cfg, func(types.LogEntry) {}, func(string) func() { return func() {} })
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
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
	}

	upstreamEffort := func(t *testing.T) string {
		t.Helper()
		var ub map[string]any
		if err := json.Unmarshal([]byte(upstreamBody), &ub); err != nil {
			t.Fatalf("upstream body not JSON: %v\n%s", err, upstreamBody)
		}
		s, _ := ub["reasoning_effort"].(string)
		return s
	}

	// A) 未携带推理深度参数 → 按默认档位 high 注入并经转换节点送达上游
	upstreamBody = ""
	post(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`)
	if got := upstreamEffort(t); got != defaultReasoningTier {
		t.Errorf("case A: 上游 reasoning_effort = %q, want %q\n%s", got, defaultReasoningTier, upstreamBody)
	}

	// B) 已携带推理深度参数 → 同型直通原样转发，不覆盖
	upstreamBody = ""
	post(`{"model":"test-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"high","temperature":0.5}`)
	if got := upstreamEffort(t); got != "high" {
		t.Errorf("case B: 上游 reasoning_effort = %q, want high\n%s", got, upstreamBody)
	}
	var ub map[string]any
	json.Unmarshal([]byte(upstreamBody), &ub)
	if ub["temperature"] != 0.5 {
		t.Errorf("case B: 直通丢失 temperature: %s", upstreamBody)
	}

	// C) 配置档位 medium → 上游收到 medium（配置生效验证）
	cfgMedium := *cfg
	cfgMedium.ReasoningDepth = "medium"
	upstreamBody = ""
	postWith := func(payload string, conf *types.Config) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
			bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ret := HandleLLM(rec, req, conf, func(types.LogEntry) {}, func(string) func() { return func() {} })
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
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
	}
	postWith(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`, &cfgMedium)
	if got := upstreamEffort(t); got != "medium" {
		t.Errorf("case C: 上游 reasoning_effort = %q, want medium\n%s", got, upstreamBody)
	}
}
