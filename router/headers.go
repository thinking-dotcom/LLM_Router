package router

import (
	"net/http"
	"strings"

	"llm-router-go/types"
)

// applyHeaderRules 按系统设置的「请求头注入」规则给出站请求追加自定义请求头。
// 规则未指定供应商（空）对全部供应商生效；指定了供应商则按名称匹配（大小写不敏感，
// 与路由解析的供应商名一致性由设置页下拉保证）。同名头后条覆盖前条、也覆盖
// 格式转换层设置的鉴权头——允许用户显式改写（如自定义 authorization）。
func applyHeaderRules(h http.Header, provider *types.Provider, config *types.Config) {
	if config == nil || provider == nil || len(config.HeaderRules) == 0 {
		return
	}
	for _, rule := range config.HeaderRules {
		name := strings.TrimSpace(rule.Name)
		if name == "" {
			continue
		}
		if rule.Provider != "" && !strings.EqualFold(rule.Provider, provider.Name) {
			continue
		}
		h.Set(name, rule.Value)
	}
}
