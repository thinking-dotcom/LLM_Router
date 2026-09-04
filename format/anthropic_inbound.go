package format

import (
	"encoding/json"
	"strings"

	"llm-router-go/types"
)

// OpenaiFromAnthropic converts an Anthropic request body to OpenAI chat shape
func OpenaiFromAnthropic(body any) map[string]any {
	m, _ := body.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	stream, _ := m["stream"].(bool)
	out := map[string]any{"model": strOr(m["model"], ""), "messages": []any{}, "stream": stream}

	var sys []string
	switch s := m["system"].(type) {
	case string:
		sys = append(sys, s)
	case []any:
		for _, item := range s {
			if str, ok := item.(string); ok {
				sys = append(sys, str)
			} else if mm, ok := item.(map[string]any); ok {
				if t, ok := mm["text"].(string); ok {
					sys = append(sys, t)
				}
			}
		}
	}
	if len(sys) > 0 {
		out["messages"] = append(out["messages"].([]any), map[string]any{"role": "system", "content": strings.Join(sys, "\n")})
	}

	messages, _ := m["messages"].([]any)
	for _, msg := range messages {
		mm, _ := msg.(map[string]any)
		if mm == nil {
			continue
		}
		role, _ := mm["role"].(string)
		if role == "system" {
			out["messages"] = append(out["messages"].([]any), map[string]any{"role": "system", "content": partsToText(mm["content"])})
			continue
		}
		if role == "user" {
			content := mm["content"]
			if arr, ok := content.([]any); ok {
				parts, trs := buildAnthropicUserParts(arr)
				// tool_result 必须先于本消息的纯文本：OpenAI 要求 tool 消息紧跟
				// 上一个携带 tool_calls 的 assistant 消息，中间插 user 会导致严格
				// 上游拒绝（tool 必须接在 tool_calls 之后）。
				if len(trs) > 0 {
					out["messages"] = append(out["messages"].([]any), trs...)
				}
				if len(parts) > 0 {
					out["messages"] = append(out["messages"].([]any), map[string]any{"role": "user", "content": parts})
				}
			} else {
				out["messages"] = append(out["messages"].([]any), map[string]any{"role": "user", "content": content})
			}
			continue
		}
		if role == "assistant" {
			content := mm["content"]
			if arr, ok := content.([]any); ok {
				textParts, toolCalls := buildAnthropicAssistantParts(arr)
				amsg := map[string]any{"role": "assistant"}
				if len(textParts) > 0 {
					amsg["content"] = textParts
				}
				if len(toolCalls) > 0 {
					amsg["tool_calls"] = toolCalls
				}
				out["messages"] = append(out["messages"].([]any), amsg)
			} else {
				out["messages"] = append(out["messages"].([]any), map[string]any{"role": "assistant", "content": content})
			}
		}
	}

	if tools, ok := m["tools"].([]any); ok {
		openaiTools := []any{}
		serverTools := []any{}
		for _, t := range tools {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			// Anthropic 客户端工具 type 为 "custom"（或缺省）。web_search /
			// web_fetch / code_execution / bash 等服务端工具由 Anthropic 侧执行，
			// 若误转成普通 function 工具，模型会发出 tool_use 要求客户端执行
			// 不存在的工具，会话就此挂死。服务端工具原样进 IR 扩展键：
			// Anthropic 出站还原，其它出站自然丢弃（stripClientExtensions）。
			if tt, _ := tm["type"].(string); tt != "" && tt != "custom" {
				serverTools = append(serverTools, tm)
				continue
			}
			openaiTools = append(openaiTools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": tm["name"], "description": tm["description"], "parameters": tm["input_schema"],
				},
			})
		}
		if len(openaiTools) > 0 {
			out["tools"] = openaiTools
		}
		if len(serverTools) > 0 {
			types.IRSet(out, types.ExtAnthropicServerTools, serverTools)
		}
	}

	if tc, ok := m["tool_choice"].(map[string]any); ok {
		if tct, _ := tc["type"].(string); tct == "tool" {
			out["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": tc["name"]}}
		} else if tct == "none" {
			out["tool_choice"] = "none"
		} else if tct == "any" {
			// Anthropic 的 any = 强制调用工具，OpenAI 对应 required
			out["tool_choice"] = "required"
		} else {
			out["tool_choice"] = "auto"
		}
	}

	copyFloat(m, out, "max_tokens")
	copyFloat(m, out, "temperature")
	copyFloat(m, out, "top_p")
	copyFloat(m, out, "top_k")
	if _, ok := out["max_tokens"]; !ok {
		if v, ok := m["max_completion_tokens"].(float64); ok {
			out["max_tokens"] = v
		}
	}
	if ss, ok := m["stop_sequences"].([]any); ok {
		out["stop"] = ss
	}
	if v, ok := m["metadata"]; ok {
		out["metadata"] = v
	}
	// 保留 thinking 配置（Anthropic extended thinking）：写入 IR 扩展键，
	// 跨型时由出站按需还原（Anthropic 出站还原回 thinking，其它格式忽略
	// 但不再作为未知顶层字段误传给上游）。
	if v, ok := m["thinking"]; ok {
		types.IRSet(out, types.ExtAnthropicThinking, v)
	}
	// 顶层 cache_control（提示缓存断点）同样进扩展键保真。
	if v, ok := m["cache_control"]; ok {
		types.IRSet(out, types.ExtAnthropicCacheCtrl, v)
	}
	// 2026-09 系列新顶层参数：output_config（effort/format 结构化输出）、
	// inference_geo（推理地域）、service_tier（服务层级），进 IR 供跨型还原。
	if v, ok := m["output_config"]; ok {
		types.IRSet(out, types.ExtAnthropicOutputConfig, v)
	}
	if v, ok := m["inference_geo"]; ok {
		types.IRSet(out, types.ExtAnthropicInferenceGeo, v)
	}
	if v, ok := m["service_tier"]; ok {
		types.IRSet(out, types.ExtAnthropicServiceTier, v)
	}
	return out
}

func buildAnthropicUserParts(arr []any) ([]any, []any) {
	parts := []any{}
	toolResults := []any{}
	for _, block := range arr {
		b, _ := block.(map[string]any)
		if b == nil {
			parts = append(parts, block)
			continue
		}
		bt, _ := b["type"].(string)
		switch bt {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": b["text"]})
		case "image":
			if src, ok := b["source"].(map[string]any); ok {
				switch st, _ := src["type"].(string); st {
				case "base64":
					sd, _ := src["data"].(string)
					mt, _ := src["media_type"].(string)
					if mt == "" {
						mt = "image/png"
					}
					parts = append(parts, map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "data:" + mt + ";base64," + sd},
					})
				case "url":
					if u, _ := src["url"].(string); u != "" {
						parts = append(parts, map[string]any{
							"type":      "image_url",
							"image_url": map[string]any{"url": u},
						})
					}
				}
			}
		case "document":
			// Anthropic document 块（含 PDF），转为 OpenAI 侧可透传的 document 类型
			if src, ok := b["source"].(map[string]any); ok {
				parts = append(parts, map[string]any{"type": "document", "source": src})
			} else {
				parts = append(parts, b)
			}
		case "thinking":
			// extended thinking 块，透传保留
			parts = append(parts, b)
		case "tool_result":
			tuid, _ := b["tool_use_id"].(string)
			if tuid == "" {
				tuid = randID("call_")
			}
			c := b["content"]
			content := ""
			if s, ok := c.(string); ok {
				content = s
			} else {
				// 保留图片等富内容，partsToText 会丢弃非 text，这里尽量 JSON 化
				if arr, ok := c.([]any); ok {
					hasNonText := false
					for _, it := range arr {
						if mm, ok := it.(map[string]any); ok {
							if t, _ := mm["type"].(string); t != "text" {
								hasNonText = true
								break
							}
						}
					}
					if hasNonText {
						bb, _ := json.Marshal(c)
						content = string(bb)
					} else {
						content = partsToText(c)
					}
				} else {
					content = partsToText(c)
				}
			}
			// is_error 标志在 OpenAI tool 消息里没有对应字段，降级为文本前缀，
			// 否则模型无法得知工具失败，可能基于错误结果继续推理
			if isErr, _ := b["is_error"].(bool); isErr {
				content = "Error: " + content
			}
			toolResults = append(toolResults, map[string]any{
				"role": "tool", "tool_call_id": tuid, "content": content,
			})
		default:
			parts = append(parts, b)
		}
	}
	return parts, toolResults
}

func buildAnthropicAssistantParts(arr []any) ([]any, []any) {
	parts := []any{}
	toolCalls := []any{}
	for _, block := range arr {
		b, _ := block.(map[string]any)
		if b == nil {
			parts = append(parts, block)
			continue
		}
		bt, _ := b["type"].(string)
		if bt == "text" {
			parts = append(parts, map[string]any{"type": "text", "text": b["text"]})
		} else if bt == "thinking" {
			parts = append(parts, b)
		} else if bt == "tool_use" {
			id, _ := b["id"].(string)
			if id == "" {
				id = randID("call_")
			}
			input, _ := b["input"].(map[string]any)
			if input == nil {
				input = map[string]any{}
			}
			args, _ := json.Marshal(input)
			toolCalls = append(toolCalls, map[string]any{
				"id": id, "type": "function",
				"function": map[string]any{"name": b["name"], "arguments": string(args)},
			})
		} else {
			parts = append(parts, b)
		}
	}
	return parts, toolCalls
}
