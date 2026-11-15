package format

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"llm-router-go/types"
)

// numInt 兼容读取 map 中 float64 / int 两种数值类型（Go 内存中构造的 usage
// 中间层是 int，JSON 解析出来的是 float64，两侧断言必须对称，否则统计静默归零）。
func numInt(m map[string]any, key string) (int, bool) {
	switch v := m[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

// isJSONModeRequest 判断归一化后的 OpenAI 请求是否声明了 response_format
// （json_object / json_schema）。请求侧降级为工具调用（json_output）时，
// 响应侧据此把 tool_use 的 input 还原回 OpenAI 的 content，而不是 tool_calls。
func isJSONModeRequest(openaiReq map[string]any) bool {
	rf, ok := openaiReq["response_format"].(map[string]any)
	if !ok {
		return false
	}
	switch t, _ := rf["type"].(string); t {
	case "json_object", "json_schema":
		return true
	}
	return false
}

// ResponsesToOpenai converts an OpenAI Responses API response body to OpenAI Chat format.
func ResponsesToOpenai(body map[string]any, openaiReq map[string]any) map[string]any {
	model := strOr(openaiReq["model"], "")
	out := map[string]any{
		"id":      strOr(body["id"], randID("resp_")),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   strOr(body["model"], model),
	}

	var textParts []string
	var toolCalls []any

	for _, item := range sliceOrEmpty(body["output"]) {
		im, _ := item.(map[string]any)
		if im == nil {
			continue
		}
		it, _ := im["type"].(string)
		if it == "message" {
			for _, c := range sliceOrEmpty(im["content"]) {
				cm, _ := c.(map[string]any)
				if cm == nil {
					continue
				}
				if ct, _ := cm["type"].(string); ct == "output_text" {
					if txt, ok := cm["text"].(string); ok {
						textParts = append(textParts, txt)
					}
				}
			}
		} else if it == "function_call" {
			cid, _ := im["call_id"].(string)
			if cid == "" {
				cid = randID("call_")
			}
			name, _ := im["name"].(string)
			args, _ := im["arguments"].(string)
			toolCalls = append(toolCalls, map[string]any{
				"id":   cid,
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": args,
				},
			})
		}
	}

	msg := map[string]any{"role": "assistant"}
	if len(textParts) > 0 {
		msg["content"] = strings.Join(textParts, "")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		// 含 function_call 输出时必须返回 tool_calls，否则 agent 类客户端
		// 不会进入工具执行循环（OpenAI 语义）；与流式 finishReasonFor 一致
		finishReason = "tool_calls"
	}
	status, _ := body["status"].(string)
	// "incomplete" 才映射为 length/content_filter；"failed" 是真正的请求失败，
	// 不应伪装成 content_filter，否则会误导调用方的重试/降级策略。
	// 已有工具调用时保持 tool_calls（调用已完整发出），与流式 finishReasonFor 一致
	if status == "incomplete" && len(toolCalls) == 0 {
		if details, ok := body["incomplete_details"].(map[string]any); ok {
			reason, _ := details["reason"].(string)
			if reason == "max_output_tokens" {
				finishReason = "length"
			} else {
				finishReason = "content_filter"
			}
		} else {
			finishReason = "content_filter"
		}
	}

	choice := map[string]any{
		"index":         0,
		"message":       msg,
		"finish_reason": finishReason,
	}
	out["choices"] = []any{choice}

	// Usage
	if usage, ok := body["usage"].(map[string]any); ok {
		u := map[string]any{}
		if v, ok := numInt(usage, "input_tokens"); ok {
			u["prompt_tokens"] = v
		}
		if v, ok := numInt(usage, "output_tokens"); ok {
			u["completion_tokens"] = v
		}
		// usage 明细：缓存命中 / 推理 token，统计与计费依赖这些字段
		if inDetails, ok := usage["input_tokens_details"].(map[string]any); ok {
			if v, ok2 := inDetails["cached_tokens"].(float64); ok2 && v > 0 {
				u["prompt_tokens_details"] = map[string]any{"cached_tokens": int(v)}
			}
		}
		if outDetails, ok := usage["output_tokens_details"].(map[string]any); ok {
			if v, ok2 := outDetails["reasoning_tokens"].(float64); ok2 && v > 0 {
				u["completion_tokens_details"] = map[string]any{"reasoning_tokens": int(v)}
			}
		}
		if pt, ptOk := u["prompt_tokens"]; ptOk {
			if ct, ctOk := u["completion_tokens"]; ctOk {
				u["total_tokens"] = pt.(int) + ct.(int)
			}
		}
		if len(u) > 0 {
			out["usage"] = u
		}
	}

	return out
}

// AnthropicToOpenai converts an Anthropic Messages response body to OpenAI Chat format.
func AnthropicToOpenai(body map[string]any, openaiReq map[string]any) map[string]any {
	model := strOr(openaiReq["model"], "")
	jsonMode := isJSONModeRequest(openaiReq)
	out := map[string]any{
		"id":      strOr(body["id"], randID("msg_")),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   strOr(body["model"], model),
	}

	var textParts []string
	var toolCalls []any
	var thinkingBlocks []any // thinking / redacted_thinking 块（明文保真，跨型可还原）
	var citations []any      // 首个 text 块的 citations 引用

	for _, block := range sliceOrEmpty(body["content"]) {
		bm, _ := block.(map[string]any)
		if bm == nil {
			continue
		}
		bt, _ := bm["type"].(string)
		if bt == "text" {
			if txt, ok := bm["text"].(string); ok {
				textParts = append(textParts, txt)
			}
			if len(citations) == 0 {
				if c, ok := bm["citations"].([]any); ok {
					citations = c
				}
			}
		} else if bt == "thinking" || bt == "redacted_thinking" {
			thinkingBlocks = append(thinkingBlocks, bm)
		} else if bt == "tool_use" {
			name, _ := bm["name"].(string)
			// response_format 降级：Anthropic 把 JSON 输出伪装成工具调用，
			// 还原为 OpenAI 的 content 字符串（JSON），而不是 tool_calls。
			if jsonMode && name == jsonModeToolName {
				input, _ := bm["input"].(map[string]any)
				if input == nil {
					input = map[string]any{}
				}
				b, _ := json.Marshal(input)
				textParts = append(textParts, string(b))
				continue
			}
			id, _ := bm["id"].(string)
			if id == "" {
				id = randID("call_")
			}
			input, _ := bm["input"].(map[string]any)
			if input == nil {
				input = map[string]any{}
			}
			argsB, _ := json.Marshal(input)
			toolCalls = append(toolCalls, map[string]any{
				"id":   id,
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": string(argsB),
				},
			})
		}
	}

	stopReason, _ := body["stop_reason"].(string)
	finishReason := mapStopReason(stopReason)
	// JSON 降级模式下若无真实 tool_calls，工具触发的 tool_use 终止原因
	// 应还原为 stop，避免客户端误判为需要继续调工具。
	if finishReason == "tool_calls" && jsonMode && len(toolCalls) == 0 {
		finishReason = "stop"
	}

	msg := map[string]any{"role": "assistant"}
	if len(textParts) > 0 {
		msg["content"] = strings.Join(textParts, "")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	// stop_details（2026-09 新增拒绝结构化信息）：Anthropic 拒绝时把可读说明
	// 挂到 OpenAI 的 message.refusal 上（refusal finish_reason 场景）。
	if sd, ok := body["stop_details"].(map[string]any); ok {
		if expl, _ := sd["explanation"].(string); expl != "" {
			msg["refusal"] = expl
		}
	}

	choice := map[string]any{
		"index":         0,
		"message":       msg,
		"finish_reason": finishReason,
	}
	out["choices"] = []any{choice}

	// IR 响应扩展键：思考块 / citations / 命中的停止序列 保真（跨型还原，
	// OpenAI 客户端不识别但不再静默丢弃，供日志与反向转换使用）
	if len(thinkingBlocks) > 0 {
		types.IRSet(out, types.ExtAnthropicThinkingBlock, thinkingBlocks)
	}
	if len(citations) > 0 {
		types.IRSet(out, types.ExtAnthropicCitations, citations)
	}
	if ss, ok := body["stop_sequence"].(string); ok && ss != "" {
		types.IRSet(out, types.ExtAnthropicStopSeq, ss)
	}

	// Usage
	if usage, ok := body["usage"].(map[string]any); ok {
		u := map[string]any{}
		if v, ok := numInt(usage, "input_tokens"); ok {
			u["prompt_tokens"] = v
		}
		if v, ok := numInt(usage, "output_tokens"); ok {
			u["completion_tokens"] = v
		}
		if v, ok := usage["cache_read_input_tokens"].(float64); ok && v > 0 {
			u["prompt_tokens_details"] = map[string]any{"cached_tokens": int(v)}
		}
		// output_tokens_details.thinking_tokens（2026-09：输出按类别细分，
		// 思考 token 即 OpenAI 的 reasoning_tokens）
		if od, ok := usage["output_tokens_details"].(map[string]any); ok {
			if v, ok := od["thinking_tokens"].(float64); ok && v > 0 {
				u["completion_tokens_details"] = map[string]any{"reasoning_tokens": int(v)}
			}
		}
		if pt, ptOk := u["prompt_tokens"]; ptOk {
			if ct, ctOk := u["completion_tokens"]; ctOk {
				u["total_tokens"] = pt.(int) + ct.(int)
			}
		}
		if len(u) > 0 {
			out["usage"] = u
		}
	}

	return out
}

// GeminiToOpenai converts a Gemini generateContent response body to OpenAI Chat format.
func GeminiToOpenai(body map[string]any, openaiReq map[string]any) map[string]any {
	// Interactions API 响应（2026-06 起 GA）：以 steps 时间线组织而非 candidates，
	// 单独解析（含 {interaction: ...} 包装）。
	if body != nil {
		if _, has := body["steps"]; has {
			return GeminiInteractionsToOpenai(body, openaiReq)
		}
		if inner, ok := body["interaction"].(map[string]any); ok {
			if _, has := inner["steps"]; has {
				return GeminiInteractionsToOpenai(body, openaiReq)
			}
		}
	}
	model := strOr(openaiReq["model"], "")
	out := map[string]any{
		"id":      randID("chatcmpl-"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
	}

	var textParts []string
	var toolCalls []any
	var thoughtSigs []any // thoughtSignature（Gemini 3 思维签名，随消息透传）

	candidates := sliceOrEmpty(body["candidates"])
	if len(candidates) > 0 {
		c0, _ := candidates[0].(map[string]any)
		if c0 != nil {
			if content, ok := c0["content"].(map[string]any); ok {
				for _, p := range sliceOrEmpty(content["parts"]) {
					pm, _ := p.(map[string]any)
					if pm == nil {
						continue
					}
					// thoughtSignature（Gemini 3 思维签名）：随消息透传给客户端，
					// 客户端回传历史时由 ToGemini 回填（仅原生 Gemini 上游）
					if sig, ok := pm["thoughtSignature"].(string); ok && sig != "" {
						thoughtSigs = append(thoughtSigs, sig)
					}
					if txt, ok := pm["text"].(string); ok {
						textParts = append(textParts, txt)
					} else if fc, ok := pm["functionCall"].(map[string]any); ok {
						name, _ := fc["name"].(string)
						args, _ := fc["args"].(map[string]any)
						if args == nil {
							args = map[string]any{}
						}
						argsB, _ := json.Marshal(args)
						toolCalls = append(toolCalls, map[string]any{
							"id":   randID("call_"),
							"type": "function",
							"function": map[string]any{
								"name":      name,
								"arguments": string(argsB),
							},
						})
					}
				}
			}

			finishReason := mapGeminiFinish(strOr(c0["finishReason"], ""))
			// Gemini 函数调用轮次以 finishReason=STOP 收尾，而 OpenAI 约定
			// 带 tool_calls 的回复 finish_reason 必须是 "tool_calls"：
			// 客户端据此才进入工具执行循环。
			if len(toolCalls) > 0 {
				finishReason = "tool_calls"
			}

			msg := map[string]any{"role": "assistant"}
			if len(textParts) > 0 {
				msg["content"] = strings.Join(textParts, "")
			}
			if len(toolCalls) > 0 {
				msg["tool_calls"] = toolCalls
			}
			if len(thoughtSigs) == 1 {
				msg["thought_signature"] = thoughtSigs[0]
			} else if len(thoughtSigs) > 1 {
				msg["thought_signature"] = thoughtSigs
			}

			choice := map[string]any{
				"index":         0,
				"message":       msg,
				"finish_reason": finishReason,
			}
			out["choices"] = []any{choice}
		}
	}

	if _, hasChoices := out["choices"]; !hasChoices {
		// 无 candidates（如内容被安全策略拦截）：映射为 content_filter，避免伪装成正常停止
		fr := "stop"
		if pf, ok := body["promptFeedback"].(map[string]any); ok {
			if br, ok := pf["blockReason"].(string); ok && br != "" {
				fr = "content_filter"
			}
		}
		out["choices"] = []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role": "assistant",
				},
				"finish_reason": fr,
			},
		}
	}

	// Usage
	if um, ok := body["usageMetadata"].(map[string]any); ok {
		u := map[string]any{}
		if v, ok := numInt(um, "promptTokenCount"); ok {
			u["prompt_tokens"] = v
		}
		if v, ok := numInt(um, "candidatesTokenCount"); ok {
			u["completion_tokens"] = v
		}
		// usage 明细：缓存命中 / 思考 token
		if v, ok := um["cachedContentTokenCount"].(float64); ok && v > 0 {
			u["prompt_tokens_details"] = map[string]any{"cached_tokens": int(v)}
		}
		if v, ok := um["thoughtsTokenCount"].(float64); ok && v > 0 {
			u["completion_tokens_details"] = map[string]any{"reasoning_tokens": int(v)}
		}
		if pt, ptOk := u["prompt_tokens"]; ptOk {
			if ct, ctOk := u["completion_tokens"]; ctOk {
				u["total_tokens"] = pt.(int) + ct.(int)
			}
		}
		if len(u) > 0 {
			out["usage"] = u
		}
	}

	return out
}

// OpenaiFromUpstream dispatches non-streaming response translation by provider type.
func OpenaiFromUpstream(ptype string, data map[string]any, openaiReq map[string]any) map[string]any {
	switch ptype {
	case "response":
		return ResponsesToOpenai(data, openaiReq)
	case "anthropic":
		return AnthropicToOpenai(data, openaiReq)
	case "gemini":
		return GeminiToOpenai(data, openaiReq)
	default:
		// Already in OpenAI shape (openai provider)
		return data
	}
}

// OpenaiToAnthropicBody converts an OpenAI Chat response shape to an Anthropic Messages response body.
func OpenaiToAnthropicBody(openaiResp map[string]any, model string) map[string]any {
	out := map[string]any{
		"id":    strOr(openaiResp["id"], randID("msg_")),
		"type":  "message",
		"role":  "assistant",
		"model": model,
	}

	var blocks []any
	// 还原思考块（IR 响应扩展键）——Anthropic 规范中 thinking 位于 text 之前，
	// 多轮续接要求原样回传。
	if tv, ok := types.IRGet(openaiResp, types.ExtAnthropicThinkingBlock); ok {
		if tblocks, ok := tv.([]any); ok {
			blocks = append(blocks, tblocks...)
		}
	}

	choices := sliceOrEmpty(openaiResp["choices"])
	if len(choices) > 0 {
		c0, _ := choices[0].(map[string]any)
		msg, _ := c0["message"].(map[string]any)

		if msg != nil {
			// Text content
			if content := msg["content"]; content != nil {
				if s, ok := content.(string); ok && s != "" {
					tb := map[string]any{"type": "text", "text": s}
					attachIRCitations(tb, openaiResp)
					blocks = append(blocks, tb)
				} else if arr, ok := content.([]any); ok {
					cited := false
					for _, p := range arr {
						pm, _ := p.(map[string]any)
						if pm == nil {
							continue
						}
						if t, _ := pm["type"].(string); t == "text" {
							if txt, ok := pm["text"].(string); ok {
								tb := map[string]any{"type": "text", "text": txt}
								if !cited {
									attachIRCitations(tb, openaiResp)
									cited = true
								}
								blocks = append(blocks, tb)
							}
						}
					}
				}
			}

			// Tool calls
			for _, tc := range sliceOrEmpty(msg["tool_calls"]) {
				tcm, _ := tc.(map[string]any)
				if tcm == nil {
					continue
				}
				id, _ := tcm["id"].(string)
				if id == "" {
					id = randID("toolu_")
				}
				fn, _ := tcm["function"].(map[string]any)
				name := ""
				input := map[string]any{}
				if fn != nil {
					name, _ = fn["name"].(string)
					if args, ok := fn["arguments"].(string); ok {
						if err := json.Unmarshal([]byte(args), &input); err != nil {
							// 上游 arguments 非法 JSON（如长输出被截断）时不能静默
							// 变 {}：客户端会以空参数执行工具。保底携带原始字符串
							log.Printf("[format] 上游 tool arguments 非法 JSON，已降级保留原始串: %v | %.200s", err, args)
							input = map[string]any{"_raw": args}
						}
					} else if m, ok := fn["arguments"].(map[string]any); ok {
						// 部分上游已把 arguments 序列化为对象，直接透传而不是丢弃
						input = m
					}
				}
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    id,
					"name":  name,
					"input": input,
				})
			}

			// Stop reason
			if c0 != nil {
				fr, _ := c0["finish_reason"].(string)
				switch fr {
				case "length":
					out["stop_reason"] = "max_tokens"
				case "tool_calls":
					out["stop_reason"] = "tool_use"
				case "content_filter", "refusal":
					out["stop_reason"] = "refusal"
				default:
					out["stop_reason"] = "end_turn"
				}
			}
		}
	}

	// 命中的停止序列（Anthropic 特有，OpenAI finish_reason=stop 不含该信息）
	if ss, ok := types.IRGet(openaiResp, types.ExtAnthropicStopSeq); ok {
		if s, ok := ss.(string); ok && s != "" {
			out["stop_sequence"] = s
		}
	}

	// 空 choices 时 blocks 为 nil，序列化成 null 且缺 stop_reason 不符合
	// Anthropic 规范；统一保证 content 为数组并给出合理的默认停止原因
	if blocks == nil {
		blocks = []any{}
	}
	out["content"] = blocks
	if _, ok := out["stop_reason"]; !ok {
		out["stop_reason"] = "end_turn"
	}

	// Usage
	if usage, ok := openaiResp["usage"].(map[string]any); ok {
		u := map[string]any{}
		if v, ok := numInt(usage, "prompt_tokens"); ok {
			u["input_tokens"] = v
		}
		if v, ok := numInt(usage, "completion_tokens"); ok {
			u["output_tokens"] = v
		}
		if ptd, ok := usage["prompt_tokens_details"].(map[string]any); ok {
			if cv, ok := numInt(ptd, "cached_tokens"); ok && cv > 0 {
				u["cache_read_input_tokens"] = int(cv)
			}
		}
		if len(u) > 0 {
			out["usage"] = u
		}
	}

	return out
}

// attachIRCitations 若 openaiResp 携带 Anthropic citations 扩展键，则把引用挂到
// text 块上（Anthropic 客户端可原样消费）。
func attachIRCitations(block map[string]any, openaiResp map[string]any) {
	if cv, ok := types.IRGet(openaiResp, types.ExtAnthropicCitations); ok {
		if c, ok := cv.([]any); ok && len(c) > 0 {
			block["citations"] = c
		}
	}
}

// OpenaiToGeminiBody converts an OpenAI Chat response shape to a Gemini generateContent response body.
func OpenaiToGeminiBody(openaiResp map[string]any, model string) map[string]any {
	out := map[string]any{}

	parts := []any{}

	choices := sliceOrEmpty(openaiResp["choices"])
	if len(choices) > 0 {
		c0, _ := choices[0].(map[string]any)
		msg, _ := c0["message"].(map[string]any)

		if msg != nil {
			// Text content
			if content := msg["content"]; content != nil {
				if s, ok := content.(string); ok && s != "" {
					parts = append(parts, map[string]any{"text": s})
				} else if arr, ok := content.([]any); ok {
					for _, p := range arr {
						pm, _ := p.(map[string]any)
						if pm == nil {
							continue
						}
						if t, _ := pm["type"].(string); t == "text" {
							if txt, ok := pm["text"].(string); ok {
								parts = append(parts, map[string]any{"text": txt})
							}
						}
					}
				}
			}

			// Tool calls → functionCall parts
			for _, tc := range sliceOrEmpty(msg["tool_calls"]) {
				tcm, _ := tc.(map[string]any)
				if tcm == nil {
					continue
				}
				fn, _ := tcm["function"].(map[string]any)
				if fn == nil {
					continue
				}
				name, _ := fn["name"].(string)
				var argsMap map[string]any
				if m, ok := fn["arguments"].(map[string]any); ok {
					// 部分上游已把 arguments 序列化为对象，直接透传
					argsMap = m
				} else if s, ok := fn["arguments"].(string); ok {
					if v, ok := safeParse(s, nil).(map[string]any); ok {
						argsMap = v
					} else {
						log.Printf("[format] 上游 tool arguments 非法 JSON，参数已丢弃: %.200s", s)
					}
				}
				if argsMap == nil {
					argsMap = map[string]any{}
				}
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": name,
						"args": argsMap,
					},
				})
			}

			// Finish reason
			var finishReason string
			if c0 != nil {
				fr, _ := c0["finish_reason"].(string)
				switch fr {
				case "length":
					finishReason = "MAX_TOKENS"
				case "content_filter":
					finishReason = "SAFETY"
				default:
					finishReason = "STOP"
				}
			}

			candidate := map[string]any{
				"content": map[string]any{
					"role":  "model",
					"parts": parts,
				},
				"finishReason": finishReason,
			}
			out["candidates"] = []any{candidate}
		}
	}

	// Usage
	if usage, ok := openaiResp["usage"].(map[string]any); ok {
		um := map[string]any{}
		if v, ok := numInt(usage, "prompt_tokens"); ok {
			um["promptTokenCount"] = v
		}
		if v, ok := numInt(usage, "completion_tokens"); ok {
			um["candidatesTokenCount"] = v
		}
		if len(um) > 0 {
			out["usageMetadata"] = um
		}
	}

	// 无 candidates 兕底（choices 为空或 message 缺失，如内容被上游安全策略
	// 拦截后经 OpenAI 形状中转）：Gemini 客户端对缺 candidates 的
	// generateContent 响应会反序列化失败（对照同文件 GeminiToOpenai 的兜底）
	if _, has := out["candidates"]; !has {
		out["candidates"] = []any{
			map[string]any{
				"content":      map[string]any{"role": "model", "parts": []any{}},
				"finishReason": "SAFETY",
			},
		}
	}

	return out
}

// mapStopReason maps Anthropic stop_reason values to OpenAI finish_reason values.
// max_tokens→length, tool_use→tool_calls, refusal→content_filter,
// model_context_window_exceeded→length, pause_turn→stop, else→stop.
func mapStopReason(r string) string {
	switch r {
	case "max_tokens", "model_context_window_exceeded":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "refusal":
		return "content_filter"
	default: // end_turn / stop_sequence / pause_turn 等
		return "stop"
	}
}

// mapGeminiFinish maps Gemini finishReason values to OpenAI finish_reason values.
// MAX_TOKENS→length, SAFETY/RECITATION/PROHIBITED_CONTENT/IMAGE_SAFETY→content_filter,
// MALFORMED_FUNCTION_CALL→stop, STOP→stop, else→stop.
func mapGeminiFinish(fr string) string {
	switch fr {
	case "MAX_TOKENS":
		return "length"
	// BLOCKLIST（屏蔽列表）/ SPII（敏感个人信息）/ OTHER 等此前落入 default 被
	// 伪装成正常结束，客户端无法感知内容缺失，统一按内容拦截处理。
	case "SAFETY", "RECITATION", "PROHIBITED_CONTENT", "IMAGE_SAFETY",
		"BLOCKLIST", "SPII":
		return "content_filter"
	// MALFORMED_FUNCTION_CALL 时函数调用本身是畸形的（往往没有合法 functionCall
	// part），映射成 tool_calls 会让客户端尝试执行一个不存在的工具而会话挂死；
	// 映射为 stop 让已产出的文本正常交付。
	case "MALFORMED_FUNCTION_CALL":
		return "stop"
	case "STOP":
		return "stop"
	default:
		return "stop"
	}
}
