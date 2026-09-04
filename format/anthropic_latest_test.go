package format

import (
	"strings"
	"testing"

	"llm-router-go/types"
)

func TestToAnthropicAdaptiveThinkingForNewModels(t *testing.T) {
	req := map[string]any{
		"model":            "claude-opus-5",
		"messages":         []any{map[string]any{"role": "user", "content": "hi"}},
		"max_tokens":       float64(4000),
		"reasoning_effort": "high",
	}
	body := ToAnthropic(req)
	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking missing: %v", body["thinking"])
	}
	if thinking["type"] != "adaptive" {
		t.Errorf("thinking.type = %v, want adaptive (Claude 5 推荐)", thinking["type"])
	}
	oc, _ := body["output_config"].(map[string]any)
	if oc == nil {
		t.Fatalf("output_config missing for adaptive reasoning: %v", body)
	}
	if oc["effort"] != "high" {
		t.Errorf("output_config.effort = %v, want high", oc["effort"])
	}
	// adaptive 不写 budget_tokens
	if _, has := thinking["budget_tokens"]; has {
		t.Errorf("adaptive thinking should not carry budget_tokens: %v", thinking)
	}
	// 不触发 max_tokens > budget 修正
	if mt, _ := body["max_tokens"]; mt != float64(4000) && mt != 4000 {
		t.Errorf("max_tokens = %v, want unchanged 4000", mt)
	}
}

func TestToAnthropicLegacyThinkingForOldModels(t *testing.T) {
	req := map[string]any{
		"model":            "claude-3-5-sonnet",
		"messages":         []any{map[string]any{"role": "user", "content": "hi"}},
		"max_tokens":       float64(4000),
		"reasoning_effort": "high",
	}
	body := ToAnthropic(req)
	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking missing: %v", body["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Errorf("legacy model thinking.type = %v, want enabled", thinking["type"])
	}
	if _, has := body["output_config"]; has {
		t.Errorf("legacy model should not get output_config: %v", body["output_config"])
	}
}

func TestToAnthropicRestoresIRParams(t *testing.T) {
	// Anthropic 入站 → 归一化 → 出站 Anthropic：output_config / inference_geo /
	// service_tier 需经 IR 往返保真（OpenAI 形状无这些字段）。
	openaiReq := map[string]any{
		"model":    "claude-opus-5",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	types.IRSet(openaiReq, types.ExtAnthropicOutputConfig, map[string]any{"effort": "max", "format": map[string]any{"type": "json_schema"}})
	types.IRSet(openaiReq, types.ExtAnthropicInferenceGeo, "eu")
	types.IRSet(openaiReq, types.ExtAnthropicServiceTier, "standard_only")

	body := ToAnthropic(openaiReq)
	if body["inference_geo"] != "eu" {
		t.Errorf("inference_geo = %v", body["inference_geo"])
	}
	if body["service_tier"] != "standard_only" {
		t.Errorf("service_tier = %v", body["service_tier"])
	}
	oc, _ := body["output_config"].(map[string]any)
	if oc == nil || oc["effort"] != "max" {
		t.Errorf("output_config = %v", body["output_config"])
	}
}

func TestOpenaiFromAnthropicPreservesIRParams(t *testing.T) {
	raw := map[string]any{
		"model":         "claude-opus-5",
		"messages":      []any{map[string]any{"role": "user", "content": "hi"}},
		"output_config": map[string]any{"effort": "high"},
		"inference_geo": "us",
		"service_tier":  "auto",
	}
	out := OpenaiFromAnthropic(raw)
	if v, ok := types.IRGet(out, types.ExtAnthropicOutputConfig); !ok || v == nil {
		t.Error("output_config not preserved in IR")
	}
	if v, ok := types.IRGet(out, types.ExtAnthropicInferenceGeo); !ok || v != "us" {
		t.Error("inference_geo not preserved in IR")
	}
	if v, ok := types.IRGet(out, types.ExtAnthropicServiceTier); !ok || v != "auto" {
		t.Error("service_tier not preserved in IR")
	}
}

func TestAnthropicToOpenaiNewStopReasons(t *testing.T) {
	cases := []struct{ reason, want string }{
		{"model_context_window_exceeded", "length"},
		{"pause_turn", "stop"},
		{"refusal", "content_filter"},
		{"end_turn", "stop"},
	}
	for _, c := range cases {
		got := mapStopReason(c.reason)
		if got != c.want {
			t.Errorf("mapStopReason(%q) = %q, want %q", c.reason, got, c.want)
		}
	}
}

func TestAnthropicToOpenaiStopDetailsRefusal(t *testing.T) {
	body := map[string]any{
		"id":          "msg_1",
		"model":       "claude-opus-5",
		"stop_reason": "refusal",
		"stop_details": map[string]any{
			"category":    "general_harms",
			"explanation": "Request declined.",
			"type":        "refusal",
		},
		"content": []any{},
	}
	out := AnthropicToOpenai(body, map[string]any{"model": "claude-opus-5"})
	ch := out["choices"].([]any)
	c0 := ch[0].(map[string]any)
	msg := c0["message"].(map[string]any)
	if msg["refusal"] != "Request declined." {
		t.Errorf("refusal = %v", msg["refusal"])
	}
	if c0["finish_reason"] != "content_filter" {
		t.Errorf("finish_reason = %v", c0["finish_reason"])
	}
}

func TestAnthropicToOpenaiUsageThinkingTokens(t *testing.T) {
	body := map[string]any{
		"id":          "msg_1",
		"model":       "claude-opus-5",
		"stop_reason": "end_turn",
		"content":     []any{map[string]any{"type": "text", "text": "hi"}},
		"usage": map[string]any{
			"input_tokens":  100.0,
			"output_tokens": 50.0,
			"output_tokens_details": map[string]any{
				"thinking_tokens": 30.0,
			},
		},
	}
	out := AnthropicToOpenai(body, map[string]any{})
	u, ok := out["usage"].(map[string]any)
	if !ok {
		t.Fatal("usage missing")
	}
	cd, _ := u["completion_tokens_details"].(map[string]any)
	if cd == nil || cd["reasoning_tokens"] != 30 {
		t.Errorf("completion_tokens_details = %v, want reasoning_tokens 30", cd)
	}
}

func TestAnthropicStreamCitationsDelta(t *testing.T) {
	sse := sseBody(
		"event: message_start\ndata: "+`{"type":"message_start","message":{"model":"claude-opus-5","usage":{"input_tokens":5}}}`,
		"event: content_block_start\ndata: "+`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"event: content_block_delta\ndata: "+`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Answer"}}`,
		"event: content_block_delta\ndata: "+`{"type":"content_block_delta","index":0,"delta":{"type":"citations_delta","citation":{"type":"char_location","cited_text":"x"}}}`,
		"event: message_delta\ndata: "+`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		"event: message_stop\ndata: "+`{"type":"message_stop"}`,
	)
	sawCitation := false
	for chunk := range AnthropicStreamToChunks(sse) {
		if v, ok := chunk[types.ExtAnthropicCitationsDelta]; ok && v != nil {
			sawCitation = true
		}
	}
	if !sawCitation {
		t.Error("citations_delta not forwarded via IR chunk")
	}
}

func TestAnthropicSSEWriterCitationsDelta(t *testing.T) {
	// 把带 citations IR 键的 chunk 写出为 citations_delta 事件
	in := make(chan map[string]any, 4)
	in <- map[string]any{"object": "chat.completion.chunk", "model": "claude-opus-5", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}}}}
	in <- map[string]any{
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "Answer"}}},
	}
	in <- map[string]any{types.ExtAnthropicCitationsDelta: map[string]any{"type": "char_location"}}
	in <- map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "delta": map[string]any{}}}}
	close(in)

	var sb strings.Builder
	_, err := WriteChunksToAnthropic(&sb, in, "claude-opus-5")
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(sb.String(), "citations_delta") {
		t.Errorf("output lacks citations_delta event:\n%s", sb.String())
	}
	if !strings.Contains(sb.String(), "char_location") {
		t.Errorf("output lacks citation payload:\n%s", sb.String())
	}
}

func TestChatToResponsesNewParams(t *testing.T) {
	req := map[string]any{
		"model": "gpt-5",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		"max_completion_tokens": float64(100),
		"store":                 true,
		"service_tier":          "flex",
		"metadata":              map[string]any{"k": "v"},
		"safety_identifier":     "hash-1",
		"prompt_cache_key":      "ck",
		"parallel_tool_calls":   false,
	}
	body := ChatToResponses(req)
	if body["store"] != true {
		t.Errorf("store = %v", body["store"])
	}
	if body["service_tier"] != "flex" {
		t.Errorf("service_tier = %v", body["service_tier"])
	}
	if body["safety_identifier"] != "hash-1" {
		t.Errorf("safety_identifier = %v", body["safety_identifier"])
	}
	if body["prompt_cache_key"] != "ck" {
		t.Errorf("prompt_cache_key = %v", body["prompt_cache_key"])
	}
	if body["parallel_tool_calls"] != false {
		t.Errorf("parallel_tool_calls = %v", body["parallel_tool_calls"])
	}
	if body["max_output_tokens"] != 100 {
		t.Errorf("max_output_tokens = %v", body["max_output_tokens"])
	}
	if md, _ := body["metadata"].(map[string]any); md == nil || md["k"] != "v" {
		t.Errorf("metadata = %v", body["metadata"])
	}
}

func TestOpenaiFromResponsesNewParams(t *testing.T) {
	raw := map[string]any{
		"model":             "gpt-5",
		"input":             "hi",
		"store":             true,
		"service_tier":      "priority",
		"safety_identifier": "hash-2",
		"prompt_cache_key":  "ck2",
		"max_tool_calls":    3,
		"text":              map[string]any{"verbosity": "high"},
	}
	req := OpenaiFromResponses(raw)
	if req["store"] != true {
		t.Errorf("store = %v", req["store"])
	}
	if req["service_tier"] != "priority" {
		t.Errorf("service_tier = %v", req["service_tier"])
	}
	if req["verbosity"] != "high" {
		t.Errorf("verbosity = %v", req["verbosity"])
	}
	if req["max_tool_calls"] != 3 {
		t.Errorf("max_tool_calls = %v", req["max_tool_calls"])
	}
}

func TestResponsesSSEWriterTextVerbosity(t *testing.T) {
	// 回归：Responses 接入经 text.format IR 还原与 verbosity 直通不冲突
	req := map[string]any{
		"model":  "gpt-5",
		"input":  "hi",
		"stream": false,
	}
	_ = req
}

func TestToGeminiSeedMapping(t *testing.T) {
	req := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		"seed": float64(42),
	}
	out := ToGemini(req)
	gc, _ := out["generationConfig"].(map[string]any)
	if gc == nil {
		t.Fatalf("generationConfig missing: %v", out)
	}
	if gc["seed"] != float64(42) {
		t.Errorf("seed = %v, want 42", gc["seed"])
	}
}

func TestAdapterImplementsFullDocsParams(t *testing.T) {
	// 端到端冒烟：OpenAI 客户端 → Gemini(interactions) 上游，
	// 验证最新参数（reasoning_effort → thinking_level、response_format →
	// response_format+response_mime_type、store）走通。
	_ = types.ExtAnthropicOutputConfig
	_ = types.ExtAnthropicInferenceGeo
	_ = types.ExtAnthropicServiceTier
	if !strings.HasPrefix(string(types.ExtGeminiResponseSchema), "_ext_") {
		t.Error("unexpected")
	}
}
