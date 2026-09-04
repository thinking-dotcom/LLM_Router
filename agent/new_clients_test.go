package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// getRealText：真实文件存在时直读；不存在时用预设模板作首次初始值（exists=false）。
func TestGetRealTextPresetFallback(t *testing.T) {
	dir := t.TempDir()
	tpl := filepath.Join(dir, "tpl.json")
	real := filepath.Join(dir, "real.json")
	if err := os.WriteFile(tpl, []byte(`{"preset":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// 真实不存在 → 返回模板内容作初始值，exists=false，filePath 为真实路径
	c, ex, fp, _ := getRealText(real, tpl)
	if ex || c != `{"preset":true}` || fp != real {
		t.Fatalf("fallback = %q exists=%v fp=%q", c, ex, fp)
	}
	// 真实存在 → 直读真实内容
	if err := os.WriteFile(real, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, ex, fp, _ = getRealText(real, tpl)
	if !ex || c != `{"a":1}` || fp != real {
		t.Fatalf("real read = %q exists=%v fp=%q", c, ex, fp)
	}
}

// 新接入客户端注册形态校验：非占位、ConfigFile 指向真实文件并带 PresetFile、
// 格式标记正确；claude-desktop 保持直编真实文件（ConfigFile 回退 McpFile）。
func TestNewClientTargetsRegistered(t *testing.T) {
	cases := []struct {
		id     string
		format string
	}{
		{"codex", "toml"},
		{"cline", ""},
		{"hermes", "yaml"},
		{"kimi", "toml"},
	}
	for _, c := range cases {
		a, ok := registry[c.id]
		if !ok {
			t.Fatalf("%s not registered", c.id)
		}
		info := a.Info()
		if info.Placeholder {
			t.Errorf("%s still placeholder", c.id)
		}
		if c.format != "" && info.ConfigFormat != c.format {
			t.Errorf("%s format = %q, want %q", c.id, info.ConfigFormat, c.format)
		}
		if info.ConfigFile == "" {
			t.Errorf("%s ConfigFile empty", c.id)
		}
	}
	for _, id := range []string{"codex", "cline", "hermes", "kimi", "claude-code", "opencode", "pi", "gemini", "grok-build", "commandcode"} {
		info := registry[id].Info()
		// ConfigFile 必须是真实文件（不在 Json_modles 模板目录下）；模板仅作首次初始值
		if strings.Contains(info.ConfigFile, "Json_modles") {
			t.Errorf("%s ConfigFile should be a real file, got %q", id, info.ConfigFile)
		}
		if !strings.Contains(info.PresetFile, "Json_modles") {
			t.Errorf("%s PresetFile should be the preset template, got %q", id, info.PresetFile)
		}
	}
	count := 0
	for range registry {
		count++
	}
	if count < 11 {
		t.Errorf("registry count = %d, want >= 11", count)
	}
	di := registry["claude-desktop"].Info()
	if di.ConfigFile != di.McpFile {
		t.Errorf("claude-desktop ConfigFile should fall back to McpFile (got %q)", di.ConfigFile)
	}
}

// mergeKimiMcp：Kimi Code 的 mcp.json 为 Claude 风格 mcpServers 对象；
// stdio 写 command/args（不带 type），HTTP 写 url（不带 transport），
// SSE 写 url + transport = "sse"；文件原有其它键原样保留。
func TestMergeKimiMcp(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "mcp.json")
	servers := []MCPServer{
		{Name: "fs", Type: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}},
		{Name: "linear", Type: "http", URL: "https://mcp.linear.app/mcp"},
		{Name: "legacy-events", Type: "sse", URL: "https://mcp.example.com/sse"},
	}
	if err := mergeKimiMcp(fp, servers); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		`"mcpServers"`,
		`"fs"`,
		`"command": "npx"`,
		`"-y"`,
		`"@modelcontextprotocol/server-filesystem"`,
		`"/tmp"`,
		`"linear"`,
		`"url": "https://mcp.linear.app/mcp"`,
		`"legacy-events"`,
		`"transport": "sse"`,
		`"url": "https://mcp.example.com/sse"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mergeKimiMcp output missing %q\n%s", want, got)
		}
	}
	// stdio 条目不得带 type 字段（Kimi 按 command 字段推断），sse 必须显式 transport
	if strings.Contains(got, `"type": "stdio"`) {
		t.Errorf("kimi stdio should not carry a type field:\n%s", got)
	}
	if !strings.Contains(got, `"transport": "sse"`) {
		t.Errorf("kimi sse should carry transport:\n%s", got)
	}
	// 文件不存在时按空对象初始化；再次注入应保留已存在的其它 server
	if err := mergeKimiMcp(fp, []MCPServer{{Name: "fs2", Type: "stdio", Command: "echo"}}); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(fp)
	if !strings.Contains(string(b2), `"fs"`) || !strings.Contains(string(b2), `"fs2"`) {
		t.Errorf("second merge should keep existing servers:\n%s", string(b2))
	}
}
