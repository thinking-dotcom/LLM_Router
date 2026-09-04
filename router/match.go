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
