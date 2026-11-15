package router

import (
	"testing"

	"llm-router-go/types"
)

// 兜底模型链：原模型未命中 → 按序改写为链上首个可路由的模型；全落空返回 nil
func TestResolveWithFallbackChain(t *testing.T) {
	config := &types.Config{
		FallbackModels: []string{"  ", "missing-model", "m2", "m1"},
		Providers: []types.Provider{
			{ID: "a", Name: "A", Type: "openai", Enabled: true, Models: []string{"m1"}},
			{ID: "b", Name: "B", Type: "openai", Enabled: true, Models: []string{"m2"}},
		},
	}
	// 直接命中：不进兜底
	p, _, m := ResolveWithFallback("m1", config)
	if p == nil || p.ID != "a" || m != "m1" {
		t.Fatalf("direct match broken: %v %q", p, m)
	}
	// 未命中：链上空串/不可路由项跳过，m2 生效（改写 + 换服务商）
	p, _, m = ResolveWithFallback("totally-unknown", config)
	if p == nil || p.ID != "b" || m != "m2" {
		t.Fatalf("fallback chain broken: %v %q", p, m)
	}
	// 链全部不可路由：nil + 原模型
	p, _, m = ResolveWithFallback("x", &types.Config{FallbackModels: []string{"nope"}, Providers: config.Providers})
	if p != nil || m != "x" {
		t.Fatalf("exhausted chain should keep original model: %v %q", p, m)
	}
}
