package transrules

// 接入方式 / 客户端声明格式（由 /oai /aai /gai 前缀或请求路径决定）。
const (
	FormatOpenAI             = "openai"              // /oai 接入，OpenAI Chat Completions 兼容
	FormatAnthropic          = "anthropic"           // /aai 接入，Anthropic Messages
	FormatGemini             = "gemini"              // /gai 接入，Gemini generateContent（legacy）
	FormatGeminiInteractions = "gemini-interactions" // /v1beta/interactions 接入，Gemini Interactions API（2026 GA 标准）
	FormatResponses          = "responses"           // /responses 接入，OpenAI Responses 协议（如新版 Codex）
)

// 服务商 API 格式（目标格式）。全矩阵注册后任意接入格式均可触达全部目标格式。
const (
	DstOpenai       = "openai"
	DstGeminiOpenAI = "gemini-openai"
	DstResponse     = "response"
	DstAnthropic    = "anthropic"
	DstGemini       = "gemini"
)

// allUpstreamFormats 列出全部可能作为上游的服务商 API 格式，用于装配
// 「各上游 → 客户端格式」的响应侧规则矩阵。
func allUpstreamFormats() []string {
	return []string{
		DstOpenai, DstGeminiOpenAI, DstResponse, DstAnthropic, DstGemini,
	}
}

// clientFormats 列出全部客户端接入格式，用于装配响应侧矩阵。
func clientFormats() []string {
	return []string{FormatOpenAI, FormatAnthropic, FormatGemini, FormatGeminiInteractions, FormatResponses}
}
