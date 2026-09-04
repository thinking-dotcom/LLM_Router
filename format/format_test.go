package format

import "testing"

func TestParseInbound_OpenAI(t *testing.T) {
	body := map[string]any{
		"model":    "gpt-4",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
		"stream":   true,
	}
	ib := ParseInbound("/v1/chat/completions", body)
	if ib == nil || ib.Format != "openai" || !ib.Stream {
		t.Error("openai parse failed")
	}
}

func TestParseInbound_AliasPaths(t *testing.T) {
	body := map[string]any{
		"model":    "gpt-4",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	streamBody := map[string]any{
		"model":    "gpt-4",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
		"stream":   true,
	}
	for _, p := range []string{"/chat/completions"} {
		if ib := ParseInbound(p, body); ib == nil || ib.Format != "openai" || ib.Stream {
			t.Errorf("%s parse failed", p)
		}
	}
	if ib := ParseInbound("/chat/completions", streamBody); ib == nil || !ib.Stream {
		t.Error("alias stream flag failed")
	}
}

func TestParseInbound_Anthropic(t *testing.T) {
	body := map[string]any{
		"model":    "claude-3",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	ib := ParseInbound("/v1/messages", body)
	if ib == nil || ib.Format != "anthropic" {
		t.Error("anthropic parse failed")
	}
}

func TestParseInbound_Gemini(t *testing.T) {
	body := map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}}},
	}
	ib := ParseInbound("/v1beta/models/gemini-pro:generateContent", body)
	if ib == nil || ib.Format != "gemini" || ib.Stream {
		t.Error("gemini parse failed")
	}
	ib2 := ParseInbound("/v1beta/models/gemini-pro:streamGenerateContent", body)
	if ib2 == nil || !ib2.Stream {
		t.Error("gemini stream parse failed")
	}
}

func TestParseInbound_Unknown(t *testing.T) {
	if ParseInbound("/v1/unknown", map[string]any{}) != nil {
		t.Error("unknown path should return nil")
	}
}
