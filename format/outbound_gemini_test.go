package format

import (
	"strings"
	"testing"

	"llm-router-go/types"
)

func TestStripGeminiUnsupported(t *testing.T) {
	req := map[string]any{
		"model":            "gemini-2.5-flash",
		"prompt_cache_key": "cache-123",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "get_weather",
					"parameters": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"exclusiveMinimum":     1,
						"$schema":              "https://json-schema.org/draft/2020-12/schema",
						"required":             []any{"city"},
						"properties": map[string]any{
							"city": map[string]any{"type": "string", "exclusiveMinimum": 0},
							"temp": map[string]any{"type": "number", "additionalProperties": true},
						},
						"items": map[string]any{
							"type":  "integer",
							"$defs": map[string]any{"c": map[string]any{"type": "string", "$schema": "x"}},
							"anyOf": []any{
								map[string]any{"type": "string", "exclusiveMaximum": 5},
							},
						},
					},
				},
			},
		},
	}

	stripGeminiUnsupported(req)

	if _, ok := req["prompt_cache_key"]; ok {
		t.Error("prompt_cache_key should be removed")
	}
	tools := req["tools"].([]any)
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	params := fn["parameters"].(map[string]any)

	if _, ok := params["additionalProperties"]; ok {
		t.Error("top-level additionalProperties should be removed")
	}
	if _, ok := params["exclusiveMinimum"]; ok {
		t.Error("top-level exclusiveMinimum should be removed")
	}
	if _, ok := params["required"]; !ok {
		t.Error("required should be preserved")
	}
	props := params["properties"].(map[string]any)
	if _, ok := props["city"].(map[string]any)["exclusiveMinimum"]; ok {
		t.Error("nested exclusiveMinimum should be removed")
	}
	if _, ok := props["temp"].(map[string]any)["additionalProperties"]; ok {
		t.Error("nested additionalProperties should be removed")
	}
	items := params["items"].(map[string]any)
	anyOf := items["anyOf"].([]any)
	if _, ok := anyOf[0].(map[string]any)["exclusiveMaximum"]; ok {
		t.Error("anyOf nested exclusiveMaximum should be removed")
	}
	// $schema（含 $ 前缀元关键词）应被删除
	if _, ok := params["$schema"]; ok {
		t.Error("top-level $schema should be removed")
	}
	if _, ok := items["$defs"]; ok {
		t.Error("$defs key should be removed")
	}
}

func TestBuildOutboundGeminiOpenAIStripsUnsupported(t *testing.T) {
	openaiReq := map[string]any{
		"model":            "gemini-2.5-flash",
		"prompt_cache_key": "cache-xyz",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":       "tool_a",
					"parameters": map[string]any{"type": "object", "additionalProperties": false},
				},
			},
		},
	}
	p := testProvider("gemini-openai")
	ob := BuildOutboundRequest(p, openaiReq, "")
	body := ob.Body.(map[string]any)

	if _, ok := body["prompt_cache_key"]; ok {
		t.Error("gemini-openai outbound should strip prompt_cache_key")
	}
	if !strings.Contains(ob.URL, "/v1beta/openai/chat/completions") {
		t.Errorf("unexpected URL: %s", ob.URL)
	}
	tools := body["tools"].([]any)
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if _, ok := fn["parameters"].(map[string]any)["additionalProperties"]; ok {
		t.Error("gemini-openai outbound should strip additionalProperties")
	}
}

func testProvider(ptype string) types.Provider {
	return types.Provider{
		ID:      "p-test",
		Name:    "test",
		Type:    types.ProviderType(ptype),
		BaseURL: "https://generativelanguage.googleapis.com",
		APIKey:  "test-key",
	}
}
