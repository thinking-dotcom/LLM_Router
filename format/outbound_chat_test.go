package format

import (
	"fmt"
	"testing"

	"llm-router-go/types"
)

func TestResolveAll(t *testing.T) {
	cases := []struct {
		ptype, base, model, expect string
	}{
		// openai
		{"openai", "https://api.deepseek.com", "deepseek-chat", "https://api.deepseek.com/v1/chat/completions"},
		{"openai", "https://api.deepseek.com/v1", "deepseek-chat", "https://api.deepseek.com/v1/chat/completions"},
		{"openai", "https://api.deepseek.com/v1/chat/completions", "deepseek-chat", "https://api.deepseek.com/v1/chat/completions"},
		{"openai", "https://opencode.ai/zen/go/v1", "minimax-m3", "https://opencode.ai/zen/go/v1/chat/completions"},
		// anthropic
		{"anthropic", "https://api.anthropic.com", "claude-3-5-sonnet", "https://api.anthropic.com/v1/messages"},
		{"anthropic", "https://api.anthropic.com/v1", "claude-3-5-sonnet", "https://api.anthropic.com/v1/messages"},
		{"anthropic", "https://api.anthropic.com/v1/messages", "claude-3-5-sonnet", "https://api.anthropic.com/v1/messages"},
		// gemini
		{"gemini", "https://generativelanguage.googleapis.com", "gemini-2.0-flash", "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent"},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta", "gemini-2.0-flash", "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent"},
		// gemini-openai
		{"gemini-openai", "https://generativelanguage.googleapis.com", "gemini-2.0-flash", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"},
		{"gemini-openai", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.0-flash", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"},
		{"gemini-openai", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions", "gemini-2.0-flash", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"},
		// 路径重复 → normalizeBaseURL 自动清除
		{"gemini-openai", "https://generativelanguage.googleapis.com/v1beta/openai/v1beta/openai", "gemini-2.0-flash", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"},
		// response
		{"response", "https://api.openai.com", "gpt-4o", "https://api.openai.com/v1/responses"},
		{"response", "https://api.openai.com/v1", "gpt-4o", "https://api.openai.com/v1/responses"},
		{"response", "https://api.openai.com/v1/responses", "gpt-4o", "https://api.openai.com/v1/responses"},
	}

	for _, c := range cases {
		p := types.Provider{Type: types.ProviderType(c.ptype), BaseURL: c.base}
		got := ResolveEndpoint(c.ptype, p, c.model)
		status := "PASS"
		if got != c.expect {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s\n  base:   %s\n  expect: %s\n  got:    %s\n\n", status, c.ptype, c.base, c.expect, got)
	}
}

// ToAnthropic 应真实拷贝 top_k（Anthropic API 支持 top_k），从而让 droplog 把
// top_k 视为折叠字段而不误报丢弃（此前被 structurallyFolded 豁免但实际未拷贝，
// 造成 anthropic 目标下 top_k「名义折叠、实为丢弃」的静默丢失盲区）。
func TestToAnthropicCopiesTopK(t *testing.T) {
	req := map[string]any{
		"model":       "m",
		"messages":    []any{},
		"temperature": float64(0.7),
		"top_p":       float64(0.9),
		"top_k":       float64(40),
	}
	body := ToAnthropic(req)
	if v, ok := body["top_k"].(float64); !ok || v != 40 {
		t.Errorf("ToAnthropic body top_k = %v (ok=%v), want 40", body["top_k"], ok)
	}
	if v, ok := body["temperature"].(float64); !ok || v != 0.7 {
		t.Errorf("ToAnthropic should keep temperature, got %v", body["temperature"])
	}
}
