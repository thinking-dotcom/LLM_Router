package format

import "testing"

// ---------------------------------------------------------------------------
// 推理参数映射表契约锁定（format/reasoning_table.go）
//
// 这张表是推理参数的唯一事实来源：任一格被改动都会在这里失败，改表必须同步
// 改测试（表即契约）。
// ---------------------------------------------------------------------------

func TestReasoningTierTableContract(t *testing.T) {
	cases := []struct {
		tier       string
		openai     string
		responses  string
		anthBudget int
		anthEffort string
		geminiLv   string
		gemini25   int
		inter      string
	}{
		{"none", "", "", 0, "", "MINIMAL", 0, "minimal"},
		{"minimal", "low", "minimal", 1024, "low", "MINIMAL", 1024, "minimal"},
		{"low", "low", "low", 2048, "low", "LOW", 1024, "low"},
		{"medium", "medium", "medium", 8192, "medium", "MEDIUM", 8192, "medium"},
		{"high", "high", "high", 24576, "high", "HIGH", 24576, "high"},
		{"xhigh", "high", "xhigh", 32768, "xhigh", "HIGH", 24576, "high"},
		{"max", "high", "max", 32768, "max", "HIGH", 24576, "high"},
	}
	for _, c := range cases {
		if got := OpenAIEffortOf(c.tier); got != c.openai {
			t.Errorf("%s: OpenAIEffortOf = %q, want %q", c.tier, got, c.openai)
		}
		if got := ResponsesEffortOf(c.tier); got != c.responses {
			t.Errorf("%s: ResponsesEffortOf = %q, want %q", c.tier, got, c.responses)
		}
		if got := AnthropicBudgetOf(c.tier); got != c.anthBudget {
			t.Errorf("%s: AnthropicBudgetOf = %d, want %d", c.tier, got, c.anthBudget)
		}
		if got := AnthropicEffortOf(c.tier); got != c.anthEffort {
			t.Errorf("%s: AnthropicEffortOf = %q, want %q", c.tier, got, c.anthEffort)
		}
		if got := GeminiLevelOf(c.tier); got != c.geminiLv {
			t.Errorf("%s: GeminiLevelOf = %q, want %q", c.tier, got, c.geminiLv)
		}
		if got := Gemini25BudgetOf(c.tier); got != c.gemini25 {
			t.Errorf("%s: Gemini25BudgetOf = %d, want %d", c.tier, got, c.gemini25)
		}
		if got := InteractionsLevelOf(c.tier); got != c.inter {
			t.Errorf("%s: InteractionsLevelOf = %q, want %q", c.tier, got, c.inter)
		}
	}
}

func TestNormalizeReasoningTier(t *testing.T) {
	for _, in := range []string{"high", "HIGH", " High ", "max", "MAX"} {
		want := in
		switch in {
		case "HIGH", " High ":
			want = "high"
		case "MAX":
			want = "max"
		}
		if got := NormalizeReasoningTier(in); got != want {
			t.Errorf("NormalizeReasoningTier(%q) = %q, want %q", in, got, want)
		}
	}
	// 未收录/空：按默认 high
	for _, in := range []string{"", "bogus", "  "} {
		if got := NormalizeReasoningTier(in); got != ReasoningHigh {
			t.Errorf("NormalizeReasoningTier(%q) = %q, want high", in, got)
		}
	}
}

// effOf 从编码结果里取某层字段，便于断言。
func effOf(t *testing.T, p *NativeReasoning, path ...string) any {
	t.Helper()
	if p == nil {
		t.Fatal("encode 返回 nil")
	}
	var cur any = p.Value
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("路径 %v 中断于 %v", path, cur)
		}
		cur = m[k]
	}
	return cur
}

func TestEncodeReasoningNativePerFamily(t *testing.T) {
	// OpenAI 兼容：注入走安全枚举（xhigh/max 封顶 high；none 不落字段）
	if got := effOf(t, EncodeReasoningNative(ReasoningFamilyOpenAI, "gpt-5", ReasoningMax), "reasoning_effort"); got != "high" {
		t.Errorf("openai max = %v, want high", got)
	}
	if got := effOf(t, EncodeReasoningNative(ReasoningFamilyOpenAI, "gpt-5", ReasoningXHigh), "reasoning_effort"); got != "high" {
		t.Errorf("openai xhigh = %v, want high", got)
	}
	if p := EncodeReasoningNative(ReasoningFamilyOpenAI, "gpt-5", ReasoningNone); p != nil {
		t.Errorf("openai none 不应落字段: %v", p.Value)
	}

	// Responses：reasoning.effort（max 同样封顶 high）
	if got := effOf(t, EncodeReasoningNative(ReasoningFamilyResponses, "gpt-5", ReasoningHigh), "reasoning", "effort"); got != "high" {
		t.Errorf("responses high = %v", got)
	}
	if p := EncodeReasoningNative(ReasoningFamilyResponses, "gpt-5", ReasoningNone); p != nil {
		t.Errorf("responses none 不应落字段: %v", p.Value)
	}

	// Anthropic 旧模型：enabled + budget_tokens
	p := EncodeReasoningNative(ReasoningFamilyAnthropic, "claude-opus-4-6", ReasoningMedium)
	if got := effOf(t, p, "thinking", "type"); got != "enabled" {
		t.Errorf("anthropic 旧模型 thinking.type = %v, want enabled", got)
	}
	if got := effOf(t, p, "thinking", "budget_tokens"); got != 8192 {
		t.Errorf("anthropic 旧模型 budget = %v, want 8192", got)
	}
	// Anthropic 新模型：adaptive + output_config.effort（无 budget_tokens）
	p = EncodeReasoningNative(ReasoningFamilyAnthropic, "claude-sonnet-5", ReasoningHigh)
	if got := effOf(t, p, "thinking", "type"); got != "adaptive" {
		t.Errorf("anthropic 新模型 thinking.type = %v, want adaptive", got)
	}
	if got := effOf(t, p, "output_config", "effort"); got != "high" {
		t.Errorf("anthropic 新模型 effort = %v, want high", got)
	}
	if th, _ := p.Value["thinking"].(map[string]any); th["budget_tokens"] != nil {
		t.Errorf("anthropic 新模型不应带 budget_tokens: %v", th)
	}
	// Anthropic none：新老模型都不落字段
	for _, m := range []string{"claude-opus-4-6", "claude-sonnet-5"} {
		if p := EncodeReasoningNative(ReasoningFamilyAnthropic, m, ReasoningNone); p != nil {
			t.Errorf("anthropic %s none 不应落字段: %v", m, p.Value)
		}
	}

	// Gemini：落点 generationConfig.thinkingConfig；家族择形
	p = EncodeReasoningNative(ReasoningFamilyGemini, "gemini-3.5-flash", ReasoningHigh)
	if got := effOf(t, p, "thinkingConfig", "thinkingLevel"); got != "HIGH" {
		t.Errorf("gemini 3 系 = %v, want HIGH", got)
	}
	p = EncodeReasoningNative(ReasoningFamilyGemini, "gemini-2.5-flash", ReasoningHigh)
	if got := effOf(t, p, "thinkingConfig", "thinkingBudget"); got != 24576 {
		t.Errorf("gemini 2.5 系 = %v, want 24576", got)
	}
	// 2.5-pro 思考不可关：none 不落字段；2.5-flash 用 budget=0 关闭
	if p := EncodeReasoningNative(ReasoningFamilyGemini, "gemini-2.5-pro", ReasoningNone); p != nil {
		t.Errorf("2.5-pro none 不应落字段: %v", p.Value)
	}
	p = EncodeReasoningNative(ReasoningFamilyGemini, "gemini-2.5-flash", ReasoningNone)
	if got := effOf(t, p, "thinkingConfig", "thinkingBudget"); got != 0 {
		t.Errorf("2.5-flash none = %v, want 0", got)
	}
	// Gemma 系：不支持思考配置
	if p := EncodeReasoningNative(ReasoningFamilyGemini, "gemma-4-31b-it", ReasoningHigh); p != nil {
		t.Errorf("gemma 不应落字段: %v", p.Value)
	}

	// Interactions：落点 generation_config.thinking_level；Gemma 跳过
	p = EncodeReasoningNative(ReasoningFamilyInteractions, "gemini-3.5-flash", ReasoningLow)
	if got := effOf(t, p, "thinking_level"); got != "low" {
		t.Errorf("interactions low = %v, want low", got)
	}
	if p := EncodeReasoningNative(ReasoningFamilyInteractions, "gemma-4-31b-it", ReasoningHigh); p != nil {
		t.Errorf("interactions + gemma 不应落字段: %v", p.Value)
	}

	// gemini-openai（Google 兼容端点，protobuf 严格校验）：不写任何推理参数
	if p := EncodeReasoningNative(ReasoningFamilyGeminiOpenAI, "gemini-3.5-flash", ReasoningHigh); p != nil {
		t.Errorf("gemini-openai 不应落字段: %v", p.Value)
	}
	if SupportsReasoningInjection(ReasoningFamilyGeminiOpenAI) {
		t.Error("gemini-openai 不应支持兜底注入")
	}
	// 支持的接入格式
	for _, f := range []string{ReasoningFamilyOpenAI, ReasoningFamilyResponses, ReasoningFamilyAnthropic,
		ReasoningFamilyGemini, ReasoningFamilyInteractions} {
		if !SupportsReasoningInjection(f) {
			t.Errorf("%s 应支持兜底注入", f)
		}
	}
	if SupportsReasoningInjection("bogus") {
		t.Error("未知格式不应支持兜底注入")
	}
}

func TestDecodeReasoningNative(t *testing.T) {
	cases := []struct {
		name     string
		inFormat string
		body     map[string]any
		tier     string
		label    string
		present  bool
	}{
		{"openai 顶层档位", ReasoningFamilyOpenAI,
			map[string]any{"reasoning_effort": "high"}, "high", "high", true},
		{"openai reasoning.effort", ReasoningFamilyOpenAI,
			map[string]any{"reasoning": map[string]any{"effort": "medium"}}, "medium", "medium", true},
		{"openai 未携带", ReasoningFamilyOpenAI, map[string]any{}, "", "", false},
		// Deep Code CLI 关思考时发的是 OpenAI 形状 thinking:{type:"disabled"}：
		// 只如实描述字段，不折算档位（enabled/disabled 不含档位信息）
		{"openai 形状 thinking", ReasoningFamilyOpenAI,
			map[string]any{"thinking": map[string]any{"type": "disabled"}}, "", "disabled", true},
		{"openai thinking 优先于档位", ReasoningFamilyOpenAI,
			map[string]any{
				"reasoning_effort": "max",
				"thinking":         map[string]any{"type": "enabled"},
			}, "max", "max", true},
		{"responses", ReasoningFamilyResponses,
			map[string]any{"reasoning": map[string]any{"effort": "low"}}, "low", "low", true},
		{"anthropic 预算优先于 type", ReasoningFamilyAnthropic,
			map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": 8192}}, "medium", "8192", true},
		{"anthropic 预算 2048 折回 low", ReasoningFamilyAnthropic,
			map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": 2048}}, "low", "2048", true},
		{"anthropic output_config.effort 优先", ReasoningFamilyAnthropic,
			map[string]any{
				"output_config": map[string]any{"effort": "xhigh"},
				"thinking":      map[string]any{"type": "enabled", "budget_tokens": 1024},
			}, "xhigh", "xhigh", true},
		{"anthropic 只有 type 不给档位", ReasoningFamilyAnthropic,
			map[string]any{"thinking": map[string]any{"type": "adaptive"}}, "", "adaptive", true},
		{"gemini thinkingLevel", ReasoningFamilyGemini,
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingLevel": "MEDIUM"},
			}}, "medium", "MEDIUM", true},
		{"gemini 动态思考 -1 不折算", ReasoningFamilyGemini,
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingBudget": -1},
			}}, "", "动态", true},
		{"gemini 关闭思考 0", ReasoningFamilyGemini,
			map[string]any{"generationConfig": map[string]any{
				"thinkingConfig": map[string]any{"thinkingBudget": 0},
			}}, "none", "0", true},
		{"interactions thinking_level", ReasoningFamilyInteractions,
			map[string]any{"generation_config": map[string]any{"thinking_level": "HIGH"}}, "high", "HIGH", true},
		{"interactions thinking_budget", ReasoningFamilyInteractions,
			map[string]any{"generation_config": map[string]any{"thinking_budget": 8192}}, "medium", "8192", true},
	}
	for _, c := range cases {
		tier, label, present := DecodeReasoningNative(c.inFormat, c.body)
		if tier != c.tier || label != c.label || present != c.present {
			t.Errorf("%s: got (%q,%q,%v), want (%q,%q,%v)",
				c.name, tier, label, present, c.tier, c.label, c.present)
		}
	}
}

// 往返：表内 low/medium/high 在各家族「编码后能被读回同档」（不丢档）。
// xhigh/max/none 属有损映射（Gemini 无 xhigh 概念、none 在 Gemini 3 折叠为
// MINIMAL），由 TestReasoningTierTableContract 显式锁定其取值。
func TestReasoningEncodeDecodeRoundTrip(t *testing.T) {
	targets := []struct{ format, model string }{
		{ReasoningFamilyOpenAI, "gpt-5"},
		{ReasoningFamilyResponses, "gpt-5"},
		{ReasoningFamilyAnthropic, "claude-opus-4-6"}, // 旧家族：enabled + budget
		{ReasoningFamilyAnthropic, "claude-sonnet-5"}, // 新家族：adaptive + effort
		{ReasoningFamilyGemini, "gemini-3.5-flash"},
		{ReasoningFamilyGemini, "gemini-2.5-flash"},
		{ReasoningFamilyInteractions, "gemini-3.5-flash"},
	}
	for _, tg := range targets {
		for _, tier := range []string{ReasoningLow, ReasoningMedium, ReasoningHigh} {
			body := map[string]any{}
			if !ApplyReasoningNative(body, tg.format, tg.model, tier) {
				t.Errorf("%s/%s %s: 应写入原生参数", tg.format, tg.model, tier)
				continue
			}
			got, _, ok := DecodeReasoningNative(tg.format, body)
			if !ok || got != tier {
				t.Errorf("%s/%s %s: 往返得 %q(ok=%v), want %s (body=%v)",
					tg.format, tg.model, tier, got, ok, tier, body)
			}
		}
	}
}
