package main

import (
	"strings"
	"testing"
)

// TestBuildTermCmdLine 校验启动系统终端的命令行组装：
// 标题在最前（可缺省）、cwd 用 cd /d、真正的命令在最后，且空命令不启动窗口。
func TestBuildTermCmdLine(t *testing.T) {
	got := buildTermCmdLine(`C:\work\proj`, `claude --resume abc`, "claude-code")
	want := `/k title claude-code & cd /d "C:\work\proj" & claude --resume abc`
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}

	// 无 cwd / 无标题：只留 /k + 命令
	if got := buildTermCmdLine("", "cmd", ""); got != "/k cmd" {
		t.Fatalf("bare = %q", got)
	}
	// 末尾反斜杠不产生 `\"`（会把引号转义掉）
	if got := buildTermCmdLine(`C:\`+"", "opencode", ""); !strings.Contains(got, `cd /d "C:"`) {
		t.Fatalf("trailing backslash = %q", got)
	}
	// 空命令：返回空串，由调用方报错，不弹空窗口
	if got := buildTermCmdLine(`C:\x`, "   ", "t"); got != "" {
		t.Fatalf("empty inner should be blank, got %q", got)
	}
}

// TestSanitizeTermTitle 标题里的命令分隔符必须被清掉，否则会把一条命令拆成多条。
func TestSanitizeTermTitle(t *testing.T) {
	cases := map[string]string{
		"claude-code":       "claude-code",
		`a & del x`:         "a del x",
		`x | y > z < w ^ v`: "x y z w v",
		`with "quote"`:      "with quote",
		"  spaced  name ":   "spaced name",
		"":                  "",
		"多行\n标题":            "多行 标题",
	}
	for in, want := range cases {
		if got := sanitizeTermTitle(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
