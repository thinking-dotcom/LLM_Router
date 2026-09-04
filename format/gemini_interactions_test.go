package format

import (
	"io"
	"strings"
	"testing"

	"llm-router-go/types"
)

func TestIsGeminiInteractions(t *testing.T) {
	if !isGeminiInteractions("https://generativelanguage.googleapis.com/v1beta/interactions") {
		t.Error("interactions base should be detected")
	}
	if isGeminiInteractions("https://generativelanguage.googleapis.com/v1beta") {
		t.Error("legacy base should not be detected as interactions")
	}
}

func TestResolveEndpointGeminiInteractions(t *testing.T) {
	p := types.Provider{Type: types.ProviderGemini, BaseURL: "https://generativelanguage.googleapis.com/v1beta/interactions"}
	got := ResolveEndpoint("gemini", p, "gemini-3.7-flash")
	if got != "https://generativelanguage.googleapis.com/v1beta/interactions" {
		t.Errorf("interactions endpoint = %q, want base verbatim (model in body)", got)
	}
	// legacy 行为不变
	p2 := types.Provider{Type: types.ProviderGemini, BaseURL: "https://generativelanguage.googleapis.com"}
	got2 := ResolveEndpoint("gemini", p2, "gemini-3.7-flash")
	if got2 != "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.7-flash:generateContent" {
		t.Errorf("legacy endpoint = %q", got2)
	}
}

func TestBuildOutboundGeminiInteractions(t *testing.T) {
	req := map[string]any{
		"model": "gemini-3.7-flash",
		"messages": []any{
			map[string]any{"role": "system", "content": "Be helpful."},
			map[string]any{"role": "user", "content": "hi"},
		},
		"max_tokens":       float64(8192),
		"temperature":      float64(1.0),
		"reasoning_effort": "high",
	}
	p := types.Provider{
		Type:    types.ProviderGemini,
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/interactions",
		APIKey:  "gk",
	}
	ob := BuildOutboundRequest(p, req, "")
	if !strings.Contains(ob.URL, "/interactions") {
		t.Errorf("URL = %q, want interactions endpoint", ob.URL)
	}
	if !strings.Contains(ob.URL, "key=gk") {
		t.Errorf("URL = %q, want key param", ob.URL)
	}
	body, _ := ob.Body.(map[string]any)
	if body["model"] != "gemini-3.7-flash" {
		t.Errorf("interactions body model = %v", body["model"])
	}
	if body["system_instruction"] != "Be helpful." {
		t.Errorf("system_instruction = %v", body["system_instruction"])
	}
	steps, _ := body["input"].([]any)
	if len(steps) != 1 {
		t.Fatalf("input steps = %v, want 1 user_input step", body["input"])
	}
	step, _ := steps[0].(map[string]any)
	if step == nil || step["type"] != "user_input" {
		t.Fatalf("first step = %v", steps[0])
	}
	gc, _ := body["generation_config"].(map[string]any)
	if gc == nil {
		t.Fatal("generation_config missing")
	}
	if gc["thinking_level"] != "high" {
		t.Errorf("thinking_level = %v, want high", gc["thinking_level"])
	}
	if gc["max_output_tokens"] != float64(8192) {
		t.Errorf("max_output_tokens = %v", gc["max_output_tokens"])
	}
	// 非流式无 alt=sse
	if strings.Contains(ob.URL, "alt=sse") {
		t.Errorf("non-stream URL should not have alt=sse: %q", ob.URL)
	}
}

func TestBuildOutboundGeminiInteractionsStream(t *testing.T) {
	req := map[string]any{
		"model": "gemini-3.7-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		"stream": true,
	}
	p := types.Provider{
		Type:    types.ProviderGemini,
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/interactions",
		APIKey:  "gk",
	}
	ob := BuildOutboundRequest(p, req, "")
	body, _ := ob.Body.(map[string]any)
	if body["stream"] != true {
		t.Errorf("interactions stream body = %v, want stream:true", body["stream"])
	}
	if !strings.Contains(ob.URL, "alt=sse") {
		t.Errorf("stream URL should have alt=sse: %q", ob.URL)
	}
}

func TestGeminiInteractionsToOpenai(t *testing.T) {
	body := map[string]any{
		"id":     "v1_abc",
		"model":  "gemini-3.7-flash",
		"status": "completed",
		"steps": []any{
			map[string]any{"type": "thought", "signature": "sig"},
			map[string]any{
				"type": "model_output",
				"content": []any{
					map[string]any{"type": "text", "text": "The answer is 42."},
				},
			},
		},
		"usage": map[string]any{
			"total_input_tokens":   100.0,
			"total_output_tokens":  50.0,
			"total_cached_tokens":  10.0,
			"total_thought_tokens": 5.0,
			"total_tokens":         150.0,
		},
	}
	out := GeminiToOpenai(body, map[string]any{"model": "gemini-3.7-flash"})
	ch := out["choices"].([]any)
	c0 := ch[0].(map[string]any)
	msg := c0["message"].(map[string]any)
	if msg["content"] != "The answer is 42." {
		t.Errorf("content = %v", msg["content"])
	}
	if c0["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", c0["finish_reason"])
	}
}

func TestGeminiInteractionsToOpenaiFunctionCall(t *testing.T) {
	body := map[string]any{
		"id":     "v1_call",
		"status": "requires_action",
		"steps": []any{
			map[string]any{
				"type": "function_call",
				"id":   "call_abc123",
				"name": "get_weather",
				"arguments": map[string]any{
					"location": "London",
				},
			},
		},
	}
	out := GeminiToOpenai(body, map[string]any{})
	ch := out["choices"].([]any)
	c0 := ch[0].(map[string]any)
	msg := c0["message"].(map[string]any)
	tcs, _ := msg["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("tool_calls = %v", msg["tool_calls"])
	}
	tc := tcs[0].(map[string]any)
	fn := tc["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("tool name = %v", fn["name"])
	}
	if c0["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v", c0["finish_reason"])
	}
}

func TestGeminiInteractionsStreamToChunks(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("event: interaction.created\ndata: " + `{"interaction":{"id":"v1_x","status":"in_progress","model":"gemini-3.7-flash"},"event_type":"interaction.created"}` + "\n\n")
	sb.WriteString("event: step.start\ndata: " + `{"index":0,"step":{"type":"thought"},"event_type":"step.start"}` + "\n\n")
	sb.WriteString("event: step.start\ndata: " + `{"index":1,"step":{"type":"model_output"},"event_type":"step.start"}` + "\n\n")
	sb.WriteString("event: step.delta\ndata: " + `{"index":1,"delta":{"type":"text","text":"AI "},"event_type":"step.delta"}` + "\n\n")
	sb.WriteString("event: step.delta\ndata: " + `{"index":1,"delta":{"type":"text","text":"works."},"event_type":"step.delta"}` + "\n\n")
	sb.WriteString("event: interaction.completed\ndata: " + `{"interaction":{"id":"v1_x","status":"completed","usage":{"total_input_tokens":8,"total_output_tokens":12}},"event_type":"interaction.completed"}` + "\n\n")

	var content strings.Builder
	finish := ""
	usageSeen := false
	for chunk := range GeminiStreamToChunks(io.NopCloser(strings.NewReader(sb.String()))) {
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			if _, ok := chunk["usage"]; ok {
				usageSeen = true
			}
			continue
		}
		c0, _ := choices[0].(map[string]any)
		if fr, ok := c0["finish_reason"].(string); ok && fr != "" {
			finish = fr
			continue
		}
		delta, _ := c0["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		if txt, ok := delta["content"].(string); ok {
			content.WriteString(txt)
		}
	}
	if content.String() != "AI works." {
		t.Errorf("stream content = %q", content.String())
	}
	if finish != "stop" {
		t.Errorf("finish = %q, want stop", finish)
	}
	if !usageSeen {
		t.Error("usage chunk missing")
	}
}

func TestGeminiInteractionsStreamToolCalls(t *testing.T) {
	sse := `event: interaction.created
data: {"interaction":{"id":"v1_x","status":"in_progress","model":"gemini-3.7-flash"},"event_type":"interaction.created"}

event: step.start
data: {"index":0,"step":{"type":"function_call","id":"call_1","name":"get_weather"},"event_type":"step.start"}

event: step.delta
data: {"index":0,"delta":{"type":"arguments_delta","partial_arguments":"{\"loc\":"},"event_type":"step.delta"}

event: step.delta
data: {"index":0,"delta":{"type":"arguments_delta","partial_arguments":"\"London\"}"},"event_type":"step.delta"}

event: interaction.completed
data: {"interaction":{"id":"v1_x","status":"completed"},"event_type":"interaction.completed"}

`
	sawTool := false
	var args strings.Builder
	finish := ""
	for chunk := range GeminiStreamToChunks(io.NopCloser(strings.NewReader(sse))) {
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		c0, _ := choices[0].(map[string]any)
		if fr, ok := c0["finish_reason"].(string); ok && fr != "" {
			finish = fr
			continue
		}
		delta, _ := c0["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		if tcs, ok := delta["tool_calls"].([]any); ok {
			for _, tc := range tcs {
				tcm, _ := tc.(map[string]any)
				sawTool = true
				if fn, ok := tcm["function"].(map[string]any); ok {
					args.WriteString(fn["arguments"].(string))
				}
			}
		}
	}
	if !sawTool {
		t.Fatal("expected tool_calls delta")
	}
	if args.String() != `{"loc":"London"}` {
		t.Errorf("accumulated args = %q", args.String())
	}
	if finish != "tool_calls" {
		t.Errorf("finish = %q, want tool_calls", finish)
	}
}
