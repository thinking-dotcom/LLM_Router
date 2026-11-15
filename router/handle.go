package router

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"llm-router-go/format"
	"llm-router-go/transrules"
	"llm-router-go/types"
)

var upstreamHTTPClient = &http.Client{
	Timeout: 10 * time.Minute,
	Transport: &http.Transport{
		DialContext:        (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:       20,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: false,
	},
}

// streamingHTTPClient 与 upstreamHTTPClient 共享连接池，但不设整体超时：
// Client.Timeout 覆盖整个响应体读取，会把超过 10 分钟的 SSE 流中途捖断
// （长推理 + 长输出很常见）。流式请求改用此 client，由客户端断连兜底。
var streamingHTTPClient = &http.Client{
	Transport: upstreamHTTPClient.Transport,
}

// isOpenaiLike returns true for provider types that use OpenAI-compatible API format
// and need no body transformation.
// 注意：gemini-openai 不能参与直通——Google 的 protobuf 校验不接受 OpenAI 扩展字段
// （prompt_cache_key、additionalProperties 等），请求必须经 BuildOutboundRequest 重建
// 以剥离这些字段，否则上游直接 400。
func isOpenaiLike(ptype string) bool {
	return ptype == "openai"
}

// hoistExtraBody 把 OpenAI SDK 的 extra_body 包裹摊平到请求体顶层（就地修改），
// 返回是否有改动。extra_body 本应由 SDK 在发请求前合并进 body；部分客户端用的是
// 自定义 HTTP 层（如 Deep Code CLI 发 {thinking:{…}, extra_body:{reasoning_effort}}），
// 包裹键被原样发到线上——上游只看到一个自己不认识的 extra_body 对象，推理档位等参数
// 静默失效，而路由器此前也因「顶层没有推理字段」判定客户端未携带 → 又按全局档位兜底
// 注入一遍。摊平后顶层就能被正常识别、转换与上送。顶层已有同名键时不覆盖（顶层优先）。
func hoistExtraBody(body map[string]any) bool {
	if body == nil {
		return false
	}
	eb, ok := body["extra_body"].(map[string]any)
	if !ok || len(eb) == 0 {
		return false
	}
	for k, v := range eb {
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}
	delete(body, "extra_body")
	return true
}

// upstreamReasoningFamily 目标上游的推理参数家族键：provider.Type 直接归一
// （response → responses 等），Gemini 系若 baseUrl 指向 Interactions API
// （含 /interactions）则用 interactions 方言——与 allowSameTypePassthrough 的
// 同型判定口径保持一致。用于日志「推理」列读取上游原生字段。
func upstreamReasoningFamily(p types.Provider) string {
	if p.Type == types.ProviderGemini && strings.Contains(strings.ToLower(p.BaseURL), "/interactions") {
		return format.ReasoningFamilyInteractions
	}
	return string(p.Type)
}

// sanitizeMessageRoles 清洗 OpenAI 兼容请求体里的消息角色（原地修改）。
// 部分上游（GLM 系等）只接受 system/user/assistant，收到 developer 等角色
// 会报 [1214] Incorrect role information。映射：developer→system，
// 空/未知角色→user。返回是否有修改。
func sanitizeMessageRoles(body map[string]any) bool {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return false
	}
	changed := false
	for _, it := range msgs {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		switch role {
		case "developer":
			m["role"] = "system"
			changed = true
		case "", "system", "user", "assistant", "tool":
			// 合法角色，保持不变
		default:
			m["role"] = "user"
			changed = true
		}
	}
	return changed
}

// sanitizeReasoningEffort 归一化推理力度（原地修改）。
// 部分始终思考的上游模型不接受 minimal（报 [1210]），统一降级为 low。
// 覆盖两种常见写法：顶层 reasoning_effort 与 reasoning:{effort}。
func sanitizeReasoningEffort(body map[string]any) bool {
	changed := false
	if v, ok := body["reasoning_effort"].(string); ok && v == "minimal" {
		body["reasoning_effort"] = "low"
		changed = true
	}
	if rs, ok := body["reasoning"].(map[string]any); ok {
		if v, ok := rs["effort"].(string); ok && v == "minimal" {
			rs["effort"] = "low"
			changed = true
		}
	}
	return changed
}

// maxPassthroughLine 限制 openai→openai 流式直通时单行缓冲的上限，
// 与 format 包 IterSSE 的单行上限一致，防止上游超长行导致内存膨胀。
const maxPassthroughLine = 8 * 1024 * 1024

// truncateRunes truncates a string to at most n runes.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
} // redactURL 抹掉错误字符串里 URL 的密钥查询参数（Gemini 把 API Key 放在 ?key= 中），
// 避免上游传输层报错时把上游密钥回传给客户端或写入日志（logs.json）。
func redactURL(s string) string {
	re := regexp.MustCompile(`(\?|&)(key=)[^&\s"']*`)
	return re.ReplaceAllString(s, "${1}${2}REDACTED")
}

// jsonDumps serializes a value to a compact JSON string.
func jsonDumps(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// randHex returns a random lowercase hex string of n bytes.
func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ensureOpenAICompletionFields 为可能来自聚合代理/非标准上游的 OpenAI 响应补齐
// 必需的顶层字段。这类上游返回的 JSON 往往缺 id / object / created 等标准字段，
// 会导致严格反序列化的客户端（如 grokcli）报 missing field 错误。
// 仅当字段缺失时才注入默认值，其余字段（thinking 等扩展字段）原样保留。
func ensureOpenAICompletionFields(m map[string]any) {
	if _, ok := m["id"]; !ok {
		m["id"] = "chatcmpl-" + randHex(16)
	}
	if _, ok := m["object"]; !ok {
		m["object"] = "chat.completion"
	}
	if _, ok := m["created"]; !ok {
		m["created"] = time.Now().Unix()
	}
	if _, ok := m["model"]; !ok {
		m["model"] = ""
	}
}

// declaredInboundFormat 判定客户端请求的接入格式：统一按标准路径判定
// （/v1/messages → anthropic、/v1/responses 与 /responses → responses、
//
//	/v1beta/*:generateContent → gemini、其余 → openai）。
//
// 客户端 base_url 统一使用 /router，SDK 自行拼接的标准路径在此识别。
func declaredInboundFormat(rawPath string) string {
	p := strings.TrimRight(rawPath, "/")
	std := normalizeDoubledPath(stripAccessPrefix(p))
	if std == "/v1/messages" {
		return "anthropic"
	}
	if std == "/v1/responses" || std == "/responses" {
		return "responses"
	}
	if std == "/v1beta/interactions" || std == "/v1alpha/interactions" {
		// Gemini Interactions API（2026 GA 标准）客户端接入
		return "gemini-interactions"
	}
	if strings.HasPrefix(std, "/v1beta/") &&
		(strings.Contains(std, ":generateContent") || strings.Contains(std, ":streamGenerateContent")) {
		return "gemini"
	}
	return "openai"
}

// stripAccessPrefix 剥掉接入前缀还原标准路径。支持统一前缀 /router 与
// 简短格式声明前缀 /oai /aai /gai。客户端把 base_url 设成
// http://host:port/router（或 /oai /aai /gai），SDK 自行补上的
// /chat/completions、/v1/messages、/v1beta/models/... 会被正确识别。
func stripAccessPrefix(pathname string) string {
	prefixes := []string{"/router", "/oai", "/aai", "/gai"}
	for _, prefix := range prefixes {
		if !strings.HasPrefix(pathname, prefix) {
			continue
		}
		rest := strings.TrimPrefix(pathname, prefix)
		// 要求前缀后紧跟 "/" 或已结束，避免 /routerxyz、/oaixyz 被误判
		if rest == "" {
			return "/"
		}
		if strings.HasPrefix(rest, "/") {
			return rest
		}
	}
	return pathname
}

// normalizeDoubledPath 把「把完整端点当 base_url」的客户端拼出的重复路径折叠回标准路径。
// 这类客户端把 base 当作前缀、自行追加资源后缀（SDK 自动补 /chat/completions、/models 等），
// 若用户把 /oai/chat/completions 之类的完整端点填进了 base_url，实际请求就会带重复段：
//
//	/chat/completions 作 base → /chat/completions/chat/completions、/chat/completions/models
//	/v1/chat/completions 作 base → /v1/chat/completions/chat/completions、/v1/chat/completions/models
//	/v1/messages 作 base → /v1/messages/v1/messages、/v1/messages/v1/models
//
// 仅在命中这些特定重复模式时折叠，不影响正常路径。
func normalizeDoubledPath(p string) string {
	switch {
	case strings.HasSuffix(p, "/chat/completions/chat/completions"):
		return strings.TrimSuffix(p, "/chat/completions")
	case strings.HasSuffix(p, "/chat/completions/models"):
		if strings.HasSuffix(p, "/v1/chat/completions/models") {
			// /v1/chat/completions/models → /v1/models
			return strings.TrimSuffix(p, "/chat/completions/models") + "/models"
		}
		// /chat/completions/models → /models
		return "/models"
	case strings.HasSuffix(p, "/v1/messages/v1/messages"):
		return strings.TrimSuffix(p, "/v1/messages")
	case strings.HasSuffix(p, "/v1/messages/v1/models"):
		// /v1/messages/v1/models → /v1/models（去掉开头的 /v1/messages）
		return strings.TrimPrefix(p, "/v1/messages")
	}
	return p
}

// isModelPath 判断路径是否属于模型调用端点：方法不对时应答 405（方法不允许），
// 不存在的路径则应答 404。用于区分「探活/探测」与「真的调错了方法」。
func isModelPath(p string) bool {
	switch p {
	case "/chat/completions", "/completions", "/responses", "/embeddings",
		"/v1", "/v1/chat/completions", "/v1/completions", "/v1/responses", "/v1/embeddings",
		"/v1/messages", "/v1/messages/count_tokens":
		return true
	}
	return strings.HasPrefix(p, "/v1/") || strings.HasPrefix(p, "/v1beta/")
}

// HandleLLM is the main LLM request handler. It processes every incoming
// API call: HTML landing page, model lists, and chat/completions proxying.
//
// It returns an *http.Response that the caller (core/server.go) wraps with
// CORS headers and writes to the actual http.ResponseWriter. For streaming
// responses the function writes SSE data directly to w and returns nil.
func HandleLLM(w http.ResponseWriter, r *http.Request, config *types.Config, logger func(types.LogEntry), onActive func(model string) func()) (resp *http.Response) {
	// panic 恢复
	defer func() {
		if rec := recover(); rec != nil {
			logMsg := fmt.Sprintf("[RECOVER] panic in HandleLLM: %v", rec)
			fmt.Println(logMsg)
			resp = ErrorResponse(500, "internal error", "server_error")
		}
	}()

	// 限制请求体大小（整体超时移到 ParseInbound 之后按是否流式决定）
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB

	// 1. Initialise log entry --------------------------------------------------
	entry := types.LogEntry{
		Time:     time.Now().Format(time.RFC3339),
		Path:     r.URL.Path,
		Model:    "-",
		Provider: "-",
		Status:   0,
		Ms:       0,
	}

	start := time.Now()
	method := r.Method
	// rawPath 保留原始请求地址（含 /oai /aai /gai 前缀），用于判定客户端声明的接入格式。
	rawPath := r.URL.Path
	pathname := strings.TrimRight(r.URL.Path, "/")
	if pathname == "" {
		pathname = "/"
	}
	// 支持简短接入前缀：/oai → OpenAI、/aai → Anthropic、/gai → Gemini。
	// 各客户端 SDK 会在前缀后自行补路径（如 /oai/chat/completions、
	// /aai/v1/messages、/gai/v1beta/models/{model}:generateContent），
	// 这里统一剥掉前缀还原成标准路径，其余逻辑无需改动。旧路径依旧可用。
	pathname = stripAccessPrefix(pathname)
	// 容错：把完整端点当 base_url 的客户端会拼出重复路径段，折叠回标准路径
	pathname = normalizeDoubledPath(pathname)
	// 日志记录折叠后的标准路径：统计端（ComputeStats）按标准路径排除
	// 模型列表/落地页，记录原始路径会让带 /router /oai 等前缀的请求虚增计数
	entry.Path = pathname

	// 2. HTML landing page -----------------------------------------------------
	if method == http.MethodGet {
		accept := r.Header.Get("Accept")
		if (pathname == "/" || pathname == "/v1") && strings.Contains(accept, "text/html") {
			entry.Status = 200
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(entry)
			html := renderLandingHTML(config.Port)
			return &http.Response{
				StatusCode: 200,
				Status:     "200 OK",
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					"Content-Type": []string{"text/html; charset=utf-8"},
				},
				Body:          io.NopCloser(strings.NewReader(html)),
				ContentLength: int64(len(html)),
			}
		}
	}

	// 3. Auth check ------------------------------------------------------------
	if !CheckAuth(r, *config) {
		entry.Status = 401
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(401, "unauthorized", "authentication_error")
	}

	// 4. Model list endpoint ---------------------------------------------------
	if method == http.MethodGet || method == http.MethodPost {
		if pathname == "/v1/models" || pathname == "/models" || pathname == "/v1beta/models" || pathname == "/v1beta/v1beta/models" {
			models := AggregateModels(config)
			// 浏览器请求统一跳转到首页模型面板（JSON 客户端请求不受影响）。
			// 明确声明 application/json 的客户端（如部分 SDK 默认 Accept 同时含 text/html）
			// 必须返回 JSON，否则会被误跳转导致模型列表拉取失败。
			accept := r.Header.Get("Accept")
			if strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json") {
				return &http.Response{
					StatusCode: 302,
					Status:     "302 Found",
					Header:     http.Header{"Location": []string{"/?tab=models"}},
					Body:       io.NopCloser(strings.NewReader("")),
				}
			}
			return buildModelListResponse(r, models, pathname)
		}
	}

	// 5. Non-POST → error or landing page -------------------------------------
	if method != http.MethodPost {
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "text/html") {
			entry.Status = 200
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(entry)
			html := renderLandingHTML(config.Port)
			return &http.Response{
				StatusCode: 200,
				Status:     "200 OK",
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					"Content-Type": []string{"text/html; charset=utf-8"},
				},
				Body:          io.NopCloser(strings.NewReader(html)),
				ContentLength: int64(len(html)),
			}
		}
		// 探活类请求：CLI / 扩展常把根地址当健康检查（Accept 不含 text/html），
		// 这不是错误，给一个 JSON 状态即可，避免日志里出现刺眼的 405。
		if pathname == "/" || pathname == "/health" || pathname == "/healthz" {
			entry.Status = 200
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(entry)
			body := `{"status":"ok","service":"llm-router"}`
			return &http.Response{
				StatusCode:    200,
				Status:        "200 OK",
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
				Header:        http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
				Body:          io.NopCloser(strings.NewReader(body)),
				ContentLength: int64(len(body)),
			}
		}
		// 浏览器自动请求的站点图标：静默 204（不产生错误日志）
		if pathname == "/favicon.ico" {
			entry.Status = 204
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(entry)
			return &http.Response{
				StatusCode: 204,
				Status:     "204 No Content",
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}
		}
		// 模型端点存在但方法不对才是 405；其它路径压根不存在，按 404 应答
		//（此前一律 405，导致 /api/hello 这类探测在日志里显示成"方法不允许"）
		if isModelPath(pathname) {
			entry.Status = 405
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(entry)
			return ErrorResponse(405, "method not allowed", "invalid_request_error")
		}
		entry.Status = 404
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(404, "not found", "invalid_request_error")
	}

	// 6. Validate path ---------------------------------------------------------
	if !strings.HasPrefix(pathname, "/v1") && pathname != "/chat/completions" &&
		pathname != "/responses" {
		entry.Status = 404
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(404, "not found", "invalid_request_error")
	}

	// 7. Read & parse body ----------------------------------------------------
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		entry.Status = 400
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(400, "failed to read request body", "invalid_request_error")
	}

	var bodyMap map[string]any
	if err := json.Unmarshal(rawBody, &bodyMap); err != nil {
		entry.Status = 400
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(400, "invalid JSON body", "invalid_request_error")
	}

	// 8. ParseInbound ---------------------------------------------------------
	inbound := format.ParseInbound(pathname, bodyMap)
	if inbound == nil {
		entry.Status = 404
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		supported := "supported paths: /v1/chat/completions, /chat/completions, /responses, /v1/messages, /v1beta/models/*:generateContent, /v1beta/models/*:streamGenerateContent"
		return buildTextResponse(404, supported)
	}

	// 超时控制：非流式请求保留 10 分钟整体超时；流式请求不设整体 deadline——
	// context 截止会覆盖整个响应体读取，超长 SSE 流会被中途捖断。
	// 流式场景由客户端断连（r.Context() 取消）兜底。
	if !inbound.Stream {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		r = r.WithContext(ctx)
	}

	// 9. Log request body (truncated) -----------------------------------------
	if inbound.Format == "openai" {
		entry.Req = truncateRunes(string(rawBody), 2000)
	} else {
		entry.Req = truncateRunes(jsonDumps(bodyMap), 2000)
	}

	// 10. Validate messages & model -------------------------------------------
	openaiReq := inbound.OpenaiReq
	if openaiReq == nil {
		entry.Status = 400
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(400, "missing request body", "invalid_request_error")
	}

	model, _ := openaiReq["model"].(string)
	if model == "" {
		entry.Status = 400
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(400, "model is required", "invalid_request_error")
	}
	entry.Model = model
	if _, ok := openaiReq["messages"]; !ok {
		entry.Status = 400
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(400, "messages is required", "invalid_request_error")
	}

	// 11. Resolve provider（未命中时按兜底模型链逐层改写重解析，见 ResolveWithFallback）
	requestedModel := model // 客户端原始模型名：兜底改写后据此判断能否原始 body 直通
	provider, stripAt, model := ResolveWithFallback(model, config)
	if provider == nil {
		msg := "no provider found for model: " + model
		if model == "" {
			msg = "no provider found"
		}
		entry.Status = 400
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(400, msg, "invalid_request_error")
	}

	entry.Provider = provider.Name

	// 接入格式以请求路径声明为准（/oai /aai /gai 前缀或标准路径）。转换引擎
	// 支持全格式矩阵（同型直通保留原生字段、跨型经 OpenAI 归一化形状转换），
	// 不再做格式亲和拦截；无对应转换规则的组合由下方查表步骤拒绝。
	declaredFmt := declaredInboundFormat(rawPath)

	// extra_body 摊平：必须在下面「是否携带推理参数」判定与原始 body 抓取之前，
	// 否则包在 extra_body 里的 reasoning_effort 会被判成「客户端未携带」→ 路由器兜底
	// 注入（日志里表现为客户端明明带了却显示 max*），且客户端档位也不会被转换上送。
	if hoistExtraBody(bodyMap) {
		if nb, err := json.Marshal(bodyMap); err == nil {
			rawBody = nb
		}
	}

	// 客户端原始推理参数（「字段=值」）在任何原地改写之前抓一份：下面的
	// sanitizeReasoningEffort 会把 minimal 改写为 low、兜底注入会往 body 里写预设
	// 参数，都会污染「客户端到底发的是什么」，而日志上行要显示后者。
	origReasoning := format.DescribeReasoningNative(inbound.Format, bodyMap)

	// 同型直通：客户端实际请求体格式（inbound.Format，由剥离前缀后的路径决定）
	// 与上游 API 格式完全一致时，不再经 OpenAI 形状中转，直接按原生格式转发，
	// 保留 thinking / safetySettings / fileData 等 OpenAI 形状无法承载的原生字段。
	// openai→openai 已有独立的原始 body 直通分支，这里覆盖 anthropic→anthropic
	// 与 gemini→gemini 两条同型路径。
	// 同型直通还需 body 形似校验：路径声明为 anthropic/gemini、但 body 实为
	// OpenAI 形状（带 max_completion_tokens / 只有 messages 无 contents）时，
	// 必须降级走归一化转换，否则错误形状会被原样上送导致上游 400。
	// 上游为 OpenAI 兼容格式时，清洗客户端消息角色后再转发：
	// 部分上游（GLM 系等）不认识 developer 等新角色，会报
	// [1214] Incorrect role information。developer→system，空角色→user。
	// 同时归一化 reasoning_effort：部分始终思考的模型不接受 minimal，
	// 会报 [1210]，统一降级为 low。
	if isOpenaiLike(string(provider.Type)) {
		changed := sanitizeMessageRoles(bodyMap)
		if sanitizeReasoningEffort(bodyMap) {
			changed = true
		}
		if changed {
			if nb, err := json.Marshal(bodyMap); err == nil {
				rawBody = nb
			}
		}
	}

	// 推理深度参数业务规则：
	//   - payload 未携带推理深度参数 → 按接入数据格式与全局配置档位
	//     （config.ReasoningDepth，默认 high）追加路由预设方案（三种之一），
	//     并强制进入归一化格式转换节点（禁用同型直通），由转换节点把追加参数
	//     映射成上游格式的推理参数；
	//   - 已携带 → 维持下方既有判定：同型直通 / 跨型转换。
	forceConvert := false
	if !HasReasoningDepth(inbound.Format, bodyMap, inbound.OpenaiReq) {
		tier := ""
		if config != nil {
			tier = config.ReasoningDepth
		}
		if InjectDefaultReasoning(inbound.Format, tier, bodyMap, inbound.OpenaiReq) {
			forceConvert = true
			// 日志「推理」列：带 "*" 后缀 = 客户端未携带，由路由器按全局档位注入
			entry.Reasoning = normalizeTier(tier) + "*"
		}
	} else {
		// 客户端显式携带：上行显示原生「字段=值」原文（不覆盖客户端的选择）；
		// 认不出具体字段时退回展示值（如 Anthropic 只给了 thinking.type）
		entry.Reasoning = origReasoning
		if entry.Reasoning == "" {
			entry.Reasoning = ClientReasoningLabel(inbound.Format, bodyMap, inbound.OpenaiReq)
		}
	}

	sameType := sameTypePassthrough(inbound.Format, provider.Type) && isBodyCompatibleWithFormat(inbound.Format, bodyMap) && !forceConvert
	// 同型直通仅信任官方上游域名：Claude Code 请求体携带大量 Anthropic 新协议
	// 字段（context_management、块级 cache_control 等），第三方 Anthropic 兼容层
	// （GLM / z.ai / 本地服务器）按旧规范实现，直通会被严格校验拒绝（[1210]）。
	// 这类上游必须走归一化转换（白名单重建天然剥离未实现字段）。
	sameType = sameType && allowSameTypePassthrough(inbound.Format, *provider)

	// 12. Strip "@provider" hint suffix — 仅当解析确实走了 @ 提示分支时才剥离，
	// 避免破坏模型名本身含 @ 的合法请求（如 gpt-4@2024-01） ---------------------
	outModel := model
	if stripAt {
		if idx := strings.LastIndex(outModel, "@"); idx >= 0 {
			outModel = outModel[:idx]
		}
	}
	// 别名 → 真实模型名。此处提前解析，供同型直通写回 body/URL；
	// 归一化转换路径在 BuildOutboundRequest 内部也会再解析一次，结果一致。
	// 支持通配别名（gemini-* 等）：精确/忽略大小写优先，通配模式兜底。
	resolved := outModel
	if alias, ok := provider.Aliases[outModel]; ok {
		resolved = alias
	} else {
		wild := ""
		for k, v := range provider.Aliases {
			if strings.EqualFold(k, outModel) {
				resolved = v
				break
			}
			if strings.Contains(k, "*") && MatchModel(k, outModel) {
				// 记录首个通配命中（同一供应商内若存在多条重名模式，取其一）
				wild = v
			}
		}
		if resolved == outModel && wild != "" {
			resolved = wild
		}
	}

	// 日志/统计记录实际生效的模型名（别名改写后）：客户端发的是内置 ID（如
	// gemini-3.8-flash），日志这列应显示改写后真正上送的模型，便于验证映射生效。
	entry.Model = resolved

	// 路由决定：此刻起该模型（别名解析后的真实模型名）视为"正在使用"
	// （概览页光波动画）。用解析后的模型名登记，与日志/统计的模型维度一致，
	// 否则走别名路由时客户端原始名与统计键对不上，"正在使用"高亮会落空。
	// 返回的 stop 在请求结束时（defer）归还计数，请求完成光波即熄灭。
	// 回调可能为 nil（测试/独立使用场景）。
	if onActive != nil && entry.Model != "" {
		stop := onActive(entry.Model)
		if stop != nil {
			defer stop()
		}
	}

	// 13. Build outbound request ----------------------------------------------
	// 经格式转换规则引擎按 (接入方式 src, 供应商 API 格式 dst) 查表取得转换规则，
	// 由该规则把客户端请求转换成目标上游格式。同型直通由规则内部按 SameType 决定
	// 是否原样转发原生字段（triggered 在 exactOrNormalize / defaultRequest 中完成）。
	reqRule, err := transrules.LookupRequest(declaredFmt, string(provider.Type))
	if err != nil {
		// 接入格式 → 目标格式无对应转换规则（如 gemini 接入指定 openai 上游），
		// 拒绝并提示，而非静默透传错误形状。
		msg := "该接入格式（" + declaredFmt + "）到供应商「" + provider.Name + "」的 API 格式（" + string(provider.Type) + "）暂无转换规则，已拒绝请求。"
		entry.Status = 400
		entry.Resp = truncateRunes(msg, 2000)
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(400, msg, "invalid_request_error")
	}
	var ob transrules.OutboundRequest
	ob, err = reqRule(transrules.RequestContext{
		Provider:  *provider,
		OpenAIReq: openaiReq,
		RawBody:   bodyMap,
		OutModel:  resolved,
		Stream:    inbound.Stream,
		SameType:  sameType,
	})
	if err != nil {
		entry.Status = 500
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(500, "failed to convert outbound request: "+err.Error(), "internal_error")
	}

	// 14. Determine if we can use raw body passthrough ------------------------
	// Passthrough: openai→openai，且模型名无变化（未走 @ 剥离、未走别名替换）、
	// 无 content-encoding 转换。若请求是按别名路由的，别名→真实模型名在
	// BuildOutboundRequest 内完成，必须走非直通分支转发 ob.Body，否则上游收到的是
	// 别名原样名字，报 model not found，别名功能等于失效。
	// 追加了默认推理参数的请求禁止原始 body 直通（直通会绕过转换节点，
	// 注入参数不会映射到上游格式），必须走重建分支。
	// 走兜底模型时 model 已被改写成兜底名，而 rawBody 里仍是客户端原始模型名：
	// 此时若直通，上游收到的还是那个解析不到的名字，兜底等于失效。故要求
	// 「客户端原始模型 == 改写后模型」，兜底命中的请求必须走重建分支。
	canPassthrough := inbound.Format == "openai" && isOpenaiLike(string(provider.Type)) &&
		resolved == model && model == requestedModel &&
		r.Header.Get("Content-Encoding") == "" && !forceConvert
	// 流式 openai→openai 若直通原始 body，BuildOutboundRequest 注入的
	// stream_options.include_usage 会被丢弃，上游 SSE 不携带 usage，Token 统计恒为 0。
	// 仅当客户端已自带 stream_options.include_usage=true 时才允许直通，否则走重建分支
	// 补上注入。（仅检查 stream_options 存在会被空对象/ include_usage:false 绕过。）
	if canPassthrough && inbound.Stream && isOpenaiLike(string(provider.Type)) {
		var parsed map[string]any
		if err := json.Unmarshal(rawBody, &parsed); err != nil {
			canPassthrough = false
		} else {
			so, _ := parsed["stream_options"].(map[string]any)
			if inc, ok := so["include_usage"].(bool); !ok || !inc {
				canPassthrough = false
			}
		}
	}

	var upstreamBody []byte

	switch {
	case sameType:
		// 同型直通：body = 客户端原生请求体（模型已按别名/@ 解析后写回）
		b, err := json.Marshal(ob.Body)
		if err != nil {
			entry.Status = 500
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(entry)
			return ErrorResponse(500, "failed to serialize outbound body", "internal_error")
		}
		upstreamBody = b
	case canPassthrough:
		upstreamBody = rawBody
	default:
		b, err := json.Marshal(ob.Body)
		if err != nil {
			entry.Status = 500
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(entry)
			return ErrorResponse(500, "failed to serialize outbound body", "internal_error")
		}
		upstreamBody = b
	}

	// 日志「推理」列（下行）：从真正发往上游的请求体里读回原生推理「字段=值」。
	// 重建/同型直通读 ob.Body（映射表落下的字段），原始 body 直通读客户端 body
	// （该分支只发生在 openai→openai，字段名与上游一致）。
	if canPassthrough {
		entry.ReasoningOut = format.DescribeReasoningNative(format.ReasoningFamilyOpenAI, bodyMap)
	} else if obBody, ok := ob.Body.(map[string]any); ok {
		entry.ReasoningOut = format.DescribeReasoningNative(upstreamReasoningFamily(*provider), obBody)
	}

	// 跨型归一化转换：目标格式白名单重建会丢弃其不认识的字段。记录这些被丢弃的
	// 请求字段（显式降级），让"静默丢失"可查；同型直通不做重建，无需检查。
	if !sameType {
		logDroppedRequestFields(openaiReq, ob.Body)
	}

	// 15. Create upstream HTTP request
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, ob.URL, bytes.NewReader(upstreamBody))
	if err != nil {
		entry.Status = 500
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(500, "failed to create upstream request", "internal_error")
	}

	for k, v := range ob.Headers {
		upstreamReq.Header.Set(k, v)
	}
	// opencode Zen GO 网关要求 x-opencode-session 会话头（缺失 400），客户端
	// 带了透传、没带则用服务商派生的确定性会话 ID
	applyOpencodeSession(upstreamReq.Header, *provider, r)
	// 系统设置的「请求头注入」规则：按供应商匹配后逐条追加自定义请求头
	applyHeaderRules(upstreamReq.Header, provider, config)

	// 16. Execute upstream request --------------------------------------------
	// 流式请求用无整体超时的 client，避免 Client.Timeout 捖断超长 SSE 流
	upstreamClient := upstreamHTTPClient
	if inbound.Stream {
		upstreamClient = streamingHTTPClient
	}
	upstreamResp, err := upstreamClient.Do(upstreamReq)
	if err != nil {
		// 区分客户端断连与上游错误：客户端主动关闭连接时 context 被取消，
		// 上游请求会失败；如果 context 已取消，说明是客户端断连，不算 502。
		if r.Context().Err() != nil {
			entry.Status = 499
			entry.Resp = truncateRunes("client disconnected: "+r.Context().Err().Error(), 2000)
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(entry)
			return ErrorResponse(499, "client disconnected", "client_error")
		}
		entry.Status = 502
		// 把具体错误写进日志（此前只返回给客户端，日志里 resp 恒为空，
		// 导致"响应到一半停止"的 502 无法追溯是超时还是被对端断连）
		entry.Resp = truncateRunes("upstream request failed: "+redactURL(err.Error()), 2000)
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		return ErrorResponse(502, "upstream request failed: "+redactURL(err.Error()), "upstream_error")
	}
	defer upstreamResp.Body.Close() // 注：同型直通流式路径会在 passthroughRawStream 内再 Close 一次，二次 Close 对 http.Response.Body 是安全的（幂等）。

	// 17. Handle upstream error -----------------------------------------------
	if upstreamResp.StatusCode >= 400 {
		if sameType {
			// 同型直通：原样透传上游的原生错误体（Anthropic/Gemini 格式），
			// 否则同格式客户端 SDK 解析不了 OpenAI 形状的错误结构。
			// 超过 64KB 上限时不能把截断的半个 JSON 原样透传（客户端 SDK
			// 反序列化失败，真实错误信息反而丢失），包装成合法错误对象。
			const errLimit = 64 << 10
			rawErr, _ := io.ReadAll(io.LimitReader(upstreamResp.Body, errLimit+1))
			if len(rawErr) > errLimit {
				wrapped, _ := json.Marshal(map[string]any{"error": map[string]any{
					"message": "upstream error body exceeds 64KB and was truncated: " + truncateRunes(string(rawErr[:errLimit]), 512),
					"type":    "upstream_error",
				}})
				rawErr = wrapped
			}
			entry.Status = upstreamResp.StatusCode
			entry.Resp = truncateRunes(string(rawErr), 2000)
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(entry)
			ct := upstreamResp.Header.Get("Content-Type")
			if ct == "" {
				ct = "application/json"
			}
			h := http.Header{"Content-Type": []string{ct}}
			passUpstreamHeaders(h, upstreamResp.Header)
			return &http.Response{
				StatusCode:    upstreamResp.StatusCode,
				Status:        upstreamResp.Status,
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
				Header:        h,
				Body:          io.NopCloser(bytes.NewReader(rawErr)),
				ContentLength: int64(len(rawErr)),
			}
		}
		status, errBody := format.UpstreamErrorForClient(inbound.Format, string(provider.Type), upstreamResp.Body, upstreamResp.StatusCode)
		b, _ := json.Marshal(errBody)
		entry.Status = status
		entry.Resp = truncateRunes(string(b), 2000)
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(entry)
		h := http.Header{"Content-Type": []string{"application/json"}}
		passUpstreamHeaders(h, upstreamResp.Header)
		return &http.Response{
			StatusCode:    status,
			Status:        http.StatusText(status),
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        h,
			Body:          io.NopCloser(bytes.NewReader(b)),
			ContentLength: int64(len(b)),
		}
	}

	// 18. Streaming response --------------------------------------------------
	if sameType {
		if inbound.Stream {
			// 同型直通流：逐字节转发原生 SSE（保留事件名与原生字段）
			return handleRawStream(w, r, upstreamResp, provider, &entry, start, logger)
		}
		// 同型直通非流：原样转发原生 JSON
		return handleRawNonStream(r, upstreamResp, provider, &entry, start, logger)
	}
	if inbound.Stream {
		// 防护：部分上游（聚合代理/本地服务）忽略 stream 字段，返回 200 +
		// application/json。仍按 SSE 解析会得到空流静默结束（状态 200、无错误、
		// 无日志，排查极难），此处检测到非流式 Content-Type 时回落非流式转换路径。
		// NDJSON 流与无 Content-Type 的情况保持原流式路径。
		ct := strings.ToLower(upstreamResp.Header.Get("Content-Type"))
		if ct != "" && strings.Contains(ct, "json") &&
			!strings.Contains(ct, "event-stream") && !strings.Contains(ct, "ndjson") {
			return handleNonStream(r, upstreamResp, provider, &entry, start, inbound.Format, outModel, openaiReq, logger)
		}
		return handleStream(w, r, upstreamResp, provider, &entry, start, inbound.Format, outModel, logger)
	}

	// 19. Non-streaming response ----------------------------------------------
	return handleNonStream(r, upstreamResp, provider, &entry, start, inbound.Format, outModel, openaiReq, logger)
}

// ---------------------------------------------------------------------------
// Model list response builder
// ---------------------------------------------------------------------------

// buildModelListResponse returns an *http.Response containing the model list
// in the format appropriate for the request (OpenAI, Gemini, or Anthropic).
func buildModelListResponse(r *http.Request, models []ModelEntry, pathname string) *http.Response {
	var body any

	if strings.Contains(pathname, "/v1beta") {
		// Gemini format
		type geminiModel struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			OwnedBy                    string   `json:"owned_by"`
		}
		geminiModels := make([]geminiModel, 0, len(models))
		for _, m := range models {
			geminiModels = append(geminiModels, geminiModel{
				Name:                       "models/" + m.ID,
				DisplayName:                m.ID,
				SupportedGenerationMethods: []string{"generateContent", "streamGenerateContent"},
				OwnedBy:                    m.Provider,
			})
		}
		body = map[string]any{"models": geminiModels}
	} else if r.Header.Get("anthropic-version") != "" {
		// Anthropic format
		type anthropicModel struct {
			ID          string `json:"id"`
			Type        string `json:"type"`
			DisplayName string `json:"display_name"`
			CreatedAt   string `json:"created_at"`
			OwnedBy     string `json:"owned_by"`
		}
		anthropicModels := make([]anthropicModel, 0, len(models))
		for _, m := range models {
			anthropicModels = append(anthropicModels, anthropicModel{
				ID:          m.ID,
				Type:        "model",
				DisplayName: m.ID,
				CreatedAt:   "2024-01-01T00:00:00.000Z",
				OwnedBy:     m.Provider,
			})
		}
		body = map[string]any{
			"data":     anthropicModels,
			"has_more": false,
		}
	} else {
		// OpenAI format
		type openaiModel struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		}
		openaiModels := make([]openaiModel, 0, len(models))
		for _, m := range models {
			openaiModels = append(openaiModels, openaiModel{
				ID:      m.ID,
				Object:  "model",
				OwnedBy: m.Provider,
			})
		}
		body = map[string]any{
			"object": "list",
			"data":   openaiModels,
		}
	}

	b, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:          io.NopCloser(bytes.NewReader(b)),
		ContentLength: int64(len(b)),
	}
}

// ---------------------------------------------------------------------------
// Streaming handler
// ---------------------------------------------------------------------------

// handleStream processes a streaming response. It sets SSE headers on w and
// writes chunk data directly. Returns nil since the response was already
// written to w.
func handleStream(w http.ResponseWriter, r *http.Request, upstreamResp *http.Response, provider *types.Provider, entry *types.LogEntry, start time.Time, inboundFormat, outModel string, logger func(types.LogEntry)) *http.Response {

	ptype := string(provider.Type)

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)

	flusher, canFlush := w.(http.Flusher)

	// Openai→openai stream passthrough: read upstream SSE byte-by-byte,
	// write directly to client while scanning for usage.
	if inboundFormat == "openai" && isOpenaiLike(ptype) {
		lastUsage, readErr := passthroughOpenAIStream(w, upstreamResp.Body, canFlush, flusher)
		entry.Status = 200
		// 读取中断（上游断连/超时/客户端断开）时记录错误详情，
		// 否则这类"响应到一半停止"只表现为 200 空日志，无法定位根因。
		// 中断的流只算失败请求，不记 usage，避免 Token 统计被虚增。
		if readErr != nil {
			// 区分客户端断连与真正的上游/写入错误：客户端主动关闭连接时 context
			// 被取消，流已部分传输成功，保持 200（与下方其它流式分支一致），
			// 仅真正的上游读取/未知错误才记为 502。
			if r.Context().Err() != nil {
				// 客户端主动断连，流已部分传输，保持 200
			} else {
				entry.Status = 502
				entry.Resp = truncateRunes("stream read failed: "+readErr.Error(), 2000)
			}
		} else if lastUsage != nil {
			entry.Usage = lastUsage
		}
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(*entry)
		return nil
	}

	// Other stream types: get chunks from upstream, convert to client format.
	// 转换结果逐事件增量写出并逐事件 flush，客户端能实时看到内容
	// （此前是先拼完整串再一次性写出，流式形同虚设）。
	chunks := upstreamChunks(ptype, upstreamResp)

	fw := &flushWriter{w: w, f: flusher}

	var lastUsage *types.Usage
	var writeErr error
	// 经格式转换规则引擎按客户端接入格式查表取得流式写出规则。
	streamRule := transrules.LookupStream(inboundFormat)
	lastUsage, writeErr = streamRule(fw, chunks, outModel)

	// 客户端断连：与 openai 直通路径保持一致，计为失败且不记 usage
	entry.Status = 200
	if writeErr != nil {
		// 区分客户端断连与真正的写入错误：客户端主动关闭连接时 context 被取消，
		// 写入会失败；如果 context 已取消，说明是客户端断连，不算 502。
		if r.Context().Err() != nil {
			// 客户端主动断连，流已部分传输成功，保持 200
		} else {
			entry.Status = 502
			entry.Resp = truncateRunes("client write failed: "+writeErr.Error(), 2000)
		}
	} else if lastUsage != nil {
		entry.Usage = lastUsage
	}
	entry.Ms = int(time.Since(start).Milliseconds())
	logger(*entry)
	return nil
}

// flushWriter 使 http.ResponseWriter 满足 format 包的 chunkFlusher 接口，
// 让流式转换器在写出每个 SSE 事件后主动 flush。
type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw *flushWriter) Write(p []byte) (int, error) { return fw.w.Write(p) }

func (fw *flushWriter) Flush() error {
	if fw.f != nil {
		fw.f.Flush()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Non-streaming handler
// ---------------------------------------------------------------------------

// handleNonStream processes a non-streaming response. It reads the upstream
// body, converts the response format if needed, and returns an *http.Response.
func handleNonStream(r *http.Request, upstreamResp *http.Response, provider *types.Provider, entry *types.LogEntry, start time.Time, inboundFormat, outModel string, openaiReq map[string]any, logger func(types.LogEntry)) *http.Response {

	ptype := string(provider.Type)

	bodyBytes, err := io.ReadAll(io.LimitReader(upstreamResp.Body, 50<<20)) // 限制 50MB
	if err != nil {
		// 区分客户端断连与上游错误：客户端主动关闭连接时 context 被取消，
		// 读取会失败；如果 context 已取消，说明是客户端断连，不算 502。
		if r.Context().Err() != nil {
			// 客户端主动断连，返回 499（客户端关闭）更准确
			entry.Status = 499
			entry.Resp = truncateRunes("client disconnected: "+r.Context().Err().Error(), 2000)
			entry.Ms = int(time.Since(start).Milliseconds())
			logger(*entry)
			return ErrorResponse(499, "client disconnected", "client_error")
		}
		entry.Status = 502
		// 记录具体读取错误（断连/超时），否则日志里只有空 502 无法追溯
		entry.Resp = truncateRunes("failed to read upstream response: "+err.Error(), 2000)
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(*entry)
		return ErrorResponse(502, "failed to read upstream response: "+err.Error(), "upstream_error")
	}
	// 读满 50MB 说明响应被截断，直接报错而不是转发损坏的 JSON
	if len(bodyBytes) >= 50<<20 {
		entry.Status = 502
		entry.Resp = "upstream response too large (>50MB)"
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(*entry)
		return ErrorResponse(502, "upstream response too large (>50MB)", "upstream_error")
	}

	// Openai→openai: parse JSON, extract usage, return raw bytes.
	if inboundFormat == "openai" && isOpenaiLike(ptype) {
		var parsed map[string]any
		if json.Unmarshal(bodyBytes, &parsed) == nil {
			// 上游返回字面 null 时 Unmarshal 成功且 parsed 为 nil，
			// 直接补齐字段会对 nil map 赋值 panic；归一化为空对象再走补齐。
			if parsed == nil {
				parsed = map[string]any{}
			}
			entry.Usage = extractUsage(parsed)
			// 某些上游（聚合代理）返回的 OpenAI 响应缺少必需的标准字段
			// （id / object / created），会导致客户端反序列化失败
			// （如 grokcli: missing field `id` / `object`）。
			ensureOpenAICompletionFields(parsed)
			bodyBytes, _ = json.Marshal(parsed)
		}
		entry.Status = upstreamResp.StatusCode
		entry.Resp = truncateRunes(string(bodyBytes), 2000)
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(*entry)

		contentType := upstreamResp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		h := http.Header{"Content-Type": []string{contentType}}
		passUpstreamHeaders(h, upstreamResp.Header)
		return &http.Response{
			StatusCode:    upstreamResp.StatusCode,
			Status:        upstreamResp.Status,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        h,
			Body:          io.NopCloser(bytes.NewReader(bodyBytes)),
			ContentLength: int64(len(bodyBytes)),
		}
	}

	// Other types: parse, convert to OpenAI, then to client format.
	var upstreamParsed map[string]any
	if err := json.Unmarshal(bodyBytes, &upstreamParsed); err != nil {
		entry.Status = 502
		entry.Ms = int(time.Since(start).Milliseconds())
		logger(*entry)
		return ErrorResponse(502, "failed to parse upstream response: "+err.Error(), "upstream_error")
	}

	// Convert to OpenAI shape first
	openaiResp := format.OpenaiFromUpstream(ptype, upstreamParsed, openaiReq)
	entry.Usage = extractUsage(openaiResp)

	// Convert to client format: 经格式转换规则引擎按 (上游格式, 客户端接入格式)
	// 查表逆向转换，把已归一化为 OpenAI 形状的响应还原为客户端声明的格式。
	conv := transrules.LookupNonStream(ptype, inboundFormat)
	respBody := conv(openaiResp, outModel)

	b, _ := json.Marshal(respBody)

	entry.Status = upstreamResp.StatusCode
	entry.Resp = truncateRunes(string(b), 2000)
	entry.Ms = int(time.Since(start).Milliseconds())
	logger(*entry)

	h := http.Header{"Content-Type": []string{"application/json"}}
	passUpstreamHeaders(h, upstreamResp.Header)
	return &http.Response{
		StatusCode:    upstreamResp.StatusCode,
		Status:        upstreamResp.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        h,
		Body:          io.NopCloser(bytes.NewReader(b)),
		ContentLength: int64(len(b)),
	}
}

// ---------------------------------------------------------------------------
// OpenAI stream passthrough (byte-by-byte with usage scanning)
// ---------------------------------------------------------------------------

// passthroughOpenAIStream 流式透传 OpenAI SSE，并在透传过程中：
//  1. 逐事件扫描 usage（供 Token 统计）；
//  2. 若某个 data: 事件的 JSON 缺少必需的 id 字段（部分上游/聚合代理不回填 id），
//     注入随机 id 再写出，避免客户端反序列化失败（如 grokcli: missing field `id`；
//     非流式的同类问题在 handleNonStream 中处理）。
//
// 返回最后一次扫描到的 usage，以及异常结束原因（上游断连/客户端写入失败），
// 供上层记录日志。客户端主动断连（context.Canceled 且已部分传输）视为干净结束。
func passthroughOpenAIStream(w http.ResponseWriter, body io.ReadCloser, canFlush bool, flusher http.Flusher) (*types.Usage, error) {
	defer body.Close()

	var lastUsage *types.Usage
	var readErr error
	var bytesWritten int64

	writeRaw := func(p []byte) error {
		if len(p) == 0 {
			return nil
		}
		if _, werr := w.Write(p); werr != nil {
			return fmt.Errorf("client write failed: %w", werr)
		}
		bytesWritten += int64(len(p))
		if canFlush {
			flusher.Flush()
		}
		return nil
	}

	// 同一响应全部分片共用 id/created（与真实 OpenAI 一致）；上游缺字段时补
	chunkID := "chatcmpl-" + randHex(16)
	created := time.Now().Unix()

	// emitLine 写出一条完整的 SSE 行（不含换行，换行由调用方补写）。
	emitLine := func(line []byte) error {
		// 仅对 data: 的 JSON 行做归一化 + usage 扫描；其余行原样透传
		trimmed := bytes.TrimSpace(line)
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			return writeRaw(line)
		}
		data := bytes.TrimSpace(trimmed[len("data:"):])
		// 保留行尾的 \r（CRLF 风格）
		hasCR := len(line) > 0 && line[len(line)-1] == '\r'

		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			return writeRaw(line)
		}

		var chunk map[string]any
		if err := json.Unmarshal(data, &chunk); err != nil {
			return writeRaw(line)
		}
		if chunk == nil {
			// data: null 原样透传：Unmarshal 对 null 成功但得到 nil map，
			// 后续补 id/object 会 panic
			return writeRaw(line)
		}
		if usage := extractUsage(chunk); usage != nil {
			lastUsage = usage
		}
		needRewrite := false
		if _, ok := chunk["id"]; !ok {
			chunk["id"] = chunkID
			needRewrite = true
		}
		if _, ok := chunk["object"]; !ok {
			chunk["object"] = "chat.completion.chunk"
			needRewrite = true
		}
		if _, ok := chunk["created"]; !ok {
			chunk["created"] = created
			needRewrite = true
		}
		if _, ok := chunk["model"]; !ok {
			chunk["model"] = ""
			needRewrite = true
		}
		if needRewrite {
			re, _ := json.Marshal(chunk)
			out := []byte("data: " + string(re))
			if hasCR {
				out = append(out, '\r')
			}
			return writeRaw(out)
		}
		return writeRaw(line)
	}

	buf := make([]byte, 4096)
	var pending []byte

	for {
		n, err := body.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			for {
				idx := bytes.IndexByte(pending, '\n')
				if idx < 0 {
					break
				}
				if werr := emitLine(pending[:idx]); werr != nil {
					return lastUsage, werr
				}
				if werr := writeRaw([]byte("\n")); werr != nil {
					return lastUsage, werr
				}
				pending = pending[idx+1:]
			}
			// 无换行的异常超长行：原样透传，避免累积导致内存膨胀
			if len(pending) > maxPassthroughLine {
				if werr := writeRaw(pending); werr != nil {
					return lastUsage, werr
				}
				pending = nil
			}
		}
		if err != nil {
			if err != io.EOF {
				if errors.Is(err, context.Canceled) && bytesWritten > 0 {
					// 客户端主动断连，流已部分传输，视为干净结束
				} else {
					readErr = fmt.Errorf("upstream body read: %w", err)
				}
			}
			break
		}
	}
	if len(pending) > 0 {
		if werr := emitLine(pending); werr != nil {
			return lastUsage, werr
		}
	}
	return lastUsage, readErr
}

// passthroughRawStream 逐字节转发上游 SSE 流并扫描 usage。
// scan 负责从单个 SSE 事件对象中提取用量（按协议定制）；除了 OpenAI 的
// extractUsage，anthropic/gemini 同型直通也复用此函数做字节转发 + 用量摄取，
// 从而在保留原生流式字段的同时维持 Token 统计。
func passthroughRawStream(w http.ResponseWriter, body io.ReadCloser, canFlush bool, flusher http.Flusher, scan func(map[string]any) *types.Usage) (*types.Usage, error) {
	defer body.Close()

	var lastUsage *types.Usage
	var readErr error
	var bytesWritten int64
	buf := make([]byte, 4096)
	var lineBuf []byte

	for {
		n, err := body.Read(buf)
		if n > 0 {
			// Write to client
			if _, werr := w.Write(buf[:n]); werr != nil {
				return lastUsage, fmt.Errorf("client write failed: %w", werr)
			}
			bytesWritten += int64(n)
			if canFlush {
				flusher.Flush()
			}

			// Scan for usage in SSE lines. lineBuf 仅用于在流中查找 usage 块，
			// 必须限制其大小：异常上游若吐出无换行的超长行，这里会无限累积导致
			// 内存膨胀。超过单行上限(maxPassthroughLine)时丢弃已缓冲内容——
			// 该行不再参与 usage 扫描，但字节仍原样转发给客户端，不影响流式输出。
			if len(lineBuf) < maxPassthroughLine {
				lineBuf = append(lineBuf, buf[:n]...)
			}
			for {
				idx := bytes.IndexByte(lineBuf, '\n')
				if idx < 0 {
					break
				}
				line := string(lineBuf[:idx])
				lineBuf = lineBuf[idx+1:]

				// Trim trailing \r
				line = strings.TrimRight(line, "\r")

				if strings.HasPrefix(line, "data:") {
					data := strings.TrimSpace(line[5:])
					if data == "[DONE]" {
						continue
					}
					var chunk map[string]any
					if json.Unmarshal([]byte(data), &chunk) == nil {
						if usage := scan(chunk); usage != nil {
							lastUsage = usage
						}
					}
				}
			}
			// 丢弃超限行后保留最后一个 \n 之后的半行尾部：若整体置空，下一块
			// 会从行中间开始累积，后续 data: 行解析全部错位（usage 静默漏采）。
			if len(lineBuf) >= maxPassthroughLine {
				if idx := bytes.LastIndexByte(lineBuf, '\n'); idx >= 0 {
					lineBuf = append(lineBuf[:0], lineBuf[idx+1:]...)
				} else {
					lineBuf = lineBuf[:0]
				}
			}
		}
		if err != nil {
			// io.EOF 是上游正常结束；其它错误（连接被断、超时）记录详情
			if err != io.EOF {
				// 区分客户端断连与上游错误：客户端断开时 context 被取消，
				// 上游读取会收到 context.Canceled；如果已经向客户端写过数据，
				// 说明流是正常的，只是客户端提前离开，不算 502。
				if errors.Is(err, context.Canceled) && bytesWritten > 0 {
					// 客户端主动断连，流已部分传输成功，不记为错误
				} else {
					readErr = fmt.Errorf("upstream body read: %w", err)
				}
			}
			break
		}
	}

	// Flush remaining buffered line
	if len(lineBuf) > 0 {
		line := string(lineBuf)
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(line[5:])
			if data != "[DONE]" {
				var chunk map[string]any
				if json.Unmarshal([]byte(data), &chunk) == nil {
					if usage := scan(chunk); usage != nil {
						lastUsage = usage
					}
				}
			}
		}
	}

	return lastUsage, readErr
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// upstreamChunks 按上游 API 格式经格式转换规则引擎查表取得分片解析规则，
// 把上游原生 SSE 响应体解析为统一的 chunk 事件流。
func upstreamChunks(ptype string, resp *http.Response) <-chan map[string]any {
	return transrules.LookupChunk(ptype)(resp.Body)
}

// extractUsage pulls token usage from a response map. Works with both
// OpenAI-shaped usage (prompt_tokens, completion_tokens) and already-parsed
// float64/int values.
func extractUsage(m map[string]any) *types.Usage {
	usage, ok := m["usage"].(map[string]any)
	if !ok {
		return nil
	}

	var pt, ct *int
	if v, ok := numInt(usage, "prompt_tokens"); ok {
		pt = &v
	}
	if v, ok := numInt(usage, "completion_tokens"); ok {
		ct = &v
	}
	if pt == nil && ct == nil {
		return nil
	}
	return &types.Usage{PromptTokens: pt, CompletionTokens: ct}
}

// numInt extracts an int value from a map[key], handling both float64 and int types.
func numInt(m map[string]any, key string) (int, bool) {
	switch v := m[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}

func buildTextResponse(status int, message string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header: http.Header{
			"Content-Type": []string{"text/plain; charset=utf-8"},
		},
		Body:          io.NopCloser(strings.NewReader(message)),
		ContentLength: int64(len(message)),
	}
}

// ---------------------------------------------------------------------------
// Landing page HTML
// ---------------------------------------------------------------------------

// renderLandingHTML returns a complete dark-themed HTML landing page showing
// the LLM Router status, base URLs, and useful links.
// renderLandingHTML returns a complete dark-themed admin dashboard SPA
// with sidebar navigation: 概览 / 模型 / 日志 / 统计.
func renderLandingHTML(port int) string {
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>LLM Router · 管理面板</title>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'%3E%3Cdefs%3E%3ClinearGradient id='g' x1='0' y1='0' x2='1' y2='1'%3E%3Cstop offset='0' stop-color='%2334d399'/%3E%3Cstop offset='1' stop-color='%2322c55e'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width='24' height='24' rx='6' fill='url(%23g)'/%3E%3Cpath d='M13 3 4.5 14h6L9 21l9-11h-6l1-7z' fill='white'/%3E%3C/svg%3E" />
<style>
:root {
  --bg: #000000; --surface: #0a0a0a; --surface-2: #141417;
  --border: #26262b; --border-strong: #3f3f48;
  --text: #f0ead9; --text-2: #c6bdae; --text-3: #9a9182;
  --accent: #34d399; --accent-2: #22c55e;
  --green: #4ade80; --red: #fb7185; --amber: #d4a72c;
  --mono: ui-monospace,"Cascadia Code","JetBrains Mono",Consolas,monospace;
  --sans: -apple-system,"Segoe UI","Microsoft YaHei","PingFang SC",sans-serif;
  --ease: cubic-bezier(0.22,1,0.36,1);
  --radius: 12px; --radius-sm: 8px;
}
* { box-sizing:border-box; margin:0; padding:0; }
body { font-family:var(--sans); background:var(--bg); color:var(--text); min-height:100vh; display:flex; flex-direction:column; -webkit-font-smoothing:antialiased; }
::selection { background:rgba(52,211,153,0.30); }
::-webkit-scrollbar { width:6px; height:6px; }
::-webkit-scrollbar-thumb { background:#1d1d22; border-radius:3px; }
::-webkit-scrollbar-track { background:transparent; }

/* ===== 顶栏：品牌 + 导航 + 状态 ===== */
.topbar {
  display:flex; align-items:center; gap:28px;
  min-height:54px; padding:0 22px; flex-shrink:0;
  background:rgba(10,10,10,0.92); backdrop-filter:blur(10px);
  border-bottom:1px solid var(--border);
  position:sticky; top:0; z-index:20; user-select:none;
}
.tb-brand { display:flex; align-items:center; gap:10px; }
.sb-logo {
  width:30px; height:30px; border-radius:9px;
  display:flex; align-items:center; justify-content:center;
  background:linear-gradient(135deg,var(--accent),var(--accent-2));
  box-shadow:0 3px 10px rgba(52,211,153,0.25);
  flex-shrink:0;
}
.sb-name { font-size:13.5px; font-weight:750; letter-spacing:0.2px; line-height:1.15; }
.sb-ver { font-size:9.5px; color:var(--text-3); }
.tb-nav { display:flex; align-items:stretch; gap:2px; align-self:stretch; }
.nav-item {
  display:flex; align-items:center; gap:7px;
  padding:0 14px;
  font-size:12.5px; font-weight:550; color:var(--text-3);
  cursor:pointer; border-bottom:2px solid transparent;
  transition:color 0.18s var(--ease), border-color 0.18s var(--ease);
}
.nav-item:hover { color:var(--text-2); }
.nav-item.active { color:var(--accent); border-bottom-color:var(--accent); }
.nav-item svg { flex-shrink:0; opacity:0.75; }
.nav-item.active svg { opacity:1; }
.tb-status { margin-left:auto; display:flex; align-items:center; gap:10px; }
.hdot {
  width:8px; height:8px; border-radius:50%;
  background:var(--green);
  box-shadow:0 0 0 3px rgba(74,222,128,0.18);
  animation:pulse 2s ease-out infinite;
}
.hdot.off { background:var(--red); box-shadow:0 0 0 3px rgba(251,113,133,0.18); animation:none; }
.hstatus { font-size:12px; font-weight:650; }
.hport {
  font-family:var(--mono); font-size:11px; color:var(--text-3);
  background:var(--surface-2); border:1px solid var(--border);
  padding:3px 10px; border-radius:999px;
}
.hrefresh { font-size:11px; color:var(--text-3); }

/* ===== 内容区 ===== */
.content { flex:1; width:100%; max-width:1060px; margin:0 auto; padding:26px 24px 48px; overflow-y:auto; }
.panel { display:none; animation:rise 0.35s var(--ease) both; }
.panel.active { display:block; }
.sec-title {
  display:flex; align-items:baseline; gap:10px;
  font-size:11px; font-weight:700; color:var(--text-2);
  letter-spacing:1.2px; text-transform:uppercase;
  margin-bottom:12px;
}
.sec-title:not(:first-child) { margin-top:26px; }
.sec-tip { font-weight:400; letter-spacing:0; text-transform:none; color:var(--text-3); font-size:10.5px; }
.btn-clear {
  margin-left:auto; align-self:center;
  font-family:var(--sans); font-size:10.5px; font-weight:600; color:var(--red);
  background:transparent; border:1px solid rgba(251,113,133,0.35); border-radius:6px;
  padding:3px 10px; cursor:pointer; letter-spacing:0; text-transform:none;
  transition:background 0.15s var(--ease), border-color 0.15s var(--ease);
}
.btn-clear:hover { background:rgba(251,113,133,0.10); border-color:var(--red); }

.stat-cards { display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); gap:10px; margin-bottom:22px; }
.stat-card {
  background:var(--surface); border:1px solid var(--border);
  border-radius:var(--radius); padding:16px 18px;
  transition:border-color 0.2s var(--ease);
}
.stat-card:hover { border-color:var(--border-strong); }
.stat-card .sc-num {
  font-family:var(--mono); font-size:22px; font-weight:700;
  letter-spacing:-0.5px;
  background:linear-gradient(120deg,#34d399,#22c55e);
  -webkit-background-clip:text; background-clip:text;
  color:transparent;
}
.stat-card .sc-label { font-size:11px; color:var(--text-3); margin-top:4px; }

.ep {
  display:flex; align-items:center; gap:10px;
  background:var(--surface); border:1px solid var(--border);
  border-radius:var(--radius-sm);
  padding:9px 10px 9px 12px; margin-bottom:8px;
  transition:border-color 0.2s var(--ease);
}
.ep:hover { border-color:rgba(52,211,153,0.40); }
.ep .tag {
  flex-shrink:0;
  font-family:var(--mono); font-size:10px; font-weight:700;
  padding:3px 8px; border-radius:6px; letter-spacing:0.3px;
}
.tag-o { color:#34d399; background:rgba(52,211,153,0.12); border:1px solid rgba(52,211,153,0.32); }
.tag-a { color:#f2a08a; background:rgba(232,130,90,0.12); border:1px solid rgba(232,130,90,0.32); }
.tag-g { color:#9db8dd; background:rgba(125,162,201,0.12); border:1px solid rgba(125,162,201,0.32); }
.ep code {
  flex:1; min-width:0;
  font-family:var(--mono); font-size:12px; color:var(--text);
  white-space:nowrap; overflow:hidden; text-overflow:ellipsis;
  user-select:text;
}
.btn-copy {
  flex-shrink:0;
  font-family:var(--sans); font-size:11px; font-weight:600;
  color:var(--text-2); background:transparent;
  border:1px solid var(--border-strong);
  border-radius:7px; padding:4px 10px; cursor:pointer;
  transition:color 0.18s, border-color 0.18s, background 0.18s;
}
.btn-copy:hover { color:var(--accent); border-color:rgba(52,211,153,0.50); background:rgba(52,211,153,0.08); }
.btn-copy.ok { color:var(--green); border-color:rgba(74,222,128,0.45); background:rgba(74,222,128,0.10); }

.data-table { width:100%; border-collapse:collapse; font-size:12px; }
.data-table th {
  text-align:left; font-weight:650; color:var(--text-3);
  padding:8px 12px; border-bottom:1px solid var(--border-strong);
  font-size:10.5px; letter-spacing:0.5px; text-transform:uppercase;
  white-space:nowrap;
}
.data-table td {
  padding:8px 12px; border-bottom:1px solid var(--border);
  font-family:var(--mono); font-size:11.5px;
  color:var(--text); white-space:nowrap;
}
.data-table tr:hover td { background:var(--surface-2); }
.data-table .s-ok { color:var(--green); }
.data-table .s-err { color:var(--red); }
.data-table .s-dim { color:var(--text-3); }
.data-table .t-right { text-align:right; }
.badge {
  display:inline-block; min-width:34px; text-align:center;
  font-family:var(--mono); font-size:10.5px; font-weight:700;
  padding:2px 7px; border-radius:6px;
}
.badge-ok { color:#86e3ae; background:rgba(74,222,128,0.10); border:1px solid rgba(74,222,128,0.32); }
.badge-warn { color:#e5c15c; background:rgba(212,167,44,0.12); border:1px solid rgba(212,167,44,0.35); }
.badge-err { color:#fb8a96; background:rgba(251,113,133,0.10); border:1px solid rgba(251,113,133,0.32); }

.model-group { margin-bottom:18px; }
.model-group .mg-name {
  display:flex; align-items:center; gap:8px;
  font-size:13px; font-weight:650;
  padding-bottom:8px; margin-bottom:8px;
  border-bottom:1px solid var(--border);
}
.model-group .mg-dot {
  width:8px; height:8px; border-radius:50%;
  background:linear-gradient(135deg,var(--accent),var(--accent-2));
  box-shadow:0 0 0 3px rgba(52,211,153,0.14);
}
.mchip {
  display:inline-flex; align-items:center;
  font-family:var(--mono); font-size:11.5px; color:var(--text-2);
  background:var(--surface-2);
  border:1px solid var(--border);
  border-radius:8px; padding:5px 11px;
  margin:0 6px 8px 0;
  transition:border-color 0.15s;
}
.mchip:hover { border-color:rgba(52,211,153,0.45); color:var(--text); }

.toast {
  position:fixed; bottom:24px; left:50%; transform:translateX(-50%) translateY(10px);
  background:#141417; border:1px solid var(--border-strong);
  color:var(--text); font-size:12px;
  padding:9px 18px; border-radius:10px;
  opacity:0; pointer-events:none;
  transition:opacity 0.25s, transform 0.25s;
  z-index:100;
}
.toast.show { opacity:1; transform:translateX(-50%) translateY(0); }
.toast.ok { border-color:rgba(74,222,128,0.5); color:#a9ecce; }

.pie-section { margin-bottom:20px; }
.pie-row { display:flex; align-items:center; gap:20px; }
.pie-ring { position:relative; width:120px; height:120px; flex-shrink:0; }
.pie-ring svg { width:100%; height:100%; }
.pie-center {
  position:absolute; inset:30px; border-radius:50%;
  background:var(--bg);
  display:flex; flex-direction:column; align-items:center; justify-content:center;
}
.pie-center .pct { font-family:var(--mono); font-size:15px; font-weight:700; color:var(--text); }
.pie-center .pct-label { font-size:9px; color:var(--text-3); margin-top:1px; }
.pie-legend { flex:1; min-width:0; display:flex; flex-direction:column; gap:4px; max-height:140px; overflow-y:auto; }
.pie-legend-item { display:flex; align-items:center; gap:7px; font-size:11px; }
.pie-legend-item .dot { width:8px; height:8px; border-radius:3px; flex-shrink:0; }
.pie-legend-item .name { flex:1; color:var(--text-2); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.pie-legend-item .val { font-family:var(--mono); font-size:10px; color:var(--text-3); }
.pie-empty { color:var(--text-3); font-size:11px; padding:8px 0; }
.active-token {
  animation:shimmer 3s linear infinite;
  background:linear-gradient(90deg,#22c55e,#34d399,#22c55e,#22c55e);
  background-size:300% 100%;
  -webkit-background-clip:text; background-clip:text;
  color:transparent; font-weight:700;
}
@keyframes shimmer { 0%{background-position:0% 50%} 100%{background-position:300% 50%} }

@keyframes rise { from { opacity:0; transform:translateY(8px); } to { opacity:1; transform:none; } }
@keyframes pulse { 0%,100% { box-shadow:0 0 0 0 rgba(74,222,128,0.40); } 50% { box-shadow:0 0 0 6px rgba(74,222,128,0); } }
@media (prefers-reduced-motion:reduce) {
  * { animation-duration:0.01ms !important; transition-duration:0.01ms !important; }
}
@media (max-width:760px) {
  .topbar { flex-wrap:wrap; padding:8px 14px; gap:10px 16px; }
  .tb-nav { order:3; width:100%; overflow-x:auto; }
  .nav-item { padding:8px 12px; border-bottom-width:2px; }
  .tb-status { margin-left:auto; }
  .content { padding:16px 14px 40px; }
  .data-table { font-size:10px; }
  .data-table th, .data-table td { padding:6px 8px; }
}
</style>
</head>
<body>

<header class="topbar">
  <div class="tb-brand">
    <div class="sb-logo">
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" aria-hidden="true"><path d="M13 3 4.5 14h6L9 21l9-11h-6l1-7z" fill="#fff"/></svg>
    </div>
    <div>
      <div class="sb-name">LLM Router</div>
      <div class="sb-ver">v3.0</div>
    </div>
  </div>
  <nav id="nav" class="tb-nav">
    <div class="nav-item active" data-tab="overview">
      <svg viewBox="0 0 18 18" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><rect x="2" y="2" width="6" height="6" rx="1.5"/><rect x="10" y="2" width="6" height="6" rx="1.5"/><rect x="2" y="10" width="6" height="6" rx="1.5"/><rect x="10" y="10" width="6" height="6" rx="1.5"/></svg>
      概览
    </div>
    <div class="nav-item" data-tab="models">
      <svg viewBox="0 0 18 18" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M9 2v14M2 6h14M2 12h14"/><rect x="3" y="3" width="12" height="12" rx="2"/></svg>
      模型
    </div>
    <div class="nav-item" data-tab="logs">
      <svg viewBox="0 0 18 18" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M3 4h12M3 9h12M3 14h8"/></svg>
      日志
    </div>
    <div class="nav-item" data-tab="stats">
      <svg viewBox="0 0 18 18" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><rect x="2" y="10" width="3" height="6" rx="0.5"/><rect x="7.5" y="6" width="3" height="10" rx="0.5"/><rect x="13" y="2" width="3" height="14" rx="0.5"/></svg>
      统计
    </div>
  </nav>
  <div class="tb-status">
    <span class="hdot" id="hdot"></span>
    <span class="hstatus" id="hstatus">运行中</span>
    <span class="hport" id="hport">:__PORT__</span>
    <span class="hrefresh" id="hrefresh"></span>
  </div>
</header>

  <div class="content" id="content">

    <div class="panel active" id="p-overview">
      <div class="stat-cards" id="overview-cards"></div>
      <div class="pie-section" id="pie-section">
        <div class="sec-title">Token 占比 <span class="sec-tip">近 7 天 · 按模型</span></div>
        <div class="pie-row" id="pie-row"></div>
      </div>
      <div class="sec-title">接入地址 <span class="sec-tip">点击复制，填入 AI 客户端</span></div>
      <div id="ep-list"></div>
    </div>

    <div class="panel" id="p-models">
      <div class="sec-title">可用模型 <span class="sec-tip" id="model-count"></span>
      </div>
      <div id="model-list"></div>
    </div>

    <div class="panel" id="p-logs">
      <div class="sec-title">请求日志 <span class="sec-tip" id="log-count"></span>
        <button class="btn-clear" id="btn-clear-logs">清空</button>
      </div>
      <div style="overflow-x:auto">
        <table class="data-table" id="log-table">
          <thead><tr><th>时间</th><th>路径</th><th>模型</th><th>供应商</th><th>状态码</th><th>输入</th><th>输出</th><th>耗时</th></tr></thead>
          <tbody></tbody>
        </table>
      </div>
    </div>

    <div class="panel" id="p-stats">
      <div class="sec-title">近 7 天用量 <span class="sec-tip" id="stats-total"></span></div>
      <div style="overflow-x:auto">
        <table class="data-table" id="stats-day-table">
          <thead><tr><th>日期</th><th>请求</th><th>输入 Token</th><th>输出 Token</th><th>合计</th></tr></thead>
          <tbody></tbody>
        </table>
      </div>
      <div class="sec-title" style="margin-top:24px">按模型 <span class="sec-tip" id="stats-model-total"></span></div>
      <div style="overflow-x:auto">
        <table class="data-table" id="stats-model-table">
          <thead><tr><th>模型</th><th>总计 Token</th></tr></thead>
          <tbody></tbody>
        </table>
      </div>
      <div class="sec-title" style="margin-top:24px">今日供应商 <span class="sec-tip">请求 · 平均延迟</span></div>
      <div style="overflow-x:auto">
        <table class="data-table" id="stats-prov-table">
          <thead><tr><th>供应商</th><th>请求</th><th>输入 Token</th><th>输出 Token</th><th>平均耗时</th></tr></thead>
          <tbody></tbody>
        </table>
      </div>
    </div>

  </div>

<div class="toast" id="toast"></div>

<script>
(function () {
  var fmt = function (n) {
    if (n >= 1e9) return (n / 1e9).toFixed(2) + "B";
    if (n >= 1e6) return (n / 1e6).toFixed(2) + "M";
    if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
    return String(n);
  };
  var dayKey = function (d) {
    return d.getFullYear() + "-" + String(d.getMonth() + 1).padStart(2, "0") + "-" + String(d.getDate()).padStart(2, "0");
  };

  var toastTimer;
  function toast(msg, cls) {
    var t = document.getElementById("toast");
    t.textContent = msg; t.className = "toast show" + (cls ? " " + cls : "");
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { t.className = "toast"; }, 2200);
  }

  var currentTab = "overview";
  document.querySelectorAll(".nav-item").forEach(function (ni) {
    ni.addEventListener("click", function () {
      var t = ni.getAttribute("data-tab");
      if (!t || t === currentTab) return;
      currentTab = t;
      document.querySelectorAll(".nav-item").forEach(function (x) { x.classList.toggle("active", x === ni); });
      document.querySelectorAll(".panel").forEach(function (p) { p.classList.toggle("active", p.id === "p-" + t); });
      refreshTab(t);
    });
  });

  function fallbackCopy(text, done) {
    var ta = document.createElement("textarea");
    ta.value = text; ta.style.position = "fixed"; ta.style.opacity = "0";
    document.body.appendChild(ta); ta.select();
    try { document.execCommand("copy"); done(); } catch (e) {}
    document.body.removeChild(ta);
  }
  function copyWithFeedback(btn, url, doneText) {
    var done = function () {
      btn.textContent = doneText; btn.classList.add("ok");
      setTimeout(function () { btn.textContent = "复制"; btn.classList.remove("ok"); }, 1400);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(url).then(done).catch(function () { fallbackCopy(url, done); });
    } else {
      fallbackCopy(url, done);
    }
  }

  var EP_PROTOCOLS = [
    { tag: "SDK 地址",   cls: "tag-o", path: "/router" },
    { tag: "OpenAI",     cls: "tag-o", path: "/router/chat/completions" },
    { tag: "Anthropic",  cls: "tag-a", path: "/router/v1/messages" },
    { tag: "Gemini",     cls: "tag-g", path: "/router/v1beta/models/{model}:generateContent" }
  ];
  var epDoneText = {};
  function renderEndpoints(base) {
    var list = document.getElementById("ep-list");
    if (!list) return;
    var h = '';
    for (var i = 0; i < EP_PROTOCOLS.length; i++) {
      var e = EP_PROTOCOLS[i];
      h += '<div class="ep"><span class="tag ' + e.cls + '">' + e.tag + '</span>' +
        '<code id="ep-code-' + i + '" title="' + esc(base + e.path) + '">' + esc(base + e.path) + '</code>' +
        '<button type="button" class="btn-copy" id="ep-copy-' + i + '">复制</button></div>';
    }
    list.innerHTML = h;
    for (var j = 0; j < EP_PROTOCOLS.length; j++) {
      (function (idx) {
        document.getElementById("ep-copy-" + idx).addEventListener("click", function () {
          copyWithFeedback(this, base + EP_PROTOCOLS[idx].path, "已复制");
        });
      })(j);
    }
  }

  function refreshTab(t) {
    if (!t) t = currentTab;
    if (t === "overview") refreshOverview();
    else if (t === "models") refreshModels();
    else if (t === "logs") refreshLogs();
    else if (t === "stats") refreshStats();
  }

  function refreshHeader(status) {
    var dot = document.getElementById("hdot");
    var st = document.getElementById("hstatus");
    var port = document.getElementById("hport");
    var r = document.getElementById("hrefresh");
    if (status) {
      dot.classList.toggle("off", !status.running);
      st.textContent = status.running ? "运行中" : "已停止";
      st.style.color = status.running ? "var(--green)" : "var(--red)";
      var hostShort = status.localBase ? status.localBase.replace(/^https?:\/\//, "") : (":" + (status.port || "-"));
      port.textContent = hostShort;
      r.textContent = "刷新于 " + new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
    }
  }

  function refreshOverview() {
    fetch("/api/status").then(function (r) { return r.json(); }).then(function (s) {
      refreshHeader(s);
      renderEndpoints(s.localBase || ("http://localhost:" + (s.port || 3000)));
      var cards = document.getElementById("overview-cards");
      cards.innerHTML =
        '<div class="stat-card"><div class="sc-num">' + (s.providers || 0) + '</div><div class="sc-label">已接入供应商</div></div>' +
        '<div class="stat-card" id="card-req"><div class="sc-num">-</div><div class="sc-label">今日请求</div></div>' +
        '<div class="stat-card" id="card-tok"><div class="sc-num">-</div><div class="sc-label">今日 Token</div></div>';
    }).catch(function () {});
    fetch("/api/stats").then(function (r) { return r.json(); }).then(function (st) {
      if (!st || !st.days) return;
      var t = st.days[dayKey(new Date())];
      var r = document.getElementById("card-req");
      var k = document.getElementById("card-tok");
      if (r) r.querySelector(".sc-num").textContent = t && t.count ? fmt(t.count) : "0";
      if (k) k.querySelector(".sc-num").textContent = t ? fmt((t.prompt || 0) + (t.completion || 0)) : "0";
      renderPie(st);
    }).catch(function () {});
  }

  var PIE_COLORS = ["#34d399", "#22c55e", "#4fb286", "#86efac", "#9db8dd", "#8a8f98"];
  function esc(s) {
    return String(s == null ? '' : s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }
  function renderPie(st) {
    var pm = st.perModel || {};
    var items = [];
    for (var m in pm) {
      var total = 0;
      for (var d in pm[m].days) total += (pm[m].days[d].prompt || 0) + (pm[m].days[d].completion || 0);
      if (total > 0) items.push({ name: m, tokens: total });
    }
    items.sort(function (a, b) { return b.tokens - a.tokens; });
    var top = items.slice(0, 5);
    var rest = items.slice(5).reduce(function (s, x) { return s + x.tokens; }, 0);
    if (rest > 0) top.push({ name: "其他", tokens: rest });
    var total = top.reduce(function (s, x) { return s + x.tokens; }, 0) || 1;

    var cx = 60, cy = 60, r = 44, ir = 30;
    var angle = -Math.PI / 2;
    var paths = "";
    for (var i = 0; i < top.length; i++) {
      var slice = top[i].tokens / total * 2 * Math.PI;
      if (slice <= 0) continue;
      var a1 = angle, a2 = angle + slice;
      if (a2 - a1 >= 2 * Math.PI) a2 = a1 + 2 * Math.PI - 0.001;
      var x1 = cx + r * Math.cos(a1), y1 = cy + r * Math.sin(a1);
      var x2 = cx + r * Math.cos(a2), y2 = cy + r * Math.sin(a2);
      var x3 = cx + ir * Math.cos(a2), y3 = cy + ir * Math.sin(a2);
      var x4 = cx + ir * Math.cos(a1), y4 = cy + ir * Math.sin(a1);
      var large = (a2 - a1) > Math.PI ? 1 : 0;
      paths += '<path class="pie-slice" data-model="' + esc(top[i].name) + '" d="M' + x1 + ',' + y1 + ' A' + r + ',' + r + ' 0 ' + large + ' 1 ' + x2 + ',' + y2 + ' L' + x3 + ',' + y3 + ' A' + ir + ',' + ir + ' 0 ' + large + ' 0 ' + x4 + ',' + y4 + ' Z" fill="' + PIE_COLORS[i % PIE_COLORS.length] + '" opacity=".85"/>';
      angle = a2;
    }

    var d = new Date(); var today = d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
    var activeModels = {};
    for (var m in pm) {
      var day = pm[m].days && pm[m].days[today];
      if (day && (day.prompt > 0 || day.completion > 0)) activeModels[m] = true;
    }
    var legend = "";
    for (var i = 0; i < top.length; i++) {
      legend += '<div class="pie-legend-item' + (activeModels[top[i].name] ? ' active-token' : '') + '" data-model="' + esc(top[i].name) + '"><span class="dot" style="background:' + PIE_COLORS[i % PIE_COLORS.length] + '"></span><span class="name" title="' + esc(top[i].name) + '">' + esc(top[i].name) + '</span><span class="val">' + Math.round(top[i].tokens / total * 100) + '%</span></div>';
    }

    var row = document.getElementById("pie-row");
    if (row) {
      row.innerHTML = '<div class="pie-ring"><svg viewBox="0 0 120 120">' + paths + '</svg><div class="pie-center"><div class="pct">' + fmt(total) + '</div><div class="pct-label">tokens</div></div></div><div class="pie-legend">' + legend + '</div>';
      row.querySelectorAll('.pie-legend-item, .pie-slice').forEach(function (el) {
        el.addEventListener('mouseenter', function () {
          var name = el.dataset.model;
          row.querySelectorAll('.pie-slice').forEach(function (s) { s.style.opacity = s.dataset.model === name ? '1' : '0.25'; });
          row.querySelectorAll('.pie-legend-item').forEach(function (l) { l.style.opacity = l.dataset.model === name ? '1' : '0.4'; });
        });
        el.addEventListener('mouseleave', function () {
          row.querySelectorAll('.pie-slice').forEach(function (s) { s.style.opacity = '.85'; });
          row.querySelectorAll('.pie-legend-item').forEach(function (l) { l.style.opacity = '1'; });
        });
      });
    }
  }

  function refreshModels() {
    Promise.all([
      fetch("/v1/models").then(function (r) { return r.json(); }),
      fetch("/api/stats").then(function (r) { return r.json(); }).catch(function () { return null; })
    ]).then(function (arr) {
      var json = arr[0], st = arr[1];
      var models = (json && json.data) ? json.data : [];
      document.getElementById("model-count").textContent = "共 " + models.length + " 个模型";

      var d = new Date(); var today = d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
      var activeModels = {};
      if (st && st.perModel) {
        for (var m in st.perModel) {
          var day = st.perModel[m].days && st.perModel[m].days[today];
          if (day && (day.prompt > 0 || day.completion > 0)) activeModels[m] = true;
        }
      }

      var groups = {};
      for (var i = 0; i < models.length; i++) {
        var prov = models[i].owned_by || models[i].provider || "未知";
        if (!groups[prov]) groups[prov] = [];
        groups[prov].push(models[i].id);
      }
      var html = "";
      for (var p in groups) {
        html += '<div class="model-group"><div class="mg-name"><span class="mg-dot"></span>' + esc(p) +
          '<span style="margin-left:auto;font-size:10.5px;color:var(--text-3);font-weight:500">' + groups[p].length + ' 个</span>' +
          '</div>';
        for (var j = 0; j < groups[p].length; j++) {
          var id = groups[p][j];
          // 今天有用过该模型则显示高亮标记
          var cls = activeModels[id] ? ' active-token' : '';
          html += '<span class="mchip' + cls + '">' + esc(id) + '</span>';
        }
        html += '</div>';
      }
      document.getElementById("model-list").innerHTML = html || '<div style="color:var(--text-3);padding:20px 0;text-align:center">暂无模型数据</div>';
    }).catch(function () {
      document.getElementById("model-list").innerHTML = '<div style="color:var(--text-3);padding:20px 0;text-align:center">加载失败</div>';
    });
  }

  function refreshLogs() {
    fetch("/api/logs").then(function (r) { return r.json(); }).then(function (logs) {
      if (!Array.isArray(logs)) logs = [];
      document.getElementById("log-count").textContent = "最近 " + logs.length + " 条";
      var tbody = document.querySelector("#log-table tbody");
      var h = "";
      for (var i = 0; i < logs.length; i++) {
        var l = logs[i];
        var ok = l.status >= 200 && l.status < 400;
        var badge = ok ? '<span class="badge badge-ok">' + l.status + '</span>'
                        : (l.status >= 400 && l.status < 500 ? '<span class="badge badge-warn">' + l.status + '</span>'
                        : '<span class="badge badge-err">' + l.status + '</span>');
        var pt = "-";
        var ct = "-";
        if (l.usage) {
          pt = fmt((l.usage.prompt_tokens != null) ? l.usage.prompt_tokens : 0);
          ct = fmt((l.usage.completion_tokens != null) ? l.usage.completion_tokens : 0);
        }
        h += '<tr><td>' + l.time.slice(11, 19) + '</td><td><span class="s-dim">' + esc(l.path) + '</span></td><td>' + esc(l.model) + '</td><td>' + esc(l.provider) + '</td><td>' + badge + '</td><td class="s-dim">' + pt + '</td><td class="s-dim">' + ct + '</td><td class="t-right s-dim">' + l.ms + 'ms</td></tr>';
      }
      tbody.innerHTML = h || '<tr><td colspan="8" style="color:var(--text-3);text-align:center;padding:24px">暂无日志</td></tr>';
    }).catch(function () {
      document.querySelector("#log-table tbody").innerHTML = '<tr><td colspan="8" style="color:var(--text-3);text-align:center;padding:24px">加载失败</td></tr>';
    });
  }

  function refreshStats() {
    fetch("/api/stats").then(function (r) { return r.json(); }).then(function (st) {
      if (!st || !st.days) return;
      var days = st.days;
      var keys = Object.keys(days).sort();
      var totalCount = 0, totalTokens = 0;
      var dayRows = "";
      for (var i = 0; i < keys.length; i++) {
        var d = days[keys[i]];
        totalCount += d.count || 0;
        var t = (d.prompt || 0) + (d.completion || 0);
        totalTokens += t;
        dayRows += '<tr><td>' + keys[i] + '</td><td class="t-right">' + fmt(d.count || 0) + '</td><td class="t-right">' + fmt(d.prompt || 0) + '</td><td class="t-right">' + fmt(d.completion || 0) + '</td><td class="t-right">' + fmt(t) + '</td></tr>';
      }
      document.getElementById("stats-total").textContent = "共 " + fmt(totalCount) + " 请求 · " + fmt(totalTokens) + " tokens";
      document.querySelector("#stats-day-table tbody").innerHTML = dayRows || '<tr><td colspan="5" style="color:var(--text-3);text-align:center;padding:24px">暂无数据</td></tr>';

      var perModel = st.perModel || {};
      var modelKeys = Object.keys(perModel);
      var modelRows = "";
      for (var j = 0; j < modelKeys.length; j++) {
        var pm = perModel[modelKeys[j]];
        var mt = 0;
        if (pm && pm.days) { var dks = Object.keys(pm.days); for (var k = 0; k < dks.length; k++) { mt += (pm.days[dks[k]].prompt || 0) + (pm.days[dks[k]].completion || 0); } }
        modelRows += '<tr><td>' + esc(modelKeys[j]) + '</td><td class="t-right">' + fmt(mt) + '</td></tr>';
      }
      document.getElementById("stats-model-total").textContent = modelKeys.length + " 个模型";
      document.querySelector("#stats-model-table tbody").innerHTML = modelRows || '<tr><td colspan="2" style="color:var(--text-3);text-align:center;padding:24px">暂无数据</td></tr>';

      var perProvider = st.perProvider || {};
      var provKeys = Object.keys(perProvider);
      var provRows = "";
      for (var w = 0; w < provKeys.length; w++) {
        var pp = perProvider[provKeys[w]];
        var avg = (pp.avg_ms || 0);
        provRows += '<tr><td>' + esc(provKeys[w]) + '</td><td class="t-right">' + fmt(pp.count || 0) + '</td><td class="t-right">' + fmt(pp.prompt || 0) + '</td><td class="t-right">' + fmt(pp.completion || 0) + '</td><td class="t-right s-dim">' + avg + 'ms</td></tr>';
      }
      document.querySelector("#stats-prov-table tbody").innerHTML = provRows || '<tr><td colspan="5" style="color:var(--text-3);text-align:center;padding:24px">今日暂无请求</td></tr>';
    }).catch(function () {});
  }

  // 清空日志
  document.getElementById("btn-clear-logs").onclick = function () {
    if (!confirm("确定清空全部请求日志？")) return;
    fetch("/api/logs/clear", { method: "POST" }).then(function (r) { return r.json(); }).then(function () {
      refreshLogs();
    }).catch(function () {});
  };

  refreshOverview();
  // 支持 ?tab= 参数打开指定面板（/api/xxx 独立页面统一跳转到此）
  var tabParam = new URLSearchParams(window.location.search).get("tab");
  var validTabs = { overview: 1, models: 1, logs: 1, stats: 1 };
  if (tabParam && validTabs[tabParam]) {
    currentTab = tabParam;
    document.querySelectorAll(".nav-item").forEach(function (x) { x.classList.toggle("active", x.getAttribute("data-tab") === tabParam); });
    document.querySelectorAll(".panel").forEach(function (p) { p.classList.toggle("active", p.id === "p-" + tabParam); });
    refreshTab(currentTab);
  }
  setInterval(function () { refreshTab(currentTab); }, 15000);
  setInterval(function () {
    fetch("/api/status").then(function (r) { return r.json(); }).then(refreshHeader).catch(function () {});
  }, 10000);
})();
</script>
</body>
</html>`

	html = strings.ReplaceAll(html, "__PORT__", fmt.Sprintf("%d", port))
	return html
}

// passUpstreamHeaders 把对客户端有意义的「受限」上游头透传到合成响应里：
// 限流、重试间隔与请求 id。合成响应默认只带 Content-Type，客户端因此看不到
// Retry-After / x-ratelimit-*，遇到 429 无法正确退避。只白名单复制，
// 避免把 Set-Cookie 之类的敏感头上带出。
func passUpstreamHeaders(dst, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	for k, vs := range src {
		lk := strings.ToLower(k)
		switch {
		case lk == "retry-after", lk == "x-request-id", lk == "request-id",
			strings.HasPrefix(lk, "x-ratelimit-"), strings.HasPrefix(lk, "ratelimit-"),
			strings.HasPrefix(lk, "anthropic-ratelimit-"):
			for _, v := range vs {
				dst.Add(k, v)
			}
		}
	}
}
