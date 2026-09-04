package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"llm-router-go/format"
	"llm-router-go/types"
)

// geminiRoot 去掉 /interactions 后缀得到模型列表等资源根地址（Interactions API
// base 与 generateContent 不同：模型列表仍用 /v1beta/models）。
func geminiRoot(base string) string {
	if idx := strings.LastIndex(base, "/interactions"); idx >= 0 {
		return base[:idx]
	}
	return base
}

var modelsClient = &http.Client{Timeout: 15 * time.Second}

// FetchModels queries a provider's upstream /models endpoint and returns the
// list of model identifiers it exposes. 依次尝试候选端点（modelsEndpoints），
// 首个 2xx 成功即返回。
func FetchModels(provider types.Provider) types.FetchModelsResult {
	lastErr := ""
	for _, ep := range modelsEndpoints(provider) {
		req, err := http.NewRequest(http.MethodGet, ep.url, nil)
		if err != nil {
			lastErr = redactURL(err.Error())
			continue
		}
		if ep.bearer {
			// OpenAI 风格的模型列表端点（DeepSeek /models）用 Bearer 认证
			if provider.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+provider.APIKey)
			}
		} else {
			setModelsHeaders(req, provider)
		}
		resp, err := modelsClient.Do(req)
		if err != nil {
			// 传输层错误串含完整 URL（Gemini 的 ?key=APIKEY），先脱敏再记录
			lastErr = redactURL(err.Error())
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			lastErr = redactURL(err.Error())
			continue
		}
		models := extractModels(provider.Type, parsed)
		return types.FetchModelsResult{Ok: true, Models: models}
	}
	return types.FetchModelsResult{Ok: false, Error: lastErr}
}

// modelsEndpoint 表示一次模型列表候选请求。
type modelsEndpoint struct {
	url    string
	bearer bool // true 用 Bearer 头（OpenAI 风格），false 按 provider type 默认头
}

// modelsEndpoints 返回模型列表候选端点，按优先级逐个尝试（首个 2xx 即成功）。
// 通用回退机制：无论服务商类型，主候选（该 type 的标准 models 端点）之外，
// 总是追加同一 base 下的 OpenAI 风格 /models 与 /v1/models 作为兜底——
// 有些上游标准端点不存在或 404（尤其 Anthropic 兼容层），用这些候选可兜底。
// Anthropic 兼容层（baseURL 以 /anthropic 结尾，如 DeepSeek /anthropic）还会
// 回退到去掉 /anthropic 后缀的根地址。
func modelsEndpoints(p types.Provider) []modelsEndpoint {
	base := format.NormalizeBaseURL(p.BaseURL)
	primary := modelsURL(p)
	eps := []modelsEndpoint{{url: primary, bearer: false}}
	seen := map[string]bool{primary: true}

	// 追加 OpenAII 兼容风格回退候选（统一用 Bearer 认证）
	add := func(url string) {
		if url != "" && !seen[url] {
			seen[url] = true
			eps = append(eps, modelsEndpoint{url: url, bearer: true})
		}
	}

	// Anthropic 兼容层：base 以 /anthropic 结尾 → 根地址的 OpenAI 风格端点
	if root := strings.TrimSuffix(base, "/anthropic"); root != base {
		add(root + "/models")
		add(root + "/v1/models")
	}

	// 通用兜底：当前 base 的 /models 与 /v1/models
	add(base + "/models")
	add(base + "/v1/models")
	return eps
}

// ---------------------------------------------------------------------------
// URL construction
// ---------------------------------------------------------------------------

func modelsURL(p types.Provider) string {
	base := format.NormalizeBaseURL(p.BaseURL)

	switch p.Type {
	case types.ProviderOpenAI, types.ProviderResponse:
		return base + "/v1/models"
	case types.ProviderAnthropic:
		return base + "/v1/models"
	case types.ProviderGemini:
		// Interactions API 的模型列表仍走 /v1beta/models（root 去掉 /interactions
		// 后按 NormalizeBaseURL 收尾，避免 /v1beta 重复）
		u := format.NormalizeBaseURL(geminiRoot(base)) + "/v1beta/models"
		if p.APIKey != "" {
			u += "?key=" + url.QueryEscape(p.APIKey)
		}
		return u
	case types.ProviderGeminiOpenAI:
		return base + "/v1beta/openai/models"
	default:
		return base + "/v1/models"
	}
}

// ---------------------------------------------------------------------------
// Headers
// ---------------------------------------------------------------------------

func setModelsHeaders(req *http.Request, p types.Provider) {
	switch p.Type {
	case types.ProviderOpenAI, types.ProviderGeminiOpenAI, types.ProviderResponse:
		if p.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
	case types.ProviderAnthropic:
		if p.APIKey != "" {
			req.Header.Set("x-api-key", p.APIKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	case types.ProviderGemini:
		// Gemini passes the key as a query parameter — already set in the URL.
	}
}

// ---------------------------------------------------------------------------
// Response parsing
// ---------------------------------------------------------------------------

// extractModels pulls model identifiers out of a provider-specific JSON body.
//
// OpenAI / Responses:  { "data": [ { "id": "..." }, ... ] }
// Anthropic:            { "data": [ { "id": "..." }, ... ] }
// Gemini:               { "models": [ { "name": "models/...", ... }, ... ] }
func extractModels(ptype types.ProviderType, body map[string]any) []string {
	switch ptype {
	case types.ProviderOpenAI, types.ProviderResponse, types.ProviderAnthropic:
		return extractFromData(body)
	case types.ProviderGeminiOpenAI:
		models := extractFromData(body)
		for i, m := range models {
			models[i] = strings.TrimPrefix(m, "models/")
		}
		return models
	case types.ProviderGemini:
		return extractFromGeminiModels(body)
	default:
		return extractFromData(body)
	}
}

func extractFromData(body map[string]any) []string {
	arr, _ := body["data"].([]any)
	if arr == nil {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		if id, ok := m["id"].(string); ok && id != "" {
			out = append(out, id)
		}
	}
	return out
}

func extractFromGeminiModels(body map[string]any) []string {
	arr, _ := body["models"].([]any)
	if arr == nil {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		if name, ok := m["name"].(string); ok && name != "" {
			// Strip "models/" prefix added by the Gemini API.
			out = append(out, strings.TrimPrefix(name, "models/"))
		}
	}
	return out
}
