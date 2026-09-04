package router

import (
	"strings"
	"testing"

	"llm-router-go/types"
)

// gemini-openai 不得参与直通：Google 兼容端点不接受 OpenAI 扩展字段，
// 必须走 BuildOutboundRequest 重建以剥离 prompt_cache_key 等字段。
func TestIsOpenaiLikeExcludesGeminiOpenAI(t *testing.T) {
	if !isOpenaiLike("openai") {
		t.Error("openai should be openai-like")
	}
	if isOpenaiLike("gemini-openai") {
		t.Error("gemini-openai must NOT be openai-like (passthrough would skip field stripping)")
	}
}

// 模型列表端点自适应：任一服务商都尝试标准端点 + 通用 /models 回退 + 兼容层回退。
func TestModelsEndpointsFallback(t *testing.T) {
	// Anthropic 兼容层（DeepSeek /anthropic）：应含根地址的 OpenAI 风格 /models
	ds := types.Provider{Type: types.ProviderAnthropic, BaseURL: "https://api.deepseek.com/anthropic"}
	deps := modelsEndpoints(ds)
	foundRoot := false
	for _, e := range deps {
		if e.url == "https://api.deepseek.com/models" {
			foundRoot = true
			if !e.bearer {
				t.Error("root /models should use bearer auth")
			}
		}
	}
	if !foundRoot {
		t.Errorf("expected root /models fallback for anthropic-compatible base, got %+v", deps)
	}

	// openai 类型：标准 /v1/models 之外应有 /models 通用回退
	o := types.Provider{Type: types.ProviderOpenAI, BaseURL: "https://api.example.com"}
	oeps := modelsEndpoints(o)
	hasModels := false
	for _, e := range oeps {
		if e.url == "https://api.example.com/models" {
			hasModels = true
		}
	}
	if !hasModels {
		t.Errorf("openai type should include /models fallback, got %+v", oeps)
	}
	// 主候选优先
	if oeps[0].url != "https://api.example.com/v1/models" {
		t.Errorf("primary should be /v1/models, got %+v", oeps[0])
	}

	// gemini 类型：主候选 /v1beta/models 保留，且不冲突
	g := types.Provider{Type: types.ProviderGemini, BaseURL: "https://generativelanguage.googleapis.com"}
	geps := modelsEndpoints(g)
	if !strings.HasPrefix(geps[0].url, "https://generativelanguage.googleapis.com/v1beta/models") {
		t.Errorf("gemini primary should be /v1beta/models, got %+v", geps[0])
	}
}

func TestNormalizeDoubledPath(t *testing.T) {
	tests := []struct{ in, want string }{
		// 正常路径不受影响
		{"/chat/completions", "/chat/completions"},
		{"/v1/chat/completions", "/v1/chat/completions"},
		{"/v1/models", "/v1/models"},
		{"/models", "/models"},
		{"/v1/messages", "/v1/messages"},
		{"/v1beta/models/x:generateContent", "/v1beta/models/x:generateContent"},
		{"/api/chat", "/api/chat"},
		// 把完整端点当 base_url 的 SDK 拼出的重复路径
		{"/chat/completions/chat/completions", "/chat/completions"},
		{"/v1/chat/completions/chat/completions", "/v1/chat/completions"},
		{"/chat/completions/models", "/models"},
		{"/v1/chat/completions/models", "/v1/models"},
		{"/v1/messages/v1/messages", "/v1/messages"},
		{"/v1/messages/v1/models", "/v1/models"},
	}
	for _, tt := range tests {
		if got := normalizeDoubledPath(tt.in); got != tt.want {
			t.Errorf("normalizeDoubledPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMatchModel(t *testing.T) {
	tests := []struct {
		pattern string
		model   string
		want    bool
	}{
		{"gpt-4", "gpt-4", true},
		{"gpt-4", "gpt-3.5", false},
		{"", "gpt-4", false},
		{"gpt-*", "gpt-4", true},
		{"gpt-*", "gpt-4-turbo", true},
		{"gpt-*", "claude-3", false},
		{"claude-*", "claude-3-opus", true},
		{"*", "anything", true},
	}

	for _, tt := range tests {
		got := MatchModel(tt.pattern, tt.model)
		if got != tt.want {
			t.Errorf("MatchModel(%q, %q) = %v, want %v", tt.pattern, tt.model, got, tt.want)
		}
	}
}

func TestResolveProvider(t *testing.T) {
	cfg := &types.Config{
		Providers: []types.Provider{
			{
				ID: "p1", Name: "ds", Type: types.ProviderOpenAI,
				Models: []string{"deepseek-*"}, Enabled: true,
			},
			{
				ID: "p2", Name: "claude", Type: types.ProviderAnthropic,
				Models: []string{"claude-*"}, Enabled: true,
			},
			{
				ID: "p3", Name: "gemini", Type: types.ProviderGemini,
				Models: []string{"gemini-*"}, Enabled: true,
				Aliases: map[string]string{"gemini-pro": "gemini-2.0-pro"},
			},
			{
				ID: "p4", Name: "disabled", Type: types.ProviderOpenAI,
				Models: []string{"disabled-model"}, Enabled: false,
			},
		},
	}

	// Wildcard match
	if p := ResolveProvider("deepseek-chat", cfg); p == nil || p.ID != "p1" {
		t.Error("deepseek-chat should match p1")
	}

	// Exact match through wildcard
	if p := ResolveProvider("claude-3-opus", cfg); p == nil || p.ID != "p2" {
		t.Error("claude-3-opus should match p2")
	}

	// Alias match
	if p := ResolveProvider("gemini-pro", cfg); p == nil || p.ID != "p3" {
		t.Error("gemini-pro should match p3 via alias")
	}

	// @ProviderName match
	if p := ResolveProvider("claude-3@claude", cfg); p == nil || p.ID != "p2" {
		t.Error("claude-3@claude should match p2")
	}

	// Disabled provider should not match
	if p := ResolveProvider("disabled-model", cfg); p != nil {
		t.Error("disabled-model should not match (provider disabled)")
	}

	// No match
	if p := ResolveProvider("unknown-model", cfg); p != nil {
		t.Error("unknown-model should not match any provider")
	}
}

func TestResolveProviderOnlyOne(t *testing.T) {
	// When only one provider is enabled, all models go to it
	cfg := &types.Config{
		Providers: []types.Provider{
			{
				ID: "p1", Name: "ds", Type: types.ProviderOpenAI,
				Models: []string{"deepseek-*"}, Enabled: true,
			},
		},
	}

	if p := ResolveProvider("any-model", cfg); p == nil || p.ID != "p1" {
		t.Error("any-model should match the sole enabled provider")
	}
}

func TestResolveProviderDefault(t *testing.T) {
	cfg := &types.Config{
		DefaultProvider: "p2",
		Providers: []types.Provider{
			{
				ID: "p1", Name: "ds", Type: types.ProviderOpenAI,
				Models: []string{"deepseek-*"}, Enabled: true,
			},
			{
				ID: "p2", Name: "fallback", Type: types.ProviderOpenAI,
				Models: []string{"gpt-*"}, Enabled: true,
			},
		},
	}

	if p := ResolveProvider("unknown", cfg); p == nil || p.ID != "p2" {
		t.Error("unknown model should fall back to default provider p2")
	}
}

func TestAggregateModels(t *testing.T) {
	cfg := &types.Config{
		Providers: []types.Provider{
			{
				ID: "p1", Name: "ds", Type: types.ProviderOpenAI,
				Models: []string{"deepseek-chat", "deepseek-coder"}, Enabled: true,
			},
			{
				ID: "p2", Name: "claude", Type: types.ProviderAnthropic,
				Models: []string{"claude-3-opus", "deepseek-chat"}, Enabled: true, // deepseek-chat appears in both
			},
			{
				ID: "p3", Name: "hidden", Type: types.ProviderOpenAI,
				Models: []string{"hidden-model"}, Enabled: false,
			},
		},
	}

	models := AggregateModels(cfg)

	// deepseek-chat appears in 2 providers → should be suffixed
	found := map[string]bool{}
	for _, m := range models {
		found[m.ID] = true
	}

	if !found["deepseek-chat@ds"] {
		t.Error("expected deepseek-chat@ds in model list")
	}
	if !found["deepseek-chat@claude"] {
		t.Error("expected deepseek-chat@claude in model list")
	}
	if !found["claude-3-opus"] {
		t.Error("expected claude-3-opus in model list")
	}
	if found["hidden-model"] {
		t.Error("disabled provider model should not appear")
	}
}

func TestClientModelID(t *testing.T) {
	cfg := &types.Config{
		Providers: []types.Provider{
			{
				ID: "p1", Name: "ds", Type: types.ProviderOpenAI,
				Models: []string{"deepseek-chat", "deepseek-coder"}, Enabled: true,
			},
			{
				ID: "p2", Name: "claude", Type: types.ProviderAnthropic,
				Models: []string{"claude-3-opus", "deepseek-chat"}, Enabled: true, // deepseek-chat appears in both
				Aliases: map[string]string{"gpt-4-turbo": "claude-3-opus"}, // 与 alias-pro 的别名重名
			},
			{
				ID: "p3", Name: "alias-pro", Type: types.ProviderOpenAI,
				Models:  []string{"gpt-4"},
				Aliases: map[string]string{"gpt-4-turbo": "gpt-4"}, Enabled: true,
			},
		},
	}

	// 跨服务商重名 → 加 @服务商名 后缀
	if got := ClientModelID(cfg, "ds", "deepseek-chat"); got != "deepseek-chat@ds" {
		t.Errorf("ClientModelID(ds, deepseek-chat) = %q, want %q", got, "deepseek-chat@ds")
	}
	if got := ClientModelID(cfg, "claude", "deepseek-chat"); got != "deepseek-chat@claude" {
		t.Errorf("ClientModelID(claude, deepseek-chat) = %q, want %q", got, "deepseek-chat@claude")
	}

	// 独有模型 → 裸名
	if got := ClientModelID(cfg, "ds", "deepseek-coder"); got != "deepseek-coder" {
		t.Errorf("ClientModelID(ds, deepseek-coder) = %q, want %q", got, "deepseek-coder")
	}
	if got := ClientModelID(cfg, "claude", "claude-3-opus"); got != "claude-3-opus" {
		t.Errorf("ClientModelID(claude, claude-3-opus) = %q, want %q", got, "claude-3-opus")
	}

	// 别名重名也算跨服务商
	if got := ClientModelID(cfg, "alias-pro", "gpt-4-turbo"); got != "gpt-4-turbo@alias-pro" {
		t.Errorf("ClientModelID(alias-pro, gpt-4-turbo) = %q, want %q", got, "gpt-4-turbo@alias-pro")
	}
}

func TestUpdateFetchedModels(t *testing.T) {
	// Test that fetching models populates the cache
	UpdateFetchedModels("p-test", []string{"model-a", "model-b"})

	prov := types.Provider{ID: "p-test", Type: types.ProviderOpenAI}
	models := GetProviderModels(prov)

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0] != "model-a" || models[1] != "model-b" {
		t.Error("model names mismatch")
	}

	// Static config should override fetched models
	prov2 := types.Provider{ID: "p-test", Models: []string{"static-model"}}
	models2 := GetProviderModels(prov2)
	if len(models2) != 1 || models2[0] != "static-model" {
		t.Error("static models should take priority over fetched")
	}
}
