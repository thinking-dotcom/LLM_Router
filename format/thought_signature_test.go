package format

import (
	"strings"
	"testing"

	"llm-router-go/types"
)

// Gemini 客户端历史里的 thoughtSignature 经 OpenAI 归一化枢纽往返到原生 Gemini 上游：
// OpenaiFromGemini 采集入 IR，ToGemini 按消息序号回填到 text / functionCall parts。
// 采集侧（msgCount）与回填侧（msgIdx）的序号口径必须都对每条消息自增（含 system），
// 否则「带 systemInstruction + 模型轮带签名」时记录整体偏移、签名被静默丢弃。
func TestThoughtSignatureRoundTripWithSystem(t *testing.T) {
	geminiReq := map[string]any{
		"model": "gemini-3-flash",
		"systemInstruction": map[string]any{
			"parts": []any{map[string]any{"text": "you are a tool user"}},
		},
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}},
			map[string]any{"role": "model", "parts": []any{
				map[string]any{"functionCall": map[string]any{"name": "f", "args": map[string]any{}}, "thoughtSignature": "SIG_FC"},
			}},
			map[string]any{"role": "user", "parts": []any{
				map[string]any{"functionResponse": map[string]any{"name": "f", "response": map[string]any{"ok": true}}},
			}},
		},
	}
	norm := OpenaiFromGemini(geminiReq)
	msgs := sliceOrEmpty(norm["messages"])
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d, want 4 (system+user+assistant+tool)", len(msgs))
	}
	if r, _ := msgs[0].(map[string]any); r == nil || r["role"] != "system" {
		t.Fatalf("messages[0] should be system, got %v", msgs[0])
	}
	recs, ok := types.IRGet(norm, types.ExtGeminiThoughtSigs)
	if !ok {
		t.Fatal("normalized request should carry thought signature records")
	}
	// 签名挂在 assistant（归一化序号 2，前有 system 与 user 各一条）
	wantIdx := -1
	for i, m := range msgs {
		if mm, _ := m.(map[string]any); mm != nil && mm["role"] == "assistant" {
			wantIdx = i
		}
	}
	gotIdx := -1
	for _, r := range sliceOrEmpty(recs) {
		rm, _ := r.(map[string]any)
		if rm == nil {
			continue
		}
		switch v := rm["message"].(type) {
		case int:
			gotIdx = v
		case float64:
			gotIdx = int(v)
		}
	}
	if gotIdx != wantIdx {
		t.Fatalf("signature record index = %d, want %d (assistant message)", gotIdx, wantIdx)
	}

	out := ToGemini(norm)
	contents := sliceOrEmpty(out["contents"])
	// systemInstruction 走顶层字段，contents 仅 3 条（user / model / user）
	if len(contents) != 3 {
		t.Fatalf("contents len = %d, want 3", len(contents))
	}
	modelContent, _ := contents[1].(map[string]any)
	if modelContent == nil {
		t.Fatal("contents[1] missing")
	}
	found := ""
	for _, p := range sliceOrEmpty(modelContent["parts"]) {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if _, isFC := pm["functionCall"]; isFC {
			if s, ok := pm["thoughtSignature"].(string); ok {
				found = s
			}
		}
	}
	if found != "SIG_FC" {
		t.Fatalf("thoughtSignature not reattached to functionCall, got %q", found)
	}
	// systemInstruction 必须原样保留（序号口径修正不应改变该行为）
	si, _ := out["systemInstruction"].(map[string]any)
	if si == nil {
		t.Fatal("systemInstruction lost")
	}
}

// Gemini 客户端历史里的 thoughtSignature 经 OpenAI 归一化枢纽往返到原生 Gemini 上游：
// OpenaiFromGemini 采集入 IR，ToGemini 按消息序号回填到 text / functionCall parts。
func TestThoughtSignatureRoundTripViaIR(t *testing.T) {
	geminiReq := map[string]any{
		"model": "gemini-3-flash",
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}},
			map[string]any{"role": "model", "parts": []any{
				map[string]any{"text": "let me call", "thoughtSignature": "SIG_TEXT"},
				map[string]any{"functionCall": map[string]any{"name": "f", "args": map[string]any{}}, "thoughtSignature": "SIG_FC"},
			}},
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "go"}}},
		},
	}
	norm := OpenaiFromGemini(geminiReq)
	recs, ok := types.IRGet(norm, types.ExtGeminiThoughtSigs)
	if !ok {
		t.Fatal("normalized request should carry thought signature records")
	}
	found := false
	for _, r := range sliceOrEmpty(recs) {
		rm, _ := r.(map[string]any)
		if rm == nil {
			continue
		}
		if sigs := sliceOrEmpty(rm["signatures"]); len(sigs) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 2 signatures on message index 1, got: %v", recs)
	}

	out := ToGemini(norm)
	contents := sliceOrEmpty(out["contents"])
	if len(contents) != 3 {
		t.Fatalf("contents len = %d, want 3", len(contents))
	}
	modelContent, _ := contents[1].(map[string]any)
	parts := sliceOrEmpty(modelContent["parts"])
	got := map[string]string{}
	for _, p := range parts {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if _, isText := pm["text"]; isText {
			if s, ok := pm["thoughtSignature"].(string); ok {
				got["text"] = s
			}
		}
		if _, isFC := pm["functionCall"]; isFC {
			if s, ok := pm["thoughtSignature"].(string); ok {
				got["functionCall"] = s
			}
		}
	}
	if got["text"] != "SIG_TEXT" || got["functionCall"] != "SIG_FC" {
		t.Fatalf("signatures not reattached in order: %v", got)
	}
}

// OpenAI 客户端在 assistant 消息 / tool_calls 上透传 thought_signature 时，
// 原生 Gemini 出站应回填到 parts。
func TestThoughtSignatureFromOpenAIMessage(t *testing.T) {
	norm := map[string]any{
		"model": "gemini-3-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{
				"role":              "assistant",
				"content":           "doing",
				"thought_signature": "SIG_MSG",
				"tool_calls": []any{
					map[string]any{
						"id":                "call_1",
						"type":              "function",
						"thought_signature": "SIG_TC",
						"function": map[string]any{
							"name":      "f",
							"arguments": "{}",
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "ok"},
			map[string]any{"role": "user", "content": "thanks"},
		},
	}
	out := ToGemini(norm)
	contents := sliceOrEmpty(out["contents"])
	// user(1) + model(1) + tool 归并为 user functionResponse(1) + user(1) => 4 条
	if len(contents) < 2 {
		t.Fatalf("contents len = %d, want >= 2", len(contents))
	}
	modelContent, _ := contents[1].(map[string]any)
	if modelContent == nil {
		t.Fatal("contents[1] missing")
	}
	seen := map[string]bool{}
	for _, p := range sliceOrEmpty(modelContent["parts"]) {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if s, ok := pm["thoughtSignature"].(string); ok {
			seen[s] = true
		}
	}
	if !seen["SIG_MSG"] || !seen["SIG_TC"] {
		t.Fatalf("expected SIG_MSG and SIG_TC reattached, got %v", seen)
	}
}

// 原生 Gemini 响应里的 thoughtSignature 转为 OpenAI 形状时随 assistant 消息透传。
func TestGeminiResponseCarriesThoughtSignature(t *testing.T) {
	body := map[string]any{
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"role": "model",
					"parts": []any{
						map[string]any{"text": "calling", "thoughtSignature": "SIG_A"},
						map[string]any{"functionCall": map[string]any{"name": "f", "args": map[string]any{}}, "thoughtSignature": "SIG_B"},
					},
				},
				"finishReason": "STOP",
			},
		},
	}
	resp := GeminiToOpenai(body, map[string]any{"model": "gemini-3-flash"})
	choices := sliceOrEmpty(resp["choices"])
	if len(choices) == 0 {
		t.Fatal("no choices")
	}
	c0, _ := choices[0].(map[string]any)
	msg, _ := c0["message"].(map[string]any)
	if msg == nil {
		t.Fatal("no message")
	}
	switch sig := msg["thought_signature"].(type) {
	case []any:
		if len(sig) != 2 || sig[0] != "SIG_A" || sig[1] != "SIG_B" {
			t.Fatalf("thought_signature = %v, want [SIG_A SIG_B]", sig)
		}
	default:
		t.Fatalf("thought_signature type %T, want []any", sig)
	}
}

// OpenAI / gemini-openai 上游不接受未知消息字段：消息级 thought_signature 必须剥离。
func TestStripMessageThoughtSignaturesForOpenAIUpstream(t *testing.T) {
	req := map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "content": "x", "thought_signature": "SIG"},
		},
	}
	p := types.Provider{ID: "p", Name: "oai", Type: types.ProviderOpenAI, BaseURL: "https://api.openai.com", APIKey: "k"}
	ob := BuildOutboundRequest(p, req, "")
	body, ok := ob.Body.(map[string]any)
	if !ok {
		t.Fatal("body not a map")
	}
	msgs := sliceOrEmpty(body["messages"])
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm == nil {
			continue
		}
		if _, has := mm["thought_signature"]; has {
			t.Fatal("thought_signature must be stripped for openai upstream")
		}
	}
}

// 签名多于可签名 part 时不得覆盖已按序配好的签名：此前兜底分支会把最后一个
// part 的签名改写成后面的签名，并丢掉其余签名（错配 → MISSING_THOUGHT_SIGNATURE）。
func TestAttachThoughtSignaturesDoesNotOverwritePaired(t *testing.T) {
	parts := []any{map[string]any{"text": "hello"}}
	out := attachThoughtSignatures(parts, []any{"s0", "s1", "s2"})
	if got, _ := out[0].(map[string]any)["thoughtSignature"].(string); got != "s0" {
		t.Fatalf("thoughtSignature = %q, want s0（多余签名不得覆盖已配对签名）", got)
	}

	// 完全没有可签名 part（异常形状）时才兜底附首个签名
	parts2 := []any{map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "x"}}}
	out2 := attachThoughtSignatures(parts2, []any{"s9"})
	if got, _ := out2[0].(map[string]any)["thoughtSignature"].(string); got != "s9" {
		t.Fatalf("fallback thoughtSignature = %q, want s9", got)
	}

	// 签名少于 part：按序部分回填，多余 part 不写
	parts3 := []any{
		map[string]any{"text": "a"},
		map[string]any{"functionCall": map[string]any{"name": "f"}},
	}
	out3 := attachThoughtSignatures(parts3, []any{"s0"})
	if got, _ := out3[0].(map[string]any)["thoughtSignature"].(string); got != "s0" {
		t.Fatalf("parts[0] thoughtSignature = %q, want s0", got)
	}
	if _, has := out3[1].(map[string]any)["thoughtSignature"]; has {
		t.Fatal("第二个 part 不应被写入签名")
	}
}

// tool_calls 上的 thought_signature 同样只被原生 Gemini 出站消费（ToGemini 读取侧
// 认消息级 + tool_calls 级两处），OpenAI / gemini-openai 上游必须一并剥离。
func TestStripToolCallThoughtSignaturesForOpenAIUpstream(t *testing.T) {
	req := map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{
				"role": "assistant", "content": "x", "thought_signature": "SIG",
				"tool_calls": []any{map[string]any{
					"id": "call_1", "type": "function", "thought_signature": "SIG_TC",
					"function": map[string]any{"name": "f", "arguments": "{}"},
				}},
			},
		},
	}
	p := types.Provider{ID: "p", Name: "oai", Type: types.ProviderOpenAI, BaseURL: "https://api.openai.com", APIKey: "k"}
	ob := BuildOutboundRequest(p, req, "")
	body, ok := ob.Body.(map[string]any)
	if !ok {
		t.Fatal("body not a map")
	}
	for _, m := range sliceOrEmpty(body["messages"]) {
		mm, _ := m.(map[string]any)
		if mm == nil {
			continue
		}
		if _, has := mm["thought_signature"]; has {
			t.Fatal("消息级 thought_signature 必须剥离")
		}
		for _, tc := range sliceOrEmpty(mm["tool_calls"]) {
			tcm, _ := tc.(map[string]any)
			if tcm == nil {
				continue
			}
			if _, has := tcm["thought_signature"]; has {
				t.Fatal("tool_calls 上的 thought_signature 必须剥离")
			}
		}
	}
}

// executableCode / codeExecutionResult 跨型渲染：保留代码与输出内容本身，
// 非 OK 结局保留结局标记。
func TestCodeExecutionPartsRendered(t *testing.T) {
	geminiReq := map[string]any{
		"model": "gemini-3-flash",
		"contents": []any{
			map[string]any{"role": "model", "parts": []any{
				map[string]any{"executableCode": map[string]any{"language": "PYTHON", "code": "print(1)"}},
				map[string]any{"codeExecutionResult": map[string]any{"outcome": "OUTCOME_OK", "output": "1"}},
				map[string]any{"codeExecutionResult": map[string]any{"outcome": "OUTCOME_FAILED"}},
			}},
		},
	}
	norm := OpenaiFromGemini(geminiReq)
	msgs := sliceOrEmpty(norm["messages"])
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	amsg, _ := msgs[0].(map[string]any)
	parts, _ := amsg["content"].([]any)
	var joined strings.Builder
	for _, p := range parts {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if txt, ok := pm["text"].(string); ok {
			joined.WriteString(txt)
			joined.WriteString("\n")
		}
	}
	text := joined.String()
	for _, want := range []string{"```python", "print(1)", "1", "OUTCOME_FAILED"} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered text missing %q, got:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[executableCode]") {
		t.Fatalf("placeholder should be gone, got:\n%s", text)
	}
}

// 单签名 + text/functionCall 并存：签名只挂在 functionCall 上时，绝不能按
// 「text 在前」的固定顺序盲配到 text part（那会让 functionCall 丢签名，
// 上游报 MISSING_THOUGHT_SIGNATURE）。此前的扁平队列 + 定序 zip 正是这个缺陷。
func TestThoughtSignatureSingleOnFunctionCallWithText(t *testing.T) {
	geminiReq := map[string]any{
		"model": "gemini-3-flash",
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}},
			map[string]any{"role": "model", "parts": []any{
				map[string]any{"text": "let me call"},
				map[string]any{"functionCall": map[string]any{"name": "f", "args": map[string]any{}}, "thoughtSignature": "SIG_FC"},
			}},
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "go"}}},
		},
	}
	norm := OpenaiFromGemini(geminiReq)
	out := ToGemini(norm)
	contents := sliceOrEmpty(out["contents"])
	if len(contents) != 3 {
		t.Fatalf("contents len = %d, want 3", len(contents))
	}
	modelContent, _ := contents[1].(map[string]any)
	if modelContent == nil {
		t.Fatal("contents[1] missing")
	}
	var textSig, fcSig string
	for _, p := range sliceOrEmpty(modelContent["parts"]) {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if _, isText := pm["text"]; isText {
			textSig, _ = pm["thoughtSignature"].(string)
		}
		if _, isFC := pm["functionCall"]; isFC {
			fcSig, _ = pm["thoughtSignature"].(string)
		}
	}
	if fcSig != "SIG_FC" {
		t.Fatalf("functionCall thoughtSignature = %q, want SIG_FC", fcSig)
	}
	if textSig != "" {
		t.Fatalf("text part 不应承载签名（会与 functionCall 错配），got %q", textSig)
	}
}
