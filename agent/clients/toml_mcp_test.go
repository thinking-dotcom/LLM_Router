package clients

import (
	"llm-router-go/agent/core"

	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 验证 TOML 段落手术（替换语义）：既有 [mcp_servers.*] 段全部清掉、
// 写入本次所选服务段，其它段落与注释原样保留、
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

	servers := []core.MCPServer{
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
	// 替换语义：非本次所选的旧 server 段被清除
	if containsStr(content, "[mcp_servers.old]") || containsStr(content, "old-cmd") {
		t.Fatalf("old server section should be replaced:\n%s", content)
	}
	if errMsg := core.ValidateToml(content); errMsg != nil {
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

// 回归：数组表 [[...]] 没被识别成段边界时，[mcp_servers.*] 之后的 [[hooks]]
// 及其内容会被当成 mcp 段一起删掉（文件仍是合法 TOML，所以只靠校验发现不了）。
func TestTomlMcpKeepsArrayOfTables(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.toml")
	original := "[mcp_servers.a]\ncommand = \"a-cmd\"\n\n" +
		"[[hooks]]\nevent = \"notify\"\nrun = \"echo hi\"\n\n" +
		"[core]\nmodel = \"x\"\n"
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// 清空全部 mcp 段（走的就是注入空列表的路径）
	if err := injectTomlMcpServers(fp, nil); err != nil {
		t.Fatalf("injectTomlMcpServers(nil): %v", err)
	}
	b, _ := os.ReadFile(fp)
	content := string(b)
	for _, want := range []string{"[[hooks]]", `event = "notify"`, `run = "echo hi"`, "[core]", `model = "x"`} {
		if !containsStr(content, want) {
			t.Fatalf("array-of-tables section was dropped, missing %q:\n%s", want, content)
		}
	}
	if containsStr(content, "mcp_servers.a") {
		t.Fatalf("mcp section should be removed:\n%s", content)
	}
	if errMsg := core.ValidateToml(content); errMsg != nil {
		t.Fatalf("result is not valid TOML: %v\n%s", errMsg, content)
	}
}

// 回归：单条移除（RemoveTomlMcpServer）同样不能吞掉后面的数组表。
func TestRemoveTomlMcpKeepsArrayOfTables(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.toml")
	original := "[mcp_servers.a]\ncommand = \"a-cmd\"\n\n[mcp_servers.b]\ncommand = \"b-cmd\"\n\n[[hooks]]\nrun = \"echo hi\"\n"
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveTomlMcpServer(fp, "a"); err != nil {
		t.Fatalf("RemoveTomlMcpServer: %v", err)
	}
	b, _ := os.ReadFile(fp)
	content := string(b)
	if containsStr(content, "mcp_servers.a") {
		t.Fatalf("section a not removed:\n%s", content)
	}
	for _, want := range []string{"[mcp_servers.b]", "b-cmd", "[[hooks]]", `run = "echo hi"`} {
		if !containsStr(content, want) {
			t.Fatalf("missing %q after removal:\n%s", want, content)
		}
	}
}
