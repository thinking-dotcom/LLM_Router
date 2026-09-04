package types

// ProviderType API format type
type ProviderType string

const (
	ProviderOpenAI       ProviderType = "openai"
	ProviderAnthropic    ProviderType = "anthropic"
	ProviderGemini       ProviderType = "gemini"
	ProviderGeminiOpenAI ProviderType = "gemini-openai"
	ProviderResponse     ProviderType = "response"
)

var ProviderTypes = []ProviderType{
	ProviderOpenAI, ProviderAnthropic, ProviderGemini, ProviderGeminiOpenAI, ProviderResponse,
}

// Provider represents a configured upstream LLM provider
type Provider struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Type    ProviderType      `json:"type"`
	BaseURL string            `json:"baseUrl"`
	APIKey  string            `json:"apiKey"`
	Models  []string          `json:"models"`
	Aliases map[string]string `json:"aliases"`
	Enabled bool              `json:"enabled"`
}

// Config is the root configuration (mirrors config.json)
type Config struct {
	Port            int        `json:"port"`
	Host            string     `json:"host"`
	AuthToken       string     `json:"authToken"`
	DefaultProvider string     `json:"defaultProvider"`
	Providers       []Provider `json:"providers"`
	// ReasoningDepth 全局推理深度档位（none/low/medium/high/max）：
	// 客户端 payload 未携带推理参数时按此档位注入对应格式的推理参数。
	ReasoningDepth string `json:"reasoningDepth,omitempty"`
	// 桌面端偏好（桌面 GUI 专用，不影响路由逻辑）
	Tray bool `json:"tray,omitempty"` // 托盘常驻：关闭窗口时隐藏到系统托盘而非退出
}

// Usage holds token counts
type Usage struct {
	PromptTokens     *int `json:"prompt_tokens,omitempty"`
	CompletionTokens *int `json:"completion_tokens,omitempty"`
}

// OutboundRequest 描述一次转换后的上游调用（最终 URL、请求头与请求体）。
// 它是格式转换规则（transrules）与格式实现（format）之间共享的纯数据结构，
// 放在 types 以保证二者互不依赖（依赖倒置：双方都只依赖 types，不依赖对方）。
type OutboundRequest struct {
	URL     string
	Headers map[string]string
	Body    any
}

// LogEntry records a single request
type LogEntry struct {
	Time     string `json:"time"`
	Path     string `json:"path"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Status   int    `json:"status"`
	Ms       int    `json:"ms"`
	Req      string `json:"req,omitempty"`
	Resp     string `json:"resp,omitempty"`
	Usage    *Usage `json:"usage,omitempty"`
}

// DayStat holds daily aggregated stats
type DayStat struct {
	Count      int `json:"count"`
	Prompt     int `json:"prompt"`
	Completion int `json:"completion"`
}

// PerModelStat holds per-model stats over days
type PerModelStat struct {
	Days map[string]DayStat `json:"days"`
}

// PerProviderStat holds per-provider stats for today
type PerProviderStat struct {
	Count      int  `json:"count"`
	Prompt     int  `json:"prompt"`
	Completion int  `json:"completion"`
	AvgMs      *int `json:"avg_ms"`
}

// StatsData is the aggregated statistics response
type StatsData struct {
	Days        map[string]DayStat         `json:"days"`
	PerModel    map[string]PerModelStat    `json:"perModel"`
	PerProvider map[string]PerProviderStat `json:"perProvider"`
}

// StatusData is the /api/status response
type StatusData struct {
	Running   bool   `json:"running"`
	Error     string `json:"error,omitempty"`
	Port      int    `json:"port"`
	Host      string `json:"host"`
	LocalBase string `json:"localBase"`
	Providers int    `json:"providers"`
}

// SaveResult is returned by saveConfig
type SaveResult struct {
	Ok            bool   `json:"ok"`
	Error         string `json:"error,omitempty"`
	RestartNeeded bool   `json:"restartNeeded,omitempty"`
}

// FetchModelsResult is returned by fetchModels
type FetchModelsResult struct {
	Ok     bool     `json:"ok"`
	Error  string   `json:"error,omitempty"`
	Models []string `json:"models,omitempty"`
}
