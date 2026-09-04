package transrules

import (
	"io"

	"llm-router-go/types"
)

// registerResponseRules 装配「产出 OpenAI Responses 格式」的规则。服务商 API
// 格式为 response 时：openai 接入由 engine.BuildOutbound 内部调用
// ChatToResponses 完成转换；responses 接入为同型组合，SameType 时经
// exactOrNormalize 原样直通（保留 previous_response_id / store 等原生字段）。
func registerResponseRules(e Engine) {
	RegisterRequest(FormatOpenAI, DstResponse, defaultRequest)
	// 全矩阵：Gemini 接入（/gai，归一化后）→ Responses 上游
	RegisterRequest(FormatGemini, DstResponse, defaultRequest)
	// 全矩阵：Gemini Interactions 接入 → Responses 上游
	RegisterRequest(FormatGeminiInteractions, DstResponse, defaultRequest)
	// Responses 同型接入：同型直通 / 归一化降级二选一
	RegisterRequest(FormatResponses, DstResponse, exactOrNormalize)

	// 上游 response 格式：按 Responses SSE 解析分片。
	RegisterChunk(DstResponse, func(body io.ReadCloser) <-chan map[string]any {
		return e.ParseChunks(DstResponse, body)
	})

	// ---- 入站：客户端以 Responses 协议接入（/responses 路径，如新版 Codex） ----
	// 请求侧：归一化为 OpenAI 形状（ParseInbound 已完成），出站按目标格式装配；
	// 响应侧：任意上游格式 → 还原为 Responses 响应对象 / SSE 事件流。
	for _, dst := range allUpstreamFormats() {
		// responses→response 已在上面注册为 exactOrNormalize（同型直通），
		// 此处不能再用 defaultRequest 覆盖，否则同型直通永远不生效
		if dst == DstResponse {
			// 仍需补齐该上游 → Responses 客户端的非流式响应转换
			RegisterNonStream(dst, FormatResponses, func(resp map[string]any, model string) map[string]any {
				return e.ToClient(FormatResponses, resp, model)
			})
			continue
		}
		RegisterRequest(FormatResponses, dst, defaultRequest)
		upstream := dst
		RegisterNonStream(upstream, FormatResponses, func(resp map[string]any, model string) map[string]any {
			return e.ToClient(FormatResponses, resp, model)
		})
	}
	RegisterStream(FormatResponses, func(w io.Writer, ch <-chan map[string]any, m string) (*types.Usage, error) {
		return e.WriteChunks(FormatResponses, w, ch, m)
	})
}
