package format

import (
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"llm-router-go/types"
)

// ---------------------------------------------------------------------------
// Gemini Interactions API（POST /v1beta/interactions，2026-06 起 GA）
//
// Google 推荐的新标准接口，取代 generateContent（legacy）。同一 provider type
// 仍为 "gemini"，按 provider.BaseURL 是否包含 /interactions 自动判别：
//   - base 含 /interactions → 走 Interactions API（模型名在 body 的 model 字段，
//     多模态输入用 input 内容块 / Steps 数组，响应按 steps 时间线组织）；
//   - 不含 → 保持 legacy generateContent / streamGenerateContent。
//
// 入站客户端格式不变（仍走 /v1beta/models/{model}:generateContent 归一化），
// 出站时按 base 自动装配 Interactions 请求体。
// ---------------------------------------------------------------------------

// isGeminiInteractions 判断归一化后的 gemini base 是否指向 Interactions API。
func isGeminiInteractions(base string) bool {
	return strings.Contains(base, "/interactions")
}

// interactionsStepContentToChat 把 Interactions step/content 块数组转为 OpenAI
// 消息 content（text/image 支持，其余原样透传）。
func interactionsStepContentToChat(content any) []any {
	var parts []any
	for _, c := range sliceOrEmpty(content) {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		ct, _ := cm["type"].(string)
		switch ct {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": cm["text"]})
		case "image":
			if data, ok := cm["data"].(string); ok && data != "" {
				mt, _ := cm["mime_type"].(string)
				if mt == "" {
					mt = "image/png"
				}
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + mt + ";base64," + data}})
			} else if uri, ok := cm["uri"].(string); ok && uri != "" {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": uri}})
			}
		default:
			// audio / document / video 等：转成 OpenAI 可透传的块
			parts = append(parts, cm)
		}
	}
	return parts
}

// interactionsMsgContent 把 Interactions step 里的 content（块数组或字符串）
// 汇总为 OpenAI 消息 content。
func interactionsMsgContent(content any) any {
	blocks := interactionsStepContentToChat(content)
	if len(blocks) == 0 {
		return ""
	}
	if len(blocks) == 1 {
		if b, ok := blocks[0].(map[string]any); ok {
			if t, _ := b["type"].(string); t == "text" {
				if s, ok := b["text"].(string); ok {
					return s
				}
			}
		}
	}
	return blocks
}

// OpenaiFromGeminiInteractions 把 Gemini Interactions API 请求体归一化为 OpenAI
// Chat Completions 形状（客户端以 /v1beta/interactions 接入，2026 GA 标准）：
//   - input 支持纯字符串 / 内容块数组 / steps 数组；
//   - system_instruction → system 消息；
//   - generation_config（snake_case）→ temperature/top_p/seed/max_output_tokens/
//     stop_sequences/thinking_level → reasoning_effort；
//   - tools（function 形状）→ OpenAI tools。
func OpenaiFromGeminiInteractions(body any) map[string]any {
	m, _ := body.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	stream, _ := m["stream"].(bool)
	out := map[string]any{"model": strOr(m["model"], ""), "messages": []any{}, "stream": stream}

	var sys []string
	if si, ok := m["system_instruction"].(string); ok && si != "" {
		sys = append(sys, si)
	} else if si, ok := m["system_instruction"].(map[string]any); ok {
		if parts, ok := si["parts"].([]any); ok {
			if text := partsToText(parts); text != "" {
				sys = append(sys, text)
			}
		}
	}
	if len(sys) > 0 {
		out["messages"] = append(out["messages"].([]any), map[string]any{"role": "system", "content": strings.Join(sys, "\n")})
	}

	// 多轮续接时用 steps 数组；纯文本 input 直接为字符串
	switch in := m["input"].(type) {
	case string:
		if in != "" {
			out["messages"] = append(out["messages"].([]any), map[string]any{"role": "user", "content": in})
		}
	case []any:
		var pendingCalls []any
		flushCalls := func() {
			if len(pendingCalls) == 0 {
				return
			}
			out["messages"] = append(out["messages"].([]any), map[string]any{"role": "assistant", "tool_calls": pendingCalls})
			pendingCalls = nil
		}
		for _, it := range in {
			im, _ := it.(map[string]any)
			if im == nil {
				continue
			}
			st, _ := im["type"].(string)
			switch st {
			case "user_input":
				flushCalls()
				content := im["content"]
				if _, isArr := content.([]any); isArr {
					out["messages"] = append(out["messages"].([]any), map[string]any{"role": "user", "content": interactionsMsgContent(content)})
				} else if s, ok := content.(string); ok && s != "" {
					out["messages"] = append(out["messages"].([]any), map[string]any{"role": "user", "content": s})
				}
			case "model_output":
				flushCalls()
				content := interactionsMsgContent(im["content"])
				if s, ok := content.(string); ok && s == "" {
					continue
				}
				out["messages"] = append(out["messages"].([]any), map[string]any{"role": "assistant", "content": content})
			case "function_call":
				cid := ""
				if id, ok := im["id"].(string); ok && id != "" {
					cid = id
				}
				name, _ := im["name"].(string)
				args := "{}"
				switch a := im["arguments"].(type) {
				case map[string]any:
					if b, err := json.Marshal(a); err == nil {
						args = string(b)
					}
				case string:
					if a != "" {
						args = a
					}
				}
				tc := map[string]any{
					"type":     "function",
					"function": map[string]any{"name": name, "arguments": args},
				}
				// 保留 call id：后续 function_result 的 tool 消息用 call_id 回指，
				// 缺失会导致严格上游校验失败（工具消息引用未知 tool_call_id）。
				if cid != "" {
					tc["id"] = cid
				}
				pendingCalls = append(pendingCalls, tc)
			case "function_result":
				flushCalls()
				cid, _ := im["call_id"].(string)
				if cid == "" {
					cid = randID("call_")
				}
				content := ""
				switch r := im["result"].(type) {
				case string:
					content = r
				case map[string]any, []any:
					if b, err := json.Marshal(r); err == nil {
						content = string(b)
					}
				}
				msg := map[string]any{"role": "tool", "tool_call_id": cid, "content": content}
				if n, _ := im["name"].(string); n != "" {
					msg["name"] = n
				}
				out["messages"] = append(out["messages"].([]any), msg)
			default:
				// content 块数组或缺省：按用户消息处理
				flushCalls()
				if cb := im["content"]; cb != nil {
					out["messages"] = append(out["messages"].([]any), map[string]any{"role": "user", "content": interactionsMsgContent(cb)})
				} else if text, _ := im["text"].(string); text != "" {
					out["messages"] = append(out["messages"].([]any), map[string]any{"role": "user", "content": text})
				}
			}
		}
		flushCalls()
	}

	// generation_config（Interactions 用 snake_case 字段名）
	if gc, ok := m["generation_config"].(map[string]any); ok {
		copyFloat(gc, out, "temperature")
		if v, ok := gc["top_p"]; ok {
			out["top_p"] = v
		}
		if v, ok := gc["seed"]; ok {
			out["seed"] = v
		}
		if mt, ok := gc["max_output_tokens"]; ok {
			if f, ok2 := mt.(float64); ok2 {
				out["max_tokens"] = f
			}
		}
		if ss, ok := gc["stop_sequences"].([]any); ok && len(ss) > 0 {
			out["stop"] = ss
		}
		if lv, ok := gc["thinking_level"].(string); ok && lv != "" {
			out["reasoning_effort"] = interactionsEffortToOpenAI(lv)
		}
		if tc, ok := gc["tool_choice"]; ok {
			mode := ""
			if tcObj, isObj := tc.(map[string]any); isObj {
				mode, _ = tcObj["mode"].(string)
			} else if s, isStr := tc.(string); isStr {
				// 兼容字符串形态（noninteractive 客户端可能直接传 "auto" 等）
				mode = s
			}
			switch mode {
			case "none":
				out["tool_choice"] = "none"
			case "any":
				out["tool_choice"] = "required"
			case "auto":
				out["tool_choice"] = "auto"
			}
		}
		// 细分配置（responseSchema 等）经 IR 保真
		if rs, ok := gc["response_schema"]; ok {
			types.IRSet(out, types.ExtGeminiResponseSchema, rs)
		}
	}

	// 顶层 response_format / response_mime_type（Interactions 结构化输出）
	if rf, ok := m["response_format"].(map[string]any); ok {
		if mt, _ := rf["mime_type"].(string); mt == "application/json" {
			if schema, ok := rf["schema"]; ok && schema != nil {
				out["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "structured", "schema": schema}}
			} else {
				out["response_format"] = map[string]any{"type": "json_object"}
			}
		}
	}

	// tools：function 形状 → OpenAI function
	if tools, ok := m["tools"].([]any); ok && len(tools) > 0 {
		ot := []any{}
		extras := []any{}
		for _, t := range tools {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			if tt, _ := tm["type"].(string); tt == "function" {
				ot = append(ot, map[string]any{
					"type": "function",
					"function": map[string]any{
						"name":        tm["name"],
						"description": tm["description"],
						"parameters":  tm["parameters"],
					},
				})
			} else if tt != "" {
				extras = append(extras, tm) // google_search / code_execution 等原生工具
			}
		}
		if len(ot) > 0 {
			out["tools"] = ot
		}
		if len(extras) > 0 {
			types.IRSet(out, types.ExtGeminiToolsExtra, extras)
		}
	}

	return out
}

// interactionsEffortToOpenAI 把 Interactions thinking_level 折算为 OpenAI
// reasoning_effort 枚举（minimal→low；none 不设置）。
func interactionsEffortToOpenAI(lv string) string {
	switch strings.ToLower(lv) {
	case "none", "minimal":
		return "none"
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	}
	return lv
}

// geminiRoot 去掉 /interactions 后缀用于模型列表等资源端点。
func geminiRoot(base string) string {
	if idx := strings.LastIndex(base, "/interactions"); idx >= 0 {
		return base[:idx]
	}
	return base
}

// interactionsContentBlocks 把 OpenAI 消息 content（string 或 parts 数组）
// 转为 Interactions 内容块（text / image）。
func interactionsContentBlocks(content any) []any {
	if s, ok := content.(string); ok {
		if s == "" {
			return nil
		}
		return []any{map[string]any{"type": "text", "text": s}}
	}
	arr := sliceOrEmpty(content)
	blocks := []any{}
	for _, p := range arr {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		pt, _ := pm["type"].(string)
		switch pt {
		case "text":
			if t, ok := pm["text"].(string); ok && t != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": t})
			}
		case "image_url":
			iu, _ := pm["image_url"].(map[string]any)
			url, _ := iu["url"].(string)
			if d := parseDataURL(url); d != nil {
				b := map[string]any{"type": "image", "data": d.Data, "mime_type": d.Mime}
				if d.Mime != "" {
					b["mime_type"] = d.Mime
				}
				blocks = append(blocks, b)
			} else if url != "" {
				blocks = append(blocks, map[string]any{"type": "image", "uri": url})
			}
		default:
			// audio / document / video 等未知块保持原样透传
			blocks = append(blocks, pm)
		}
	}
	return blocks
}

// interactionsThinkingLevel 把统一推理档位（reasoning_effort）折算为
// Interactions generation_config.thinking_level（minimal/low/medium/high）。
func interactionsThinkingLevel(eff string) string {
	switch eff {
	case "none", "minimal":
		return "minimal"
	case "low":
		return "low"
	case "medium":
		return "medium"
	default: // high / xhigh / max
		return "high"
	}
}

// ToGeminiInteractions 把归一化 OpenAI Chat 请求装配为 Interactions API 请求体。
// 与 ToGemini（legacy generateContent）并存：模型在顶层 model，输入用 steps
// 数组，系统指令用 system_instruction，多模态输入用 input 内容块。
func ToGeminiInteractions(openaiReq map[string]any) map[string]any {
	out := map[string]any{}
	if m, ok := openaiReq["model"].(string); ok && m != "" {
		out["model"] = m
	}

	var sys []string
	steps := []any{}

	for _, m := range sliceOrEmpty(openaiReq["messages"]) {
		mm, _ := m.(map[string]any)
		if mm == nil {
			continue
		}
		role, _ := mm["role"].(string)
		content := mm["content"]

		switch role {
		case "system":
			sys = append(sys, stringify(content))

		case "tool":
			cid, _ := mm["tool_call_id"].(string)
			name, _ := mm["name"].(string)
			if name == "" {
				if tcs, ok := mm["tool_calls"].([]any); ok && len(tcs) > 0 {
					if tcm, ok := tcs[0].(map[string]any); ok {
						if fn, ok := tcm["function"].(map[string]any); ok {
							name, _ = fn["name"].(string)
						}
					}
				}
			}
			result := ""
			if s, ok := content.(string); ok {
				result = s
			} else {
				b, _ := json.Marshal(content)
				result = string(b)
			}
			step := map[string]any{"type": "function_result", "result": result}
			if cid != "" {
				step["call_id"] = cid
			}
			if name != "" {
				step["name"] = name
			}
			steps = append(steps, step)

		case "assistant":
			blocks := interactionsContentBlocks(content)
			if len(blocks) > 0 {
				steps = append(steps, map[string]any{"type": "model_output", "content": blocks})
			}
			for _, tc := range sliceOrEmpty(mm["tool_calls"]) {
				tcm, _ := tc.(map[string]any)
				if tcm == nil {
					continue
				}
				fn, _ := tcm["function"].(map[string]any)
				id, _ := tcm["id"].(string)
				if id == "" {
					id = randID("call_")
				}
				name, _ := fn["name"].(string)
				args := map[string]any{}
				if a, ok := fn["arguments"].(string); ok && a != "" {
					if v, ok := safeParse(a, map[string]any{}).(map[string]any); ok {
						args = v
					}
				}
				steps = append(steps, map[string]any{"type": "function_call", "id": id, "name": name, "arguments": args})
			}

		default: // user
			blocks := interactionsContentBlocks(content)
			if len(blocks) > 0 {
				steps = append(steps, map[string]any{"type": "user_input", "content": blocks})
			}
		}
	}

	if len(sys) > 0 {
		out["system_instruction"] = strings.Join(sys, "\n")
	}
	if len(steps) > 0 {
		out["input"] = steps
	}

	// generation_config（Interactions 使用 snake_case 字段名）
	gc := map[string]any{}
	if v, ok := openaiReq["temperature"].(float64); ok {
		gc["temperature"] = v
	}
	if v, ok := openaiReq["top_p"].(float64); ok {
		gc["top_p"] = v
	}
	if v, ok := openaiReq["seed"].(float64); ok {
		gc["seed"] = v
	}
	if v, ok := openaiReq["max_tokens"].(float64); ok {
		gc["max_output_tokens"] = v
	} else if v, ok := openaiReq["max_completion_tokens"].(float64); ok {
		gc["max_output_tokens"] = v
	}
	if v, ok := openaiReq["stop"]; ok {
		if sl := strList(v); len(sl) > 0 {
			gc["stop_sequences"] = sl
		}
	}
	if eff, _ := openaiReq["reasoning_effort"].(string); eff != "" {
		gc["thinking_level"] = interactionsThinkingLevel(eff)
	}
	if tc, ok := openaiReq["tool_choice"]; ok {
		gc["tool_choice"] = interactionsToolChoice(tc)
	}
	if len(gc) > 0 {
		out["generation_config"] = gc
	}

	// 结构化输出：Interactions 用顶层 response_format + response_mime_type
	if rf, ok := openaiReq["response_format"].(map[string]any); ok {
		f := interactionsResponseFormat(rf)
		if len(f) > 0 {
			out["response_format"] = f
			out["response_mime_type"] = "application/json"
		}
	}
	if v, ok := types.IRGet(openaiReq, types.ExtGeminiResponseSchema); ok {
		out["response_format"] = map[string]any{
			"type":      "text",
			"mime_type": "application/json",
			"schema":    v,
		}
		out["response_mime_type"] = "application/json"
	}

	// tools：function 工具 + 原生内置工具（google_search 等，经 IR 还原）
	if tools, ok := openaiReq["tools"].([]any); ok {
		it := []any{}
		for _, t := range tools {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			if tType, _ := tm["type"].(string); tType != "" && tType != "function" {
				it = append(it, tm) // 原生类型（google_search 等）原样透传
				continue
			}
			fn, _ := tm["function"].(map[string]any)
			if fn == nil {
				continue
			}
			it = append(it, map[string]any{
				"type":        "function",
				"name":        fn["name"],
				"description": fn["description"],
				"parameters":  fn["parameters"],
			})
		}
		if len(it) > 0 {
			out["tools"] = it
		}
	}
	if extraV, ok := types.IRGet(openaiReq, types.ExtGeminiToolsExtra); ok {
		if extra, ok := extraV.([]any); ok && len(extra) > 0 {
			existing, _ := out["tools"].([]any)
			for _, e := range extra {
				if em, ok := e.(map[string]any); ok {
					existing = append(existing, em)
				}
			}
			out["tools"] = existing
		}
	}

	if stream, _ := openaiReq["stream"].(bool); stream {
		out["stream"] = true
	}
	return out
}

// interactionsResponseFormat 把 OpenAI response_format 转为 Interactions
// response_format（{type:"text", mime_type, schema}）。
func interactionsResponseFormat(rf map[string]any) map[string]any {
	t, _ := rf["type"].(string)
	switch t {
	case "json_object":
		return map[string]any{"type": "text", "mime_type": "application/json"}
	case "json_schema":
		if js, ok := rf["json_schema"].(map[string]any); ok {
			if s, ok := js["schema"].(map[string]any); ok {
				f := map[string]any{"type": "text", "mime_type": "application/json"}
				if len(s) > 0 {
					f["schema"] = s
				}
				return f
			}
		}
		return map[string]any{"type": "text", "mime_type": "application/json"}
	}
	return nil
}

// interactionsToolChoice 把 OpenAI tool_choice 转为 Interactions
// generation_config.tool_choice（auto/any/none + allowed_tools 白名单）。
func interactionsToolChoice(tc any) map[string]any {
	if s, ok := tc.(string); ok {
		switch s {
		case "none":
			return map[string]any{"mode": "none"}
		case "required":
			return map[string]any{"mode": "any"}
		default:
			return map[string]any{"mode": "auto"}
		}
	}
	if m, ok := tc.(map[string]any); ok {
		if t, _ := m["type"].(string); t == "function" {
			if fn, ok := m["function"].(map[string]any); ok {
				if name, ok := fn["name"].(string); ok && name != "" {
					return map[string]any{
						"mode":          "any",
						"allowed_tools": map[string]any{"mode": "any", "tools": []string{name}},
					}
				}
			}
		}
	}
	return map[string]any{"mode": "auto"}
}

// geminiInteractionsStream 是 Interactions API 流式事件的有状态转换器：
// 按 event_type 把 interaction.created / step.start / step.delta /
// interaction.completed 等事件转为 OpenAI 形状 chunk。
type geminiInteractionsStream struct {
	model      string
	seenStart  bool
	seenFinish bool

	// 步骤序号（step index）→ OpenAI 工具调用 index（steps 可能含 thought /
	// model_output / function_call 等类型，index 不能直接当作 tool index）
	stepToolIdx map[int]int
	nextToolIdx int
	toolID      map[int]string
	toolName    map[int]string
	toolArgs    map[int]string

	promptTokens  int
	outputTokens  int
	cachedTokens  int
	thoughtTokens int
	hasToolCall   bool
}

// handle 处理一个 Interactions 事件；返回 false 表示流应提前结束。
func (is *geminiInteractionsStream) handle(ch chan map[string]any, data map[string]any, et string) bool {
	if is.stepToolIdx == nil {
		is.stepToolIdx = map[int]int{}
		is.toolID = map[int]string{}
		is.toolName = map[int]string{}
		is.toolArgs = map[int]string{}
	}

	// 事件顺序：interaction.created → step.start/step.delta/step.stop* → interaction.completed
	switch et {
	case "error":
		msg := "upstream error"
		if em, ok := data["error"].(map[string]any); ok {
			if m, ok := em["message"].(string); ok {
				msg = m
			}
		}
		ch <- openaiErrorChunk(msg)
		return false

	case "interaction.created":
		if !is.seenStart {
			if it, ok := data["interaction"].(map[string]any); ok {
				if m, _ := it["model"].(string); m != "" {
					is.model = m
				}
			}
			is.seenStart = true
			ch <- map[string]any{
				"object": "chat.completion.chunk",
				"model":  is.model,
				"choices": []any{
					map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}},
				},
			}
		}

	case "step.start":
		idx, _ := numFloat(data, "index")
		step, _ := data["step"].(map[string]any)
		if step == nil {
			return true
		}
		st, _ := step["type"].(string)
		if st == "function_call" {
			toolIdx := is.nextToolIdx
			is.nextToolIdx++
			is.stepToolIdx[idx] = toolIdx
			is.hasToolCall = true
			if id, _ := step["id"].(string); id != "" {
				is.toolID[toolIdx] = id
			}
			if n, _ := step["name"].(string); n != "" {
				is.toolName[toolIdx] = n
			}
			ch <- map[string]any{
				"choices": []any{
					map[string]any{
						"index": 0,
						"delta": map[string]any{
							"tool_calls": []any{
								map[string]any{
									"index": toolIdx,
									"id":    is.toolID[toolIdx],
									"type":  "function",
									"function": map[string]any{
										"name":      is.toolName[toolIdx],
										"arguments": "",
									},
								},
							},
						},
					},
				},
			}
		}

	case "step.delta":
		idx, _ := numFloat(data, "index")
		delta, _ := data["delta"].(map[string]any)
		if delta == nil {
			return true
		}
		if !is.seenStart {
			is.seenStart = true
			ch <- map[string]any{
				"object": "chat.completion.chunk",
				"model":  is.model,
				"choices": []any{
					map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}},
				},
			}
		}
		dt, _ := delta["type"].(string)
		switch dt {
		case "text":
			if txt, ok := delta["text"].(string); ok && txt != "" {
				ch <- map[string]any{
					"choices": []any{
						map[string]any{"index": 0, "delta": map[string]any{"content": txt}},
					},
				}
			}
		case "arguments_delta":
			if pj, ok := delta["partial_arguments"].(string); ok {
				toolIdx, has := is.stepToolIdx[idx]
				if !has {
					// 未见过 step.start 的增量：分配新工具槽位
					toolIdx = is.nextToolIdx
					is.nextToolIdx++
					is.stepToolIdx[idx] = toolIdx
					is.hasToolCall = true
				}
				is.toolArgs[toolIdx] += pj
				ch <- map[string]any{
					"choices": []any{
						map[string]any{
							"index": 0,
							"delta": map[string]any{
								"tool_calls": []any{
									map[string]any{
										"index": toolIdx,
										"function": map[string]any{
											"arguments": pj,
										},
									},
								},
							},
						},
					},
				}
			}
		}

	case "interaction.completed", "interaction.status_update":
		it, _ := data["interaction"].(map[string]any)
		if it != nil {
			if u, ok := it["usage"].(map[string]any); ok {
				if v, ok := u["total_input_tokens"].(float64); ok {
					is.promptTokens = int(v)
				}
				if v, ok := u["total_output_tokens"].(float64); ok {
					is.outputTokens = int(v)
				}
				if v, ok := u["total_cached_tokens"].(float64); ok {
					is.cachedTokens = int(v)
				}
				if v, ok := u["total_thought_tokens"].(float64); ok {
					is.thoughtTokens = int(v)
				}
			}
		}
		if et == "interaction.completed" && !is.seenFinish {
			is.seenFinish = true
			status := ""
			if itm, ok := data["interaction"].(map[string]any); ok {
				status, _ = itm["status"].(string)
			}
			fr := "stop"
			switch status {
			case "requires_action":
				fr = "tool_calls"
			case "incomplete":
				fr = "length"
			case "failed", "cancelled":
				fr = "content_filter"
			}
			if is.hasToolCall {
				fr = "tool_calls"
			}
			ch <- map[string]any{
				"choices": []any{
					map[string]any{"index": 0, "finish_reason": fr, "delta": map[string]any{}},
				},
			}
			// usage chunk
			if is.promptTokens > 0 || is.outputTokens > 0 {
				u := map[string]any{}
				if is.promptTokens > 0 {
					u["prompt_tokens"] = is.promptTokens
				}
				if is.outputTokens > 0 {
					u["completion_tokens"] = is.outputTokens
				}
				if is.cachedTokens > 0 {
					u["prompt_tokens_details"] = map[string]any{"cached_tokens": is.cachedTokens}
				}
				if is.thoughtTokens > 0 {
					u["completion_tokens_details"] = map[string]any{"reasoning_tokens": is.thoughtTokens}
				}
				if pt, ok := u["prompt_tokens"]; ok {
					if ct, ok2 := u["completion_tokens"]; ok2 {
						u["total_tokens"] = pt.(int) + ct.(int)
					}
				}
				ch <- map[string]any{"usage": u, "choices": []any{}}
			}
			return false // 完成后结束流，关闭上游连接
		}
	}
	return true
}

// GeminiInteractionsToOpenai 把 Interactions API 非流式响应（Interaction 对象）
// 转为 OpenAI Chat 形状。step 时间线里取 model_output 文本与 function_call。
// body 可能直接是 Interaction，也可能是 {interaction: {...}} 包装。
func GeminiInteractionsToOpenai(body map[string]any, openaiReq map[string]any) map[string]any {
	if inner, ok := body["interaction"].(map[string]any); ok {
		body = inner
	}
	model := strOr(body["model"], strOr(openaiReq["model"], ""))
	out := map[string]any{
		"id":      strOr(body["id"], randID("chatcmpl-")),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
	}

	var textParts []string
	var toolCalls []any

	for _, s := range sliceOrEmpty(body["steps"]) {
		sm, _ := s.(map[string]any)
		if sm == nil {
			continue
		}
		st, _ := sm["type"].(string)
		switch st {
		case "model_output":
			for _, c := range sliceOrEmpty(sm["content"]) {
				cm, _ := c.(map[string]any)
				if cm == nil {
					continue
				}
				if t, _ := cm["type"].(string); t == "text" {
					textParts = append(textParts, strOr(cm["text"], ""))
				}
			}
		case "function_call":
			id, _ := sm["id"].(string)
			if id == "" {
				id = randID("call_")
			}
			name, _ := sm["name"].(string)
			args := ""
			switch a := sm["arguments"].(type) {
			case string:
				args = a
			case map[string]any:
				if b, err := json.Marshal(a); err == nil {
					args = string(b)
				}
			}
			toolCalls = append(toolCalls, map[string]any{
				"id": id, "type": "function",
				"function": map[string]any{"name": name, "arguments": args},
			})
		}
	}

	status, _ := body["status"].(string)
	fr := "stop"
	switch status {
	case "requires_action":
		fr = "tool_calls"
	case "incomplete":
		fr = "length"
	case "failed", "cancelled":
		fr = "content_filter"
	}
	if len(toolCalls) > 0 {
		fr = "tool_calls"
	}

	msg := map[string]any{"role": "assistant"}
	if len(textParts) > 0 {
		msg["content"] = strings.Join(textParts, "")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	out["choices"] = []any{map[string]any{"index": 0, "message": msg, "finish_reason": fr}}

	// usage：total_input_tokens / total_output_tokens 等
	if u, ok := body["usage"].(map[string]any); ok {
		uu := map[string]any{}
		if v, ok := u["total_input_tokens"].(float64); ok {
			uu["prompt_tokens"] = int(v)
		}
		if v, ok := u["total_output_tokens"].(float64); ok {
			uu["completion_tokens"] = int(v)
		}
		if v, ok := u["total_cached_tokens"].(float64); ok && v > 0 {
			uu["prompt_tokens_details"] = map[string]any{"cached_tokens": int(v)}
		}
		if v, ok := u["total_thought_tokens"].(float64); ok && v > 0 {
			uu["completion_tokens_details"] = map[string]any{"reasoning_tokens": int(v)}
		}
		if v, ok := u["total_tokens"].(float64); ok && v > 0 {
			uu["total_tokens"] = int(v)
		}
		if len(uu) > 0 {
			out["usage"] = uu
		}
	}
	return out
}

// interactionUsageFromOpenAI 把 OpenAI 形状 usage 映射为 Interactions usage。
func interactionUsageFromOpenAI(usage map[string]any) map[string]any {
	u := map[string]any{}
	if v, ok := usage["prompt_tokens"].(float64); ok {
		u["total_input_tokens"] = int(v)
	}
	if v, ok := usage["completion_tokens"].(float64); ok {
		u["total_output_tokens"] = int(v)
	}
	if ptd, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		if cv, ok := ptd["cached_tokens"].(float64); ok && cv > 0 {
			u["total_cached_tokens"] = int(cv)
		}
	}
	if ctd, ok := usage["completion_tokens_details"].(map[string]any); ok {
		if rv, ok := ctd["reasoning_tokens"].(float64); ok && rv > 0 {
			u["total_thought_tokens"] = int(rv)
		}
	}
	if pt, ck := u["total_input_tokens"]; ck {
		if ct, ck2 := u["total_output_tokens"]; ck2 {
			u["total_tokens"] = pt.(int) + ct.(int)
		}
	}
	if len(u) == 0 {
		return nil
	}
	return u
}

// OpenaiToGeminiInteractionsBody 把归一化 OpenAI 响应还原为 Interactions API
// 的 Interaction 对象（steps 时间线：model_output 文本 + function_call）。
// 仅用于客户端以 /v1beta/interactions 接入时（gemini-interactions 客户端格式）。
func OpenaiToGeminiInteractionsBody(openaiResp map[string]any, model string) map[string]any {
	outModel := model
	if m, ok := openaiResp["model"].(string); ok && m != "" {
		outModel = m
	}
	out := map[string]any{
		"id":     strOr(openaiResp["id"], randID("v1_")),
		"object": "interaction",
		"model":  outModel,
		"status": "completed",
	}

	steps := []any{}
	hadToolCalls := false

	choices := sliceOrEmpty(openaiResp["choices"])
	if len(choices) > 0 {
		c0, _ := choices[0].(map[string]any)
		msg, _ := c0["message"].(map[string]any)
		if msg != nil {
			content := msg["content"]
			if s, ok := content.(string); ok && s != "" {
				steps = append(steps, map[string]any{
					"type":    "model_output",
					"content": []any{map[string]any{"type": "text", "text": s}},
				})
			} else if arr, ok := content.([]any); ok {
				blocks := []any{}
				for _, p := range arr {
					pm, _ := p.(map[string]any)
					if pm == nil {
						continue
					}
					if t, _ := pm["type"].(string); t == "text" {
						blocks = append(blocks, map[string]any{"type": "text", "text": pm["text"]})
					}
				}
				if len(blocks) > 0 {
					steps = append(steps, map[string]any{"type": "model_output", "content": blocks})
				}
			}
			// tool_calls → function_call steps
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
				args := map[string]any{}
				if a, ok := fn["arguments"].(string); ok && a != "" {
					if v, ok := safeParse(a, map[string]any{}).(map[string]any); ok {
						args = v
					}
				}
				id, _ := tcm["id"].(string)
				if id == "" {
					id = randID("call_")
				}
				steps = append(steps, map[string]any{"type": "function_call", "id": id, "name": name, "arguments": args})
				hadToolCalls = true
			}
		}
		if hadToolCalls {
			status := "requires_action"
			if fr, _ := c0["finish_reason"].(string); fr == "tool_calls" {
				status = "requires_action"
			} else {
				status = "completed"
			}
			out["status"] = status
		}
	}

	// JSON 降级模式还原（response_format json_schema → tool_use 还原）
	if errV, ok := openaiResp[types.ExtAnthropicThinkingBlock]; ok && errV != nil {
		_ = errV // 保留字段占位
	}

	if usage, ok := openaiResp["usage"].(map[string]any); ok {
		if u := interactionUsageFromOpenAI(usage); u != nil {
			out["usage"] = u
		}
	}
	if len(steps) == 0 {
		// 空 choices（内容被过滤等）：给客户端一个完整生命周期
		steps = append(steps, map[string]any{"type": "model_output", "content": []any{}})
	}
	out["steps"] = steps
	return out
}

// geminiInteractionsSSEWriter 以 Interactions API 流式事件写出归一化 chunk。
// 事件序列遵循官方：interaction.created → step.start/step.delta/step.stop*
// → interaction.completed；函数参数按 arguments_delta 增量流式传输。
type geminiInteractionsSSEWriter struct {
	w   io.Writer
	err error

	seq       int
	respID    string
	model     string
	started   bool
	stepIdx   int
	textOpen  bool
	tools     map[int]*geminiInteractionTool
	nextOutIt int
	finish    string

	promptTokens int
	outputTokens int
	usageSeen    bool
}

type geminiInteractionTool struct {
	stepIdx int
	id      string
	name    string
	args    string
	started bool
}

func (gw *geminiInteractionsSSEWriter) writeString(s string) {
	if gw.err != nil {
		return
	}
	if _, err := io.WriteString(gw.w, s); err != nil {
		gw.err = err
	}
}

// writeEvent 写出一条 Interactions SSE 事件（event 行 + data 行）。
func (gw *geminiInteractionsSSEWriter) writeEvent(event string, data map[string]any) {
	data["event_type"] = event
	gw.seq++
	data["event_id"] = "evt_" + strconv.Itoa(gw.seq)
	gw.writeString("event: " + event + "\ndata: " + jsonDumps(data) + "\n\n")
	if gw.err == nil {
		if err := maybeFlush(gw.w); err != nil {
			gw.err = err
		}
	}
}

func (gw *geminiInteractionsSSEWriter) start() {
	if gw.started {
		return
	}
	gw.started = true
	gw.writeEvent("interaction.created", map[string]any{
		"interaction": map[string]any{"id": gw.respID, "status": "in_progress", "model": gw.model},
	})
}

// openTextStep 惰性开启 model_output 步骤。
// 步骤索引与 function_call 步骤共用同一序号源（nextOutIt），保证同一流中
// 文本与工具步骤的 index 不冲突（此前文本步骤 index 恒为 0）。
func (gw *geminiInteractionsSSEWriter) openTextStep() {
	if gw.textOpen {
		return
	}
	gw.textOpen = true
	gw.stepIdx = gw.nextOutIt
	gw.nextOutIt++
	gw.writeEvent("step.start", map[string]any{
		"index": gw.stepIdx,
		"step":  map[string]any{"type": "model_output"},
	})
}

// openToolStep 开启 function_call 步骤（按 OpenAI 工具 index 维护独立逐步序号）。
func (gw *geminiInteractionsSSEWriter) openToolStep(idx int, id, name string) *geminiInteractionTool {
	t, ok := gw.tools[idx]
	if !ok {
		t = &geminiInteractionTool{stepIdx: gw.nextOutIt}
		gw.nextOutIt++
		if id != "" {
			t.id = id
		}
		if name != "" {
			t.name = name
		}
		gw.tools[idx] = t
	}
	if !t.started {
		t.started = true
		gw.writeEvent("step.start", map[string]any{
			"index": t.stepIdx,
			"step":  map[string]any{"type": "function_call", "id": t.id, "name": t.name},
		})
	}
	return t
}

func (gw *geminiInteractionsSSEWriter) Write(chunk map[string]any) bool {
	if gw.err != nil {
		return false
	}

	// 上游错误块 → error 事件
	if errM, ok := chunk["error"].(map[string]any); ok {
		gw.start()
		gw.writeEvent("error", map[string]any{"error": map[string]any{"code": "upstream_error", "message": errMsgText(errM)}})
		return false
	}
	if usage, ok := chunk["usage"].(map[string]any); ok {
		if v, ok := usage["prompt_tokens"].(float64); ok {
			gw.promptTokens = int(v)
		} else if v, ok := usage["prompt_tokens"].(int); ok {
			gw.promptTokens = v
		}
		if v, ok := usage["completion_tokens"].(float64); ok {
			gw.outputTokens = int(v)
		} else if v, ok := usage["completion_tokens"].(int); ok {
			gw.outputTokens = v
		}
		gw.usageSeen = true
	}
	if m, ok := chunk["model"].(string); ok && m != "" && gw.model == "" {
		gw.model = m
	}

	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return true
	}
	c0, _ := choices[0].(map[string]any)
	if c0 == nil {
		return true
	}
	delta, _ := c0["delta"].(map[string]any)
	fr, _ := c0["finish_reason"].(string)

	if delta != nil {
		if txt, ok := delta["content"].(string); ok && txt != "" {
			gw.start()
			gw.openTextStep()
			gw.writeEvent("step.delta", map[string]any{
				"index": gw.stepIdx,
				"delta": map[string]any{"type": "text", "text": txt},
			})
		}
		if tcs, ok := delta["tool_calls"].([]any); ok {
			for _, tc := range tcs {
				tcm, _ := tc.(map[string]any)
				if tcm == nil {
					continue
				}
				idx := 0
				if v, ok := numFloatAny(tcm, "index"); ok {
					idx = v
				}
				id, _ := tcm["id"].(string)
				name := ""
				if fn, ok := tcm["function"].(map[string]any); ok {
					name, _ = fn["name"].(string)
				}
				gw.start()
				t := gw.openToolStep(idx, id, name)
				if fn, ok := tcm["function"].(map[string]any); ok {
					if a, ok := fn["arguments"].(string); ok && a != "" {
						t.args += a
						gw.writeEvent("step.delta", map[string]any{
							"index": t.stepIdx,
							"delta": map[string]any{"type": "arguments_delta", "partial_arguments": a},
						})
					}
				}
			}
		}
	}
	if fr != "" {
		gw.finish = fr
	}
	return true
}

// Finish 收尾：关闭所有开放步骤并发出 interaction.completed。
func (gw *geminiInteractionsSSEWriter) Finish() {
	if !gw.started {
		gw.start()
	}
	// 关闭 text 步骤
	if gw.textOpen {
		gw.writeEvent("step.stop", map[string]any{"index": gw.stepIdx})
		gw.textOpen = false
	}
	// 关闭 function_call 步骤
	idxs := make([]int, 0, len(gw.tools))
	for k := range gw.tools {
		idxs = append(idxs, k)
	}
	sort.Ints(idxs)
	for _, k := range idxs {
		t := gw.tools[k]
		if t.started {
			gw.writeEvent("step.stop", map[string]any{"index": t.stepIdx})
		}
	}
	// status 由 finish_reason 推断
	status := "completed"
	if gw.finish == "tool_calls" {
		status = "completed"
	}
	interaction := map[string]any{"id": gw.respID, "status": status, "model": gw.model}
	if gw.promptTokens > 0 || gw.outputTokens > 0 {
		u := map[string]any{}
		if gw.promptTokens > 0 {
			u["total_input_tokens"] = gw.promptTokens
		}
		if gw.outputTokens > 0 {
			u["total_output_tokens"] = gw.outputTokens
		}
		if gw.promptTokens > 0 && gw.outputTokens > 0 {
			u["total_tokens"] = gw.promptTokens + gw.outputTokens
		}
		interaction["usage"] = u
	}
	gw.writeEvent("interaction.completed", map[string]any{"interaction": interaction})
}

// WriteChunksToGeminiInteractions 消费归一化 chunk 并写出完整 Interactions SSE
// 事件流。仅用于客户端以 /v1beta/interactions 接入时（gemini-interactions）。
func WriteChunksToGeminiInteractions(w io.Writer, chunks <-chan map[string]any, model string) (*types.Usage, error) {
	gw := &geminiInteractionsSSEWriter{w: w, model: model, respID: randID("v1_"), tools: map[int]*geminiInteractionTool{}}
	var lastUsage *types.Usage
	for chunk := range chunks {
		if u := usageFromChunk(chunk); u != nil {
			lastUsage = u
		}
		if !gw.Write(chunk) {
			drainChunks(chunks)
			return lastUsage, gw.err
		}
	}
	gw.Finish()
	return lastUsage, gw.err
}
