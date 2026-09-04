package config

import (
	"encoding/json"
	"os"
	"testing"

	"llm-router-go/types"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Port != 3000 {
		t.Errorf("expected port 3000, got %d", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("expected host 127.0.0.1, got %s", cfg.Host)
	}
}

func TestNormalize(t *testing.T) {
	raw := map[string]any{
		"port": float64(8080),
		"host": "0.0.0.0",
		"providers": []any{
			map[string]any{
				"name":    "test",
				"type":    "openai",
				"baseUrl": "https://api.openai.com",
			},
		},
	}
	cfg := Normalize(raw)
	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("expected host 0.0.0.0, got %s", cfg.Host)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(cfg.Providers))
	}
	p := cfg.Providers[0]
	if p.Name != "test" {
		t.Errorf("expected name test, got %s", p.Name)
	}
	if p.ID == "" {
		t.Error("expected auto-generated provider ID")
	}
	if !p.Enabled {
		t.Error("expected provider to be enabled by default")
	}
}

func TestLoadSaveConfig(t *testing.T) {
	// Use temp file
	tmp := t.TempDir()
	os.Setenv("LLMROUTER_CONFIG", tmp+"/test-config.json")
	defer os.Unsetenv("LLMROUTER_CONFIG")
	configPathCache = "" // reset cache

	// Load should create default config
	cfg := LoadConfig()
	if cfg.Port != 3000 {
		t.Errorf("expected default port 3000, got %d", cfg.Port)
	}

	// Save modified config
	cfg.Port = 9999
	SaveConfig(cfg)

	// Load again
	cfg2 := LoadConfig()
	if cfg2.Port != 9999 {
		t.Errorf("expected port 9999 after reload, got %d", cfg2.Port)
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     any
		wantErr bool
	}{
		{"valid", map[string]any{
			"port": float64(3000),
			"host": "127.0.0.1",
			"providers": []any{
				map[string]any{
					"name":    "test",
					"type":    "openai",
					"baseUrl": "https://api.test.com",
				},
			},
		}, false},
		{"invalid port", map[string]any{
			"port": float64(99999),
			"host": "127.0.0.1",
		}, true},
		{"missing host", map[string]any{
			"port": float64(3000),
		}, true},
		{"missing provider name", map[string]any{
			"port": float64(3000),
			"host": "127.0.0.1",
			"providers": []any{
				map[string]any{
					"type":    "openai",
					"baseUrl": "https://api.test.com",
				},
			},
		}, true},
		{"invalid baseUrl", map[string]any{
			"port": float64(3000),
			"host": "127.0.0.1",
			"providers": []any{
				map[string]any{
					"name":    "test",
					"type":    "openai",
					"baseUrl": "not-a-url",
				},
			},
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.raw)
			if (err != "") != tt.wantErr {
				t.Errorf("ValidateConfig() error = %q, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigRoundtrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Port = 5555
	cfg.Providers = []types.Provider{
		{ID: "p1", Name: "ds", Type: "openai", BaseURL: "https://api.deepseek.com", Enabled: true},
	}
	b, _ := json.Marshal(cfg)
	var raw any
	json.Unmarshal(b, &raw)
	normalized := Normalize(raw)
	if normalized.Port != 5555 {
		t.Error("port mismatch")
	}
	if len(normalized.Providers) != 1 {
		t.Fatal("provider count mismatch")
	}
	if normalized.Providers[0].Name != "ds" {
		t.Error("provider name mismatch")
	}
}
