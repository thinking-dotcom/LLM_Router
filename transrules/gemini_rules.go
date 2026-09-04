package transrules

import (
	"io"

	"llm-router-go/types"
)

// registerGeminiRules 装配「产出 Google Gemini 格式」的规则。
// 全矩阵注册：任意接入格式（/oai /aai /gai /responses）均可到达 gemini 上游，
// 缺规则时由 LookupRequest 显式报错。
func registerGeminiRules(e Engine) {
	// 请求侧：/gai 接入 → gemini 服务商。
	// 同型直通时（客户端原生 Gemini generateContent body）原样转发保留
	// safetySettings / fileData 等原生字段，否则经 OpenAI 归一化形状转换。
	RegisterRequest(FormatGemini, DstGemini, exactOrNormalize)
	// 全矩阵：openai / anthropic 接入 → gemini 原生 generateContent 上游
	//（出站装配由 engine.BuildOutbound 的 gemini 分支调用 ToGemini 完成；
	//  推理档位、safetySettings 等经 IR 扩展键还原，见 format/outbound_gemini.go）。
	RegisterRequest(FormatOpenAI, DstGemini, defaultRequest)
	RegisterRequest(FormatAnthropic, DstGemini, defaultRequest)
	RegisterRequest(FormatGeminiInteractions, DstGemini, exactOrNormalize)

	// 响应侧：任何上游格式 → 客户端 gemini 接入（/gai）。
	for _, upstream := range allUpstreamFormats() {
		RegisterNonStream(upstream, FormatGemini, func(resp map[string]any, model string) map[string]any {
			return e.ToClient(FormatGemini, resp, model)
		})
		RegisterNonStream(upstream, FormatGeminiInteractions, func(resp map[string]any, model string) map[string]any {
			return e.ToClient(FormatGeminiInteractions, resp, model)
		})
	}

	// 流式：/gai 接入以 Gemini SSE 事件写出。
	RegisterStream(FormatGemini, func(w io.Writer, ch <-chan map[string]any, m string) (*types.Usage, error) {
		return e.WriteChunks(FormatGemini, w, ch, m)
	})
	RegisterStream(FormatGeminiInteractions, func(w io.Writer, ch <-chan map[string]any, m string) (*types.Usage, error) {
		return e.WriteChunks(FormatGeminiInteractions, w, ch, m)
	})

	// 上游 gemini 格式：按 Gemini SSE 解析分片。
	RegisterChunk(DstGemini, func(body io.ReadCloser) <-chan map[string]any {
		return e.ParseChunks(DstGemini, body)
	})
}
