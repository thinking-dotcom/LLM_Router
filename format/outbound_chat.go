package format

import (
	"encoding/json"
	"fmt"
	"strings"

	"llm-router-go/types"
)

// ChatToResponses converts an OpenAI chat request to OpenAI Responses format
func ChatToResponses(openaiReq map[string]any) map[string]any {
	instructions := []string{}
	items := []any{}
	for _, m := range sliceOrEmpty(openaiReq["messages"]) {
		mm, _ := m.(map[string]any)
		if mm == nil {
			continue
		}
		role, _ := mm["role"].(string)
		content := mm["content"]
		if role == "system" {
			if s, ok := content.(string); ok {
				instructions = append(instructions, s)
			} else {
				instructions = append(instructions, partsToText(content))
			}
			continue
		}
		if role == "tool" {
			cid, _ := mm["tool_call_id"].(string)
			if cid == "" {
				cid = randID("call_")
			}
			c := content
			output := ""
			if c == nil {
				output = ""
			} else if s, ok := c.(string); ok {
				output = s
			} else {
				b, _ := json.Marshal(c)
				output = string(b)
			}
			items = append(items, map[string]any{"type": "function_call_output", "call_id": cid, "output": output})
			continue
		}
		parts := []any{}
		if s, ok := content.(string); ok {
			if s != "" {
				parts = append(parts, map[string]any{"type": "input_text", "text": s})
			}
		} else if arr, ok := content.([]any); ok {
			for _, p := range arr {
				pm, _ := p.(map[string]any)
				if pm == nil {
					continue
				}
				pt, _ := pm["type"].(string)
				if pt == "text" {
					parts = append(parts, map[string]any{"type": "input_text", "text": pm["text"]})
				} else if pt == "image_url" {
					if iu, ok := pm["image_url"].(map[string]any); ok {
						parts = append(parts, map[string]any{"type": "input_image", "image_url": iu["url"]})
					}
				}
			}
		}
		itemRole := role
		hasToolCalls := len(sliceOrEmpty(mm["tool_calls"])) > 0
		// 跳过空 content 消息（Responses API 拒绝空 content）。但 assistant 发起
		// 工具调用时 content 通常为 null，此时 tool_calls 仍有内容，必须保留转换，
		// 否则后续 tool 角色消息会引用上游从未见过的 call_id，导致会话损坏。
		if len(parts) == 0 && !hasToolCalls {
			continue
		}
		// 仅有工具调用、content 为空时只输出 function_call item，不再携带空 content
		// 的 message（Responses API 拒绝空 content，会破坏多轮工具会话）。
		if len(parts) > 0 {
			items = append(items, map[string]any{"type": "message", "role": itemRole, "content": parts})
		}
		for _, tc := range sliceOrEmpty(mm["tool_calls"]) {
			tcm, _ := tc.(map[string]any)
			if tcm == nil {
				continue
			}
			fn, _ := tcm["function"].(map[string]any)
			cid, _ := tcm["id"].(string)
			if cid == "" {
				cid = randID("call_")
			}
			name := ""
			args := "{}"
			if fn != nil {
				name, _ = fn["name"].(string)
				if a, ok := fn["arguments"].(string); ok {
					args = a
				}
			}
			items = append(items, map[string]any{"type": "function_call", "call_id": cid, "name": name, "arguments": args})
		}
	}
	req := map[string]any{"model": openaiReq["model"], "input": items}
	if len(instructions) > 0 {
		req["instructions"] = strings.Join(instructions, "\n")
	}
	if v, ok := numVal(openaiReq, "max_tokens", "max_completion_tokens"); ok {
		req["max_output_tokens"] = v
	}
	if v, ok := openaiReq["temperature"]; ok {
		req["temperature"] = v
	}
	if v, ok := openaiReq["top_p"]; ok {
		req["top_p"] = v
	}
	// 推理力度：优先还原客户端显式的完整 reasoning 配置（IR 扩展键，跨型往返
	// 保真），否则用统一档位（reasoning_effort）→ Responses API 的 reasoning.effort。
	if v, ok := types.IRGet(openaiReq, types.ExtResponsesReasoning); ok {
		if rm, ok := v.(map[string]any); ok && len(rm) > 0 {
			req["reasoning"] = rm
		} else if eff, ok := openaiReq["reasoning_effort"].(string); ok && eff != "" && eff != "none" {
			req["reasoning"] = map[string]any{"effort": eff}
		}
	} else if eff, ok := openaiReq["reasoning_effort"].(string); ok && eff != "" && eff != "none" {
		req["reasoning"] = map[string]any{"effort": eff}
	}
	// text.format（结构化输出 json_schema/json_object）与 prompt_cache_options 还原
	if v, ok := types.IRGet(openaiReq, types.ExtResponsesTextFormat); ok {
		txt := map[string]any{"format": v}
		if tv, ok := openaiReq["text"]; ok { // 保留已有 text 配置（verbosity 等）
			if tm, ok := tv.(map[string]any); ok {
				for k, vv := range tm {
					txt[k] = vv
				}
			}
		}
		req["text"] = txt
	}
	if v, ok := types.IRGet(openaiReq, types.ExtResponsesPromptCacheOptions); ok {
		req["prompt_cache_options"] = v
	}
	// 2026-09 系列新增参数：Chat 与 Responses 共用的会话/缓存/服务层级字段直通。
	// 注意 store 语义不同（Responses 默认 true、Chat 默认 false）——显式传入才透传。
	if v, ok := openaiReq["store"]; ok {
		req["store"] = v
	}
	if v, ok := openaiReq["service_tier"]; ok {
		req["service_tier"] = v
	}
	if v, ok := openaiReq["metadata"]; ok {
		req["metadata"] = v
	}
	if v, ok := openaiReq["safety_identifier"]; ok {
		req["safety_identifier"] = v
	}
	if v, ok := openaiReq["prompt_cache_key"]; ok {
		req["prompt_cache_key"] = v
	}
	if v, ok := openaiReq["parallel_tool_calls"].(bool); ok {
		req["parallel_tool_calls"] = v
	}
	// max_tool_calls（Responses 内置工具调用上限）：Chat 无此参数，客户端显式
	// 携带（如 via Responses 接入再路由）时原样透传。
	if v, ok := openaiReq["max_tool_calls"]; ok {
		req["max_tool_calls"] = v
	}
	if v, ok := openaiReq["stop"]; ok {
		req["stop"] = strList(v)
	}
	if tools, ok := openaiReq["tools"].([]any); ok {
		rt := []any{}
		for _, t := range tools {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			fn, _ := tm["function"].(map[string]any)
			if fn == nil {
				continue
			}
			rt = append(rt, map[string]any{"type": "function", "name": fn["name"], "description": fn["description"], "parameters": fn["parameters"]})
		}
		if len(rt) > 0 {
			req["tools"] = rt
		}
	} else if fns, ok := openaiReq["functions"].([]any); ok && len(fns) > 0 {
		rt := []any{}
		for _, f := range fns {
			fm, _ := f.(map[string]any)
			if fm == nil {
				continue
			}
			rt = append(rt, map[string]any{"type": "function", "name": fm["name"], "description": fm["description"], "parameters": fm["parameters"]})
		}
		if len(rt) > 0 {
			req["tools"] = rt
		}
	}
	if tc, ok := openaiReq["tool_choice"]; ok {
		if tcm, ok := tc.(map[string]any); ok && tcm["type"] == "function" {
			if fn, ok := tcm["function"].(map[string]any); ok {
				if name, ok := fn["name"].(string); ok {
					req["tool_choice"] = map[string]any{"type": "function", "name": name}
				}
			}
		} else {
			req["tool_choice"] = tc
		}
	} else if fc, ok := openaiReq["function_call"]; ok {
		if s, ok := fc.(string); ok && s == "none" {
			req["tool_choice"] = "none"
		} else if m, ok := fc.(map[string]any); ok {
			if name, ok := m["name"].(string); ok && name != "" {
				req["tool_choice"] = map[string]any{"type": "function", "name": name}
			}
		}
	}
	if stream, _ := openaiReq["stream"].(bool); stream {
		req["stream"] = true
	}
	return req
}

func numVal(m map[string]any, keys ...string) (int, bool) {
	for _, k := range keys {
		if v, ok := m[k].(float64); ok {
			return int(v), true
		}
	}
	return 0, false
}

func strList(v any) []string {
	var out []string
	for _, item := range toList(v) {
		if s, ok := item.(string); ok {
			out = append(out, s)
		} else {
			out = append(out, fmt.Sprint(item))
		}
	}
	return out
}
