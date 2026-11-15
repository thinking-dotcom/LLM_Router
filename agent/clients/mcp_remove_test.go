package clients

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 踢出：JSON 文件定点删除单个服务（其余服务与其它键保留）
func TestRemoveMcpFromClientJSON(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "mcp.json")
	orig := "{\n  \"other\": 1,\n  \"mcpServers\": {\n    \"a\": {\"command\": \"x\"},\n    \"b\": {\"url\": \"http://b\"}\n  }\n}"
	_ = os.WriteFile(fp, []byte(orig), 0o644)
	if err := removeMcpJSONFile(fp, "a"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	b, _ := os.ReadFile(fp)
	s := string(b)
	if strings.Contains(s, "\"a\"") {
		t.Fatalf("server a should be removed:\n%s", s)
	}
	if !strings.Contains(s, "\"b\"") || !strings.Contains(s, "\"other\"") {
		t.Fatalf("b/other must be kept:\n%s", s)
	}
	// 踢出不存在的服务：报错且不改文件
	if err := removeMcpJSONFile(fp, "nope"); err == nil {
		t.Fatal("removing absent server should fail")
	}
}

// 踢出：TOML 段落手术（仅摘除目标段，其余段落保留）
func TestRemoveMcpFromClientTOML(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "config.toml")
	orig := "# c\n[ui]\nx = 1\n\n[mcp_servers.keep]\ncommand = \"k\"\n\n[mcp_servers.gone]\ncommand = \"g\"\n"
	_ = os.WriteFile(fp, []byte(orig), 0o644)
	if err := RemoveTomlMcpServer(fp, "gone"); err != nil {
		t.Fatalf("remove toml: %v", err)
	}
	b, _ := os.ReadFile(fp)
	s := string(b)
	if strings.Contains(s, "gone") {
		t.Fatalf("section gone should be removed:\n%s", s)
	}
	for _, keep := range []string{"# c", "[ui]", "[mcp_servers.keep]", "command = \"k\""} {
		if !strings.Contains(s, keep) {
			t.Fatalf("missing %q:\n%s", keep, s)
		}
	}
}

// 一键踢出：clearMcpFile 清空全部服务（JSON 其它键保留、mcpServers 置空；TOML 全段摘除）
func TestClearMcpFile(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "settings.json")
	_ = os.WriteFile(fp, []byte(`{"model":"x","mcpServers":{"a":{"command":"a"},"b":{"url":"u"}}}`), 0o644)
	if err := clearMcpFile(fp); err != nil {
		t.Fatalf("clear json: %v", err)
	}
	b, _ := os.ReadFile(fp)
	if !strings.Contains(string(b), `"model"`) || strings.Contains(string(b), `"a"`) || strings.Contains(string(b), `"b"`) {
		t.Fatalf("clear json wrong:\n%s", b)
	}

	tf := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(tf, []byte("[ui]\nx=1\n\n[mcp_servers.a]\ncommand=\"a\"\n\n[mcp_servers.b]\nurl=\"u\"\n"), 0o644)
	if err := clearMcpFile(tf); err != nil {
		t.Fatalf("clear toml: %v", err)
	}
	tb, _ := os.ReadFile(tf)
	if !strings.Contains(string(tb), "[ui]") || strings.Contains(string(tb), "mcp_servers") {
		t.Fatalf("clear toml wrong:\n%s", tb)
	}
}

// 回归：踢出（定点删除）与清空都属于破坏性写入，必须先留一份 .bak；
// JSONC 落盘会退化为严格 JSON，注释一旦丢失只能靠备份找回。
func TestMcpRemoveKeepsBackup(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "mimocode.jsonc")
	original := "{\n  // 用户注释\n  \"mcpServers\": {\n    \"a\": { \"command\": \"a-cmd\" },\n  },\n}\n"
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeMcpJSONFile(fp, "a"); err != nil {
		t.Fatalf("removeMcpJSONFile: %v", err)
	}
	bak, err := os.ReadFile(fp + ".bak")
	if err != nil {
		t.Fatalf("缺少备份文件: %v", err)
	}
	if string(bak) != original {
		t.Fatalf("备份内容不是原始文件:\n%s", bak)
	}
}
