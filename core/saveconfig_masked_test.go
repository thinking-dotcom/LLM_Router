package core

import (
	"testing"

	"llm-router-go/types"
)

// 掩码回写保护：提交值等于 apiKeyMask 时保留服务端现有密钥；
// 非掩码值（真实新 key）照常采用，不被误改写。
func TestPreserveMaskedAPIKeys(t *testing.T) {
	cfg := types.Config{Providers: []types.Provider{
		{ID: "p1", APIKey: apiKeyMask}, // 掩码哨兵 → 应回填真实 key
		{ID: "p2", APIKey: "real-2"},   // 非掩码 → 保持新值
		{ID: "p3", APIKey: apiKeyMask}, // 掩码但服务端无 key → 置空（不能把 "****" 当真实密钥落盘）
	}}
	cur := types.Config{Providers: []types.Provider{
		{ID: "p1", APIKey: "real-1"},
		{ID: "p2", APIKey: "old-2"},
		// p3 在服务端不存在
	}}

	preserveMaskedAPIKeys(&cfg, cur)

	if cfg.Providers[0].APIKey != "real-1" {
		t.Errorf("p1 key = %q, want preserved real-1 (masked should be replaced)", cfg.Providers[0].APIKey)
	}
	if cfg.Providers[1].APIKey != "real-2" {
		t.Errorf("p2 key = %q, want kept real-2 (non-masked must not be overwritten)", cfg.Providers[1].APIKey)
	}
	// 无匹配旧值时必须置空：哨兵 "****" 落盘会被当作真实密钥，上游鉴权必然 401
	if cfg.Providers[2].APIKey != "" {
		t.Errorf("p3 key = %q, want empty (masked sentinel must never be persisted as a real key)", cfg.Providers[2].APIKey)
	}
}
