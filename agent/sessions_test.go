package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// withTestStore 临时替换会话注册表中一个测试客户端条目，测试结束恢复。
func withTestStore(t *testing.T, id string, st SessionStore) {
	t.Helper()
	old, had := sessionStores[id]
	sessionStores[id] = st
	t.Cleanup(func() {
		if had {
			sessionStores[id] = old
		} else {
			delete(sessionStores, id)
		}
	})
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, size)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// 文件型会话（Claude Code / pi / CodeBuddy 布局）：Root/<分组>/<会话>.jsonl
func TestSessionItemsFileKind(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proj1", "a.jsonl"), 10)
	writeFile(t, filepath.Join(root, "proj1", "b.jsonl"), 20)
	writeFile(t, filepath.Join(root, "proj2", "c.jsonl"), 30)
	writeFile(t, filepath.Join(root, "proj1", "notes.txt"), 99)      // 扩展名不符 → 忽略
	writeFile(t, filepath.Join(root, "proj2", "d", "deep.jsonl"), 5) // 层级过深 → 忽略
	withTestStore(t, "test-file", SessionStore{Root: root, Kind: "file", Pattern: "*.jsonl", Depth: 2})

	items, err := ListSessionItems("test-file")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	total := int64(0)
	for _, it := range items {
		total += it.Size
		if it.IsDir || it.Files != 1 {
			t.Fatalf("file item should be file: %+v", it)
		}
		if it.Group != "proj1" && it.Group != "proj2" {
			t.Fatalf("unexpected group: %q", it.Group)
		}
	}
	if total != 60 {
		t.Fatalf("total size = %d, want 60", total)
	}

	// 删除一个会话文件；分组目录（层级 1）必须被拒绝
	target := filepath.Join(root, "proj1", "a.jsonl")
	if err := DeleteSessionItems("test-file", []string{target}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
	if err := DeleteSessionItems("test-file", []string{filepath.Join(root, "proj1")}); err == nil {
		t.Fatal("deleting group dir should be rejected")
	}
	// 重复删除幂等
	if err := DeleteSessionItems("test-file", []string{target}); err != nil {
		t.Fatalf("idempotent delete failed: %v", err)
	}
}

// 目录型会话（Kimi / Grok / Cline 布局）：Root/<分组>/<会话目录>/
func TestSessionItemsDirKind(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "wd1", "s1", "x.jsonl"), 10)
	writeFile(t, filepath.Join(root, "wd1", "s1", "y.jsonl"), 15)
	writeFile(t, filepath.Join(root, "wd1", "s2", "x.jsonl"), 20)
	writeFile(t, filepath.Join(root, "wd2", "s3", "x.jsonl"), 30)
	writeFile(t, filepath.Join(root, "wd1", "prompt_history.jsonl"), 99) // 文件 → 忽略
	withTestStore(t, "test-dir", SessionStore{Root: root, Kind: "dir", Depth: 2})

	items, err := ListSessionItems("test-dir")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	for _, it := range items {
		if !it.IsDir {
			t.Fatalf("dir item should be dir: %+v", it)
		}
		if it.Group != "wd1" && it.Group != "wd2" {
			t.Fatalf("unexpected group: %q", it.Group)
		}
	}
	// s1 目录体积 = 10 + 15、文件数 2
	var s1 SessionItem
	for _, it := range items {
		if it.Name == "s1" {
			s1 = it
		}
	}
	if s1.Size != 25 || s1.Files != 2 {
		t.Fatalf("s1 size/files = %d/%d, want 25/2", s1.Size, s1.Files)
	}

	// 删除目录型会话：整棵移除；同级其它会话不受影响
	target := filepath.Join(root, "wd1", "s1")
	if err := DeleteSessionItems("test-dir", []string{target}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("dir should be deleted")
	}
	if _, err := os.Stat(filepath.Join(root, "wd1", "s2", "x.jsonl")); err != nil {
		t.Fatal("sibling session should survive")
	}
	// 文件条目按目录口径删除 → 拒绝
	if err := DeleteSessionItems("test-dir", []string{filepath.Join(root, "wd1", "prompt_history.jsonl")}); err == nil {
		t.Fatal("deleting file under dir-kind store should be rejected")
	}
}

// 根目录不存在 → 空列表不报错；未注册客户端 → 报错
func TestSessionItemsMissingRootAndUnknownClient(t *testing.T) {
	withTestStore(t, "test-missing", SessionStore{Root: filepath.Join(t.TempDir(), "nope"), Kind: "file", Pattern: "*.jsonl", Depth: 2})
	items, err := ListSessionItems("test-missing")
	if err != nil || len(items) != 0 {
		t.Fatalf("missing root should yield empty list, got items=%d err=%v", len(items), err)
	}
	if err := DeleteSessionItems("no-such-client", nil); err == nil {
		t.Fatal("unknown client should be rejected")
	}
}

// 根目录外的路径必须被拒绝删除（越界防护）
func TestSessionDeleteOutsideRoot(t *testing.T) {
	root := t.TempDir()
	withTestStore(t, "test-out", SessionStore{Root: root, Kind: "file", Pattern: "*.jsonl", Depth: 2})
	outside := filepath.Join(t.TempDir(), "evil.jsonl")
	writeFile(t, outside, 1)
	if err := DeleteSessionItems("test-out", []string{outside}); err == nil {
		t.Fatal("outside path should be rejected")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("outside file must survive")
	}
	// 根目录本身也必须被拒绝
	if err := DeleteSessionItems("test-out", []string{root}); err == nil {
		t.Fatal("root itself should be rejected")
	}
}
