package router

import (
	"regexp"
	"strings"
	"sync"
)

var patternCache sync.Map

// MatchModel checks whether a model name matches a pattern.
// Patterns may contain "*" as a wildcard; literal matching is used otherwise.
// 匹配忽略大小写，与别名查找（EqualFold）行为一致：客户端发 GPT-4o
// 而配置为 gpt-4o 时不应落到默认服务商后被上游拒绝。
func MatchModel(pattern, model string) bool {
	if pattern == "" {
		return false
	}
	if strings.EqualFold(pattern, model) {
		return true
	}
	if strings.Contains(pattern, "*") {
		var re *regexp.Regexp
		if v, ok := patternCache.Load(pattern); ok {
			re = v.(*regexp.Regexp)
		} else {
			escaped := regexp.QuoteMeta(pattern)
			escaped = strings.ReplaceAll(escaped, `\*`, ".*")
			re = regexp.MustCompile("(?i)^" + escaped + "$")
			patternCache.Store(pattern, re)
		}
		return re.MatchString(model)
	}
	return false
}

// ResolveAlias 确定性地把模型名解析到别名目标：精确（忽略大小写）优先，
// 否则按「模式长度降序、字典序」选最具体的通配项。Go map 遍历顺序随机，
// 直接取首个命中会让同一请求在不同时刻落到不同的上游模型。
func ResolveAlias(aliases map[string]string, model string, skipWildcard bool) (string, bool) {
	if v, ok := aliases[model]; ok {
		return v, true
	}
	for k, v := range aliases {
		if strings.EqualFold(k, model) {
			return v, true
		}
	}
	if skipWildcard {
		return "", false
	}
	best, found := "", false
	for k := range aliases {
		if !strings.Contains(k, "*") || !MatchModel(k, model) {
			continue
		}
		if !found || len(k) > len(best) || (len(k) == len(best) && k < best) {
			best, found = k, true
		}
	}
	if !found {
		return "", false
	}
	return aliases[best], true
}
