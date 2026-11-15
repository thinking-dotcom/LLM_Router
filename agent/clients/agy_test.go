package clients

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"llm-router-go/agent/core"
)

// 验证 Antigravity CLI（agy）目标注册形态：非占位、json 格式、ConfigFile 指向
// 真实 settings.json、PresetFile 指向参考模板、skills+mcp 均支持。
func TestAgyTargetRegistered(t *testing.T) {
	info := targetInfoOf(t, "agy")
	if info.Placeholder {
		t.Fatal("agy should not be placeholder")
	}
	if info.ConfigFormat != "json" {
		t.Errorf("agy format = %q, want json", info.ConfigFormat)
	}
	if !info.SkillsSupported || !info.McpSupported {
		t.Errorf("agy should support skills+mcp: %+v", info)
	}
	if strings.Contains(info.ConfigFile, "Json_modles") {
		t.Errorf("agy ConfigFile should be a real file, got %q", info.ConfigFile)
	} else if !strings.HasSuffix(strings.ReplaceAll(info.ConfigFile, "\\", "/"), ".gemini/antigravity-cli/settings.json") {
		t.Errorf("agy ConfigFile = %q, want .../.gemini/antigravity-cli/settings.json", info.ConfigFile)
	}
	if !strings.Contains(strings.ReplaceAll(info.PresetFile, "\\", "/"), "Json_modles/agy/") {
		t.Errorf("agy PresetFile should point to Json_modles/agy, got %q", info.PresetFile)
	}
}

// mergeAgyMcp：Antigravity CLI 全局 mcp_config.json 顶层 mcpServers 键合并，
// stdio 用 command/args，远程用 serverUrl；文件原有其它键保留。
func TestMergeAgyMcp(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "mcp_config.json")
	original := `{"userSettings": {"theme": "dark"}}`
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	servers := []core.MCPServer{
		{Name: "fs", Type: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}},
		{Name: "remote", Type: "sse", URL: "https://mcp.example.com/sse"},
		{Name: "http-bin", Type: "http", URL: "https://mcp.example.com/http"},
	}
	if err := mergeAgyMcp(fp, servers); err != nil {
		t.Fatalf("mergeAgyMcp: %v", err)
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		`"mcpServers"`,
		`"userSettings"`,
		`"fs"`,
		`"command": "npx"`,
		`"-y"`,
		`"serverUrl": "https://mcp.example.com/sse"`,
		`"serverUrl": "https://mcp.example.com/http"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mergeAgyMcp output missing %q\n%s", want, got)
		}
	}

	// 再次注入不产生重复键
	if err := mergeAgyMcp(fp, servers); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(fp)
	if strings.Count(string(b2), `"command": "npx"`) != 1 {
		t.Fatalf("duplicate server entries after re-inject:\n%s", b2)
	}
}

// SaveAgyMcp：格式校验分支正确返回错误信息（真实路径不可在测试中改写到主目录）。
func TestSaveAgyMcp(t *testing.T) {
	if errMsg := SaveAgyMcp(`{"mcpServers": [}`); errMsg == "" {
		t.Fatal("invalid JSON should be rejected")
	}
}
