package transrules

import (
	"io"

	"llm-router-go/types"
)

// registerOpenAIRules 装配「产出 OpenAI 兼容格式」的规则。服务商 API 格式为
// openai 或 gemini-openai 时，两者都以 OpenAI 兼容格式交互，转换即视为恒等
// （保留归一化后的 OpenAI 形状）。任一客户端接入格式都可达：/oai 直接恒等，
// /aai /gai /responses 先经 ParseInbound 归一化为 OpenAI 形状后同规则出站。
func registerOpenAIRules(e Engine) {
	// 请求侧：任意接入 → openai / gemini-openai 服务商，恒等转换。
	// 出站装配（URL/头/拆字段）交由 engine.BuildOutbound 按目标类型处理。
	RegisterRequest(FormatOpenAI, DstOpenai, defaultRequest)
	RegisterRequest(FormatOpenAI, DstGeminiOpenAI, defaultRequest)
	// Gemini 接入 → OpenAI 兼容上游（新增全矩阵组合；
	// anthropic/responses 接入的这两种目标已在对应规则文件登记）
	RegisterRequest(FormatGemini, DstOpenai, defaultRequest)
	RegisterRequest(FormatGemini, DstGeminiOpenAI, defaultRequest)
	// Gemini Interactions 接入 → OpenAI 兼容上游（2026 GA 标准入站）
	RegisterRequest(FormatGeminiInteractions, DstOpenai, defaultRequest)
	RegisterRequest(FormatGeminiInteractions, DstGeminiOpenAI, defaultRequest)

	// 响应侧：任何上游格式 → 客户端 openai 接入（/oai），以 OpenAI 形状输出。
	// 经 e.ToClient 剥离 IR 扩展键（_ext_*），避免跨型转换的临时载体字段
	// 泄漏到客户端响应（其余客户端格式均走 ToClient，这里保持一致）。
	for _, upstream := range allUpstreamFormats() {
		RegisterNonStream(upstream, FormatOpenAI, func(resp map[string]any, model string) map[string]any {
			return e.ToClient(FormatOpenAI, resp, model)
		})
	}

	// 流式：/oai 接入以 OpenAI SSE 事件写出。
	RegisterStream(FormatOpenAI, func(w io.Writer, ch <-chan map[string]any, m string) (*types.Usage, error) {
		return e.WriteChunks(FormatOpenAI, w, ch, m)
	})

	// 上游 openai 格式：按 OpenAI SSE 解析分片。
	RegisterChunk(DstOpenai, func(body io.ReadCloser) <-chan map[string]any {
		return e.ParseChunks(DstOpenai, body)
	})

	// 上游 gemini-openai 格式：SSE 与 OpenAI 兼容，此前靠 LookupChunk 的
	// openai 兜底，这里显式注册避免兜底语义变化时回归。
	RegisterChunk(DstGeminiOpenAI, func(body io.ReadCloser) <-chan map[string]any {
		return e.ParseChunks(DstGeminiOpenAI, body)
	})
}
