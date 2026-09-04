package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 验证 Kilo kilo.jsonc 顶层的 mcp 键合并：其余键保留、Kilo 形
// 态（local/remote + enabled）正确、重复注入幂等、JSONC 注释兼容。
func TestMergeKiloMcp(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "kilo.jsonc")
	original := `{"$schema":"https://app.kilo.ai/config.json","model":"llm/glm-5.3-flash","permission":{"bash":"allow"}}`
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	servers := []MCPServer{
		{Name: "fs", Type: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}},
		{Name: "remote-http", Type: "http", URL: "http://127.0.0.1:3000/mcp"},
	}
	if err := mergeKiloMcp(fp, servers); err != nil {
		t.Fatalf("mergeKiloMcp: %v", err)
	}
	b, _ := os.ReadFile(fp)
	content := string(b)
	for _, want := range []string{
		`"model":"llm/glm-5.3-flash"`, `"permission"`, `"bash":"allow"`,
		`"mcp"`, `"fs"`, `"type": "local"`, `"command"`, `"npx"`,
		`"remote-http"`, `"type": "remote"`, `"url": "http://127.0.0.1:3000/mcp"`, `"enabled": true`,
	} {
		if !containsStr(content, want) {
			t.Fatalf("merged config missing %q:\n%s", want, content)
		}
	}

	// 再次注入：不产生重复键
	if err := mergeKiloMcp(fp, servers); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(fp)
	if strings.Count(string(b2), `"npx"`) != 1 {
		t.Fatalf("duplicate server entries after re-inject:\n%s", b2)
	}
}

// JSONC 文档（含 // 注释 / 尾逗号）也能正常合并 mcp 键。
func TestMergeKiloMcpJSONC(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "kilo.jsonc")
	original := "{\n  \"$schema\": \"https://app.kilo.ai/config.json\", // model 配置\n  \"model\": \"llm/glm-5.3-flash\",\n  \"permission\": {\n    \"bash\": \"allow\",\n  },\n}"
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mergeKiloMcp(fp, []MCPServer{{Name: "remote-sse", Type: "sse", URL: "http://127.0.0.1:3000/sse"}}); err != nil {
		t.Fatalf("mergeKiloMcp (JSONC): %v", err)
	}
	b, _ := os.ReadFile(fp)
	content := string(b)
	for _, want := range []string{`"mcp"`, `"remote-sse"`, `"http://127.0.0.1:3000/sse"`, `"type": "remote"`} {
		if !containsStr(content, want) {
			t.Fatalf("merged JSONC config missing %q:\n%s", want, content)
		}
	}
}

// 非法文档（坏 JSON）拒绝覆写并保留原文。
func TestMergeKiloMcpRejectBroken(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "kilo.jsonc")
	broken := `{this is not json`
	if err := os.WriteFile(fp, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mergeKiloMcp(fp, []MCPServer{{Name: "x", Type: "sse", URL: "u"}}); err == nil {
		t.Fatal("expected error for broken JSONC, got nil")
	}
	b, _ := os.ReadFile(fp)
	if string(b) != broken {
		t.Fatalf("broken file was overwritten:\n got %q", b)
	}
}
