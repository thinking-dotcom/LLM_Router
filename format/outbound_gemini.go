package format

import (
	"encoding/json"
	"strings"

	"llm-router-go/types"
)

// ToGemini converts an OpenAI chat request shape to Gemini API format.
// Returns a map with keys: contents, systemInstruction (optional),
// generationConfig (optional), tools (optional), toolConfig (optional).
func ToGemini(openaiReq map[string]any) map[string]any {
	out := map[string]any{}

	var systemParts []any
	var contents []any
	var pendingFRs []any // pending functionResponse parts from tool messages

	// OpenAI 现行规范的 tool 消息只带 tool_call_id（name 字段已弃用），主流 SDK
	// 均不发 name。Gemini 要求 functionResponse.name 必填，这里预扫历史 assistant
	// 消息的 tool_calls 建立 id → name 反查表。
	callNames := map[string]string{}
	for _, m := range sliceOrEmpty(openaiReq["messages"]) {
		mm, _ := m.(map[string]any)
		if mm == nil {
			continue
		}
		if r, _ := mm["role"].(string); r != "assistant" {
			continue
		}
		for _, tc := range sliceOrEmpty(mm["tool_calls"]) {
			tcm, _ := tc.(map[string]any)
			if tcm == nil {
				continue
			}
			fn, _ := tcm["function"].(map[string]any)
			id, _ := tcm["id"].(string)
			n, _ := fn["name"].(string)
			if id != "" && n != "" {
				callNames[id] = n
			}
		}
	}

	// thoughtSignature 回填队列，两种来源：
	//   1) OpenAI 客户端在 assistant 消息 / tool_calls 上透传的 thought_signature；
	//   2) Gemini 客户端历史经 IR（ExtGeminiThoughtSigs）按消息序号携带的签名。
	// 仅原生 Gemini 上游能表达该字段；openai / gemini-openai 上游由
	// stripMessageThoughtSignatures / stripClientExtensions 剥离。
	sigByMsg := map[int][]any{}
	textSigByMsg := map[int][]any{}
	fcSigByMsg := map[int][]any{}
	if recs, ok := types.IRGet(openaiReq, types.ExtGeminiThoughtSigs); ok {
		for _, r := range sliceOrEmpty(recs) {
			rm, _ := r.(map[string]any)
			if rm == nil {
				continue
			}
			mi := 0
			// 进程内直传时为 int，经 JSON 往返后为 float64，两者都要兼容
			switch v := rm["message"].(type) {
			case float64:
				mi = int(v)
			case int:
				mi = v
			}
			if sigs := sliceOrEmpty(rm["signatures"]); len(sigs) > 0 {
				sigByMsg[mi] = append(sigByMsg[mi], sigs...)
			}
			if sigs := sliceOrEmpty(rm["textSignatures"]); len(sigs) > 0 {
				textSigByMsg[mi] = append(textSigByMsg[mi], sigs...)
			}
			if sigs := sliceOrEmpty(rm["fcSignatures"]); len(sigs) > 0 {
				fcSigByMsg[mi] = append(fcSigByMsg[mi], sigs...)
			}
		}
	}
	msgIdx := 0

	flushFRs := func() {
		if len(pendingFRs) == 0 {
			return
		}
		contents = append(contents, map[string]any{
			"role":  "user",
			"parts": pendingFRs,
		})
		pendingFRs = nil
	}

	for _, m := range sliceOrEmpty(openaiReq["messages"]) {
		mm, _ := m.(map[string]any)
		if mm == nil {
			continue
		}
		curIdx := msgIdx
		msgIdx++
		role, _ := mm["role"].(string)

		if role == "system" {
			text := ""
			if s, ok := mm["content"].(string); ok {
				text = s
			} else {
				text = partsToText(mm["content"])
			}
			if text != "" {
				systemParts = append(systemParts, map[string]any{"text": text})
			}
			continue
		}

		if role == "tool" {
			cid, _ := mm["tool_call_id"].(string)
			if cid == "" {
				cid = randID("")
			}
			name, _ := mm["name"].(string)
			if name == "" {
				name = callNames[cid]
			}

			var response any
			switch c := mm["content"].(type) {
			case string:
				response = map[string]any{"content": c}
			case nil:
				response = map[string]any{}
			case []any:
				// 多块 content（含 image_url 等）→ 文本化后包裹；Gemini 要求 response 是 JSON object
				response = map[string]any{"content": partsToText(c)}
			default:
				b, err := json.Marshal(c)
				if err != nil {
					response = map[string]any{}
				} else {
					response = map[string]any{"content": string(b)}
				}
			}

			fnResp := map[string]any{
				"name":     name,
				"response": response,
			}
			if cid != "" {
				fnResp["id"] = cid
			}

			pendingFRs = append(pendingFRs, map[string]any{
				"functionResponse": fnResp,
			})
			continue
		}

		if role == "assistant" {
			// Flush pending functionResponse parts before model turn
			flushFRs()

			parts := buildGeminiParts(mm, true)
			// thoughtSignature 回填：消息级 thought_signature（字符串或数组）与
			// tool_calls 上的 thought_signature 优先，其次 IR 按序队列
			var sigs []any
			switch ts := mm["thought_signature"].(type) {
			case string:
				if ts != "" {
					sigs = append(sigs, ts)
				}
			case []any:
				sigs = append(sigs, ts...)
			}
			for _, tc := range sliceOrEmpty(mm["tool_calls"]) {
				tcm, _ := tc.(map[string]any)
				if tcm == nil {
					continue
				}
				if sig, ok := tcm["thought_signature"].(string); ok && sig != "" {
					sigs = append(sigs, sig)
				}
			}
			// thoughtSignature 回填：消息级 thought_signature（字符串或数组）与
			// tool_calls 上的 thought_signature 来自 OpenAI 客户端，按序贴合。
			parts = attachThoughtSignatures(parts, sigs)
			// 经 IR 携带的 Gemini 历史签名与具体 part 绑定，优先按类型精确回填
			// （按序盲配会把只在 functionCall 上的签名贴到 text）；仅当记录不含
			// 分类信息时才退回按序兜底。
			if ts, fs := textSigByMsg[curIdx], fcSigByMsg[curIdx]; len(ts) > 0 || len(fs) > 0 {
				parts = attachTypedThoughtSignatures(parts, ts, fs)
			} else if irSigs := sigByMsg[curIdx]; len(irSigs) > 0 {
				parts = attachThoughtSignatures(parts, irSigs)
			}
			if len(parts) > 0 {
				contents = append(contents, map[string]any{
					"role":  "model",
					"parts": parts,
				})
			}
		} else {
			// user role
			parts := buildGeminiParts(mm, false)
			// Flush accumulated functionResponse parts before user parts
			allParts := make([]any, 0, len(pendingFRs)+len(parts))
			allParts = append(allParts, pendingFRs...)
			pendingFRs = nil
			allParts = append(allParts, parts...)

			if len(allParts) > 0 {
				contents = append(contents, map[string]any{
					"role":  "user",
					"parts": allParts,
				})
			}
		}
	}

	flushFRs()

	// Merge adjacent same-role contents (Gemini requires alternating roles)
	merged := mergeGeminiContents(contents)
	out["contents"] = merged

	// System instruction
	if len(systemParts) > 0 {
		out["systemInstruction"] = map[string]any{"parts": systemParts}
	}

	// generationConfig
	genConfig := map[string]any{}
	if v, ok := openaiReq["temperature"].(float64); ok {
		genConfig["temperature"] = v
	}
	if v, ok := openaiReq["top_p"].(float64); ok {
		genConfig["topP"] = v
	}
	if v, ok := openaiReq["top_k"].(float64); ok {
		genConfig["topK"] = v
	}
	if v, ok := openaiReq["max_tokens"].(float64); ok {
		genConfig["maxOutputTokens"] = v
	} else if v, ok := openaiReq["max_completion_tokens"].(float64); ok {
		genConfig["maxOutputTokens"] = v
	}
	if v, ok := openaiReq["stop"]; ok {
		sl := strList(v)
		if len(sl) > 0 {
			genConfig["stopSequences"] = sl
		}
	}
	if v, ok := openaiReq["n"]; ok {
		genConfig["candidateCount"] = v
	}
	if v, ok := openaiReq["seed"].(float64); ok {
		genConfig["seed"] = v
	}
	// response_format.type == "json_object" -> responseMimeType
	if rf, ok := openaiReq["response_format"].(map[string]any); ok {
		if t, _ := rf["type"].(string); t == "json_object" {
			genConfig["responseMimeType"] = "application/json"
		} else if t == "json_schema" {
			genConfig["responseMimeType"] = "application/json"
			if js, ok := rf["json_schema"].(map[string]any); ok {
				// Gemini 的 responseSchema 只接受 JSON Schema 本体；此前把整个
				// json_schema 对象（含 name/strict 等非法字段）直接赋值导致 400。
				if s, ok := js["schema"].(map[string]any); ok {
					genConfig["responseSchema"] = s
				} else if sStr, ok := js["schema"].(string); ok && sStr != "" {
					// 部分 SDK 把 schema 序列化为 JSON 字符串传递
					var schema map[string]any
					if json.Unmarshal([]byte(sStr), &schema) == nil && schema != nil {
						genConfig["responseSchema"] = schema
					}
				}
			}
		}
	}
	if v, ok := types.IRGet(openaiReq, types.ExtGeminiResponseSchema); ok {
		genConfig["responseSchema"] = v
	}
	// 统一推理档位（reasoning_effort，来自 /oai 系客户端如 pi/grok/opencode 的
	// 原生参数）→ Gemini thinkingConfig。跨型转换；Gemini 同型直通不经过此处。
	// 字段与取值、模型家族择形（Gemma 不携带、2.5 系用 thinkingBudget、3 系用
	// thinkingLevel）全部来自映射表 format/reasoning_table.go。
	if eff, _ := openaiReq["reasoning_effort"].(string); eff != "" {
		if p := EncodeReasoningNative(ReasoningFamilyGemini, strOr(openaiReq["model"], ""), eff); p != nil {
			for k, v := range p.Value {
				genConfig[k] = v
			}
		}
	}
	if len(genConfig) > 0 {
		out["generationConfig"] = genConfig
	}
	if v, ok := types.IRGet(openaiReq, types.ExtGeminiSafetySettings); ok {
		out["safetySettings"] = v
	}
	if v, ok := types.IRGet(openaiReq, types.ExtGeminiCachedContent); ok {
		out["cachedContent"] = v
	}

	// tools
	var fnDecls []any
	if tools, ok := openaiReq["tools"].([]any); ok {
		for _, t := range tools {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			if tType, _ := tm["type"].(string); tType != "function" {
				continue
			}
			fn, _ := tm["function"].(map[string]any)
			if fn == nil {
				continue
			}
			fnDecls = append(fnDecls, map[string]any{
				"name":        fn["name"],
				"description": fn["description"],
				"parameters":  fn["parameters"],
			})
		}
	}
	// legacy functions field compat：客户端只给 functions 且未给 tools 时也要可用
	//（此前嵌套在 tools 分支内导致仅有 functions 的请求工具被静默丢弃）
	if len(fnDecls) == 0 {
		if fns, ok := openaiReq["functions"].([]any); ok {
			for _, f := range fns {
				if fm, ok := f.(map[string]any); ok {
					fnDecls = append(fnDecls, map[string]any{
						"name": fm["name"], "description": fm["description"], "parameters": fm["parameters"],
					})
				}
			}
		}
	}
	if len(fnDecls) > 0 {
		out["tools"] = []any{map[string]any{"functionDeclarations": fnDecls}}
	}
	if extraV, ok := types.IRGet(openaiReq, types.ExtGeminiToolsExtra); ok {
		if extra, ok := extraV.([]any); ok && len(extra) > 0 {
			existing, _ := out["tools"].([]any)
			for _, e := range extra {
				if em, ok := e.(map[string]any); ok {
					if t, _ := em["type"].(string); t == "code_execution" {
						existing = append(existing, map[string]any{"codeExecution": map[string]any{}})
					} else if t == "google_search" {
						existing = append(existing, map[string]any{"googleSearch": map[string]any{}})
					}
				}
			}
			out["tools"] = existing
		}
	}

	// tool_choice
	if tc, ok := openaiReq["tool_choice"]; ok {
		var fcc map[string]any
		if s, isStr := tc.(string); isStr {
			switch s {
			case "none":
				fcc = map[string]any{"mode": "NONE"}
			case "required":
				// 若入站保留了多白名单，优先使用
				if namesV, ok := types.IRGet(openaiReq, types.ExtGeminiAllowedFns); ok {
					if names, ok := namesV.([]string); ok && len(names) > 0 {
						fcc = map[string]any{"mode": "ANY", "allowedFunctionNames": names}
					} else {
						fcc = map[string]any{"mode": "ANY"}
					}
				} else {
					fcc = map[string]any{"mode": "ANY"}
				}
			case "auto":
				fcc = map[string]any{"mode": "AUTO"}
			}
		} else if tcm, ok := tc.(map[string]any); ok {
			if t, _ := tcm["type"].(string); t == "function" {
				if fn, ok := tcm["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok {
						fcc = map[string]any{
							"mode":                 "ANY",
							"allowedFunctionNames": []string{name},
						}
						if namesV, ok := types.IRGet(openaiReq, types.ExtGeminiAllowedFns); ok {
							if names, ok := namesV.([]string); ok && len(names) > 1 {
								fcc["allowedFunctionNames"] = names
							}
						}
					}
				}
			}
		}
		if fcc != nil {
			out["toolConfig"] = map[string]any{"functionCallingConfig": fcc}
		}
	} else if namesV, ok := types.IRGet(openaiReq, types.ExtGeminiAllowedFns); ok {
		if names, ok := namesV.([]string); ok && len(names) > 0 {
			out["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY", "allowedFunctionNames": names}}
		}
	}

	return out
}

// buildGeminiParts converts OpenAI message content into Gemini parts.
// For assistant messages (isAssistant=true) it also processes tool_calls into functionCall parts.
// attachThoughtSignatures 按顺序把 thoughtSignature 回填到可签名 parts
// （text / functionCall）上。
//
// 签名数多于可签名 part 数时，多余签名无法表达（每个 part 只能承载一个
// thoughtSignature）：此时绝不能拿它们去覆盖已按序配好的签名——覆盖会让
// part 与签名错配、并静默丢掉其余签名，反而制造 MISSING_THOUGHT_SIGNATURE。
// 因此仅在完全没有可签名 part（异常形状）时兜底附首个签名到最后一个 part。
func attachThoughtSignatures(parts []any, sigs []any) []any {
	if len(sigs) == 0 || len(parts) == 0 {
		return parts
	}
	signable := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		_, isText := pm["text"]
		_, isFC := pm["functionCall"]
		if isText || isFC {
			signable = append(signable, pm)
		}
	}
	if len(signable) == 0 {
		if pm, ok := parts[len(parts)-1].(map[string]any); ok {
			pm["thoughtSignature"] = sigs[0]
		}
		return parts
	}
	for i, pm := range signable {
		if i >= len(sigs) {
			break
		}
		pm["thoughtSignature"] = sigs[i]
	}
	return parts
}

// attachTypedThoughtSignatures 把签名贴回同类型的 part：functionCall 队列只消费
// functionCall part，text 队列只消费 text part。
//
// Gemini 3 的 thoughtSignature 与它所属的 part 绑定，而回填时 parts 的顺序是重建
// 出来的（text 恒排在 functionCall 之前），与原始消息里的先后无关。因此不能用
// 「扁平队列 + 定序 zip」：一条消息里既有文本又有工具调用、而签名只挂在
// functionCall 上时，队列里唯一的签名会被贴给 text，functionCall 反而无签名，
// 上游随即以 MISSING_THOUGHT_SIGNATURE 拒绝。
func attachTypedThoughtSignatures(parts []any, textSigs, fcSigs []any) []any {
	if len(textSigs) == 0 && len(fcSigs) == 0 {
		return parts
	}
	ti, fi := 0, 0
	for _, p := range parts {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if _, isFC := pm["functionCall"]; isFC {
			if fi < len(fcSigs) {
				if s, ok := fcSigs[fi].(string); ok && s != "" {
					pm["thoughtSignature"] = s
				}
				fi++
			}
			continue
		}
		if _, isText := pm["text"]; isText {
			if ti < len(textSigs) {
				if s, ok := textSigs[ti].(string); ok && s != "" {
					pm["thoughtSignature"] = s
				}
				ti++
			}
		}
	}
	return parts
}

func buildGeminiParts(mm map[string]any, isAssistant bool) []any {
	var parts []any
	content := mm["content"]

	if s, ok := content.(string); ok {
		if s != "" {
			parts = append(parts, map[string]any{"text": s})
		}
	} else if arr, ok := content.([]any); ok {
		for _, p := range arr {
			pm, _ := p.(map[string]any)
			if pm == nil {
				continue
			}
			pt, _ := pm["type"].(string)
			if pt == "text" {
				if text, ok := pm["text"].(string); ok && text != "" {
					parts = append(parts, map[string]any{"text": text})
				}
			} else if pt == "image_url" {
				if iu, ok := pm["image_url"].(map[string]any); ok {
					url, _ := iu["url"].(string)
					if dataURL := parseDataURL(url); dataURL != nil {
						parts = append(parts, map[string]any{
							"inlineData": map[string]any{
								"mimeType": dataURL.Mime,
								"data":     dataURL.Data,
							},
						})
					} else if url != "" {
						// http(s) 图片：Gemini 用 fileData.fileUri 承载，非 data: 需走 fileData
						// mimeType 按扩展名推断，无法推断时省略交由上游探测，
						// 避免把 png/webp/gif 一律按 image/jpeg 上送导致解码失败。
						fd := map[string]any{"fileUri": url}
						if mt := mimeFromImageURL(url); mt != "" {
							fd["mimeType"] = mt
						}
						parts = append(parts, map[string]any{"fileData": fd})
					}
				}
			} else if pt == "document" {
				if src, ok := pm["source"].(map[string]any); ok {
					if _, ok := src["data"]; ok {
						// base64 document -> inlineData
						if data, _ := src["data"].(string); data != "" {
							mt, _ := src["media_type"].(string)
							if mt == "" {
								mt = "application/pdf"
							}
							parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": mt, "data": data}})
						}
					}
				}
			}
		}
	}

	if isAssistant {
		// 保留文本 + functionCall：Gemini 已支持同轮 text 与 functionCall 并存，
		// 强制 parts=nil 会丢弃工具调用前的解释文本，改为保留两者
		// （历史注释中"二选一"已过时，实测 tool_calls 与 content 并存合法）
		for _, tc := range sliceOrEmpty(mm["tool_calls"]) {
			tcm, _ := tc.(map[string]any)
			if tcm == nil {
				continue
			}
			fn, _ := tcm["function"].(map[string]any)
			if fn == nil {
				continue
			}
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)
			args := safeParse(argsStr, map[string]any{})
			argsMap, _ := args.(map[string]any)
			if argsMap == nil {
				argsMap = map[string]any{}
			}
			// 携带 id，使其与后续 functionResponse 的 id 对应（并行工具调用场景）。
			fc := map[string]any{"name": name, "args": argsMap}
			if cid, _ := tcm["id"].(string); cid != "" {
				fc["id"] = cid
			}
			parts = append(parts, map[string]any{
				"functionCall": fc,
			})
		}
	}

	return parts
}

// mimeFromImageURL 从图片 URL 的扩展名推断 MIME 类型；无法推断时返回空串，
// 调用方应省略 mimeType 让上游自行探测。
func mimeFromImageURL(u string) string {
	u = strings.ToLower(u)
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	switch {
	case strings.HasSuffix(u, ".png"):
		return "image/png"
	case strings.HasSuffix(u, ".webp"):
		return "image/webp"
	case strings.HasSuffix(u, ".gif"):
		return "image/gif"
	case strings.HasSuffix(u, ".jpg"), strings.HasSuffix(u, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(u, ".heic"), strings.HasSuffix(u, ".heif"):
		return "image/heic"
	}
	return ""
}

// mergeGeminiContents merges adjacent content blocks that share the same role.
// Gemini requires contents to alternate between user and model roles.
func mergeGeminiContents(contents []any) []any {
	if len(contents) <= 1 {
		return contents
	}
	merged := make([]any, 0, len(contents))
	for _, c := range contents {
		cm, _ := c.(map[string]any)
		if cm == nil {
			merged = append(merged, c)
			continue
		}
		role, _ := cm["role"].(string)
		parts, _ := cm["parts"].([]any)

		if len(merged) > 0 {
			last, _ := merged[len(merged)-1].(map[string]any)
			if last != nil {
				lr, _ := last["role"].(string)
				if lr == role {
					lp, _ := last["parts"].([]any)
					last["parts"] = append(lp, parts...)
					continue
				}
			}
		}
		merged = append(merged, c)
	}
	return merged
}

// ---------------------------------------------------------------------------
// 模型家族 × 推理档位折算（thinkingConfig 择形）
// ---------------------------------------------------------------------------

// isGemmaModel 判断是否 Gemma 系模型（不支持任何思考配置，发送 thinkingConfig
// 会被上游以 400 拒绝）。
// 模型名大小写不做要求：官方 ID 全小写，但网关/别名里常见首字母大写写法，
// 判定若区分大小写，这类模型会被误当作 Gemini 3 系而收到非法的 thinkingLevel。
func isGemmaModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "gemma")
}

// isGemini25Model 判断是否 Gemini 2.5 系模型（只认 thinkingBudget，不接受
// thinkingLevel 档位）。
func isGemini25Model(model string) bool {
	return strings.Contains(strings.ToLower(model), "gemini-2.5")
}

// geminiThinkingConfigForModel 按模型家族把统一推理档位（reasoning_effort）
// 折算为 Gemini thinkingConfig。映射表（format/reasoning_table.go）的封装：
// 返回 nil 表示该模型不应携带思考配置（Gemma 系，或 2.5-pro + none——其思考
// 不可关闭），调用方应跳过写入。
func geminiThinkingConfigForModel(model, eff string) map[string]any {
	p := EncodeReasoningNative(ReasoningFamilyGemini, model, eff)
	if p == nil {
		return nil
	}
	tc, _ := p.Value["thinkingConfig"].(map[string]any)
	return tc
}
