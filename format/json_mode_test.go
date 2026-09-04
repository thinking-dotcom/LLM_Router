package format

import (
	"io"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 请求侧：response_format → Anthropic 工具调用降级
// ---------------------------------------------------------------------------

func TestToAnthropicResponseFormatJSONSchema(t *testing.T) {
	req := map[string]any{
		"model": "claude-3",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "weather",
				"schema": map[string]any{
					"type":     "object",
					"required": []any{"city"},
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	out := ToAnthropic(req)

	tools, ok := out["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 injected tool, got %v", out["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if tool == nil || tool["name"] != jsonModeToolName {
		t.Fatalf("expected tool %q, got %v", jsonModeToolName, tool)
	}
	inputSchema, _ := tool["input_schema"].(map[string]any)
	if inputSchema == nil || inputSchema["type"] != "object" {
		t.Fatalf("input_schema not passed through: %v", tool["input_schema"])
	}
	tc, _ := out["tool_choice"].(map[string]any)
	if tc == nil || tc["type"] != "tool" || tc["name"] != jsonModeToolName {
		t.Fatalf("expected forced tool_choice for %q, got %v", jsonModeToolName, out["tool_choice"])
	}
}

func TestToAnthropicResponseFormatJSONSchemaString(t *testing.T) {
	req := map[string]any{
		"model":    "claude-3",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "weather",
				"schema": `{"type":"object","properties":{"city":{"type":"string"}}}`,
			},
		},
	}
	out := ToAnthropic(req)
	tools, _ := out["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 injected tool, got %v", out["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	schema, _ := tool["input_schema"].(map[string]any)
	if schema == nil || schema["type"] != "object" {
		t.Fatalf("expected parsed string schema, got %v", tool["input_schema"])
	}
}

func TestToAnthropicResponseFormatJSONObject(t *testing.T) {
	req := map[string]any{
		"model":           "claude-3",
		"messages":        []any{map[string]any{"role": "user", "content": "hi"}},
		"response_format": map[string]any{"type": "json_object"},
	}
	out := ToAnthropic(req)
	tools, _ := out["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 injected tool, got %v", out["tools"])
	}
	// 无 schema 时回退为宽松对象约束
	tool, _ := tools[0].(map[string]any)
	schema, _ := tool["input_schema"].(map[string]any)
	if schema == nil || schema["type"] != "object" {
		t.Fatalf("expected loose object schema, got %v", tool["input_schema"])
	}
}

func TestToAnthropicKeepsExistingToolsWithJSONMode(t *testing.T) {
	req := map[string]any{
		"model":    "claude-3",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":       "get_weather",
					"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"schema": map[string]any{"type": "object"}},
		},
	}
	out := ToAnthropic(req)
	tools, _ := out["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected existing tool + json tool, got %v", out["tools"])
	}
	// tool_choice 被强制到 json 工具
	tc, _ := out["tool_choice"].(map[string]any)
	if tc == nil || tc["name"] != jsonModeToolName {
		t.Fatalf("expected forced json tool_choice, got %v", out["tool_choice"])
	}
}

func TestToAnthropicNoResponseFormatUnchanged(t *testing.T) {
	req := map[string]any{
		"model":       "claude-3",
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"tool_choice": "required",
	}
	out := ToAnthropic(req)
	if _, ok := out["tools"]; ok {
		t.Fatalf("unexpected tools injected: %v", out["tools"])
	}
	if out["tool_choice"] == nil {
		t.Fatal("existing tool_choice should be preserved")
	}
}

// ---------------------------------------------------------------------------
// 响应侧非流式：tool_use（json 工具）还原为 content
// ---------------------------------------------------------------------------

func TestAnthropicToOpenaiJSONModeRecovery(t *testing.T) {
	req := map[string]any{
		"model":           "claude-3",
		"response_format": map[string]any{"type": "json_schema"},
	}
	body := map[string]any{
		"id":          "msg_1",
		"model":       "claude-3",
		"stop_reason": "tool_use",
		"usage":       map[string]any{"input_tokens": float64(10), "output_tokens": float64(20)},
		"content": []any{
			map[string]any{"type": "tool_use", "id": "toolu_1", "name": jsonModeToolName, "input": map[string]any{"city": "Beijing", "temp": 18}},
		},
	}
	out := AnthropicToOpenai(body, req)

	choice, _ := out["choices"].([]any)
	if len(choice) != 1 {
		t.Fatalf("expected 1 choice, got %v", out)
	}
	c0, _ := choice[0].(map[string]any)
	msg, _ := c0["message"].(map[string]any)

	if _, hasTC := msg["tool_calls"]; hasTC {
		t.Fatalf("json mode must not surface tool_calls: %v", msg)
	}
	content, _ := msg["content"].(string)
	if !strings.Contains(content, `"city":"Beijing"`) {
		t.Fatalf("expected JSON serialized content, got %q", content)
	}
	if fr, _ := c0["finish_reason"].(string); fr != "stop" {
		t.Fatalf("expected finish_reason stop, got %q", fr)
	}
}

func TestAnthropicToOpenaiJSONModeTextPlusJson(t *testing.T) {
	req := map[string]any{
		"model":           "claude-3",
		"response_format": map[string]any{"type": "json_object"},
	}
	body := map[string]any{
		"id":          "msg_1",
		"model":       "claude-3",
		"stop_reason": "tool_use",
		"content": []any{
			map[string]any{"type": "text", "text": ""},
			map[string]any{"type": "tool_use", "id": "toolu_1", "name": jsonModeToolName, "input": map[string]any{"ok": true}},
		},
	}
	out := AnthropicToOpenai(body, req)
	choice, _ := out["choices"].([]any)
	c0, _ := choice[0].(map[string]any)
	msg, _ := c0["message"].(map[string]any)
	content, _ := msg["content"].(string)
	if !strings.Contains(content, `"ok":true`) {
		t.Fatalf("expected json content, got %q", content)
	}
}

func TestAnthropicToOpenaiNormalToolCallUnaffected(t *testing.T) {
	// 未声明 response_format 时，真实工具调用照常转为 tool_calls 且终止为 tool_calls
	req := map[string]any{"model": "claude-3"}
	body := map[string]any{
		"id":          "msg_1",
		"model":       "claude-3",
		"stop_reason": "tool_use",
		"content": []any{
			map[string]any{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": map[string]any{"city": "Beijing"}},
		},
	}
	out := AnthropicToOpenai(body, req)
	choice, _ := out["choices"].([]any)
	c0, _ := choice[0].(map[string]any)
	msg, _ := c0["message"].(map[string]any)
	if _, hasTC := msg["tool_calls"]; !hasTC {
		t.Fatal("normal tool_use must stay tool_calls")
	}
	if fr, _ := c0["finish_reason"].(string); fr != "tool_calls" {
		t.Fatalf("expected finish_reason tool_calls, got %q", fr)
	}
}

// ---------------------------------------------------------------------------
// 流式：Anthropic SSE → OpenAI chunks（JSON 降级）
// ---------------------------------------------------------------------------

func sseBody(events ...string) io.ReadCloser {
	var sb strings.Builder
	for _, e := range events {
		sb.WriteString(e)
		sb.WriteString("\n\n")
	}
	return io.NopCloser(strings.NewReader(sb.String()))
}

func TestAnthropicStreamJSONMode(t *testing.T) {
	sse := sseBody(
		"event: message_start\ndata: "+`{"type":"message_start","message":{"model":"claude-3","usage":{"input_tokens":5}}}`,
		"event: content_block_start\ndata: "+`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"json_output"}}`,
		"event: content_block_delta\ndata: "+`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`,
		"event: content_block_delta\ndata: "+`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"Beijing\"}"}}`,
		"event: message_delta\ndata: "+`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":10}}`,
		"event: message_stop\ndata: "+`{"type":"message_stop"}`,
	)

	var content strings.Builder
	finish := ""
	usage := false
	for chunk := range AnthropicStreamToChunks(sse) {
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			if _, ok := chunk["usage"]; ok {
				usage = true
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
		if _, hasTC := delta["tool_calls"]; hasTC {
			t.Fatal("json mode stream must not emit tool_calls deltas")
		}
	}

	if content.String() != `{"city":"Beijing"}` {
		t.Fatalf("expected json content stream, got %q", content.String())
	}
	if finish != "stop" {
		t.Fatalf("expected finish stop, got %q", finish)
	}
	if !usage {
		t.Fatal("expected usage chunk from message_stop")
	}
}

func TestAnthropicStreamNormalToolCallsUnaffected(t *testing.T) {
	sse := sseBody(
		"event: message_start\ndata: "+`{"type":"message_start","message":{"model":"claude-3","usage":{"input_tokens":5}}}`,
		"event: content_block_start\ndata: "+`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather"}}`,
		"event: content_block_delta\ndata: "+`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`,
		"event: content_block_delta\ndata: "+`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"Beijing\"}"}}`,
		"event: message_delta\ndata: "+`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
		"event: message_stop\ndata: "+`{"type":"message_stop"}`,
	)

	sawToolDelta := false
	sawFinish := ""
	for chunk := range AnthropicStreamToChunks(sse) {
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		c0, _ := choices[0].(map[string]any)
		if fr, ok := c0["finish_reason"].(string); ok && fr != "" {
			sawFinish = fr
			continue
		}
		delta, _ := c0["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		if _, hasTC := delta["tool_calls"]; hasTC {
			sawToolDelta = true
		}
	}
	if !sawToolDelta {
		t.Fatal("normal tool stream must emit tool_calls deltas")
	}
	if sawFinish != "tool_calls" {
		t.Fatalf("expected finish tool_calls, got %q", sawFinish)
	}
}
