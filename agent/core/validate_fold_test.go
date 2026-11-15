package core

import (
	"strings"
	"testing"
)

// 跨行内联表（Grok Build 文档风格）必须能通过校验并折叠为单行
func TestFoldTomlInlineTablesMultiLine(t *testing.T) {
	src := `[permission]
rules = [
  { tool = "bash",
    action = "allow" },
  { action = "deny", tool = "mcp" },
]
`
	folded := foldTomlInlineTables(src)
	want := `[permission]
rules = [
  { tool = "bash",     action = "allow" },
  { action = "deny", tool = "mcp" },
]
`
	if folded != want {
		t.Fatalf("折叠结果不符：\n%s", folded)
	}
	if err := ValidateToml(src); err != nil {
		t.Fatalf("跨行内联表应通过校验: %v", err)
	}
}

// 折叠不得破坏：普通/字面量/多行字符串内的换行与花括号、注释、数组跨行
func TestFoldTomlInlineTablesPreserves(t *testing.T) {
	src := "# 注释里的 { 括号\n" +
		"a = \"line1{\\nline2}\"          # 行内注释 }\n" +
		"b = '''raw {\nmulti}line'''\n" +
		"c = \"\"\"multi\nline { with brace\nstill inside\"\"\"\n" +
		"arr = [\n  1,\n  2,\n]\n" +
		"tbl = { x = 1, y = { z = 2 } }\n"
	folded := foldTomlInlineTables(src)
	if folded != src {
		t.Fatalf("合法内容不应被改动：\n--- got ---\n%s\n--- want ---\n%s", folded, src)
	}
	if err := ValidateToml(src); err != nil {
		t.Fatalf("原文应通过校验: %v", err)
	}
}

// 嵌套内联表跨行
func TestFoldTomlInlineTablesNested(t *testing.T) {
	src := "tbl = {\n  x = 1,\n  y = {\n    z = 2\n  }\n}\n"
	folded := foldTomlInlineTables(src)
	if err := ValidateToml(folded); err != nil {
		t.Fatalf("折叠后应合法: %v", err)
	}
	if !strings.HasPrefix(folded, "tbl = {") || strings.Contains(folded, "}\n}\n") {
		t.Fatalf("嵌套内联表应全部折叠为单行：\n%s", folded)
	}
}

// CRLF 行尾的跨行内联表
func TestFoldTomlInlineTablesCRLF(t *testing.T) {
	src := "[permission]\r\nrules = [\r\n  { tool = \"bash\",\r\n    action = \"allow\" },\r\n]\r\n"
	folded := foldTomlInlineTables(src)
	if err := ValidateToml(folded); err != nil {
		t.Fatalf("CRLF 折叠后应合法: %v", err)
	}
	if strings.Contains(folded, "{ tool = \"bash\",\r") {
		t.Fatalf("内联表内换行应被折叠：\n%q", folded)
	}
}

// 真实 Grok 报错场景端到端：保存前校验（含跨行内联表 + 既有段落）
func TestValidateTomlGrokPermissionRules(t *testing.T) {
	src := `# ~/.grok/config.toml

[models]
default = "llm"

[ui]
permission_mode = "always-approve"

[permission]
rules = [
  { tool = "bash",
    action = "allow" },
]
`
	if err := ValidateToml(src); err != nil {
		t.Fatalf("Grok 文档风格权限规则应通过校验: %v", err)
	}
}
