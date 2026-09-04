package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 验证 Gemini settings.json 的 mcpServers 合并：其余键保留、
// stdio/sse/http 三种形态字段正确、重复注入幂等。
func TestMergeGeminiMcp(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "settings.json")
	original := `{"autoAccept": true, "model": {"name": "x-preview-f-free"}, "ui": {"theme": "dark"}}`
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	servers := []MCPServer{
		{Name: "fs", Type: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}},
		{Name: "remote-http", Type: "http", URL: "http://127.0.0.1:3000/mcp"},
		{Name: "remote-sse", Type: "sse", URL: "http://127.0.0.1:3000/sse"},
	}
	if err := mergeGeminiMcp(fp, servers); err != nil {
		t.Fatalf("mergeGeminiMcp: %v", err)
	}
	b, _ := os.ReadFile(fp)
	content := string(b)

	for _, want := range []string{
		`"autoAccept": true`, `"ui"`, `"theme": "dark"`,
		`"mcpServers"`, `"command": "npx"`, `"@mcp/fs"`,
		`"httpUrl": "http://127.0.0.1:3000/mcp"`, `"url": "http://127.0.0.1:3000/sse"`,
	} {
		if !containsStr(content, want) {
			t.Fatalf("merged config missing %q:\n%s", want, content)
		}
	}

	// 再次注入：不产生重复键
	if err := mergeGeminiMcp(fp, servers); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(fp)
	if strings.Count(string(b2), `"command": "npx"`) != 1 {
		t.Fatalf("duplicate server entries after re-inject:\n%s", b2)
	}
}
