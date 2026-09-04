package format

import (
	"io"

	"llm-router-go/types"
)

// Engine 是 format 包对「格式转换规则引擎」（transrules.Engine 接口）的实现。
// format 不依赖 transrules：Go 接口为隐式满足，wiring 层（router）将本类型
// 作为 transrules.Engine 注入 SetEngine。二者仅共享 types 包，满足依赖倒置。
type Engine struct{}

// NewEngine 返回一个新的转换实现。
func NewEngine() *Engine { return &Engine{} }

// BuildOutbound 按服务商 API 格式把归一化 OpenAI 请求装配为上游调用。
func (Engine) BuildOutbound(provider types.Provider, openaiReq map[string]any, model string) types.OutboundRequest {
	return BuildOutboundRequest(provider, openaiReq, model)
}

// BuildExactOutbound 用于同型直通：不转换、原样转发客户端原生字段。
func (Engine) BuildExactOutbound(provider types.Provider, rawBody map[string]any, model string, stream bool) types.OutboundRequest {
	return BuildExactOutbound(provider, rawBody, model, stream)
}

// ToClient 把归一化 OpenAI 形状的响应还原成目标客户端格式 dst 的响应。
func (Engine) ToClient(dst string, openaiResp map[string]any, model string) map[string]any {
	switch dst {
	case "anthropic":
		return OpenaiToAnthropicBody(openaiResp, model)
	case "gemini":
		return OpenaiToGeminiBody(openaiResp, model)
	case "gemini-interactions": // Gemini Interactions API 客户端（2026 GA 标准）
		return OpenaiToGeminiInteractionsBody(openaiResp, model)
	case "responses": // OpenAI Responses 协议接入（如新版 Codex wire_api=responses）
		return OpenaiToResponsesBody(openaiResp, model)
	default: // openai（含未知格式兜底）：响应本就是 OpenAI 形状
		types.IRDropAll(openaiResp) // 剥离 IR 扩展键，OpenAI 兼容客户端不识别
		return openaiResp
	}
}

// WriteChunks 按客户端格式 dst 把归一化 chunk 增量写出为客户端 SSE。
func (Engine) WriteChunks(dst string, w io.Writer, chunks <-chan map[string]any, model string) (*types.Usage, error) {
	switch dst {
	case "anthropic":
		return WriteChunksToAnthropic(w, chunks, model)
	case "gemini":
		return WriteChunksToGemini(w, chunks, model)
	case "gemini-interactions":
		return WriteChunksToGeminiInteractions(w, chunks, model)
	case "responses":
		return WriteChunksToResponses(w, chunks, model)
	default: // openai（含未知格式兜底）
		return WriteChunksToOpenai(w, chunks, model)
	}
}

// ParseChunks 按上游 API 格式 src 把上游流式响应体解析为归一化 chunk 流。
func (Engine) ParseChunks(src string, body io.ReadCloser) <-chan map[string]any {
	switch src {
	case "anthropic":
		return AnthropicStreamToChunks(body)
	case "gemini":
		return GeminiStreamToChunks(body)
	case "response":
		return ResponsesStreamToChunks(body)
	default: // openai / gemini-openai（含未知格式兜底）
		return OpenaiStreamToChunks(body)
	}
}
