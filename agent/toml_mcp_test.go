package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 验证 TOML 段落手术：注入同名的旧段被替换、新段写入、
// 其它段落与注释及用户自有的其它 server 段原样保留（合并语义）、
// 重复注入不产生重复段、空列表全量清除、产物仍是合法 TOML。
func TestInjectTomlMcpServers(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.toml")
	original := "# 用户注释\n[models]\ndefault = \"llm\"\n" +
		"\n[mcp_servers.old]\ncommand = \"old-cmd\"\n\n" +
		"[ui]\nyolo = false\n"
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	servers := []MCPServer{
		{Name: "fs", Type: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}},
		{Name: "remote", Type: "http", URL: "http://127.0.0.1:3000/mcp"},
	}
	if err := injectTomlMcpServers(fp, servers); err != nil {
		t.Fatalf("injectTomlMcpServers: %v", err)
	}
	b, _ := os.ReadFile(fp)
	content := string(b)

	for _, want := range []string{"# 用户注释", "[models]", "[ui]", "yolo", "[mcp_servers.fs]", `"@mcp/fs"`, "[mcp_servers.remote]"} {
		if !containsStr(content, want) {
			t.Fatalf("merged config missing %q:\n%s", want, content)
		}
	}
	// 合并语义：非本次注入的用户自有 server 段必须原样保留
	if !containsStr(content, "[mcp_servers.old]") || !containsStr(content, "old-cmd") {
		t.Fatalf("user-owned section [mcp_servers.old] was dropped:\n%s", content)
	}
	if errMsg := ValidateToml(content); errMsg != nil {
		t.Fatalf("merged config is not valid TOML: %v\n%s", errMsg, content)
	}

	// 再次注入同名服务：不产生重复段
	if err := injectTomlMcpServers(fp, servers); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(fp)
	if strings.Count(string(b2), "[mcp_servers.fs]") != 1 {
		t.Fatalf("duplicate sections after re-inject:\n%s", b2)
	}

	// 全量清除（空列表）：所有 [mcp_servers.*] 段被摘除，其余内容保留
	if err := injectTomlMcpServers(fp, nil); err != nil {
		t.Fatal(err)
	}
	b3, _ := os.ReadFile(fp)
	c3 := string(b3)
	if containsStr(c3, "mcp_servers") {
		t.Fatalf("wipe left mcp_servers content:\n%s", c3)
	}
	for _, want := range []string{"# 用户注释", "[models]", "[ui]", "yolo"} {
		if !containsStr(c3, want) {
			t.Fatalf("wipe dropped non-mcp content %q:\n%s", want, c3)
		}
	}
}
