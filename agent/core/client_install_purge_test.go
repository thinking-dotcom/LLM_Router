package core

import (
	"os"
	"path/filepath"
	"testing"
)

// purge 测试的公共环境：把主目录与 APPDATA 全部指到临时目录，
// 避免测试进程碰真实用户数据；LLMROUTER_DATA 隔离 skills 注入清单。
func purgeTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LLMROUTER_DATA", filepath.Join(t.TempDir(), "data"))
	return home
}

// mkFile 在临时主目录下造一个带内容的文件（自动建父目录），返回其绝对路径。
func mkFile(t *testing.T, home, rel string) string {
	t.Helper()
	p := filepath.Join(home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustExist(t *testing.T, home, rel string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(rel))); err != nil {
		t.Errorf("%s 应当保留，实际不存在（%v）", rel, err)
	}
}

func mustGone(t *testing.T, home, rel string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Errorf("%s 应当已删除", rel)
	}
}

// purge「彻底清理」应整删登记足迹（含会话、凭据），并通过同名清扫捞到
// 登记之外的漏网目录；无关客户端的文件不受影响。
func TestPurgeClientConfigRemovesAllResidue(t *testing.T) {
	home := purgeTestHome(t)

	mkFile(t, home, ".claude/settings.json")
	mkFile(t, home, ".claude/projects/s1/conv.json") // 会话记录
	mkFile(t, home, ".claude/.credentials.json")     // 登录凭据
	mkFile(t, home, ".claude.json")
	mkFile(t, home, ".cache/claude/extra.bin") // 登记外，靠同名清扫兜底
	// 无关客户端 codex：不在本次清理范围，必须原样保留
	mkFile(t, home, ".codex/config.toml")
	mkFile(t, home, ".codex/auth.json")

	removed := purgeClientConfig("claude-code")
	if len(removed) == 0 {
		t.Fatal("期望清理到残留，实际没有删除任何路径")
	}
	for _, rel := range []string{".claude", ".claude.json", ".cache/claude"} {
		mustGone(t, home, rel)
	}
	for _, rel := range []string{".codex/config.toml", ".codex/auth.json"} {
		mustExist(t, home, rel)
	}
}

// 共用根保护：agy 与 gemini CLI 共用 ~/.gemini，purge agy 只能删 agy 自己的
// 子目录，~/.gemini 本体与 gemini CLI 的文件必须原样保留。
func TestPurgeClientConfigKeepsSharedRoots(t *testing.T) {
	home := purgeTestHome(t)

	mkFile(t, home, ".gemini/settings.json")                 // gemini CLI 的，不碰
	mkFile(t, home, ".gemini/extensions/x.json")             // gemini CLI 的，不碰
	mkFile(t, home, ".gemini/antigravity-cli/settings.json") // agy 的
	mkFile(t, home, ".gemini/config/mcp_config.json")        // agy 的
	mkFile(t, home, "AppData/Local/agy/bin/agy.exe")         // agy 本体
	purgeClientConfig("agy")

	mustGone(t, home, ".gemini/antigravity-cli")
	mustGone(t, home, ".gemini/config")
	mustGone(t, home, "AppData/Local/agy")
	mustExist(t, home, ".gemini/settings.json")
	mustExist(t, home, ".gemini/extensions/x.json")
}
