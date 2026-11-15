package format

import (
	"encoding/json"
	"strings"

	"llm-router-go/types"
)

// jsonModeToolName 是 response_format 降级为工具调用时注入的工具名。
// 与 LiteLLM 的约定一致（RESPONSE_FORMAT_TOOL_NAME = "json_output"）：
// Anthropic 原生不支持 response_format，把 JSON schema 伪装成单个强制
// 工具调用，响应侧再按此名字把 tool_use 的 input 还原为 content。
const jsonModeToolName = "json_output"

// ToAnthropic converts OpenAI request shape to Anthropic Messages format
func ToAnthropic(openaiReq map[string]any) map[string]any {
	system := []string{}
	messages := []any{}
	for _, m := range sliceOrEmpty(openaiReq["messages"]) {
		mm, _ := m.(map[string]any)
		if mm == nil {
			continue
		}
		role, _ := mm["role"].(string)
		if role == "system" {
			system = append(system, stringify(mm["content"]))
			continue
		}
		if role == "tool" {
			block := toolResultBlock(mm)
			// merge with previous user message if it already has tool results
			if len(messages) > 0 {
				last, _ := messages[len(messages)-1].(map[string]any)
				if lr, _ := last["role"].(string); lr == "user" {
					if lc, ok := last["content"].([]any); ok && hasToolResult(lc) {
						last["content"] = append(lc, block)
						continue
					}
				}
			}
			messages = append(messages, map[string]any{"role": "user", "content": []any{block}})
			continue
		}
		content := convertContent(mm["content"])
		if role == "assistant" && mm["tool_calls"] != nil {
			blocks := make([]any, len(content))
			copy(blocks, content)
			for _, tc := range sliceOrEmpty(mm["tool_calls"]) {
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
				args := map[string]any{}
				if fn != nil {
					name, _ = fn["name"].(string)
					if a, ok := fn["arguments"].(string); ok && a != "" {
						// 参数字符串损坏时保留原文而非静默变 {}，避免工具以空参数
						// 执行；与非流式侧工具参数处理保持一致
						if json.Unmarshal([]byte(a), &args) != nil {
							args = map[string]any{"_raw": a}
						}
					}
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": id, "name": name, "input": args})
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": blocks})
			continue
		}
		msgRole := "user"
		if role == "assistant" {
			msgRole = "assistant"
		}
		// 避免把空内容序列化成 "content": []（Anthropic 需要非空内容块数组），
		// 内容为空时直接省略 content 字段。
		msg := map[string]any{"role": msgRole}
		if len(content) > 0 {
			msg["content"] = content
		}
		// OpenAI 形态允许连续同角色消息（assistant(tool_calls) → tool → user 归一化
		// 后即连续两条 user），Anthropic Messages API 严格校验 user/assistant 严格
		// 交替，连续同角色会整请求 400：把当前消息合并进前一条同角色消息。
		if len(messages) > 0 {
			if last, _ := messages[len(messages)-1].(map[string]any); last != nil {
				if lr, _ := last["role"].(string); lr == msgRole {
					lc, _ := last["content"].([]any)
					cc, _ := msg["content"].([]any)
					merged := append(append([]any{}, lc...), cc...)
					if len(merged) > 0 {
						last["content"] = merged
					} else {
						delete(last, "content")
					}
					continue
				}
			}
		}
		messages = append(messages, msg)
	}

	req := map[string]any{"model": openaiReq["model"], "messages": messages}
	if mt, ok := numVal(openaiReq, "max_tokens", "max_completion_tokens"); ok {
		req["max_tokens"] = mt
	} else {
		// Anthropic API 必填 max_tokens（与 model/messages 并列），客户端不传时
		// 不补默认值会直接 400。8192 为常用安全值；thinking 预算分支会按
		// max_tokens > budget_tokens 约束覆盖此值。
		req["max_tokens"] = 8192
	}
	if len(system) > 0 {
		req["system"] = strings.Join(system, "\n")
	}
	copyMapVal(openaiReq, req, "temperature", "top_p", "top_k")
	if v, ok := openaiReq["stop"]; ok {
		req["stop_sequences"] = strList(v)
	}
	// thinking 透传（Anthropic extended thinking）。取值顺序：
	// 1) IR 扩展键中的显式 thinking（跨型往返保真）；
	// 2) 客户端为 Anthropic 家族显式配置的思考预算；
	// 3) 统一推理档位（如来自 Gemini thinkingConfig 的归一化结果）。
	if v, ok := types.IRGet(openaiReq, types.ExtAnthropicThinking); ok {
		req["thinking"] = v
	} else if v, ok := openaiReq["thinking"]; ok {
		// 兼容：OpenAI 形状直接携带 thinking 字段的客户端（网关/直连场景）
		req["thinking"] = v
	} else if b, ok := types.IRGet(openaiReq, types.ExtClientAnthropicBudget); ok {
		if bf := toF(b); bf > 0 {
			budget := int(bf)
			req["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
			// Anthropic 要求 max_tokens > budget_tokens
			if mt := intVal(openaiReq, "max_tokens", "max_completion_tokens"); mt <= budget {
				req["max_tokens"] = budget + 1024
			}
		}
	} else if eff, _ := openaiReq["reasoning_effort"].(string); eff != "" {
		// 统一档位 → Anthropic 原生参数。字段与取值、模型家族择形（Claude 4.7+/5
		// 与 Mythos/Fable 走 thinking adaptive + output_config.effort，enabled 已
		// 弃用；旧模型走 enabled + budget_tokens）全部来自映射表
		// format/reasoning_table.go，这里只负责落字段与协议约束。
		if p := EncodeReasoningNative(ReasoningFamilyAnthropic, strOr(openaiReq["model"], ""), eff); p != nil {
			for k, v := range p.Value {
				req[k] = v
			}
			// Anthropic 协议要求 max_tokens > budget_tokens（仅 enabled 家族带预算）
			if th, ok := req["thinking"].(map[string]any); ok {
				if b := toF(th["budget_tokens"]); b > 0 {
					if mt := intVal(openaiReq, "max_tokens", "max_completion_tokens"); float64(mt) <= b {
						req["max_tokens"] = int(b) + 1024
					}
				}
			}
		}
	}
	// 顶层 cache_control（提示缓存断点）从 IR 扩展键还原
	if v, ok := types.IRGet(openaiReq, types.ExtAnthropicCacheCtrl); ok {
		req["cache_control"] = v
	}
	// 2026-09 系列新顶层参数还原：output_config（优先于档位折算）、inference_geo、
	// service_tier。显式 output_config 存在时不覆盖（上面档位折算只写入无 IR 版本）。
	if v, ok := types.IRGet(openaiReq, types.ExtAnthropicOutputConfig); ok {
		req["output_config"] = v
	}
	if v, ok := types.IRGet(openaiReq, types.ExtAnthropicInferenceGeo); ok {
		req["inference_geo"] = v
	}
	if v, ok := types.IRGet(openaiReq, types.ExtAnthropicServiceTier); ok {
		req["service_tier"] = v
	}
	if v, ok := openaiReq["metadata"]; ok {
		req["metadata"] = v
	}
	// 旧版 functions / function_call 兼容
	if _, ok := req["tools"]; !ok {
		if fns, ok := openaiReq["functions"].([]any); ok && len(fns) > 0 {
			req["tools"] = toolsFromFunctions(fns)
		}
	}
	if _, ok := req["tool_choice"]; !ok {
		if fc, ok := openaiReq["function_call"]; ok {
			req["tool_choice"] = functionCallToToolChoice(fc)
		}
	}
	if tools, ok := openaiReq["tools"].([]any); ok {
		rt := toolsToAnthropic(tools)
		if len(rt) > 0 {
			req["tools"] = rt
		}
	}
	// 还原 Anthropic 服务端工具（web_search / code_execution 等）：入站时与
	// 客户端工具分离存入 IR，避免被误转成 function 工具。
	if sv, ok := types.IRGet(openaiReq, types.ExtAnthropicServerTools); ok {
		if arr, ok2 := sv.([]any); ok2 && len(arr) > 0 {
			existing, _ := req["tools"].([]any)
			req["tools"] = append(existing, arr...)
		}
	}
	if tc, ok := openaiReq["tool_choice"]; ok {
		req["tool_choice"] = tcToAnthropic(tc)
	}
	// response_format 降级（JSON mode）：Anthropic 原生不支持 OpenAI 的
	// response_format 参数，借鉴 LiteLLM 的做法——把 JSON schema 伪装成
	// 单个强制工具调用（名为 json_output），请求侧用 tool_choice 强制模型
	// 只走该工具输出 JSON，响应侧再把 tool_use 的 input 还原为 OpenAI 的
	// content 字符串（非流式见 AnthropicToOpenai，流式见 AnthropicStreamToChunks）。
	if rf, ok := openaiReq["response_format"].(map[string]any); ok {
		var schema map[string]any
		switch rfType, _ := rf["type"].(string); rfType {
		case "json_schema":
			if js, ok := rf["json_schema"].(map[string]any); ok {
				if s, ok := js["schema"].(map[string]any); ok {
					schema = s
				} else if sStr, ok := js["schema"].(string); ok && sStr != "" {
					// 部分 SDK 把 schema 序列化为 JSON 字符串传递
					json.Unmarshal([]byte(sStr), &schema)
				} else {
					schema = js
				}
			}
		case "json_object":
			// 无 schema 的宽松 JSON 模式：只约束输出为 JSON 对象
			schema = map[string]any{"type": "object"}
		}
		if schema != nil {
			jsonTool := map[string]any{
				"name":         jsonModeToolName,
				"description":  "Return a single valid JSON object that strictly conforms to the provided input schema. Output only the JSON, with no surrounding prose.",
				"input_schema": schema,
			}
			if tools, ok := req["tools"].([]any); ok {
				req["tools"] = append(tools, jsonTool)
			} else {
				req["tools"] = []any{jsonTool}
			}
			req["tool_choice"] = map[string]any{"type": "tool", "name": jsonModeToolName}
		}
	}
	if stream, _ := openaiReq["stream"].(bool); stream {
		req["stream"] = true
	}
	return req
}

// isAnthropicAdaptiveModel 判断目标 Anthropic 模型是否为「推荐 thinking.type=
// adaptive」的新模型家族（Claude 4.7+/5 系列、Mythos Preview、Fable 5）。
// enabled 已弃用但对这些新模型会触发弃用警告；旧模型（4.6 及更早）仍用 enabled。
func isAnthropicAdaptiveModel(model string) bool {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "mythos"),
		strings.Contains(m, "fable-5"),
		strings.Contains(m, "opus-5"),
		strings.Contains(m, "sonnet-5"),
		strings.Contains(m, "haiku-5"),
		// Claude 4.7+ 模型（Opus/Sonnet 4.7、4.8）
		strings.Contains(m, "opus-4-7"),
		strings.Contains(m, "opus-4-8"):
		return true
	}
	return false
}

// anthropicEffortOf 把统一推理档位折算为 Anthropic output_config.effort 枚举。
// 取值来自映射表（format/reasoning_table.go）的 Anthropic(新 adaptive) 列；
// 保留本函数仅为兼容既有调用方。
func anthropicEffortOf(eff string) string {
	return AnthropicEffortOf(eff)
}

// anthropicThinkingBudget 把统一推理档位映射为 Anthropic 思考预算（token）。
// 取值来自映射表的 Anthropic(旧 enabled) 列；0 表示不启用 extended thinking。
func anthropicThinkingBudget(eff string) int {
	return AnthropicBudgetOf(eff)
}

func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return partsToText(v)
}

func toolResultBlock(mm map[string]any) map[string]any {
	cid, _ := mm["tool_call_id"].(string)
	if cid == "" {
		cid = randID("toolu_")
	}
	content := ""
	if v, ok := mm["content"]; ok && v != nil {
		if s, ok := v.(string); ok {
			content = s
		} else {
			b, _ := json.Marshal(v)
			content = string(b)
		}
	}
	return map[string]any{"type": "tool_result", "tool_use_id": cid, "content": content}
}

func hasToolResult(arr []any) bool {
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			if t, _ := m["type"].(string); t == "tool_result" {
				return true
			}
		}
	}
	return false
}

func intVal(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k].(float64); ok {
			return int(v)
		}
	}
	return 0
}

// toF 把 int/float64 数值安全转为 float64；非数值返回 0。
// 用于把 IR 扩展键中任意数值表示（内部写入的 int 或 JSON 反序列化的 float64）
// 统一折算，避免类型断言脆断。
func toF(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func copyMapVal(src, dst map[string]any, keys ...string) {
	for _, k := range keys {
		if v, ok := src[k]; ok {
			dst[k] = v
		}
	}
}

func tcToAnthropic(tc any) map[string]any {
	if s, ok := tc.(string); ok {
		switch s {
		case "none":
			return map[string]any{"type": "none"}
		case "required":
			return map[string]any{"type": "any"}
		default:
			return map[string]any{"type": "auto"}
		}
	}
	if m, ok := tc.(map[string]any); ok {
		if t, _ := m["type"].(string); t == "function" {
			if fn, ok := m["function"].(map[string]any); ok {
				if name, ok := fn["name"].(string); ok {
					return map[string]any{"type": "tool", "name": name}
				}
			}
		}
	}
	return map[string]any{"type": "auto"}
}

func toolsToAnthropic(tools []any) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		tm, _ := t.(map[string]any)
		if tm == nil {
			continue
		}
		fn, _ := tm["function"].(map[string]any)
		if fn == nil {
			continue
		}
		params, _ := fn["parameters"].(map[string]any)
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"name":         fn["name"],
			"description":  fn["description"],
			"input_schema": params,
		})
	}
	return out
}

func toolsFromFunctions(fns []any) []any {
	out := make([]any, 0, len(fns))
	for _, f := range fns {
		fm, _ := f.(map[string]any)
		if fm == nil {
			continue
		}
		params, _ := fm["parameters"].(map[string]any)
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{"name": fm["name"], "description": fm["description"], "input_schema": params})
	}
	return out
}

func functionCallToToolChoice(fc any) map[string]any {
	if s, ok := fc.(string); ok {
		switch s {
		case "none":
			return map[string]any{"type": "none"}
		case "auto":
			return map[string]any{"type": "auto"}
		}
	}
	if m, ok := fc.(map[string]any); ok {
		if name, ok := m["name"].(string); ok && name != "" {
			return map[string]any{"type": "tool", "name": name}
		}
	}
	return map[string]any{"type": "auto"}
}
