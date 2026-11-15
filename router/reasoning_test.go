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
		// 「携带了但值为关闭/动态」也算已携带：客户端已表态，不得再用全局档位覆盖。
		{"openai 顶层 thinking.type=disabled 算已携带", "openai",
			map[string]any{"thinking": map[string]any{"type": "disabled"}}, map[string]any{}, true},
		{"openai reasoning_effort=none 算已携带", "openai",
			map[string]any{"reasoning_effort": "none"}, map[string]any{}, true},
		{"anthropic thinking.type=disabled 算已携带", "anthropic",
			map[string]any{"thinking": map[string]any{"type": "disabled"}}, map[string]any{}, true},
		{"interactions thinking_budget=0 算已携带", "gemini-interactions",
			map[string]any{"generation_config": map[string]any{"thinking_budget": 0}}, map[string]any{}, true},
		{"gemini thinkingBudget=-1 算已携带", "gemini",
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingBudget": -1},
			}}, map[string]any{}, true},
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
// 单元测试：日志「推理」列取值（客户端显式携带时的展示值）
// ---------------------------------------------------------------------------

func TestClientReasoningLabel(t *testing.T) {
	cases := []struct {
		name   string
		format string
		body   map[string]any
		norm   map[string]any
		want   string
	}{
		{"openai 顶层档位", "openai",
			map[string]any{"reasoning_effort": "low"}, map[string]any{}, "low"},
		{"openai reasoning.effort", "openai",
			map[string]any{"reasoning": map[string]any{"effort": "high"}}, map[string]any{}, "high"},
		{"anthropic output_config.effort 优先于 thinking", "anthropic",
			map[string]any{
				"output_config": map[string]any{"effort": "high"},
				"thinking":      map[string]any{"type": "enabled", "budget_tokens": 8192},
			}, map[string]any{}, "high"},
		{"anthropic thinking 预算", "anthropic",
			map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": 8192}},
			map[string]any{}, "8192"},
		{"anthropic thinking.type", "anthropic",
			map[string]any{"thinking": map[string]any{"type": "adaptive"}}, map[string]any{}, "adaptive"},
		{"gemini 思考档位", "gemini",
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingLevel": "HIGH"},
			}}, map[string]any{}, "HIGH"},
		{"gemini 思考预算", "gemini",
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingBudget": 1024},
			}}, map[string]any{}, "1024"},
		{"gemini 预算 0（显式关闭）也要显示", "gemini",
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingBudget": 0},
			}}, map[string]any{}, "0"},
		{"gemini 预算 -1（动态思考）显示为动态", "gemini",
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingBudget": -1},
			}}, map[string]any{}, "动态"},
		{"gemini 固定预算照常显示数字", "gemini",
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingBudget": 4000},
			}}, map[string]any{}, "4000"},
		{"gemini 预算为字符串不硬凑成 0", "gemini",
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingBudget": "high"},
			}}, map[string]any{}, ""},
		{"responses effort", "responses",
			map[string]any{"reasoning": map[string]any{"effort": "medium"}}, map[string]any{}, "medium"},
		{"interactions thinking_level", "gemini-interactions",
			map[string]any{"generation_config": map[string]any{"thinking_level": "high"}},
			map[string]any{}, "high"},
		{"原生取不到回退归一化档位", "openai",
			map[string]any{}, map[string]any{"reasoning_effort": "medium"}, "medium"},
		{"回退 anthropic 预算扩展键（旧键名兼容）", "anthropic",
			map[string]any{}, map[string]any{"_client_anthropic_budget": 2048}, "2048"},
		{"回退 gemini thinkingConfig 扩展键（旧键名兼容）", "gemini",
			map[string]any{}, map[string]any{"_gemini_thinkingConfig": map[string]any{"thinkingLevel": "LOW"}}, "LOW"},
		{"完全未携带", "openai",
			map[string]any{"model": "m"}, map[string]any{}, ""},
		{"body 为 nil 不 panic", "gemini", nil, nil, ""},
	}
	for _, c := range cases {
		if got := ClientReasoningLabel(c.format, c.body, c.norm); got != c.want {
			t.Errorf("%s: ClientReasoningLabel = %q, want %q", c.name, got, c.want)
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

	// Anthropic 方案：max_tokens 放不下有效预算 → 不写 body thinking，
	// 但仍在归一化形状落档位（出站映射会抬升 max_tokens），不再静默放弃
	body = map[string]any{"max_tokens": float64(1500)}
	norm = map[string]any{}
	if !InjectDefaultReasoning("anthropic", "", body, norm) {
		t.Fatal("max_tokens 过小时也应注入（归一化形状兜底）")
	}
	if _, ok := body["thinking"]; ok {
		t.Error("预算放不下时不应写入 body thinking")
	}
	if norm["reasoning_effort"] != defaultReasoningTier {
		t.Errorf("归一化形状应保留档位: %v", norm["reasoning_effort"])
	}

	// HasReasoningDepth：Anthropic 新协议 output_config.effort 也算显式配置
	if !HasReasoningDepth("anthropic",
		map[string]any{"output_config": map[string]any{"effort": "high"}}, map[string]any{}) {
		t.Error("output_config.effort 应判定为已携带推理参数")
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

// 日志「推理」列上下行：上行 = 客户端原始「字段=值」（未携带时带 * 的注入档位），
// 下行 = 按推理映射表转换后真正发往上游的「字段=值」。端到端验证三种典型链路。
func TestLogReasoningColumnsE2E(t *testing.T) {
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
		Port: 3993, Host: "127.0.0.1",
		Providers: []types.Provider{{
			ID: "po", Name: "O", Type: types.ProviderOpenAI,
			BaseURL: mock.URL, APIKey: "k", Models: []string{"test-model"}, Enabled: true,
		}},
	}

	last := types.LogEntry{}
	post := func(path, payload string) {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ret := HandleLLM(rec, req, cfg, func(e types.LogEntry) { last = e }, func(string) func() { return func() {} })
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
			t.Fatalf("%s status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}

	// A) OpenAI 客户端未携带 → 注入：上行标注注入档位（high*），下行是上游真实字段
	upstreamBody = ""
	last = types.LogEntry{}
	post("/v1/chat/completions", `{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`)
	if last.Reasoning != "high*" {
		t.Errorf("A 原始列 = %q, want high*", last.Reasoning)
	}
	if last.ReasoningOut != "reasoning_effort=high" {
		t.Errorf("A 转换后列 = %q, want reasoning_effort=high", last.ReasoningOut)
	}

	// B) OpenAI 客户端携带、同型直通 → 两行一致（原样转发，不过映射表）
	upstreamBody = ""
	last = types.LogEntry{}
	post("/v1/chat/completions",
		`{"model":"test-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`)
	if last.Reasoning != "reasoning_effort=xhigh" {
		t.Errorf("B 原始列 = %q, want reasoning_effort=xhigh", last.Reasoning)
	}
	if last.ReasoningOut != "reasoning_effort=xhigh" {
		t.Errorf("B 转换后列 = %q, want reasoning_effort=xhigh（同型直通原样）", last.ReasoningOut)
	}

	// C) Anthropic 客户端 thinking 预算 → OpenAI 上游：跨型，按映射表折成档位
	upstreamBody = ""
	last = types.LogEntry{}
	post("/v1/messages", `{"model":"test-model","max_tokens":4096,`+
		`"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":2048}}`)
	if last.Reasoning != "thinking.budget_tokens=2048" {
		t.Errorf("C 原始列 = %q, want thinking.budget_tokens=2048", last.Reasoning)
	}
	if last.ReasoningOut != "reasoning_effort=low" {
		t.Errorf("C 转换后列 = %q, want reasoning_effort=low", last.ReasoningOut)
	}
	var ub map[string]any
	if err := json.Unmarshal([]byte(upstreamBody), &ub); err != nil {
		t.Fatalf("C 上游 body 不是 JSON: %v\n%s", err, upstreamBody)
	}
	if eff, _ := ub["reasoning_effort"].(string); eff != "low" {
		t.Errorf("C 上游实际收到 reasoning_effort = %v, want low\n%s", ub["reasoning_effort"], upstreamBody)
	}
}
