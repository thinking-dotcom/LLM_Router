package router

import (
	"net/http"
	"testing"

	"llm-router-go/types"
)

func TestApplyHeaderRules(t *testing.T) {
	cfg := &types.Config{HeaderRules: []types.HeaderRule{
		{Name: "X-Trace-Id", Value: "t1"},                          // 全部供应商
		{Name: "X-Org", Value: "acme", Provider: "DeepSeek"},       // 指定供应商（大小写不敏感）
		{Name: "X-Other", Value: "no", Provider: "Other Provider"}, // 不匹配
		{Name: "   ", Value: "skip"},                               // 空名称丢弃
	}}
	p := &types.Provider{Name: "deepseek"}
	h := http.Header{}
	applyHeaderRules(h, p, cfg)
	if h.Get("X-Trace-Id") != "t1" {
		t.Fatalf("X-Trace-Id = %q", h.Get("X-Trace-Id"))
	}
	if h.Get("X-Org") != "acme" {
		t.Fatalf("X-Org = %q", h.Get("X-Org"))
	}
	if h.Get("X-Other") != "" {
		t.Fatalf("X-Other should not be set, got %q", h.Get("X-Other"))
	}

	// 覆盖语义：规则同名头后条覆盖前条，也覆盖已有头
	cfg2 := &types.Config{HeaderRules: []types.HeaderRule{
		{Name: "Authorization", Value: "Bearer custom"},
	}}
	h2 := http.Header{"Authorization": []string{"Bearer built-in"}}
	applyHeaderRules(h2, p, cfg2)
	if h2.Get("Authorization") != "Bearer custom" {
		t.Fatalf("Authorization = %q", h2.Get("Authorization"))
	}

	// 空规则 / nil 安全
	h3 := http.Header{"X-Keep": []string{"1"}}
	applyHeaderRules(h3, p, &types.Config{})
	applyHeaderRules(h3, nil, cfg)
	applyHeaderRules(h3, p, nil)
	if h3.Get("X-Keep") != "1" {
		t.Fatal("nil safety: header changed")
	}
}
