package format

import (
	"testing"

	"llm-router-go/types"
)

// ---------------------------------------------------------------------------
// 统一推理档位 → Gemini thinkingConfig 的模型家族折算：
//   - 2.5 系只认 thinkingBudget（官方映射 low=1024 / medium=8192 / high=24576）；
//   - 3 系用 thinkingLevel；
//   - Gemma 不支持思考配置，不应携带 thinkingConfig；
//   - none 仅 Flash 系可关（budget=0），2.5-pro 不可关则不携带。
// ---------------------------------------------------------------------------

func geminiThinkingOf(t *testing.T, model, eff string) map[string]any {
	t.Helper()
	req := map[string]any{
		"model":            model,
		"reasoning_effort": eff,
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	out := ToGemini(req)
	gc, _ := out["generationConfig"].(map[string]any)
	if gc == nil {
		return nil
	}
	tc, _ := gc["thinkingConfig"].(map[string]any)
	return tc
}

func TestGeminiThinkingFamilyMapping(t *testing.T) {
	cases := []struct {
		name  string
		model string
		eff   string
		want  map[string]any // nil = 不应携带 thinkingConfig
	}{
		{"2.5-flash high 折算预算", "gemini-2.5-flash", "high",
			map[string]any{"thinkingBudget": 24576}},
		{"2.5-flash medium 折算预算", "gemini-2.5-flash", "medium",
			map[string]any{"thinkingBudget": 8192}},
		{"2.5-flash low 折算预算", "gemini-2.5-flash", "low",
			map[string]any{"thinkingBudget": 1024}},
		{"2.5-flash none 可关闭", "gemini-2.5-flash", "none",
			map[string]any{"thinkingBudget": 0}},
		{"2.5-pro none 不可关闭不携带", "gemini-2.5-pro", "none", nil},
		{"2.5-pro high 折算预算", "gemini-2.5-pro", "high",
			map[string]any{"thinkingBudget": 24576}},
		{"3 系 high 走档位", "gemini-3.5-flash", "high",
			map[string]any{"thinkingLevel": "HIGH"}},
		{"3 系 medium 走档位", "gemini-3.1-pro-preview", "medium",
			map[string]any{"thinkingLevel": "MEDIUM"}},
		{"3 系 none 折算 MINIMAL", "gemini-3.5-flash", "none",
			map[string]any{"thinkingLevel": "MINIMAL"}},
		{"gemma 不携带思考配置", "gemma-4-31b-it", "high", nil},
		{"gemma low 同样不携带", "gemma-4-26b-a4b-it", "low", nil},
		{"2.5-flash 大写写法仍折算预算", "Gemini-2.5-Flash", "high",
			map[string]any{"thinkingBudget": 24576}},
		{"2.5-pro 大写写法的 none 仍不可关闭", "Gemini-2.5-Pro", "none", nil},
		{"gemma 大写写法不携带思考配置", "Gemma-4-31B-IT", "high", nil},
	}
	for _, c := range cases {
		got := geminiThinkingOf(t, c.model, c.eff)
		if c.want == nil {
			if got != nil {
				t.Errorf("%s: 不应携带 thinkingConfig, got %v", c.name, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("%s: 应携带 thinkingConfig", c.name)
			continue
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v (full: %v)", c.name, k, got[k], v, got)
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: thinkingConfig 应只含 %v, got %v", c.name, c.want, got)
		}
	}
}

func TestGeminiInteractionsGemmaNoThinking(t *testing.T) {
	req := map[string]any{
		"model":            "gemma-4-31b-it",
		"reasoning_effort": "high",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	out := ToGeminiInteractions(req)
	gc, _ := out["generation_config"].(map[string]any)
	if gc != nil {
		if _, ok := gc["thinking_level"]; ok {
			t.Errorf("gemma 不应携带 thinking_level: %v", gc)
		}
	}

	req["model"] = "gemini-3.7-flash"
	out = ToGeminiInteractions(req)
	gc, _ = out["generation_config"].(map[string]any)
	if gc == nil || gc["thinking_level"] != "high" {
		t.Errorf("gemini 3 系应携带 thinking_level=high: %v", gc)
	}
}

// ---------------------------------------------------------------------------
// 入站折算：Gemini thinkingConfig → 统一推理档位（reasoning_effort）。
// thinkingBudget 的 0 与 -1 语义不同（0=关闭思考、-1=动态思考），
// 不能一概折成 none。
// ---------------------------------------------------------------------------

func TestReasoningEffortFromThinkingBudgetSemantics(t *testing.T) {
	cases := []struct {
		name string
		tc   map[string]any
		want string
	}{
		{"动态思考 -1 不折算", map[string]any{"thinkingBudget": float64(-1)}, ""},
		{"关闭 0 折为 none", map[string]any{"thinkingBudget": float64(0)}, "none"},
		{"1024 折为 low", map[string]any{"thinkingBudget": float64(1024)}, "low"},
		{"8192 折为 medium", map[string]any{"thinkingBudget": float64(8192)}, "medium"},
		{"24576 折为 high", map[string]any{"thinkingBudget": float64(24576)}, "high"},
		{"档位优先于预算", map[string]any{"thinkingLevel": "HIGH", "thinkingBudget": float64(-1)}, "high"},
		{"空 thinkingConfig 不折算", map[string]any{}, ""},
	}
	for _, c := range cases {
		if got := reasoningEffortFromThinking(c.tc); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// agy 等 Gemini 系 CLI 默认携带 thinkingBudget:-1（动态思考）。跨型转换到
// OpenAI 兼容上游时不应写出 reasoning_effort（既不能写 none=禁用，也不该替
// 客户端定一个固定档位）；同时原生 thinkingConfig 仍留在 IR 扩展键里，既供
// Gemini 系上游取用、也让路由器认定「客户端已配置」而不再注入全局档位。
func TestGeminiInboundDynamicThinkingNotFolded(t *testing.T) {
	body := map[string]any{
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}},
		},
		"generationConfig": map[string]any{
			"thinkingConfig": map[string]any{"thinkingBudget": float64(-1)},
		},
	}
	out := OpenaiFromGemini(body)
	if eff, ok := out["reasoning_effort"]; ok {
		t.Errorf("动态思考不应折算 reasoning_effort, got %v", eff)
	}
	if _, ok := types.IRGet(out, types.ExtGeminiThinkingConfig); !ok {
		t.Error("应保留原生 thinkingConfig 扩展键（避免路由器再注入全局档位）")
	}

	// 对照组：0（显式关闭）仍应折为 none，行为不变。
	body["generationConfig"] = map[string]any{
		"thinkingConfig": map[string]any{"thinkingBudget": float64(0)},
	}
	out = OpenaiFromGemini(body)
	if eff, _ := out["reasoning_effort"].(string); eff != "none" {
		t.Errorf("预算 0 应折为 none, got %v", out["reasoning_effort"])
	}
}
