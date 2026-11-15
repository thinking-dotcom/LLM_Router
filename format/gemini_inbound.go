package format

import (
	"encoding/json"
	"fmt"
	"strings"

	"llm-router-go/types"
)

// normFnParams 归一 Gemini 函数声明的 parameters。Gemini 协议中该字段可选
// （无参函数可整体省略），原样透传会得到 parameters:null，严格的 OpenAI 兼容
// 上游（serde 反序列化）按 missing field `parameters` 报 400。处理规则：
// 优先取 parameters，其次新式 parametersJsonSchema（标准 JSON Schema 形态），
// 都没有则补空对象 schema；已有 schema 缺 type/properties 时浅克隆补齐，
// 不改动调用方的原始 map。
func normFnParams(fdm map[string]any) map[string]any {
	p, _ := fdm["parameters"].(map[string]any)
	if p == nil {
		p, _ = fdm["parametersJsonSchema"].(map[string]any)
	}
	if p == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if _, hasType := p["type"]; hasType {
		if _, hasProps := p["properties"]; hasProps {
			return p
		}
	}
	cloned := make(map[string]any, len(p)+2)
	for k, v := range p {
		cloned[k] = v
	}
	if _, ok := cloned["type"]; !ok {
		cloned["type"] = "object"
	}
	if _, ok := cloned["properties"]; !ok {
		cloned["properties"] = map[string]any{}
	}
	return cloned
}

// OpenaiFromGemini converts a Gemini request body to OpenAI chat shape
func OpenaiFromGemini(body any) map[string]any {
	m, _ := body.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	out := map[string]any{"model": strOr(m["model"], ""), "messages": []any{}, "stream": false}

	// thoughtSignature 往返：按「归一化消息序号 → 签名队列」记录，由 ToGemini 按同序回填。
	// 序号口径必须与 ToGemini 的 msgIdx 完全一致：对 messages 中每条消息（含 system）
	// 都自增，否则带 systemInstruction 时记录整体偏移、签名被静默丢弃。
	msgCount := 0
	var sigRecs []any

	// systemInstruction 兼容两种形式：字符串与 {parts:[...]}。
	// 此前只处理 map 形式，字符串形式的系统提示整条丢失。
	if si, ok := m["systemInstruction"].(string); ok && si != "" {
		out["messages"] = append(out["messages"].([]any), map[string]any{"role": "system", "content": si})
		msgCount++
	} else if si, ok := m["systemInstruction"].(map[string]any); ok {
		if siParts, ok := si["parts"].([]any); ok {
			text := partsToText(siParts)
			if text != "" {
				out["messages"] = append(out["messages"].([]any), map[string]any{"role": "system", "content": text})
				msgCount++
			}
		}
	}

	seq := 0
	pendingCallIDs := []string{} // assistant 轮生成的 functionCall id 队列，供后续 functionResponse 复用
	contents, _ := m["contents"].([]any)
	for _, c := range contents {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		partsIn, _ := cm["parts"].([]any)
		isModel := cm["role"] == "model"
		if !isModel {
			userParts := []any{}
			toolResults := []any{}
			for _, p := range partsIn {
				pm, _ := p.(map[string]any)
				if pm == nil {
					continue
				}
				// thought summary parts（思考摘要）不属于对话内容，回传会污染
				// 下游上下文，跳过。
				if th, _ := pm["thought"].(bool); th {
					continue
				}
				if text, _ := pm["text"].(string); text != "" {
					userParts = append(userParts, map[string]any{"type": "text", "text": text})
				} else if inline, ok := pm["inlineData"].(map[string]any); ok {
					data, _ := inline["data"].(string)
					mime, _ := inline["mimeType"].(string)
					if mime == "" {
						mime = "image/png"
					}
					userParts = append(userParts, map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "data:" + mime + ";base64," + data},
					})
				} else if fd, ok := pm["fileData"].(map[string]any); ok {
					if u, _ := fd["fileUri"].(string); u != "" {
						userParts = append(userParts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": u}})
					} else if fu, _ := fd["file_uri"].(string); fu != "" {
						userParts = append(userParts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": fu}})
					}
				} else if ec, ok := pm["executableCode"].(map[string]any); ok {
					userParts = append(userParts, map[string]any{"type": "text", "text": renderExecutableCode(ec)})
				} else if cr, ok := pm["codeExecutionResult"].(map[string]any); ok {
					userParts = append(userParts, map[string]any{"type": "text", "text": renderCodeExecutionResult(cr)})
				} else if fr, ok := pm["functionResponse"].(map[string]any); ok {
					cid, _ := fr["id"].(string)
					if cid == "" {
						// 复用最近一次 functionCall 的 id，保证 tool_call_id 能对上；队列空才合成
						if len(pendingCallIDs) > 0 {
							cid = pendingCallIDs[0]
							pendingCallIDs = pendingCallIDs[1:]
						} else {
							cid = fmt.Sprintf("call_%d", seq)
						}
					}
					content := ""
					switch v := fr["response"].(type) {
					case string:
						content = v
					case map[string]any:
						b, _ := json.Marshal(v)
						content = string(b)
					default:
						if v != nil {
							// 数组/数字等类型兜底序列化，避免工具结果静默变空
							if bb, err := json.Marshal(v); err == nil {
								content = string(bb)
							}
						}
					}
					toolResults = append(toolResults, map[string]any{
						"role": "tool", "tool_call_id": cid, "name": fr["name"], "content": content,
					})
					seq++
				}
			}
			if len(toolResults) > 0 {
				msgs := out["messages"].([]any)
				out["messages"] = append(msgs, toolResults...)
				msgCount += len(toolResults)
			}
			if len(userParts) > 0 {
				if len(userParts) == 1 && isTextPart(userParts[0]) {
					out["messages"] = append(out["messages"].([]any), map[string]any{"role": "user", "content": textFromPart(userParts[0])})
				} else {
					out["messages"] = append(out["messages"].([]any), map[string]any{"role": "user", "content": userParts})
				}
				msgCount++
			}
		} else {
			modelParts := []any{}
			toolCalls := []any{}
			var modelSigs []any // 本条模型消息的全部签名（并集，供无分类信息时按序兜底）
			var textSigs []any  // 挂在 text 类 part 上的签名（按序）
			var fcSigs []any    // 挂在 functionCall 上的签名（按序）
			for _, p := range partsIn {
				pm, _ := p.(map[string]any)
				if pm == nil {
					continue
				}
				// 同上：model 回合的思考摘要 parts 不作为普通文本回传
				if th, _ := pm["thought"].(bool); th {
					continue
				}
				// thoughtSignature（Gemini 3 思维签名，不透明 base64）：跨型往返保真
				// 所需，随消息序号入 IR。签名与具体 part 绑定，故按 part 类型分别入队，
				// 由 ToGemini 精确回填——只按序盲配会在「一条消息里既有文本又有工具
				// 调用、签名却只在 functionCall 上」时把签名贴到 text，反使
				// functionCall 丢签名（上游报 MISSING_THOUGHT_SIGNATURE）。
				sig, _ := pm["thoughtSignature"].(string)
				if text, _ := pm["text"].(string); text != "" {
					modelParts = append(modelParts, map[string]any{"type": "text", "text": text})
					if sig != "" {
						textSigs = append(textSigs, sig)
						modelSigs = append(modelSigs, sig)
					}
				} else if fc, ok := pm["functionCall"].(map[string]any); ok {
					if sig != "" {
						fcSigs = append(fcSigs, sig)
						modelSigs = append(modelSigs, sig)
					}
					cid, _ := fc["id"].(string)
					if cid == "" {
						cid = fmt.Sprintf("call_%d", seq)
					}
					// 记录本次调用的 id，供后续 functionResponse 匹配
					pendingCallIDs = append(pendingCallIDs, cid)
					args, _ := fc["args"].(map[string]any)
					if args == nil {
						args = map[string]any{}
					}
					argsB, _ := json.Marshal(args)
					toolCalls = append(toolCalls, map[string]any{
						"id": cid, "type": "function",
						"function": map[string]any{"name": fc["name"], "arguments": string(argsB)},
					})
					seq++
				} else if ec, ok := pm["executableCode"].(map[string]any); ok {
					modelParts = append(modelParts, map[string]any{"type": "text", "text": renderExecutableCode(ec)})
					if sig != "" {
						textSigs = append(textSigs, sig)
						modelSigs = append(modelSigs, sig)
					}
				} else if cr, ok := pm["codeExecutionResult"].(map[string]any); ok {
					modelParts = append(modelParts, map[string]any{"type": "text", "text": renderCodeExecutionResult(cr)})
					if sig != "" {
						textSigs = append(textSigs, sig)
						modelSigs = append(modelSigs, sig)
					}
				}
			}
			amsg := map[string]any{"role": "assistant"}
			if len(toolCalls) > 0 {
				amsg["tool_calls"] = toolCalls
				if len(modelParts) > 0 {
					amsg["content"] = modelParts
				} else {
					amsg["content"] = ""
				}
			} else if len(modelParts) > 0 {
				if len(modelParts) == 1 && isTextPart(modelParts[0]) {
					amsg["content"] = textFromPart(modelParts[0])
				} else {
					amsg["content"] = modelParts
				}
			}
			if len(modelSigs) > 0 {
				rec := map[string]any{
					"message":    msgCount,
					"signatures": modelSigs,
				}
				// 分类队列：回填端据此把签名贴回同类型 part；signatures 保留为
				// 并集，供记录缺分类信息时退回按序兜底。
				if len(textSigs) > 0 {
					rec["textSignatures"] = textSigs
				}
				if len(fcSigs) > 0 {
					rec["fcSignatures"] = fcSigs
				}
				sigRecs = append(sigRecs, rec)
			}
			msgCount++
			out["messages"] = append(out["messages"].([]any), amsg)
		}
	}

	// thoughtSignature 经 IR 随请求携带：原生 Gemini 出站（ToGemini）按消息序号
	// 回填；openai / gemini-openai 上游不识别该概念，由 stripClientExtensions 剥离。
	if len(sigRecs) > 0 {
		types.IRSet(out, types.ExtGeminiThoughtSigs, sigRecs)
	}

	// Generation config
	if cfg, ok := m["generationConfig"].(map[string]any); ok {
		copyFloat(cfg, out, "temperature")
		if tp, ok := cfg["topP"]; ok {
			out["top_p"] = tp
		}
		if tk, ok := cfg["topK"]; ok {
			out["top_k"] = tk
		}
		if mt, ok := cfg["maxOutputTokens"]; ok {
			out["max_tokens"] = mt
		}
		if ss, ok := cfg["stopSequences"].([]any); ok {
			out["stop"] = ss
		}
		if v, ok := cfg["candidateCount"]; ok {
			out["n"] = v
		}
		if v, ok := cfg["responseMimeType"].(string); ok && v != "" {
			// OpenAI 的 response_format.type 只接受 text/json_object/json_schema，
			// 直接透传 Gemini 的 MIME 值（如 "application/json"）会被严格上游 400。
			switch v {
			case "application/json":
				out["response_format"] = map[string]any{"type": "json_object"}
			case "text/plain":
				out["response_format"] = map[string]any{"type": "text"}
			}
		}
		if v, ok := cfg["responseSchema"]; ok {
			types.IRSet(out, types.ExtGeminiResponseSchema, v)
		}
		// 客户端多协议推理深度扩展键（IR）:Gemini CLI 的 generateContentConfig 会原样
		// 透传请求体里的自定义字段，这里按「实际模型类型择一使用」的原则提取后
		// 立即从原始 body 剥离（bodyMap 与 RawBody 同源，同型直通同样生效），
		// 避免真实 Gemini 上游因未知字段报 400。
		if v, ok := cfg["reasoningEffort"].(string); ok && v != "" {
			types.IRSet(out, types.ExtClientReasoningEffort, v) // OpenAI 兼容 / Responses 上游使用
			delete(cfg, "reasoningEffort")
		}
		if b, ok := cfg["anthropicThinkingBudget"].(float64); ok && b > 0 {
			types.IRSet(out, types.ExtClientAnthropicBudget, int(b)) // Anthropic 上游使用
			delete(cfg, "anthropicThinkingBudget")
		}
		// thinkingConfig 归一化：把 Gemini 原生思考参数折算成统一推理档位
		// （reasoning_effort），作为未显式配置对应家族深度时的回退值。
		// Gemini 同型直通不走此处，原生 thinkingConfig 原样转发不受影响。
		if tc, ok := cfg["thinkingConfig"].(map[string]any); ok {
			if eff := reasoningEffortFromThinking(tc); eff != "" {
				out["reasoning_effort"] = eff
			}
			types.IRSet(out, types.ExtGeminiThinkingConfig, tc)
		}
		// 显式配置的 OpenAI 兼容深度优先于折算值
		if v, ok := types.IRGet(out, types.ExtClientReasoningEffort); ok {
			if s, ok := v.(string); ok && s != "" {
				out["reasoning_effort"] = s
			}
		}
	}

	// safetySettings / cachedContent 是 generateContent 的顶层字段，
	// 与 generationConfig 无关，必须在条件块之外提取，
	// 否则不带 generationConfig 的请求会丢失这两个字段。
	if v, ok := m["safetySettings"]; ok {
		types.IRSet(out, types.ExtGeminiSafetySettings, v)
	}
	if v, ok := m["cachedContent"]; ok {
		types.IRSet(out, types.ExtGeminiCachedContent, v)
	}

	// Tools
	if tools, ok := m["tools"].([]any); ok {
		openaiTools := []any{}
		geminiExtras := []any{}
		for _, t := range tools {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			for _, fd := range sliceOrEmpty(tm["functionDeclarations"]) {
				fdm, _ := fd.(map[string]any)
				if fdm == nil {
					continue
				}
				openaiTools = append(openaiTools, map[string]any{
					"type":     "function",
					"function": map[string]any{"name": fdm["name"], "description": fdm["description"], "parameters": normFnParams(fdm)},
				})
			}
			if _, ok := tm["codeExecution"]; ok {
				geminiExtras = append(geminiExtras, map[string]any{"type": "code_execution"})
			}
			if _, ok := tm["googleSearch"]; ok {
				geminiExtras = append(geminiExtras, map[string]any{"type": "google_search"})
			}
		}
		if len(openaiTools) > 0 {
			out["tools"] = openaiTools
		}
		if len(geminiExtras) > 0 {
			types.IRSet(out, types.ExtGeminiToolsExtra, geminiExtras)
		}
	}

	// Tool config
	var tcc map[string]any
	if tc, ok := m["toolConfig"].(map[string]any); ok {
		if fcc, ok := tc["functionCallingConfig"].(map[string]any); ok {
			tcc = fcc
		} else {
			tcc = tc
		}
	}
	if tcc != nil {
		mode, _ := tcc["mode"].(string)
		names := strSlice(tcc["allowedFunctionNames"])
		switch {
		case mode == "NONE":
			out["tool_choice"] = "none"
		case mode == "ANY" && len(names) == 1:
			out["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": names[0]}}
		case mode == "ANY" && len(names) > 1:
			// 多个白名单：保留完整列表，OpenAI 侧无一一对应，存于扩展字段
			out["tool_choice"] = "required"
			types.IRSet(out, types.ExtGeminiAllowedFns, names)
		case mode == "ANY":
			out["tool_choice"] = "required"
		case mode == "AUTO" && len(names) == 1:
			out["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": names[0]}}
		case mode == "AUTO" && len(names) > 1:
			out["tool_choice"] = "auto"
			types.IRSet(out, types.ExtGeminiAllowedFns, names)
		default:
			out["tool_choice"] = "auto"
		}
	}
	return out
}

// reasoningEffortFromThinking 把 Gemini thinkingConfig 折算为统一推理档位
// （reasoning_effort）。取值与阈值集中在映射表（format/reasoning_table.go）：
// 3 系模型用 thinkingLevel（LOW/MEDIUM/HIGH），2.5 系用 thinkingBudget（token
// 预算），两者取其一。
//
// thinkingBudget 的官方语义：0 = 关闭思考，-1 = 动态思考（模型自行决定思考量，
// 是 Gemini 系 CLI 的常见默认值），正数 = 固定 token 预算。负值不能折成 none
// ——那会把「模型自决」变成「禁用思考」，语义正好相反，严格校验的 OpenAI 兼容
// 上游还可能因 reasoning_effort=none 非法而 400；这里返回空串表示不折算，保持
// 「未指定」，由上游按其默认动态策略执行（客户端已携带参数，路由器不会再注入
// 全局档位：ExtGeminiThinkingConfig 存在即视为已配置）。
func reasoningEffortFromThinking(tc map[string]any) string {
	return decodeGeminiThinking(tc).tier
}

// renderExecutableCode 把 Gemini executableCode part 渲染为 Markdown 代码文本，
// 跨型转换时保留代码内容本身（此前仅输出占位符，非 Gemini 上游看到的是空占位，
// 代码执行语境完全丢失）。
func renderExecutableCode(ec map[string]any) string {
	code, _ := ec["code"].(string)
	if code == "" {
		return "[executableCode]"
	}
	lang, _ := ec["language"].(string)
	if lang == "" {
		lang = "python"
	}
	return "```" + strings.ToLower(lang) + "\n" + code + "\n```"
}

// renderCodeExecutionResult 把 codeExecutionResult part 渲染为执行输出文本；
// 非 OK 结局保留结局标记，便于上游模型理解执行失败语境。
func renderCodeExecutionResult(cr map[string]any) string {
	output, _ := cr["output"].(string)
	outcome, _ := cr["outcome"].(string)
	if output == "" {
		if outcome == "" {
			return "[codeExecutionResult]"
		}
		return "[codeExecutionResult:" + outcome + "]"
	}
	if outcome != "" && outcome != "OUTCOME_OK" {
		return "[codeExecutionResult:" + outcome + "]\n" + output
	}
	return output
}

func isTextPart(p any) bool {
	if m, ok := p.(map[string]any); ok {
		t, _ := m["type"].(string)
		return t == "text"
	}
	return false
}

func textFromPart(p any) string {
	if m, ok := p.(map[string]any); ok {
		if t, ok := m["text"].(string); ok {
			return t
		}
	}
	return ""
}
