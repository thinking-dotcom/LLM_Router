package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPurgeKeepsAppData 卸载客户端（含 purge）不得删掉本程序的数据目录，
// 也不得通过删除其祖先目录把它连坐掉。
func TestPurgeKeepsAppData(t *testing.T) {
	dataDir := DataDir()
	if dataDir == "" {
		t.Skip("no data dir")
	}
	skill := filepath.Join(dataDir, "skills", "probe-keep", "SKILL.md")
	mcpFile := filepath.Join(dataDir, "mcp", "probe-keep.json")
	for _, p := range []string{skill, mcpFile} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("probe"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(dataDir, "skills", "probe-keep"))
		_ = os.Remove(mcpFile)
	})

	const id = "probe-keep-data"
	// ① 残留清单直接登记数据目录 ② 登记其父目录（删父目录=连坐）
	for _, target := range [][]string{{dataDir}, {filepath.Dir(dataDir)}} {
		clientResidue[id] = clientResidueInfo{Paths: target}
		removed := purgeClientConfig(id)
		for _, r := range removed {
			if isAppProtectedPath(r, []string{dataDir}) {
				t.Fatalf("purge 删掉了受保护路径: %s", r)
			}
		}
		for _, p := range []string{skill, mcpFile} {
			if _, err := os.Stat(p); err != nil {
				t.Fatalf("数据目录被清理: %s (%v)", p, err)
			}
		}
	}
	delete(clientResidue, id)
}

// TestIsAppProtectedPath 保护判定：等于 / 内部 / 祖先都算保护。
func TestIsAppProtectedPath(t *testing.T) {
	sep := string(os.PathSeparator)
	data := filepath.Join("C:", "app", "data")
	protected := []string{data}
	cases := map[string]bool{
		data:                                 true,
		filepath.Join(data, "skills"):        true,
		filepath.Join("C:", "app"):           true,
		filepath.Join("C:", "appdata"):       false, // 前缀相近但不同层级
		filepath.Join("C:", "other", "data"): false,
		"":                                   false,
	}
	for p, want := range cases {
		if got := isAppProtectedPath(p, protected); got != want {
			t.Fatalf("isAppProtectedPath(%q) = %v, want %v (sep=%s)", p, got, want, sep)
		}
	}
}
