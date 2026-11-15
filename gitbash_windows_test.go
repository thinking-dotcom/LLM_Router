//go:build windows

package main

import "testing"

// TestBashQuote 单引号内除 ' 外全部字面：& | %VAR% 空格等无需额外转义。
func TestBashQuote(t *testing.T) {
	cases := map[string]string{
		"claude":            `'claude'`,
		"--resume abc":      `'--resume abc'`,
		`C:\Users\a b\proj`: `'C:\Users\a b\proj'`,
		"a&b|c%d$e`f":       "'a&b|c%d$e`f'",
		"it's":              `'it'\''s'`,
		"":                  `''`,
	}
	for in, want := range cases {
		if got := bashQuote(in); got != want {
			t.Errorf("bashQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBuildBashCommand 组装与空 token 占位（保持参数位置不漂移）。
func TestBuildBashCommand(t *testing.T) {
	got := buildBashCommand([]string{"claude", "--resume", "01a-b"})
	if got != `'claude' '--resume' '01a-b'` {
		t.Fatalf("got %q", got)
	}
	if got := buildBashCommand([]string{"mimo", "", "x"}); got != `'mimo' '' 'x'` {
		t.Fatalf("empty token = %q", got)
	}
}

// TestFindGitBash 本机集成探测：PATH/注册表/常见路径至少应命中一个
// （开发机均装有 Git for Windows；没有时跳过而非失败）。
func TestFindGitBash(t *testing.T) {
	bash, err := findGitBash()
	if err != nil {
		t.Skip("本机未找到 Git Bash，跳过集成探测")
	}
	if !isGitBashExe(bash) {
		t.Fatalf("findGitBash 返回 %q 但未通过 git.exe 验证", bash)
	}
	t.Logf("findGitBash = %s", bash)
}
