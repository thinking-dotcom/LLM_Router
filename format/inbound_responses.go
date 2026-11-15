package format

import (
	"encoding/json"
	"io"
	"sort"

	"llm-router-go/types"
)

// ---------------------------------------------------------------------------
// 入站 OpenAI Responses 协议（客户端以 wire_api="responses" 接入，如新版 Codex）
//
// 与 anthropic/gemini 接入同构：ParseInbound 把 Responses 请求归一化为 OpenAI
// Chat 形状（OpenaiFromResponses），响应侧再由 OpenaiToResponsesBody /
// WriteChunksToResponses 逆向还原为 Responses 响应对象 / SSE 事件流。
// ---------------------------------------------------------------------------

// responsesPartToChat 把 Responses 内容块转为 Chat content 块。
func responsesPartToChat(pm map[string]any) (map[string]any, bool) {
	pt, _ := pm["type"].(string)
	switch pt {
	case "input_text", "output_text", "text":
		txt, _ := pm["text"].(string)
		return map[string]any{"type": "text", "text": txt}, true
	case "input_image":
		url, _ := pm["image_url"].(string)
		return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}, true
	case "refusal":
		txt, _ := pm["refusal"].(string)
		return map[string]any{"type": "text", "text": txt}, true
	}
	return nil, false
}

// responsesContentToChat 把 Responses 消息 content（字符串或块数组）转为
// Chat 形状。单一文本块折叠为纯字符串，兼容对 content 结构严格的下游。
func responsesContentToChat(content any) any {
	if s, ok := content.(string); ok {
		return s
	}
	arr := sliceOrEmpty(content)
	var blocks []any
	var texts []string
	for _, p := range arr {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if b, ok := responsesPartToChat(pm); ok {
			blocks = append(blocks, b)
			if t, ok := b["text"].(string); ok {
				texts = append(texts, t)
			}
		}
	}
	if len(blocks) == 1 {
		if _, isText := blocks[0].(map[string]any)["text"]; isText {
			return texts[0]
		}
	}
	if len(blocks) == 0 {
		return ""
	}
	return blocks
}

// OpenaiFromResponses 把 OpenAI Responses 请求体归一化为 OpenAI Chat Completions
// 形状。input 支持字符串与 item 数组（message / function_call /
// function_call_output / reasoning）。
func OpenaiFromResponses(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	req := map[string]any{}
	if v, ok := m["model"].(string); ok && v != "" {
		req["model"] = v
	}
	if b, ok := m["stream"].(bool); ok {
		req["stream"] = b
	}

	msgs := []any{}
	if ins, ok := m["instructions"].(string); ok && ins != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": ins})
	}
	// Chat 规范要求并行工具调用合并在同一条 assistant 消息的 tool_calls 数组里；
	// 此前每个 function_call item 单独成一条 assistant 消息，严格上游会拒绝，
	// 且 fixOrphanToolMessages 按「最近一条 assistant」判定会把部分工具结果
	// 错误降级为 user 文本。这里把连续的 function_call 聚合后再落消息。
	var pendingCalls []any
	flushCalls := func() {
		if len(pendingCalls) == 0 {
			return
		}
		msgs = append(msgs, map[string]any{"role": "assistant", "tool_calls": pendingCalls})
		pendingCalls = nil
	}
	switch input := m["input"].(type) {
	case string:
		if input != "" {
			msgs = append(msgs, map[string]any{"role": "user", "content": input})
		}
	case []any:
		seenSystem := false
		for _, it := range input {
			im, _ := it.(map[string]any)
			if im == nil {
				continue
			}
			t, _ := im["type"].(string)
			if t != "function_call" {
				flushCalls()
			}
			switch t {
			case "function_call":
				name, _ := im["name"].(string)
				args, _ := im["arguments"].(string)
				cid, _ := im["call_id"].(string)
				if cid == "" {
					cid = randID("call_")
				}
				if args == "" {
					args = "{}"
				}
				// 不携带 content 键：部分严格上游（GLM 系）拒绝 content:null 的
				// assistant 消息（[1214] Incorrect role information）
				pendingCalls = append(pendingCalls, map[string]any{
					"id":       cid,
					"type":     "function",
					"function": map[string]any{"name": name, "arguments": args},
				})
			case "function_call_output":
				cid, _ := im["call_id"].(string)
				outStr := ""
				if s, ok := im["output"].(string); ok {
					outStr = s
				} else if im["output"] != nil {
					b, _ := json.Marshal(im["output"])
					outStr = string(b)
				}
				msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": cid, "content": outStr})
			case "reasoning":
				// 思考项不回传上游
			default: // message 或缺省 type
				role, _ := im["role"].(string)
				content := responsesContentToChat(im["content"])
				// 角色白名单归一：developer→system、未知/空→user；
				// system 只允许出现在开头，中途 system 降级为 user
				//（GLM 系要求 system 居首，否则 [1214]）
				switch role {
				case "system":
					if seenSystem {
						role = "user"
					}
				case "developer":
					if seenSystem {
						role = "user"
					} else {
						role = "system"
					}
				case "user", "assistant":
				default:
					role = "user"
				}
				if role == "system" {
					seenSystem = true
				}
				// 空内容且无意义的消息直接丢弃
				if s, ok := content.(string); ok && s == "" && role != "assistant" {
					continue
				}
				msgs = append(msgs, map[string]any{"role": role, "content": content})
			}
		}
	}
	flushCalls()
	msgs = fixOrphanToolMessages(msgs)
	req["messages"] = msgs
	// 必须存 float64：下游所有出站转换对 max_tokens 的读取都是 .(float64) 断言，
	// 存 int 会导致 Responses 接入的输出上限在所有上游上被静默丢弃。
	if v, ok := numVal(m, "max_output_tokens"); ok {
		req["max_tokens"] = float64(v)
	}
	if v, ok := m["temperature"]; ok {
		req["temperature"] = v
	}
	if v, ok := m["top_p"]; ok {
		req["top_p"] = v
	}
	if rs, ok := m["reasoning"].(map[string]any); ok {
		if eff, ok := rs["effort"].(string); ok && eff != "" {
			req["reasoning_effort"] = eff
		}
		// 完整 reasoning 配置（summary/mode/context 等）进 IR 扩展键保真
		types.IRSet(req, types.ExtResponsesReasoning, rs)
	}
	// text.format（结构化输出 json_schema/json_object）进 IR 扩展键
	if txt, ok := m["text"].(map[string]any); ok {
		if f, ok := txt["format"]; ok {
			types.IRSet(req, types.ExtResponsesTextFormat, f)
		}
	}
	// prompt_cache_options（缓存 ttl/mode）进 IR 扩展键
	if v, ok := m["prompt_cache_options"]; ok {
		types.IRSet(req, types.ExtResponsesPromptCacheOptions, v)
	}
	// 2026-09 系列新参数：Responses 与 Chat 共用的会话/缓存/服务层级字段直接落入
	// OpenAI 形状（下游 openai 兼容上游可原样消费；跨型出站按目标格式转换）。
	if v, ok := m["store"]; ok {
		req["store"] = v
	}
	if v, ok := m["service_tier"]; ok {
		req["service_tier"] = v
	}
	if v, ok := m["metadata"]; ok {
		req["metadata"] = v
	}
	if v, ok := m["safety_identifier"]; ok {
		req["safety_identifier"] = v
	}
	if v, ok := m["prompt_cache_key"]; ok {
		req["prompt_cache_key"] = v
	}
	if v, ok := m["max_tool_calls"]; ok {
		req["max_tool_calls"] = v
	}
	// text.verbosity（Responses 输出详细程度）→ Chat 顶层 verbosity
	if txt, ok := m["text"].(map[string]any); ok {
		if vb, ok := txt["verbosity"].(string); ok && vb != "" {
			req["verbosity"] = vb
		}
	}
	// tools：Responses 扁平形状 {type:"function",name,...} → Chat 嵌套形状；
	// web_search 等内置工具类型无法映射为 Chat 工具，丢弃。
	if tools, ok := m["tools"].([]any); ok {
		ct := []any{}
		for _, tl := range tools {
			tm, _ := tl.(map[string]any)
			if tm == nil {
				continue
			}
			if tt, _ := tm["type"].(string); tt == "function" {
				fn := map[string]any{}
				if n, ok := tm["name"].(string); ok {
					fn["name"] = n
				}
				if d, ok := tm["description"].(string); ok {
					fn["description"] = d
				}
				if p, ok := tm["parameters"]; ok {
					fn["parameters"] = p
				}
				ct = append(ct, map[string]any{"type": "function", "function": fn})
			}
		}
		if len(ct) > 0 {
			req["tools"] = ct
		}
	}
	switch tc := m["tool_choice"].(type) {
	case string:
		if tc != "" {
			req["tool_choice"] = tc
		}
	case map[string]any:
		if n, ok := tc["name"].(string); ok {
			req["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": n}}
		}
	}
	if v, ok := m["parallel_tool_calls"].(bool); ok {
		req["parallel_tool_calls"] = v
	}
	return req
}

// fixOrphanToolMessages 保证每条 tool 消息都紧跟在携带对应 tool_calls 的
// assistant 消息之后；不满足时降级为 user 纯文本。严格上游（GLM 系）对孤儿
// tool 消息会报 [1214] Incorrect role information。
func fixOrphanToolMessages(msgs []any) []any {
	out := make([]any, 0, len(msgs))
	pending := map[string]bool{} // 最近一条 assistant 声明的 call_id 集合
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm == nil {
			continue
		}
		role, _ := mm["role"].(string)
		switch role {
		case "assistant":
			pending = map[string]bool{}
			for _, tc := range sliceOrEmpty(mm["tool_calls"]) {
				tcm, _ := tc.(map[string]any)
				if tcm == nil {
					continue
				}
				if id, _ := tcm["id"].(string); id != "" {
					pending[id] = true
				}
			}
			out = append(out, mm)
		case "tool":
			cid, _ := mm["tool_call_id"].(string)
			if cid != "" && pending[cid] {
				delete(pending, cid)
				out = append(out, mm)
				break
			}
			if cid == "" && len(pending) > 0 {
				// 匿名工具消息（无 tool_call_id）：配对一个 pending 调用并消费
				// 一个槽位。此前 delete(pending, "") 是空操作，pending 永不排空，
				// 后续真正的孤儿工具消息无法被识别。
				for k := range pending {
					delete(pending, k)
					break
				}
				out = append(out, mm)
				break
			}
			// 孤儿（cid 无法对应最近 assistant 声明的调用）：降级为 user 纯文本
			out = append(out, toolMsgAsUser(mm))
		default:
			pending = map[string]bool{}
			out = append(out, mm)
		}
	}
	return out
}

// toolMsgAsUser 把无法配对的 tool 消息降级为 user 纯文本消息，
// 内容为块数组时也转为文本而非丢弃（此前只取字符串导致工具输出丢失）。
func toolMsgAsUser(mm map[string]any) map[string]any {
	cid, _ := mm["tool_call_id"].(string)
	return map[string]any{"role": "user", "content": "[工具调用结果 " + cid + "] " + chatContentText(mm["content"])}
}

// chatContentText 提取 Chat 消息 content 的纯文本（用于拼装 output_text）。
func chatContentText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	texts := []string{}
	for _, p := range sliceOrEmpty(content) {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if t, ok := pm["text"].(string); ok {
			texts = append(texts, t)
		}
	}
	out := ""
	for i, t := range texts {
		if i > 0 {
			out += "\n"
		}
		out += t
	}
	return out
}

// OpenaiToResponsesBody 把归一化的 OpenAI Chat 响应还原为 Responses 响应对象。
func OpenaiToResponsesBody(openaiResp map[string]any, model string) map[string]any {
	id, _ := openaiResp["id"].(string)
	if id == "" {
		id = randID("resp_")
	}
	outModel := model
	if m, ok := openaiResp["model"].(string); ok && m != "" {
		outModel = m
	}
	createdAt, _ := numVal(openaiResp, "created")

	output := []any{}
	msgID := randID("msg_")
	fullText := ""
	finish := ""
	if chs := sliceOrEmpty(openaiResp["choices"]); len(chs) > 0 {
		c0, _ := chs[0].(map[string]any)
		if c0 != nil {
			msg, _ := c0["message"].(map[string]any)
			if msg != nil {
				fullText = chatContentText(msg["content"])
				for _, tc := range sliceOrEmpty(msg["tool_calls"]) {
					tcm, _ := tc.(map[string]any)
					if tcm == nil {
						continue
					}
					fn, _ := tcm["function"].(map[string]any)
					name, args := "", "{}"
					if fn != nil {
						if n, ok := fn["name"].(string); ok {
							name = n
						}
						if a, ok := fn["arguments"].(string); ok && a != "" {
							args = a
						}
					}
					cid, _ := tcm["id"].(string)
					if cid == "" {
						cid = randID("call_")
					}
					output = append(output, map[string]any{
						"type": "function_call", "id": randID("fc_"), "call_id": cid,
						"name": name, "arguments": args, "status": "completed",
					})
				}
			}
			fr, _ := c0["finish_reason"].(string)
			finish = fr
		}
	}
	// 文本消息 item 放在最前（与原生 Responses 输出顺序一致）
	output = append([]any{map[string]any{
		"type": "message", "id": msgID, "role": "assistant", "status": "completed",
		"content": []any{map[string]any{"type": "output_text", "text": fullText, "annotations": []any{}}},
	}}, output...)

	status := "completed"
	if finish == "length" {
		status = "incomplete"
	}
	resp := map[string]any{
		"id":         id,
		"object":     "response",
		"created_at": createdAt,
		"status":     status,
		"model":      outModel,
		"output":     output,
		"usage": map[string]any{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0,
			"input_tokens_details":  map[string]any{"cached_tokens": 0},
			"output_tokens_details": map[string]any{"reasoning_tokens": 0}},
	}
	if u, ok := openaiResp["usage"].(map[string]any); ok {
		in, _ := numInt(u, "prompt_tokens")
		out2, _ := numInt(u, "completion_tokens")
		// input/output_tokens_details 是 Codex CLI 的 serde 必填字段（缺了会整条丢弃回复），
		// 上游带 prompt/completion_tokens_details 时取其真实明细，否则填 0
		cached, reasoning := 0, 0
		if d, ok2 := u["prompt_tokens_details"].(map[string]any); ok2 {
			if v, ok3 := numInt(d, "cached_tokens"); ok3 {
				cached = v
			}
		}
		if d, ok2 := u["completion_tokens_details"].(map[string]any); ok2 {
			if v, ok3 := numInt(d, "reasoning_tokens"); ok3 {
				reasoning = v
			}
		}
		resp["usage"] = map[string]any{
			"input_tokens": int(in), "output_tokens": int(out2),
			"total_tokens":          int(in) + int(out2),
			"input_tokens_details":  map[string]any{"cached_tokens": cached},
			"output_tokens_details": map[string]any{"reasoning_tokens": reasoning},
		}
	}
	return resp
}

// responsesSSEWriter 以 Responses 协议事件流写出归一化 chunk，
// 维护 sequence_number 与输出项状态。
type responsesSSEWriter struct {
	w   io.Writer
	err error

	seq       int
	respID    string
	model     string
	started   bool
	msgID     string
	fullText  string
	tools     map[int]*respToolItem
	nextOutIt int // 下一个 output_index（0 固定给 message）

	promptTokens int
	complTokens  int
	// 明细字段：Responses 协议的 usage.input_tokens_details / output_tokens_details
	// 是 Codex CLI 的必填项（serde 反序列化，缺字段直接整条丢弃并报
	// missing field `input_tokens_details`），故即使上游未提供也要回填 0。
	// 上游给得出时（OpenAI 形状的 prompt_tokens_details / completion_tokens_details）取其真实值。
	cachedTokens    int
	reasoningTokens int
	finish          string
	usageSeen       bool
}

type respToolItem struct {
	itemID string
	callID string
	name   string
	args   string
	outIdx int
}

func (rw *responsesSSEWriter) writeString(s string) {
	if rw.err != nil {
		return
	}
	if _, err := io.WriteString(rw.w, s); err != nil {
		rw.err = err
	}
}

// writeEvent 写出一条 Responses SSE 事件（event 行 + data 行），并递增序号。
func (rw *responsesSSEWriter) writeEvent(event string, data map[string]any) {
	data["sequence_number"] = rw.seq
	rw.seq++
	rw.writeString("event: ")
	rw.writeString(event)
	rw.writeString("\ndata: ")
	rw.writeString(jsonDumps(data))
	rw.writeString("\n\n")
	if rw.err == nil {
		if err := maybeFlush(rw.w); err != nil {
			rw.err = err
		}
	}
}

func (rw *responsesSSEWriter) baseResponse(status string) map[string]any {
	return map[string]any{
		"id":         rw.respID,
		"object":     "response",
		"created_at": 0,
		"status":     status,
		"model":      rw.model,
		"output":     []any{},
	}
}

func (rw *responsesSSEWriter) start() {
	if rw.started {
		return
	}
	rw.started = true
	base := rw.baseResponse("in_progress")
	rw.writeEvent("response.created", map[string]any{"type": "response.created", "response": base})
	rw.writeEvent("response.in_progress", map[string]any{"type": "response.in_progress", "response": rw.baseResponse("in_progress")})
	// 消息输出项 + 文本内容块
	rw.writeEvent("response.output_item.added", map[string]any{
		"type": "response.output_item.added", "output_index": 0,
		"item": map[string]any{"id": rw.msgID, "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}},
	})
	rw.writeEvent("response.content_part.added", map[string]any{
		"type": "response.content_part.added", "item_id": rw.msgID,
		"output_index": 0, "content_index": 0,
		"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
	})
}

func (rw *responsesSSEWriter) Write(chunk map[string]any) bool {
	if rw.err != nil {
		return false
	}
	// 上游错误块 → response.failed 事件
	if errM, ok := chunk["error"].(map[string]any); ok {
		msg := jsonDumps(errM)
		if m, ok := errM["message"].(string); ok {
			msg = m
		}
		rw.writeEvent("response.failed", map[string]any{
			"type": "response.failed",
			"response": map[string]any{
				"id": rw.respID, "object": "response", "status": "failed",
				"error": map[string]any{"code": "upstream_error", "message": msg},
			},
		})
		return false
	}
	if m, ok := chunk["model"].(string); ok && m != "" && rw.model == "" {
		rw.model = m
	}
	if usage, ok := chunk["usage"].(map[string]any); ok {
		if v, ok := usage["prompt_tokens"].(float64); ok {
			rw.promptTokens = int(v)
		} else if v, ok := usage["prompt_tokens"].(int); ok {
			rw.promptTokens = v
		}
		if v, ok := usage["completion_tokens"].(float64); ok {
			rw.complTokens = int(v)
		} else if v, ok := usage["completion_tokens"].(int); ok {
			rw.complTokens = v
		}
		// 缓存命中 / 推理 token 明细：回填到 Responses 的 input/output_tokens_details
		if d, ok := usage["prompt_tokens_details"].(map[string]any); ok {
			if v, ok2 := d["cached_tokens"].(float64); ok2 {
				rw.cachedTokens = int(v)
			}
		}
		if d, ok := usage["completion_tokens_details"].(map[string]any); ok {
			if v, ok2 := d["reasoning_tokens"].(float64); ok2 {
				rw.reasoningTokens = int(v)
			}
		}
		rw.usageSeen = true
	}
	chs := sliceOrEmpty(chunk["choices"])
	if len(chs) == 0 {
		return true
	}
	c0, _ := chs[0].(map[string]any)
	if c0 == nil {
		return true
	}
	delta, _ := c0["delta"].(map[string]any)

	if delta != nil {
		if txt, ok := delta["content"].(string); ok && txt != "" {
			rw.start()
			rw.fullText += txt
			rw.writeEvent("response.output_text.delta", map[string]any{
				"type": "response.output_text.delta", "item_id": rw.msgID,
				"output_index": 0, "content_index": 0, "delta": txt,
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
				ti, exists := rw.tools[idx]
				if !exists {
					rw.start()
					ti = &respToolItem{itemID: randID("fc_"), outIdx: rw.nextOutIt}
					rw.nextOutIt++
					if id, ok := tcm["id"].(string); ok && id != "" {
						ti.callID = id
					}
					if fn, ok := tcm["function"].(map[string]any); ok {
						if n, ok := fn["name"].(string); ok {
							ti.name = n
						}
					}
					rw.tools[idx] = ti
					rw.writeEvent("response.output_item.added", map[string]any{
						"type": "response.output_item.added", "output_index": ti.outIdx,
						"item": map[string]any{"id": ti.itemID, "type": "function_call",
							"call_id": ti.callID, "name": ti.name, "arguments": "", "status": "in_progress"},
					})
				} else {
					// 部分上游首块只带 index + 空参数，后续块才补 id/name：
					// 只在首次创建时读取会把这些字段永久丢成 ""
					if ti.callID == "" {
						if id, ok := tcm["id"].(string); ok && id != "" {
							ti.callID = id
						}
					}
					if ti.name == "" {
						if fn, ok := tcm["function"].(map[string]any); ok {
							if n, ok := fn["name"].(string); ok && n != "" {
								ti.name = n
							}
						}
					}
				}
				if fn, ok := tcm["function"].(map[string]any); ok {
					if a, ok := fn["arguments"].(string); ok && a != "" {
						ti.args += a
						rw.writeEvent("response.function_call_arguments.delta", map[string]any{
							"type": "response.function_call_arguments.delta", "item_id": ti.itemID,
							"output_index": ti.outIdx, "delta": a,
						})
					}
				}
			}
		}
	}
	if fr, _ := c0["finish_reason"].(string); fr != "" {
		rw.finish = fr
	}
	return true
}

// Finish 收尾：关闭文本/工具输出项并发出 response.completed。
func (rw *responsesSSEWriter) Finish() {
	if !rw.started {
		// 全程无内容的空流也要有 created/completed 生命周期
		rw.start()
	}
	items := []any{}
	// 关闭文本项
	partDone := map[string]any{"type": "output_text", "text": rw.fullText, "annotations": []any{}}
	rw.writeEvent("response.output_text.done", map[string]any{
		"type": "response.output_text.done", "item_id": rw.msgID,
		"output_index": 0, "content_index": 0, "text": rw.fullText,
	})
	rw.writeEvent("response.content_part.done", map[string]any{
		"type": "response.content_part.done", "item_id": rw.msgID,
		"output_index": 0, "content_index": 0, "part": partDone,
	})
	msgItem := map[string]any{"id": rw.msgID, "type": "message", "role": "assistant",
		"status": "completed", "content": []any{partDone}}
	rw.writeEvent("response.output_item.done", map[string]any{
		"type": "response.output_item.done", "output_index": 0, "item": msgItem,
	})
	items = append(items, msgItem)
	// 关闭工具调用项：按 chunk index 排序遍历（index 不保证从 0 连续，
	// 按 0..len 遍历会让稀疏 index 的工具项永远收不了尾）
	idxs := make([]int, 0, len(rw.tools))
	for k := range rw.tools {
		idxs = append(idxs, k)
	}
	sort.Ints(idxs)
	for _, k := range idxs {
		ti := rw.tools[k]
		rw.writeEvent("response.function_call_arguments.done", map[string]any{
			"type": "response.function_call_arguments.done", "item_id": ti.itemID,
			"output_index": ti.outIdx, "arguments": ti.args,
		})
		item := map[string]any{"id": ti.itemID, "type": "function_call", "call_id": ti.callID,
			"name": ti.name, "arguments": ti.args, "status": "completed"}
		rw.writeEvent("response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": ti.outIdx, "item": item,
		})
		items = append(items, item)
	}
	status := "completed"
	if rw.finish == "length" {
		status = "incomplete"
	}
	final := rw.baseResponse(status)
	final["output"] = items
	// input/output_tokens_details 是 Codex CLI 的 serde 必填字段，缺失会让它整条丢弃
	// 回复（missing field `input_tokens_details`）；上游给了缓存/推理明细就填真实值，否则 0
	usage := map[string]any{"input_tokens": rw.promptTokens, "output_tokens": rw.complTokens,
		"total_tokens":          rw.promptTokens + rw.complTokens,
		"input_tokens_details":  map[string]any{"cached_tokens": rw.cachedTokens},
		"output_tokens_details": map[string]any{"reasoning_tokens": rw.reasoningTokens}}
	final["usage"] = usage
	rw.writeEvent("response.completed", map[string]any{"type": "response.completed", "response": final})
}

// WriteChunksToResponses 消费归一化 chunk 并写出完整 Responses SSE 事件流。
func WriteChunksToResponses(w io.Writer, chunks <-chan map[string]any, model string) (*types.Usage, error) {
	rw := &responsesSSEWriter{w: w, model: model, respID: randID("resp_"), msgID: randID("msg_"), tools: map[int]*respToolItem{}, nextOutIt: 1}
	var lastUsage *types.Usage
	for chunk := range chunks {
		if u := usageFromChunk(chunk); u != nil {
			lastUsage = u
		}
		if !rw.Write(chunk) {
			// 写失败（客户端断连）：停止写出但仍需排空剩余 chunk，
			// 让上游生产者走完并关闭连接，避免 goroutine 泄漏；
			// 上游错误块已透传时 err 为 nil，与原有行为一致
			drainChunks(chunks)
			return lastUsage, rw.err
		}
	}
	rw.Finish()
	return lastUsage, rw.err
}
