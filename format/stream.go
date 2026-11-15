package format

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"llm-router-go/types"
)

// panicToErrorChunk 将转换 goroutine 中的 panic 转化为错误块发往下游，
// 避免静默吞掉后客户端流被无声截断。
func panicToErrorChunk(ch chan map[string]any) {
	if r := recover(); r != nil {
		msg := "stream conversion panic"
		if err, ok := r.(error); ok {
			msg = err.Error()
		} else {
			msg = fmt.Sprintf("stream conversion panic: %v", r)
		}
		select {
		case ch <- openaiErrorChunk(msg):
		default:
		}
	}
}

// ---------------------------------------------------------------------------
// Stream → OpenAI chunks
// ---------------------------------------------------------------------------

// OpenaiStreamToChunks is a pass-through for OpenAI SSE data: each parsed SSE
// event is emitted as a map. If an event contains an "error" key the stream
// emits an error chunk and stops.
func OpenaiStreamToChunks(r io.ReadCloser) <-chan map[string]any {
	ch := make(chan map[string]any, 16)
	go func() {
		defer close(ch)
		defer panicToErrorChunk(ch)
		evts, stopIter := IterSSE(r)
		defer stopIter()
		for evt := range evts {
			if _, hasErr := evt.Data["error"]; hasErr {
				msg := "upstream error"
				if em, ok := evt.Data["error"].(map[string]any); ok {
					if m, ok := em["message"].(string); ok {
						msg = m
					}
				} else if s, ok := evt.Data["error"].(string); ok {
					msg = s
				}
				ch <- openaiErrorChunk(msg)
				// 提前退出前关闭上游 body，解除 IterSSE 的阻塞读取，避免连接泄漏
				r.Close()
				return
			}
			ch <- evt.Data
		}
	}()
	return ch
}

// AnthropicStreamToChunks converts an Anthropic SSE stream to OpenAI-shaped chunks.
func AnthropicStreamToChunks(r io.ReadCloser) <-chan map[string]any {
	ch := make(chan map[string]any, 16)
	go func() {
		defer close(ch)
		defer panicToErrorChunk(ch)

		var (
			model        string
			inputTokens  int
			outputTokens int
			seenStart    bool
			// response_format 降级（json_output 工具）：按块 index 标记，
			// 其 input_json_delta 直接作为 content 增量输出，不发 tool_calls。
			jsonToolIdx = map[int]bool{}
			jsonMode    bool
		)

		emit := func(c map[string]any) { ch <- c }

		evts, stopIter := IterSSE(r)
		defer stopIter()
		for evt := range evts {
			switch evt.Event {
			case "error":
				// 上游错误事件转发为错误块，避免错误流被静默截断
				msg := "upstream error"
				if em, ok := evt.Data["error"].(map[string]any); ok {
					if m, ok := em["message"].(string); ok {
						msg = m
					}
				} else if s, ok := evt.Data["error"].(string); ok {
					msg = s
				}
				ch <- openaiErrorChunk(msg)
				r.Close()
				return
			case "message_start":
				if msg, ok := evt.Data["message"].(map[string]any); ok {
					if m, ok := msg["model"].(string); ok {
						model = m
					}
					if usage, ok := msg["usage"].(map[string]any); ok {
						if v, ok := usage["input_tokens"].(float64); ok {
							inputTokens = int(v)
						}
					}
				}
				if !seenStart {
					seenStart = true
					emit(map[string]any{
						"object": "chat.completion.chunk",
						"model":  model,
						"choices": []any{
							map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}},
						},
					})
				}

			case "content_block_start":
				cb, _ := evt.Data["content_block"].(map[string]any)
				if cb == nil {
					continue
				}
				idx, _ := numFloat(evt.Data, "index")
				cbType, _ := cb["type"].(string)

				if cbType == "tool_use" {
					id, _ := cb["id"].(string)
					if id == "" {
						id = randID("call_")
					}
					name, _ := cb["name"].(string)

					// JSON 降级工具：不向客户端暴露 tool_calls，后续
					// input_json_delta 以 content 增量形态还原。
					if name == jsonModeToolName {
						jsonToolIdx[idx] = true
						jsonMode = true
						continue
					}

					emit(map[string]any{
						"choices": []any{
							map[string]any{
								"index": 0,
								"delta": map[string]any{
									"tool_calls": []any{
										map[string]any{
											"index": idx,
											"id":    id,
											"type":  "function",
											"function": map[string]any{
												"name":      name,
												"arguments": "",
											},
										},
									},
								},
							},
						},
					})
				}

			case "content_block_delta":
				delta, _ := evt.Data["delta"].(map[string]any)
				if delta == nil {
					continue
				}
				idx, _ := numFloat(evt.Data, "index")
				dType, _ := delta["type"].(string)

				if dType == "text_delta" {
					txt, _ := delta["text"].(string)
					emit(map[string]any{
						"choices": []any{
							map[string]any{
								"index": 0,
								"delta": map[string]any{"content": txt},
							},
						},
					})
				} else if dType == "input_json_delta" {
					pj, _ := delta["partial_json"].(string)
					if jsonToolIdx[idx] {
						emit(map[string]any{
							"choices": []any{
								map[string]any{
									"index": 0,
									"delta": map[string]any{"content": pj},
								},
							},
						})
						continue
					}
					emit(map[string]any{
						"choices": []any{
							map[string]any{
								"index": 0,
								"delta": map[string]any{
									"tool_calls": []any{
										map[string]any{
											"index": idx,
											"function": map[string]any{
												"arguments": pj,
											},
										},
									},
								},
							},
						},
					})
				} else if dType == "thinking_delta" {
					// thinking 明文增量：OpenAI 形状无位置，写入 IR 流式扩展键
					//（anthropic 客户端写出时还原为 thinking_delta 事件）
					if td, ok := delta["thinking"].(string); ok {
						emit(map[string]any{types.ExtAnthropicThinkingDelta: td})
					}
				} else if dType == "signature_delta" {
					if sg, ok := delta["signature"].(string); ok {
						emit(map[string]any{types.ExtAnthropicSignatureDelta: sg})
					}
				} else if dType == "citations_delta" {
					// citations 增量（2026-09：text 块在线引用）→ IR 扩展键，
					// anthropic 客户端写出时还原 citations_delta 事件
					if c, ok := delta["citation"]; ok {
						emit(map[string]any{types.ExtAnthropicCitationsDelta: c})
					}
				}

			case "message_delta":
				delta, _ := evt.Data["delta"].(map[string]any)
				if delta != nil {
					if sr, ok := delta["stop_reason"].(string); ok {
						fr := mapStopReason(sr)
						// JSON 降级模式下工具触发的终止不是工具调用，映射回 stop
						if fr == "tool_calls" && jsonMode {
							fr = "stop"
						}
						emit(map[string]any{
							"choices": []any{
								map[string]any{
									"index":         0,
									"finish_reason": fr,
									"delta":         map[string]any{},
								},
							},
						})
					}
				}
				if usage, ok := evt.Data["usage"].(map[string]any); ok {
					if v, ok := usage["output_tokens"].(float64); ok {
						outputTokens = int(v)
					}
				}

			case "message_stop":
				u := map[string]any{}
				if inputTokens > 0 {
					u["prompt_tokens"] = inputTokens
				}
				if outputTokens > 0 {
					u["completion_tokens"] = outputTokens
				}
				if pt, ck := u["prompt_tokens"]; ck {
					if ct, ck2 := u["completion_tokens"]; ck2 {
						u["total_tokens"] = pt.(int) + ct.(int)
					}
				}
				if len(u) > 0 {
					chunk := map[string]any{"usage": u}
					chunk["choices"] = []any{}
					emit(chunk)
				}
			}
		}
	}()
	return ch
}

// GeminiStreamToChunks converts a Gemini SSE stream to OpenAI-shaped chunks.
// 同时兼容两种上游：
//   - legacy generateContent / streamGenerateContent（candidates/usageMetadata）；
//   - Interactions API 流式事件（event_type=interaction.created|step.start|...）。
//
// 按事件负载自动识别（interactions 事件带 event_type 字段）。
func GeminiStreamToChunks(r io.ReadCloser) <-chan map[string]any {
	ch := make(chan map[string]any, 16)
	go func() {
		defer close(ch)
		defer panicToErrorChunk(ch)

		var (
			inter         geminiInteractionsStream
			detectedInter bool

			seenStart     bool
			seenFinish    bool
			promptTokens  int
			candTokens    int
			cachedTokens  int
			thoughtTokens int
			// Gemini 流式 functionCall 可能跨 chunk 重复（增量），按 id 维持稳定的 index
			callIdx      = map[string]int{}
			lastCallArgs = map[string]string{}
			nextToolIdx  int
		)

		emit := func(c map[string]any) { ch <- c }

		evts, stopIter := IterSSE(r)
		defer stopIter()
		for evt := range evts {
			// --- Interactions API 事件识别 ---
			if et, ok := evt.Data["event_type"].(string); ok && et != "" {
				detectedInter = true
				if !inter.handle(ch, evt.Data, et) {
					r.Close()
					return
				}
				continue
			}

			if detectedInter {
				// 已进入 interactions 模式的流，后续事件也应带 event_type；
				// 缺 event_type 的裸 data 行忽略，避免误入 legacy 分支。
				continue
			}
			// --- 上游错误事件转发为错误块 ---
			if _, hasErr := evt.Data["error"]; hasErr {
				msg := "upstream error"
				if em, ok := evt.Data["error"].(map[string]any); ok {
					if m, ok := em["message"].(string); ok {
						msg = m
					}
				} else if s, ok := evt.Data["error"].(string); ok {
					msg = s
				}
				ch <- openaiErrorChunk(msg)
				r.Close()
				return
			}
			candidates, _ := evt.Data["candidates"].([]any)

			// --- usage metadata ---
			if um, ok := evt.Data["usageMetadata"].(map[string]any); ok {
				if v, ok := um["promptTokenCount"].(float64); ok {
					promptTokens = int(v)
				}
				if v, ok := um["candidatesTokenCount"].(float64); ok {
					candTokens = int(v)
				}
				if v, ok := um["cachedContentTokenCount"].(float64); ok {
					cachedTokens = int(v)
				}
				if v, ok := um["thoughtsTokenCount"].(float64); ok {
					thoughtTokens = int(v)
				}
			}

			// --- initial role chunk ---
			if !seenStart && len(candidates) > 0 {
				seenStart = true
				emit(map[string]any{
					"object": "chat.completion.chunk",
					"choices": []any{
						map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}},
					},
				})
			}

			// --- process candidate content ---
			if len(candidates) == 0 {
				continue
			}
			c0, _ := candidates[0].(map[string]any)
			if c0 == nil {
				continue
			}

			content, _ := c0["content"].(map[string]any)
			// "parts" 是 JSON 数组（[]any），不能用 getMapVal（它要求值必须能强转为
			// map[string]any）。用 getMapVal 会把内容块里的文本/工具调用全部丢掉，
			// 导致 Gemini 流式响应只输出 role 起始块和结尾 usage，正文永远为空。
			parts := sliceOrEmpty(content["parts"])

			var fullText string
			for _, p := range parts {
				pm, _ := p.(map[string]any)
				if pm == nil {
					continue
				}
				// functionCall
				if fc, ok := pm["functionCall"].(map[string]any); ok {
					name, _ := fc["name"].(string)
					args, _ := fc["args"].(map[string]any)
					if args == nil {
						args = map[string]any{}
					}
					argsB, _ := json.Marshal(args)
					id, _ := fc["id"].(string)
					if id == "" {
						id = randID("call_")
					}
					idx, ok := callIdx[id]
					if !ok {
						idx = nextToolIdx
						nextToolIdx++
						callIdx[id] = idx
						lastCallArgs[id] = string(argsB)
					} else if last, seen := lastCallArgs[id]; seen && last == string(argsB) {
						// Gemini 流式 functionCall 可能跨 chunk 全量重复：客户端按
						// index 累积 arguments 字符串，重复全量下发会拼出损坏的
						// JSON。与上次完全相同则跳过本事件
						continue
					} else {
						lastCallArgs[id] = string(argsB)
					}
					tcEntry := map[string]any{
						"index": idx,
						"id":    id,
						"type":  "function",
						"function": map[string]any{
							"name":      name,
							"arguments": string(argsB),
						},
					}
					// thoughtSignature（Gemini 3 思维签名）随工具调用分片透传，
					// 客户端回传历史时由 ToGemini 回填（仅原生 Gemini 上游）
					if sig, ok := pm["thoughtSignature"].(string); ok && sig != "" {
						tcEntry["thought_signature"] = sig
					}
					emit(map[string]any{
						"choices": []any{
							map[string]any{
								"index": 0,
								"delta": map[string]any{
									"tool_calls": []any{tcEntry},
								},
							},
						},
					})
				}
				// text
				if txt, ok := pm["text"].(string); ok {
					fullText += txt
				}
			}

			// Emit text delta。
			// Gemini streamGenerateContent 的每个 chunk 只携带新增文本（增量，非
			// 累积全文），直接作为 delta 发出。此前 fullText[len(prevText):] 的差分
			// 前提是 fullText 为累积全文，但它只是本事件的增量：会导致文本被截断，
			// 且增量短于 prevText 时 slice 越界 panic（recover 吞掉后流静默中断）。
			if fullText != "" {
				emit(map[string]any{
					"choices": []any{
						map[string]any{
							"index": 0,
							"delta": map[string]any{"content": fullText},
						},
					},
				})
			}

			// --- finish reason ---
			if !seenFinish {
				fr, _ := c0["finishReason"].(string)
				if fr != "" {
					seenFinish = true
					mapped := mapGeminiFinish(fr)
					// 出现过工具调用则覆盖为 tool_calls：Gemini 的 STOP 收尾对
					// OpenAI 客户端意味着「正常结束」，工具循环不会触发
					if len(callIdx) > 0 {
						mapped = "tool_calls"
					}
					emit(map[string]any{
						"choices": []any{
							map[string]any{
								"index":         0,
								"finish_reason": mapped,
								"delta":         map[string]any{},
							},
						},
					})
				}
			}
		}

		// --- final usage chunk if not already emitted ---
		if promptTokens > 0 || candTokens > 0 {
			u := map[string]any{}
			if promptTokens > 0 {
				u["prompt_tokens"] = promptTokens
			}
			if candTokens > 0 {
				u["completion_tokens"] = candTokens
			}
			// usage 明细：Gemini 流式 usageMetadata 携带的缓存命中 / 思考 token
			if cachedTokens > 0 {
				u["prompt_tokens_details"] = map[string]any{"cached_tokens": cachedTokens}
			}
			if thoughtTokens > 0 {
				u["completion_tokens_details"] = map[string]any{"reasoning_tokens": thoughtTokens}
			}
			if pt, ok := u["prompt_tokens"]; ok {
				if ct, ok2 := u["completion_tokens"]; ok2 {
					u["total_tokens"] = pt.(int) + ct.(int)
				}
			}
			if len(u) > 0 {
				chunk := map[string]any{"usage": u}
				chunk["choices"] = []any{}
				emit(chunk)
			}
		}
	}()
	return ch
}

// ResponsesStreamToChunks converts an OpenAI Responses SSE stream to OpenAI Chat chunks.
func ResponsesStreamToChunks(r io.ReadCloser) <-chan map[string]any {
	ch := make(chan map[string]any, 16)
	go func() {
		defer close(ch)
		defer panicToErrorChunk(ch)

		var (
			seenStart    bool
			toolIndex    int
			hasToolCall  bool
			toolAccID    = map[int]string{}
			promptTokens int
			outputTokens int
			// output_index 是所有输出项（message、function_call 等）的序号，
			// 不等于 tool_calls 的 index，需要显式映射，否则混合输出时参数错位
			outputIdxToTool = map[int]int{}
		)

		emit := func(c map[string]any) { ch <- c }

		evts, stopIter := IterSSE(r)
		defer stopIter()
		for evt := range evts {
			switch evt.Event {
			case "error":
				// 上游错误事件转发为错误块，避免错误流被静默截断
				msg := "upstream error"
				if em, ok := evt.Data["error"].(map[string]any); ok {
					if m, ok := em["message"].(string); ok {
						msg = m
					}
				} else if s, ok := evt.Data["error"].(string); ok {
					msg = s
				}
				ch <- openaiErrorChunk(msg)
				r.Close()
				return
			case "response.output_item.added":
				item, _ := evt.Data["item"].(map[string]any)
				if item == nil {
					continue
				}
				it, _ := item["type"].(string)
				if it != "function_call" {
					continue
				}
				if !seenStart {
					seenStart = true
					emit(map[string]any{
						"object": "chat.completion.chunk",
						"choices": []any{
							map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}},
						},
					})
				}
				id, _ := item["call_id"].(string)
				if id == "" {
					id = randID("call_")
				}
				hasToolCall = true
				name, _ := item["name"].(string)
				toolAccID[toolIndex] = id
				if oi, ok := numFloat(evt.Data, "output_index"); ok {
					outputIdxToTool[oi] = toolIndex
				}

				emit(map[string]any{
					"choices": []any{
						map[string]any{
							"index": 0,
							"delta": map[string]any{
								"tool_calls": []any{
									map[string]any{
										"index": toolIndex,
										"id":    id,
										"type":  "function",
										"function": map[string]any{
											"name":      name,
											"arguments": "",
										},
									},
								},
							},
						},
					},
				})
				toolIndex++

			case "response.output_text.delta":
				if !seenStart {
					seenStart = true
					emit(map[string]any{
						"object": "chat.completion.chunk",
						"choices": []any{
							map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}},
						},
					})
				}
				delta, _ := evt.Data["delta"].(string)
				emit(map[string]any{
					"choices": []any{
						map[string]any{
							"index": 0,
							"delta": map[string]any{"content": delta},
						},
					},
				})

			case "response.function_call_arguments.delta":
				delta, _ := evt.Data["delta"].(string)
				// 优先用 output_index → tool index 映射（added 事件时登记）；
				// 找不到再按 call_id 反查；最后回退到最近一个工具调用。
				idx := -1
				if oi, ok := evt.Data["output_index"].(float64); ok {
					if t, ok2 := outputIdxToTool[int(oi)]; ok2 {
						idx = t
					}
				}
				if idx < 0 {
					if ci, ok := evt.Data["call_id"].(string); ok && ci != "" {
						for k, v := range toolAccID {
							if v == ci {
								idx = k
								break
							}
						}
					}
				}
				if idx < 0 {
					idx = toolIndex - 1
					if idx < 0 {
						idx = 0
					}
				}
				emit(map[string]any{
					"choices": []any{
						map[string]any{
							"index": 0,
							"delta": map[string]any{
								"tool_calls": []any{
									map[string]any{
										"index": idx,
										"function": map[string]any{
											"arguments": delta,
										},
									},
								},
							},
						},
					},
				})

			case "response.completed":
				resp, _ := evt.Data["response"].(map[string]any)
				if resp != nil {
					if usage, ok := resp["usage"].(map[string]any); ok {
						if v, ok := usage["input_tokens"].(float64); ok {
							promptTokens = int(v)
						}
						if v, ok := usage["output_tokens"].(float64); ok {
							outputTokens = int(v)
						}
					}
				}
				emit(map[string]any{
					"choices": []any{
						map[string]any{
							"index": 0,
							// 含 function_call 输出时必须返回 tool_calls，否则
							// agent 类客户端不会进入工具执行循环（OpenAI 语义）
							"finish_reason": finishReasonFor(hasToolCall, false),
							"delta":         map[string]any{},
						},
					},
				})
				// usage
				if promptTokens > 0 || outputTokens > 0 {
					u := map[string]any{}
					if promptTokens > 0 {
						u["prompt_tokens"] = promptTokens
					}
					if outputTokens > 0 {
						u["completion_tokens"] = outputTokens
					}
					if pt, ok := u["prompt_tokens"]; ok {
						if ct, ok2 := u["completion_tokens"]; ok2 {
							u["total_tokens"] = pt.(int) + ct.(int)
						}
					}
					if len(u) > 0 {
						emit(map[string]any{"usage": u, "choices": []any{}})
					}
				}

			case "response.incomplete":
				// max_tokens 截断属于正常结束，以 finish_reason=length 收尾而非报错
				// response.incomplete 事件同样携带 response.usage（截断场景往往
				// 是唯一的 usage 来源），此前遗漏导致截断响应的 token 统计全为 0。
				if resp, ok := evt.Data["response"].(map[string]any); ok {
					if usage, ok2 := resp["usage"].(map[string]any); ok2 {
						if v, ok3 := usage["input_tokens"].(float64); ok3 {
							promptTokens = int(v)
						}
						if v, ok3 := usage["output_tokens"].(float64); ok3 {
							outputTokens = int(v)
						}
					}
				}
				emit(map[string]any{
					"choices": []any{
						map[string]any{"index": 0, "finish_reason": finishReasonFor(hasToolCall, true), "delta": map[string]any{}},
					},
				})
				if promptTokens > 0 || outputTokens > 0 {
					u := map[string]any{}
					if promptTokens > 0 {
						u["prompt_tokens"] = promptTokens
					}
					if outputTokens > 0 {
						u["completion_tokens"] = outputTokens
					}
					if pt, ok := u["prompt_tokens"]; ok {
						if ct, ok2 := u["completion_tokens"]; ok2 {
							u["total_tokens"] = pt.(int) + ct.(int)
						}
					}
					if len(u) > 0 {
						emit(map[string]any{"usage": u, "choices": []any{}})
					}
				}
				// 提前退出前关闭上游 body，解除 IterSSE 的阻塞读取，避免连接泄漏
				r.Close()
				return

			case "response.failed":
				emit(openaiErrorChunk("upstream response failed"))
				r.Close()
				return
			}
		}
	}()
	return ch
}

// ---------------------------------------------------------------------------
// OpenAI chunks → downstream SSE strings (incremental writers)
// ---------------------------------------------------------------------------

// chunkFlusher is implemented by writers that can flush after each SSE event
// (e.g. an http.ResponseWriter-backed adapter in the router).
type chunkFlusher interface{ Flush() error }

// maybeFlush flushes after each event when the target writer supports it.
func maybeFlush(w io.Writer) error {
	if f, ok := w.(chunkFlusher); ok {
		return f.Flush()
	}
	return nil
}

// drainChunks 在消费者提前退出（下游写失败/上游错误块）时排空剩余 chunk，
// 让 IterSSE 生产者不被阻塞在 ch <- 上，从而能走完 defer body.Close()
// 关闭上游连接；否则 goroutine 与上游连接都会永久泄漏。
func drainChunks(chunks <-chan map[string]any) {
	for range chunks {
	}
}

// WriteChunksToOpenai consumes OpenAI-shaped chunks and writes a complete SSE
// stream (including the final "data: [DONE]\n\n") incrementally to w, flushing
// after each event when supported. It returns the last usage chunk seen.
func WriteChunksToOpenai(w io.Writer, chunks <-chan map[string]any, model string) (*types.Usage, error) {
	var lastUsage *types.Usage
	// 同一响应的全部分片共用 id/created（与真实 OpenAI 一致）：格式互译产生的
	// 增量 chunk 只带 choices，严格反序列化的客户端（grokcli 等 serde 实现）会因
	// 缺顶层 id 整条丢弃并报 missing field `id`（非流式见 handleNonStream）。
	chunkID := randID("chatcmpl-")
	created := time.Now().Unix()
	for chunk := range chunks {
		// 上游 SSE 里的 `data: null` 解析出来是 nil map（Unmarshal 成功但无内容）：
		// 后面 `chunk["model"] = model` 对 nil map 赋值会 panic，直接跳过。
		if chunk == nil {
			continue
		}
		// 剥离 IR 扩展键：OpenAI 兼容客户端不识别 _ext_* 内部字段，
		// 原样输出会污染 wire（thinkng 明文等仅供内部还原用）。
		types.IRDropAll(chunk)
		// Inject model if missing from the chunk
		if _, ok := chunk["model"]; !ok && model != "" {
			chunk["model"] = model
		}
		ensureOpenAIChunkFields(chunk, chunkID, created)
		if u := usageFromChunk(chunk); u != nil {
			lastUsage = u
		}
		if _, err := io.WriteString(w, "data: "); err != nil {
			drainChunks(chunks)
			return lastUsage, err
		}
		if _, err := io.WriteString(w, jsonDumps(chunk)); err != nil {
			drainChunks(chunks)
			return lastUsage, err
		}
		if _, err := io.WriteString(w, "\n\n"); err != nil {
			drainChunks(chunks)
			return lastUsage, err
		}
		if err := maybeFlush(w); err != nil {
			drainChunks(chunks)
			return lastUsage, err
		}
	}
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return lastUsage, err
	}
	return lastUsage, maybeFlush(w)
}

// ensureOpenAIChunkFields 为 chunk 补齐 OpenAI 流式响应的必需顶层字段
// （id / object / created）。错误块（顶层 error）不补，保持原样让客户端直接报错。
func ensureOpenAIChunkFields(m map[string]any, id string, created int64) {
	if _, ok := m["error"]; ok {
		return
	}
	if _, ok := m["id"]; !ok {
		m["id"] = id
	}
	if _, ok := m["object"]; !ok {
		m["object"] = "chat.completion.chunk"
	}
	if _, ok := m["created"]; !ok {
		m["created"] = created
	}
}

// usageFromChunk extracts a *types.Usage from an OpenAI-shaped chunk.
func usageFromChunk(chunk map[string]any) *types.Usage {
	usage, ok := chunk["usage"].(map[string]any)
	if !ok {
		return nil
	}
	var pt, ct *int
	if v, ok := usage["prompt_tokens"].(float64); ok {
		n := int(v)
		pt = &n
	} else if v, ok := usage["prompt_tokens"].(int); ok {
		pt = &v
	}
	if v, ok := usage["completion_tokens"].(float64); ok {
		n := int(v)
		ct = &n
	} else if v, ok := usage["completion_tokens"].(int); ok {
		ct = &v
	}
	if pt == nil && ct == nil {
		return nil
	}
	return &types.Usage{PromptTokens: pt, CompletionTokens: ct}
}

// anthropicSSEWriter streams OpenAI-shaped chunks as Anthropic SSE events,
// emitting each event immediately (message_delta is deferred until usage or
// stream end, matching the buffered implementation).
type anthropicSSEWriter struct {
	w     io.Writer
	model string
	err   error

	msgID   string
	started bool

	inputTokens      int
	outputTokens     int
	finishReason     string
	usageSeen        bool
	messageDeltaSent bool

	textBlockIdx   int
	textBlockOpen  bool
	thinkBlockIdx  int
	thinkBlockOpen bool
	toolBlocks     map[int]*toolBlock
	nextCBIdx      int
	openBlocks     map[int]bool
}

type toolBlock struct {
	id           string
	name         string
	args         string
	started      bool
	anthropicIdx int
}

func (aw *anthropicSSEWriter) writeString(s string) {
	if aw.err != nil {
		return
	}
	if _, err := io.WriteString(aw.w, s); err != nil {
		aw.err = err
	}
}

func (aw *anthropicSSEWriter) writeSSE(event string, data map[string]any) {
	if event != "" {
		aw.writeString("event: ")
		aw.writeString(event)
		aw.writeString("\n")
	}
	aw.writeString("data: ")
	aw.writeString(jsonDumps(data))
	aw.writeString("\n\n")
	if aw.err == nil {
		if err := maybeFlush(aw.w); err != nil {
			aw.err = err
		}
	}
}

func (aw *anthropicSSEWriter) closeBlock(idx int) {
	if !aw.openBlocks[idx] {
		return
	}
	aw.openBlocks[idx] = false
	aw.writeSSE("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	})
}

func (aw *anthropicSSEWriter) closeAllBlocks() {
	// Close in reverse to be safe
	for i := aw.nextCBIdx - 1; i >= 0; i-- {
		aw.closeBlock(i)
	}
}

// openThinkBlock 惰性开启 thinking 内容块（首次收到 thinking/signature 增量时）。
// Anthropic 协议要求块顺序、不可重叠：thinking 块先于 text/tool_use 块出现，
// 若文本块已开启则先关闭（与 tool_use 块的处理一致）。
func (aw *anthropicSSEWriter) openThinkBlock() {
	if aw.thinkBlockOpen {
		return
	}
	if aw.textBlockOpen {
		aw.closeBlock(aw.textBlockIdx)
		aw.textBlockOpen = false
		aw.textBlockIdx = -1
	}
	aw.thinkBlockIdx = aw.nextCBIdx
	aw.nextCBIdx++
	aw.thinkBlockOpen = true
	aw.openBlocks[aw.thinkBlockIdx] = true
	aw.writeSSE("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": aw.thinkBlockIdx,
		"content_block": map[string]any{
			"type":     "thinking",
			"thinking": "",
		},
	})
}

// OpenAI 流式规范中 usage chunk 在 finish chunk 之后才到达，因此
// message_delta 延迟到 usage 到达（或流结束兜底）时才发出，
// 否则输出时 outputTokens 还是 0，客户端永远收不到 token 统计。
func (aw *anthropicSSEWriter) emitMessageDelta() {
	if aw.messageDeltaSent || aw.finishReason == "" {
		return
	}
	aw.messageDeltaSent = true
	md := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   mapOpenAIStopToAnthropic(aw.finishReason),
			"stop_sequence": nil,
		},
	}
	// message_start 发出时上游 usage 通常尚未到达，input_tokens 恒为 0；
	// Anthropic 允许在收尾的 message_delta.usage 里补带最终确定的 input_tokens
	if aw.outputTokens > 0 || aw.inputTokens > 0 {
		u := map[string]any{"output_tokens": aw.outputTokens}
		if aw.inputTokens > 0 {
			u["input_tokens"] = aw.inputTokens
		}
		md["usage"] = u
	}
	aw.writeSSE("message_delta", md)
}

// Write processes a single OpenAI-shaped chunk. Returns false if the stream
// must stop immediately (e.g. an upstream error chunk was forwarded).
func (aw *anthropicSSEWriter) Write(chunk map[string]any) bool {
	// 客户端已断连（前一次写入失败）：立即停止，交由外层提前结束
	if aw.err != nil {
		return false
	}

	// --- 上游错误块透传（错误块没有 choices，不能静默跳过） ---
	if errM, ok := chunk["error"].(map[string]any); ok {
		aw.writeSSE("error", map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "api_error", "message": errMsgText(errM)},
		})
		return false
	}

	// --- usage ---
	if usage, ok := chunk["usage"].(map[string]any); ok {
		if v, ok := usage["prompt_tokens"].(float64); ok {
			aw.inputTokens = int(v)
		} else if v, ok := usage["prompt_tokens"].(int); ok {
			aw.inputTokens = v
		}
		if v, ok := usage["completion_tokens"].(float64); ok {
			aw.outputTokens = int(v)
		} else if v, ok := usage["completion_tokens"].(int); ok {
			aw.outputTokens = v
		}
		// usage 已到达；若 finish 已先到，立即补发带 token 的 message_delta
		aw.usageSeen = true
		if aw.finishReason != "" {
			aw.emitMessageDelta()
		}
	}

	// --- thinking / signature 增量（IR 流式扩展键，Anthropic 上游 thinking 明文保真） ---
	if td, ok := chunk[types.ExtAnthropicThinkingDelta].(string); ok && td != "" {
		aw.openThinkBlock()
		aw.writeSSE("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": aw.thinkBlockIdx,
			"delta": map[string]any{"type": "thinking_delta", "thinking": td},
		})
	}
	if sg, ok := chunk[types.ExtAnthropicSignatureDelta].(string); ok && sg != "" {
		aw.openThinkBlock()
		aw.writeSSE("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": aw.thinkBlockIdx,
			"delta": map[string]any{"type": "signature_delta", "signature": sg},
		})
	}
	// citations 增量：Anthropic 协议把引用挂在 text 块上，先确保 text 块开启
	if c, ok := chunk[types.ExtAnthropicCitationsDelta]; ok && c != nil {
		if aw.textBlockIdx < 0 {
			if aw.thinkBlockOpen {
				aw.closeBlock(aw.thinkBlockIdx)
				aw.thinkBlockOpen = false
				aw.thinkBlockIdx = -1
			}
			aw.textBlockIdx = aw.nextCBIdx
			aw.nextCBIdx++
		}
		if !aw.textBlockOpen {
			aw.textBlockOpen = true
			aw.openBlocks[aw.textBlockIdx] = true
			aw.writeSSE("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": aw.textBlockIdx,
				"content_block": map[string]any{
					"type": "text",
					"text": "",
				},
			})
		}
		aw.writeSSE("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": aw.textBlockIdx,
			"delta": map[string]any{"type": "citations_delta", "citation": c},
		})
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

	// --- first chunk with model → message_start ---
	if m, ok := chunk["model"].(string); ok && m != "" && aw.model == "" {
		aw.model = m
	}
	if !aw.started {
		// Emit message_start before the first content
		aw.started = true
		aw.writeSSE("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      aw.msgID,
				"type":    "message",
				"role":    "assistant",
				"model":   aw.model,
				"content": []any{},
				"usage": map[string]any{
					"input_tokens": aw.inputTokens,
				},
			},
		})
	}

	// --- text delta ---
	if delta != nil {
		if txt, ok := delta["content"].(string); ok {
			if aw.textBlockIdx < 0 {
				// Anthropic 协议要求内容块不可重叠：开启新 text 块前
				// 先关闭仍打开的 thinking 块（与 openThinkBlock 关闭 text 块对称）
				if aw.thinkBlockOpen {
					aw.closeBlock(aw.thinkBlockIdx)
					aw.thinkBlockOpen = false
					aw.thinkBlockIdx = -1
				}
				aw.textBlockIdx = aw.nextCBIdx
				aw.nextCBIdx++
			}
			if !aw.textBlockOpen {
				aw.textBlockOpen = true
				aw.openBlocks[aw.textBlockIdx] = true
				aw.writeSSE("content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": aw.textBlockIdx,
					"content_block": map[string]any{
						"type": "text",
						"text": "",
					},
				})
			}
			aw.writeSSE("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": aw.textBlockIdx,
				"delta": map[string]any{
					"type": "text_delta",
					"text": txt,
				},
			})
		}

		// --- tool_calls delta ---
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
				tb, exists := aw.toolBlocks[idx]
				if !exists {
					tb = &toolBlock{anthropicIdx: aw.nextCBIdx}
					aw.nextCBIdx++
					aw.toolBlocks[idx] = tb
				}

				// id / name → start tool_use block
				if id, ok := tcm["id"].(string); ok && id != "" && !tb.started {
					// Anthropic 协议要求内容块顺序、不可重叠：开始 tool_use 块前，
					// 若文本块或 thinking 块仍开着先关闭它们，否则严格客户端会拒绝非法 SSE。
					if aw.textBlockOpen {
						aw.closeBlock(aw.textBlockIdx)
						aw.textBlockOpen = false
						aw.textBlockIdx = -1
					}
					if aw.thinkBlockOpen {
						aw.closeBlock(aw.thinkBlockIdx)
						aw.thinkBlockOpen = false
						aw.thinkBlockIdx = -1
					}
					tb.id = id
					tb.started = true
					aw.openBlocks[tb.anthropicIdx] = true
					fn, _ := tcm["function"].(map[string]any)
					if fn != nil {
						if n, ok := fn["name"].(string); ok {
							tb.name = n
						}
					}
					aw.writeSSE("content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": tb.anthropicIdx,
						"content_block": map[string]any{
							"type":  "tool_use",
							"id":    tb.id,
							"name":  tb.name,
							"input": map[string]any{},
						},
					})
				}

				// arguments delta
				if fn, ok := tcm["function"].(map[string]any); ok {
					if args, ok := fn["arguments"].(string); ok && args != "" {
						tb.args += args
						aw.writeSSE("content_block_delta", map[string]any{
							"type":  "content_block_delta",
							"index": tb.anthropicIdx,
							"delta": map[string]any{
								"type":         "input_json_delta",
								"partial_json": args,
							},
						})
					}
				}
			}
		}
	}

	// --- finish reason ---
	if fr != "" {
		aw.finishReason = fr
		aw.closeAllBlocks()
		// usage chunk 在 finish 之后才到达，延迟发 message_delta，
		// 待 usage 到达或流结束兜底时由 emitMessageDelta 发出。
		if aw.usageSeen {
			aw.emitMessageDelta()
		}
	}
	return true
}

// Finish emits trailing events: close any remaining open blocks and message_stop.
func (aw *anthropicSSEWriter) Finish() {
	aw.closeAllBlocks()
	// 流结束兜底：若 usage 始终未到达，至少用当前已统计的 token 发出 message_delta
	aw.emitMessageDelta()
	// 只有真正发出过 message_start 才收尾 message_stop，避免空流/错误流产生孤儿事件
	if aw.started {
		aw.writeSSE("message_stop", map[string]any{"type": "message_stop"})
	}
}

// WriteChunksToAnthropic consumes OpenAI-shaped chunks and writes a complete
// Anthropic SSE stream incrementally to w. Returns the last usage chunk seen.
func WriteChunksToAnthropic(w io.Writer, chunks <-chan map[string]any, model string) (*types.Usage, error) {
	aw := &anthropicSSEWriter{
		w:            w,
		model:        model,
		msgID:        randID("msg_"),
		toolBlocks:   map[int]*toolBlock{},
		openBlocks:   map[int]bool{},
		textBlockIdx: -1,
	}
	var lastUsage *types.Usage
	for chunk := range chunks {
		if chunk == nil { // data: null：nil map 直接跳过
			continue
		}
		if u := usageFromChunk(chunk); u != nil {
			lastUsage = u
		}
		if !aw.Write(chunk) {
			// 写失败（客户端断连）：停止写出但仍需排空剩余 chunk，
			// 让上游生产者走完并关闭连接，避免 goroutine 泄漏
			drainChunks(chunks)
			return lastUsage, aw.err
		}
	}
	aw.Finish()
	return lastUsage, aw.err
}

// geminiSSEWriter streams OpenAI-shaped chunks as Gemini alt=sse events.
// Text deltas are emitted per chunk; tool calls, finish reason and usage
// metadata are emitted once complete at stream end (native Gemini behavior).
type geminiSSEWriter struct {
	w   io.Writer
	err error

	accumText     string
	haveToolCalls bool
	toolCalls     []map[string]any // accumulated tool calls with full args
	// toolIdxMap 把上游可能稀疏的工具序号（如 Anthropic 内容块序号：文本块在前
	// 时首个工具 index=1）映射为密集槽位，避免空洞占位生成空名字的幽灵工具调用
	toolIdxMap   map[int]int
	finishReason string
	promptTokens int
	outputTokens int
	finished     bool
}

func (gw *geminiSSEWriter) writeString(s string) {
	if gw.err != nil {
		return
	}
	if _, err := io.WriteString(gw.w, s); err != nil {
		gw.err = err
	}
}

func (gw *geminiSSEWriter) writeData(data map[string]any) {
	gw.writeString("data: ")
	gw.writeString(jsonDumps(data))
	gw.writeString("\n\n")
	if gw.err == nil {
		if err := maybeFlush(gw.w); err != nil {
			gw.err = err
		}
	}
}

// Write processes a single OpenAI-shaped chunk. Returns false if the stream
// must stop immediately (upstream error chunk forwarded).
func (gw *geminiSSEWriter) Write(chunk map[string]any) bool {
	// 客户端已断连（前一次写入失败）：立即停止，交由外层提前结束
	if gw.err != nil {
		return false
	}

	// --- 上游错误块透传 ---
	if errM, ok := chunk["error"].(map[string]any); ok {
		gw.writeData(map[string]any{
			"error": map[string]any{"code": 500, "message": errMsgText(errM), "status": "INTERNAL"},
		})
		return false
	}

	// usage
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
		// text content：逐块输出增量文本
		if txt, ok := delta["content"].(string); ok && txt != "" {
			gw.accumText += txt
			gw.writeData(map[string]any{
				"candidates": []any{
					map[string]any{
						"content": map[string]any{
							"role":  "model",
							"parts": []any{map[string]any{"text": txt}},
						},
					},
				},
			})
		}

		// tool calls（累积参数，流结束时统一输出）
		if tcs, ok := delta["tool_calls"].([]any); ok {
			for _, tc := range tcs {
				tcm, _ := tc.(map[string]any)
				if tcm == nil {
					continue
				}
				rawIdx := 0
				if v, ok := numFloatAny(tcm, "index"); ok {
					rawIdx = v
				}
				// 稀疏序号（Anthropic 内容块 index 等）→ 密集槽位：
				// 直接用稀疏序号做切片下标会在文本在前时留空洞，Finish 会把
				// 空洞输出成 name="" 的幽灵 functionCall，客户端误调用空函数。
				idx, ok := gw.toolIdxMap[rawIdx]
				if !ok {
					if gw.toolIdxMap == nil {
						gw.toolIdxMap = map[int]int{}
					}
					idx = len(gw.toolCalls)
					gw.toolIdxMap[rawIdx] = idx
				}
				// Extend slice if needed
				for len(gw.toolCalls) <= idx {
					gw.toolCalls = append(gw.toolCalls, map[string]any{
						"name": "",
						"args": "",
					})
				}
				if id, ok := tcm["id"].(string); ok && id != "" {
					gw.toolCalls[idx]["id"] = id
				}
				if fn, ok := tcm["function"].(map[string]any); ok {
					if n, ok := fn["name"].(string); ok && n != "" {
						gw.toolCalls[idx]["name"] = n
					}
					if a, ok := fn["arguments"].(string); ok {
						gw.toolCalls[idx]["args"] = gw.toolCalls[idx]["args"].(string) + a
					}
				}
			}
			gw.haveToolCalls = true
		}
	}

	if fr != "" {
		gw.finishReason = mapOpenAIStopToGemini(fr)
	}
	return true
}

// Finish emits the tool calls, finish reason and usage metadata events.
func (gw *geminiSSEWriter) Finish() {
	if gw.finished {
		return
	}
	gw.finished = true

	// 工具调用（参数完整后一次输出，位于文本块之后，与 Gemini 原生流式一致）
	if gw.haveToolCalls {
		parts := []any{}
		for _, tc := range gw.toolCalls {
			argsStr, _ := tc["args"].(string)
			argsParsed := safeParse(argsStr, map[string]any{})
			argsMap, _ := argsParsed.(map[string]any)
			if argsMap == nil {
				argsMap = map[string]any{}
			}
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{
					"name": tc["name"],
					"args": argsMap,
				},
			})
		}
		gw.writeData(map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{"role": "model", "parts": parts},
				},
			},
		})
	}

	// finishReason
	if gw.finishReason != "" {
		gw.writeData(map[string]any{
			"candidates": []any{
				map[string]any{
					"finishReason": gw.finishReason,
					"content":      map[string]any{"role": "model", "parts": []any{}},
				},
			},
		})
	}

	// usage metadata
	if gw.promptTokens > 0 || gw.outputTokens > 0 {
		um := map[string]any{}
		if gw.promptTokens > 0 {
			um["promptTokenCount"] = gw.promptTokens
		}
		if gw.outputTokens > 0 {
			um["candidatesTokenCount"] = gw.outputTokens
		}
		if gw.promptTokens > 0 && gw.outputTokens > 0 {
			um["totalTokenCount"] = gw.promptTokens + gw.outputTokens
		}
		gw.writeData(map[string]any{"usageMetadata": um})
	}
}

// WriteChunksToGemini consumes OpenAI-shaped chunks and writes a complete
// Gemini alt=sse stream incrementally to w. Returns the last usage chunk seen.
func WriteChunksToGemini(w io.Writer, chunks <-chan map[string]any, model string) (*types.Usage, error) {
	gw := &geminiSSEWriter{w: w}
	var lastUsage *types.Usage
	for chunk := range chunks {
		if chunk == nil { // data: null：nil map 直接跳过
			continue
		}
		if u := usageFromChunk(chunk); u != nil {
			lastUsage = u
		}
		if !gw.Write(chunk) {
			// 写失败（客户端断连）：停止写出但仍需排空剩余 chunk，
			// 让上游生产者走完并关闭连接，避免 goroutine 泄漏
			drainChunks(chunks)
			return lastUsage, gw.err
		}
	}
	gw.Finish()
	return lastUsage, gw.err
}

// ---------------------------------------------------------------------------
// Helpers shared by this file
// ---------------------------------------------------------------------------

// convertContent converts an OpenAI message content field to an Anthropic
// content block slice. Used by ToAnthropic in outbound_anthropic.go.
func convertContent(v any) []any {
	if v == nil {
		return []any{}
	}
	if s, ok := v.(string); ok {
		if s == "" {
			return []any{}
		}
		return []any{map[string]any{"type": "text", "text": s}}
	}
	arr, ok := v.([]any)
	if !ok {
		return []any{}
	}
	var blocks []any
	for _, p := range arr {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		pt, _ := pm["type"].(string)
		if pt == "text" {
			blocks = append(blocks, map[string]any{"type": "text", "text": pm["text"]})
			continue
		}
		if pt == "image_url" {
			if iu, ok := pm["image_url"].(map[string]any); ok {
				url, _ := iu["url"].(string)
				d := parseDataURL(url)
				if d != nil {
					blocks = append(blocks, map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": d.Mime,
							"data":       d.Data,
						},
					})
				} else if url != "" {
					// http(s) 公网图片：Anthropic 已支持 {"type":"url"} source，
					// 此前静默丢弃导致模型只看到文字、图片内容凭空消失
					blocks = append(blocks, map[string]any{
						"type": "image",
						"source": map[string]any{
							"type": "url",
							"url":  url,
						},
					})
				}
			}
			continue
		}
		// Pass through unknown block types as-is
		blocks = append(blocks, p)
	}
	return blocks
}

// numFloat extracts a float64 value cast to int from a map, with default 0.
func numFloat(m map[string]any, key string) (int, bool) {
	if v, ok := m[key].(float64); ok {
		return int(v), true
	}
	return 0, false
}

// errMsgText 从上游错误对象中提取可读 message。提取不到时回退为整体 JSON
// 序列化；此前直接把整个错误对象序列化成字符串塞进 message，客户端看到的是
// 转义 JSON 而非人话。
func errMsgText(errM map[string]any) string {
	if m, ok := errM["message"].(string); ok && m != "" {
		return m
	}
	return jsonDumps(errM)
}

// finishReasonFor 计算 Responses→OpenAI 的 finish_reason：本次响应含
// function_call 输出时必须为 tool_calls（否则下游 agent 客户端不会执行工具），
// 截断（incomplete）时为 length；tool_calls 与截断同时存在时按 OpenAI 惯例
// 仍返回 tool_calls（调用已被完整发出）。
func finishReasonFor(hasToolCall, incomplete bool) string {
	if hasToolCall {
		return "tool_calls"
	}
	if incomplete {
		return "length"
	}
	return "stop"
}

// numFloatAny is like numFloat but works on any map type (used in tool-call maps).
func numFloatAny(m map[string]any, key string) (int, bool) {
	return numFloat(m, key)
}

// mapOpenAIStopToAnthropic converts an OpenAI finish_reason to Anthropic stop_reason.
func mapOpenAIStopToAnthropic(fr string) string {
	switch fr {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "refusal"
	default:
		return "end_turn"
	}
}

// mapOpenAIStopToGemini converts an OpenAI finish_reason to Gemini finishReason.
func mapOpenAIStopToGemini(fr string) string {
	switch fr {
	case "length":
		return "MAX_TOKENS"
	case "content_filter":
		return "SAFETY"
	default:
		return "STOP"
	}
}
