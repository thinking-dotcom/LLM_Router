package format

import "testing"

func TestParseInboundGeminiInteractions(t *testing.T) {
	body := map[string]any{
		"model":  "gemini-3.7-flash",
		"input":  "Explain how AI works",
		"stream": true,
	}
	ib := ParseInbound("/v1beta/interactions", body)
	if ib == nil || ib.Format != "gemini-interactions" {
		t.Fatalf("format = %v, want gemini-interactions", ib)
	}
	if !ib.Stream {
		t.Error("stream should be true")
	}
	msgs, _ := ib.OpenaiReq["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", ib.OpenaiReq["messages"])
	}
	m0, _ := msgs[0].(map[string]any)
	if m0["role"] != "user" || m0["content"] != "Explain how AI works" {
		t.Errorf("first message = %v", m0)
	}
}

func TestParseInboundGeminiInteractionsV1alpha(t *testing.T) {
	ib := ParseInbound("/v1alpha/interactions", map[string]any{"model": "m", "input": "hi"})
	if ib == nil || ib.Format != "gemini-interactions" {
		t.Fatalf("format = %v", ib)
	}
}

func TestOpenaiFromGeminiInteractionsSteps(t *testing.T) {
	body := map[string]any{
		"model":              "gemini-3.7-flash",
		"system_instruction": "You are helpful.",
		"input": []any{
			map[string]any{"type": "user_input", "content": []any{map[string]any{"type": "text", "text": "weather?"}}},
			map[string]any{"type": "function_call", "id": "call_1", "name": "get_weather", "arguments": map[string]any{"city": "London"}},
			map[string]any{"type": "function_result", "call_id": "call_1", "name": "get_weather", "result": map[string]any{"content": "sunny"}},
		},
		"generation_config": map[string]any{
			"thinking_level":    "high",
			"max_output_tokens": float64(2048),
		},
	}
	out := OpenaiFromGeminiInteractions(body)
	msgs, _ := out["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d: %v", len(msgs), out["messages"])
	}
	m0, _ := msgs[0].(map[string]any)
	if m0["role"] != "system" {
		t.Errorf("messages[0] = %v, want system (from system_instruction)", m0)
	}
	if out["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v", out["reasoning_effort"])
	}
	if out["max_tokens"] != float64(2048) {
		t.Errorf("max_tokens = %v", out["max_tokens"])
	}
}

func TestOpenaiFromGeminiInteractionsTools(t *testing.T) {
	body := map[string]any{
		"model": "gemini-3.7-flash",
		"input": "hi",
		"tools": []any{
			map[string]any{"type": "function", "name": "get_weather", "description": "w", "parameters": map[string]any{"type": "object"}},
			map[string]any{"type": "google_search"},
		},
	}
	out := OpenaiFromGeminiInteractions(body)
	tools, _ := out["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", out["tools"])
	}
	t0, _ := tools[0].(map[string]any)
	if t0["type"] != "function" {
		t.Errorf("tool[0] = %v", t0)
	}
}

func TestOpenaiFromGeminiInteractionsResponseFormat(t *testing.T) {
	body := map[string]any{
		"model": "gemini-3.7-flash",
		"input": "hi",
		"response_format": map[string]any{
			"type":      "text",
			"mime_type": "application/json",
			"schema":    map[string]any{"type": "object"},
		},
		"response_mime_type": "application/json",
	}
	out := OpenaiFromGeminiInteractions(body)
	rf, _ := out["response_format"].(map[string]any)
	if rf == nil || rf["type"] != "json_schema" {
		t.Errorf("response_format = %v", out["response_format"])
	}
}

func TestOpenaiToGeminiInteractionsBody(t *testing.T) {
	resp := map[string]any{
		"id":    "ch_1",
		"model": "gemini-3.7-flash",
		"choices": []any{
			map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "Hi!"},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{"prompt_tokens": float64(10), "completion_tokens": float64(5)},
	}
	out := OpenaiToGeminiInteractionsBody(resp, "gemini-3.7-flash")
	if out["object"] != "interaction" {
		t.Errorf("object = %v", out["object"])
	}
	steps, _ := out["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("steps = %v", out["steps"])
	}
	s0, _ := steps[0].(map[string]any)
	if s0["type"] != "model_output" {
		t.Errorf("step[0].type = %v", s0["type"])
	}
	u, ok := out["usage"].(map[string]any)
	if !ok || u["total_input_tokens"] != 10 {
		t.Errorf("usage = %v", out["usage"])
	}
}

func TestOpenaiToGeminiInteractionsBodyWithToolCall(t *testing.T) {
	resp := map[string]any{
		"id":    "ch_1",
		"model": "gemini-3.7-flash",
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{
						map[string]any{
							"id": "call_9", "type": "function",
							"function": map[string]any{"name": "get_weather", "arguments": `{"city":"Paris"}`},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	}
	out := OpenaiToGeminiInteractionsBody(resp, "gemini-3.7-flash")
	if out["status"] != "requires_action" {
		t.Errorf("status = %v, want requires_action", out["status"])
	}
	steps, _ := out["steps"].([]any)
	found := false
	for _, s := range steps {
		sm, _ := s.(map[string]any)
		if sm["type"] == "function_call" {
			found = true
			args, _ := sm["arguments"].(map[string]any)
			if args["city"] != "Paris" {
				t.Errorf("function_call arguments = %v", sm["arguments"])
			}
		}
	}
	if !found {
		t.Errorf("missing function_call step: %v", out["steps"])
	}
}
