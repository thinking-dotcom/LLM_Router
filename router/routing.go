package router

import (
	"strings"
	"sync"

	"llm-router-go/types"
)

// ModelEntry is a model exposed via the /v1/models endpoint.
type ModelEntry struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
}

// ---------------------------------------------------------------------------
// Fetched models cache (populated by background model fetchers)
// ---------------------------------------------------------------------------

var fetchedModels sync.Map // providerId → []string

// UpdateFetchedModels stores the models fetched from an upstream provider.
func UpdateFetchedModels(providerID string, models []string) {
	if models == nil {
		models = []string{}
	}
	fetchedModels.Store(providerID, models)
}

// ClearFetchedModel removes the cached model list for a provider.
func ClearFetchedModel(providerID string) {
	fetchedModels.Delete(providerID)
}

// GetFetchedModels returns the upstream-fetched model list cached for a
// provider id (populated by FetchModels and the background fetchers). Unlike
// GetProviderModels it deliberately ignores the statically configured
// p.Models, so callers that must report exactly what the upstream /models
// endpoint returned (桌面端供应商卡片的模型个数) get the raw upstream list.
// Returns a copy so callers cannot mutate the cache.
func GetFetchedModels(providerID string) []string {
	if v, ok := fetchedModels.Load(providerID); ok {
		if m, ok := v.([]string); ok {
			return append([]string(nil), m...)
		}
	}
	return nil
}

// GetProviderModels returns the model list for a provider.
// It prefers the statically configured p.Models; if that is empty it falls
// back to the runtime-fetched cache.
func GetProviderModels(p types.Provider) []string {
	if len(p.Models) > 0 {
		return p.Models
	}
	return GetFetchedModels(p.ID)
}

// ResolveProvider determines which provider should handle the given model
// name. Resolution order:
//
//  1. Exact / wildcard match against each enabled provider's model list.
//  2. Alias lookup (case-insensitive key match).
//  3. "@" suffix: "model@providerName" → validate providerName and resolve
//     modelPart against that provider's models/aliases.
//  4. Fall back to config.DefaultProvider.
//  5. If exactly one provider is enabled, return it.
//  6. Return nil.
func ResolveProvider(model string, config *types.Config) *types.Provider {
	p, _ := ResolveProviderWithStrip(model, config)
	return p
}

// ResolveProviderWithStrip is like ResolveProvider but also reports whether
// the model name carried an "@provider" hint (step 3). When true the caller
// should strip the "@..." suffix before sending the model upstream; when the
// match came from a literal model name containing "@", stripping must NOT
// happen.
func ResolveProviderWithStrip(model string, config *types.Config) (*types.Provider, bool) {
	if config == nil {
		return nil, false
	}
	// 空 model 走默认/唯一启用服务商，或与 * 通配模型匹配；
	// 带 model 的请求路由行为不受影响。
	return resolveProviderInner(model, config)
}

// ResolveWithFallback 原模型未命中时按配置的兜底模型链（FallbackModels，按序
// 多层兜底）逐个改写重解析，首个能路由的生效。返回 (服务商, stripAt, 生效模型)；
// 全部落空时 provider 为 nil、model 原样返回。
func ResolveWithFallback(model string, config *types.Config) (*types.Provider, bool, string) {
	p, strip := ResolveProviderWithStrip(model, config)
	if p != nil {
		return p, strip, model
	}
	for _, fb := range config.FallbackModels {
		fb = strings.TrimSpace(fb)
		if fb == "" || fb == model {
			continue
		}
		if p2, s2 := ResolveProviderWithStrip(fb, config); p2 != nil {
			return p2, s2, fb
		}
	}
	return nil, false, model
}

func resolveProviderInner(model string, config *types.Config) (*types.Provider, bool) {
	// 带 "@服务商" 后缀且后缀能命中某启用服务商时，跳过步骤 1 的通配匹配，
	// 交给步骤 3 精确路由：否则任一服务商的通配模式（如 claude-*）会吞掉
	// 整个含 @ 后缀的模型名，把明确指定的服务商绕过。
	atHint := false
	if idx := strings.LastIndex(model, "@"); idx >= 0 {
		hint := model[idx+1:]
		for i := range config.Providers {
			p := &config.Providers[i]
			if p.Enabled && (strings.EqualFold(p.ID, hint) || strings.EqualFold(p.Name, hint)) {
				atHint = true
				break
			}
		}
	}

	// 0. 精确别名优先于模型列表：显式配置的非通配别名是用户对模型名的强制覆盖
	//（如 agy 等客户端把内置 ID 定点改写/定向到某服务商），即使其它启用服务商
	// 的模型列表也声明了同名模型，也不能让列表兜底先把请求吞掉。
	if !atHint {
		for i := range config.Providers {
			p := &config.Providers[i]
			if !p.Enabled {
				continue
			}
			for alias := range p.Aliases {
				if strings.Contains(alias, "*") {
					continue
				}
				if MatchModel(alias, model) {
					return p, false
				}
			}
		}
	}

	// 1. Match against every enabled provider's model list (with wildcards).
	for i := range config.Providers {
		p := &config.Providers[i]
		if !p.Enabled {
			continue
		}
		for _, m := range GetProviderModels(*p) {
			if atHint && strings.Contains(m, "*") {
				continue
			}
			if MatchModel(m, model) {
				return p, false
			}
		}
	}

	// 2. Alias lookup（含通配：gemini-* 等模式可一次捕获多个模型名）。
	for i := range config.Providers {
		p := &config.Providers[i]
		if !p.Enabled {
			continue
		}
		for alias := range p.Aliases {
			// 与步骤 1 的 atHint 保护一致：携带 @服务商 后缀时跳过通配别名，
			// 避免其它服务商的通配别名吞掉整串（如 claude-* 匹配 claude-x@provider）
			if atHint && strings.Contains(alias, "*") {
				continue
			}
			if MatchModel(alias, model) {
				return p, false
			}
		}
	}

	// 3. "@" suffix: split at last "@" and validate.
	if idx := strings.LastIndex(model, "@"); idx >= 0 {
		modelPart := model[:idx]
		hint := model[idx+1:]
		hintMatched := false
		for i := range config.Providers {
			p := &config.Providers[i]
			if !p.Enabled {
				continue
			}
			if !strings.EqualFold(p.ID, hint) && !strings.EqualFold(p.Name, hint) {
				continue
			}
			hintMatched = true
			for _, m := range GetProviderModels(*p) {
				if MatchModel(m, modelPart) {
					return p, true
				}
			}
			for alias, target := range p.Aliases {
				if MatchModel(alias, modelPart) || MatchModel(target, modelPart) {
					return p, true
				}
			}
		}
		// hint 明确指向某个服务商、但该服务商下找不到匹配的模型/别名时，
		// 返回错误而不是静默落入默认服务商，避免上游抛出误导性的"模型不存在"。
		if hintMatched {
			return nil, false
		}
	}

	// 4. Default provider.
	if config.DefaultProvider != "" {
		for i := range config.Providers {
			p := &config.Providers[i]
			if !p.Enabled {
				continue
			}
			if p.ID == config.DefaultProvider || p.Name == config.DefaultProvider {
				return p, false
			}
		}
	}

	// 5. Only one enabled provider → use it.
	var enabled []*types.Provider
	for i := range config.Providers {
		if config.Providers[i].Enabled {
			enabled = append(enabled, &config.Providers[i])
		}
	}
	if len(enabled) == 1 {
		return enabled[0], false
	}

	return nil, false
}

// ModelOccurrences 统计裸模型名在启用的服务商中出现的次数（模型列表 + 别名，
// 每个服务商最多计一次）。用于判断客户端模型名是否需要 "@服务商名" 后缀。
func ModelOccurrences(config *types.Config, model string) int {
	if config == nil {
		return 0
	}
	count := 0
	for i := range config.Providers {
		p := &config.Providers[i]
		if !p.Enabled {
			continue
		}
		found := false
		for _, m := range GetProviderModels(*p) {
			if strings.Contains(m, "*") {
				continue
			}
			if m == model {
				found = true
				break
			}
		}
		if !found {
			for alias := range p.Aliases {
				if strings.Contains(alias, "*") {
					continue
				}
				if alias == model {
					found = true
					break
				}
			}
		}
		if found {
			count++
		}
	}
	return count
}

// ClientModelID 返回模型在客户端（/v1/models）中展示的名称：跨服务商重名的
// 模型加 "@服务商名" 后缀，独有模型保持裸名。与 AggregateModels 的命名规则
// 一致，保证路由匹配、管理页面与客户端列表三者统一。
func ClientModelID(config *types.Config, provName, model string) string {
	if ModelOccurrences(config, model) > 1 {
		return model + "@" + provName
	}
	return model
}

// AggregateModels builds the model list for a /v1/models response.
//
// Every model and every alias from every enabled provider is listed. When a
// model (or alias) name is offered by more than one provider the entry ID is
// suffixed with "@providerName" so clients can differentiate.
//
// Wildcard patterns (containing "*") are excluded from the output.
func AggregateModels(config *types.Config) []ModelEntry {
	if config == nil {
		return nil
	}

	type pm struct {
		id   string
		prov string
	}
	var flat []pm

	for _, p := range config.Providers {
		if !p.Enabled {
			continue
		}
		for _, m := range GetProviderModels(p) {
			if strings.Contains(m, "*") {
				continue
			}
			flat = append(flat, pm{m, p.Name})
		}
		for alias := range p.Aliases {
			if strings.Contains(alias, "*") {
				continue
			}
			flat = append(flat, pm{alias, p.Name})
		}
	}

	// Build entries, suffixing names shared by more than one provider.
	seen := make(map[string]bool)
	var entries []ModelEntry
	for _, x := range flat {
		id := ClientModelID(config, x.prov, x.id)
		if seen[id] {
			continue
		}
		seen[id] = true
		entries = append(entries, ModelEntry{ID: id, Provider: x.prov})
	}

	return entries
}
