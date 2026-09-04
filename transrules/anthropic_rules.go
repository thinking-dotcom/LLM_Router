package transrules

import (
	"io"

	"llm-router-go/types"
)

// registerAnthropicRules 装配「产出 Anthropic Messages 格式」的规则。
// 全矩阵注册：任意接入格式（/oai /aai /gai /responses）均可到达 anthropic
// 上游，缺规则时由 LookupRequest 显式报错。
func registerAnthropicRules(e Engine) {
	// 请求侧：/aai 接入 → anthropic 服务商。
	// 同型直通时（客户端原生 Anthropic body）原样转发保留 thinking 等原生字段，
	// 否则经 OpenAI 归一化形状转换。
	RegisterRequest(FormatAnthropic, DstAnthropic, exactOrNormalize)
	// 跨型：Anthropic 客户端（/aai 或 /v1/messages）也可指定 OpenAI 兼容上游
	//（openai / gemini-openai），经 OpenAI 归一化形状转换（defaultRequest）；
	// 对称地，OpenAI 客户端也可指定 anthropic 上游。这些组合不再被格式亲和
	// 校验拦截——转换引擎本就支持全矩阵，字段无法承载时才丢弃并记日志。
	RegisterRequest(FormatAnthropic, DstOpenai, defaultRequest)
	RegisterRequest(FormatAnthropic, DstGeminiOpenAI, defaultRequest)
	RegisterRequest(FormatAnthropic, DstResponse, defaultRequest)
	// 全矩阵：Gemini 接入（/gai，归一化后）→ anthropic 上游
	RegisterRequest(FormatGemini, DstAnthropic, defaultRequest)
	RegisterRequest(FormatOpenAI, DstAnthropic, defaultRequest)
	// 全矩阵：Gemini Interactions 接入 → anthropic 上游
	RegisterRequest(FormatGeminiInteractions, DstAnthropic, defaultRequest)

	// 响应侧：任何上游格式 → 客户端 anthropic 接入（/aai）。
	for _, upstream := range allUpstreamFormats() {
		RegisterNonStream(upstream, FormatAnthropic, func(resp map[string]any, model string) map[string]any {
			return e.ToClient(FormatAnthropic, resp, model)
		})
	}

	// 流式：/aai 接入以 Anthropic SSE 事件写出。
	RegisterStream(FormatAnthropic, func(w io.Writer, ch <-chan map[string]any, m string) (*types.Usage, error) {
		return e.WriteChunks(FormatAnthropic, w, ch, m)
	})

	// 上游 anthropic 格式：按 Anthropic SSE 解析分片。
	RegisterChunk(DstAnthropic, func(body io.ReadCloser) <-chan map[string]any {
		return e.ParseChunks(DstAnthropic, body)
	})
}
