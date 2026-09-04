package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 验证 YAML 段落手术：注入同名的旧条目被替换、新块写入、
// 其它段落与注释及用户自有的其它条目原样保留（合并语义）、
// 重复注入不产生重复键、空列表全量清除、产物仍是合法 YAML。
func TestInjectYamlMcpServers(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.yaml")
	original := "# 用户注释\nmodel:\n  provider: openai-compatible\n  api_key: \"1\"\n" +
		"\nmcp_servers:\n  old:\n    command: \"old-cmd\"\n\n" +
		"# 尾部说明\nsandbox:\n  enabled: true\n"
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	servers := []MCPServer{
		{Name: "fs", Type: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}},
		{Name: "remote", Type: "http", URL: "http://127.0.0.1:3000/mcp"},
	}
	if err := injectYamlMcpServers(fp, servers); err != nil {
		t.Fatalf("injectYamlMcpServers: %v", err)
	}
	b, _ := os.ReadFile(fp)
	content := string(b)

	for _, want := range []string{"# 用户注释", "model:", "openai-compatible", "mcp_servers:", "command: npx", "@mcp/fs", "url: http://127.0.0.1:3000/mcp", "sandbox:", "# 尾部说明"} {
		if !containsStr(content, want) {
			t.Fatalf("merged config missing %q:\n%s", want, content)
		}
	}
	// 合并语义：非本次注入的用户自有条目必须原样保留
	if !containsStr(content, "old-cmd") {
		t.Fatalf("user-owned entry `old` was dropped:\n%s", content)
	}
	if errMsg := ValidateYaml(content); errMsg != nil {
		t.Fatalf("merged config is not valid YAML: %v\n%s", errMsg, content)
	}

	// 再次注入同名服务：不产生重复键
	if err := injectYamlMcpServers(fp, servers); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(fp)
	if strings.Count(string(b2), "fs:") != 1 {
		t.Fatalf("duplicate keys after re-inject:\n%s", b2)
	}

	// 全量清除（空列表）：mcp_servers 块被摘除，其余内容保留
	if err := injectYamlMcpServers(fp, nil); err != nil {
		t.Fatal(err)
	}
	b3, _ := os.ReadFile(fp)
	c3 := string(b3)
	if containsStr(c3, "mcp_servers") || containsStr(c3, "old-cmd") {
		t.Fatalf("wipe left mcp_servers content:\n%s", c3)
	}
	for _, want := range []string{"# 用户注释", "model:", "openai-compatible", "sandbox:", "enabled: true", "# 尾部说明"} {
		if !containsStr(c3, want) {
			t.Fatalf("wipe dropped non-mcp content %q:\n%s", want, c3)
		}
	}
}
