package clients

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"llm-router-go/agent/core"
)

// containsStr 判断 hay 是否包含 needle（本包测试多处断言注入产物的子串）。
func containsStr(hay, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	return strings.Contains(hay, needle)
}

// targetInfoOf 取已注册客户端的适配器信息；未注册直接失败（注册自检测试用）。
func targetInfoOf(t *testing.T, id string) core.TargetInfo {
	t.Helper()
	a, ok := core.TargetOf(id)
	if !ok {
		t.Fatalf("%s not registered", id)
	}
	return a.Info()
}

// ---------------------------------------------------------------------------
// TOML 注入：冒烟校验 + 首次备份（内核的 json_patch_test.go 只管
// upsert/mergeMcpServers，段落手术属本包，故冒烟用例随实现一起放在这里）
// ---------------------------------------------------------------------------

func TestInjectTomlSmokeAndBackup(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.toml")
	original := "[core]\nkey = \"v\"\n\n[mcp_servers.old]\nurl = \"http://old\"\n"
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := injectTomlMcpServers(fp, []core.MCPServer{{Name: "fs", Type: "stdio", Command: "npx", Args: []string{"-y"}}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(fp)
	if err := core.ValidateToml(string(b)); err != nil {
		t.Fatalf("injected TOML not parseable: %v\n%s", err, b)
	}
	if !strings.Contains(string(b), "[mcp_servers.fs]") {
		t.Errorf("mcp_servers.fs missing:\n%s", b)
	}
	bb, err := os.ReadFile(fp + ".bak")
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(bb) != original {
		t.Errorf("backup mismatch:\n got: %q\nwant: %q", bb, original)
	}
}
