package core

import (
	"os"
	"path/filepath"
	"testing"
)

// 一键注入/踢出：整体复制到注册客户端的技能目录、按清单移除副本，
// 客户端自有的无关文件保留；同一目录登记多个客户端只处理一次。
func TestBulkSyncSkills(t *testing.T) {
	SkillsDir() // 固定 data 目录（sync.Once），SaveSkill/DeleteSkill 与 ListSkills 同源
	for _, name := range []string{"bulk-skill-a", "bulk-skill-b"} {
		if err := SaveSkill(name, "---\ndescription: bulk test\n---\n# "+name); err != "" {
			t.Fatalf("SaveSkill %s: %s", name, err)
		}
	}
	defer func() {
		_ = DeleteSkill("bulk-skill-a")
		_ = DeleteSkill("bulk-skill-b")
	}()

	dirA := filepath.Join(t.TempDir(), "skills-a")
	dirB := filepath.Join(t.TempDir(), "skills-b")
	RegisterTarget(&dirTarget{id: "bulk-x", name: "BulkX", skillsDir: dirA})
	RegisterTarget(&dirTarget{id: "bulk-y", name: "BulkY", skillsDir: dirA}) // 与 X 同目录：应去重
	RegisterTarget(&dirTarget{id: "bulk-z", name: "BulkZ", skillsDir: dirB})
	defer func() {
		UnregisterTarget("bulk-x")
		UnregisterTarget("bulk-y")
		UnregisterTarget("bulk-z")
	}()

	// 注入：每个目录里出现全部技能副本（数量对齐 ListSkills，测试间共享 data 目录）+ 注入清单
	wantCount := len(ListSkills())
	res := BulkSyncSkills(true)
	byDir := map[string]SkillSyncResult{}
	for _, r := range res {
		if r.ID == "bulk-x" || r.ID == "bulk-y" || r.ID == "bulk-z" {
			byDir[r.ID] = r
		}
	}
	if len(byDir) != 2 {
		t.Fatalf("expect 2 results (same-dir dedup), got %d: %+v", len(byDir), res)
	}
	_, hasX := byDir["bulk-x"]
	_, hasY := byDir["bulk-y"]
	if hasX == hasY {
		t.Fatal("exactly one of same-dir clients should be processed per dir")
	}
	for _, id := range []string{"bulk-x", "bulk-y", "bulk-z"} {
		if r, ok := byDir[id]; ok && (!r.OK || r.Count != wantCount) {
			t.Fatalf("%s inject result: %+v (want count %d)", id, r, wantCount)
		}
	}
	for _, dir := range []string{dirA, dirB} {
		for _, name := range []string{"bulk-skill-a", "bulk-skill-b"} {
			if _, err := os.Stat(filepath.Join(dir, name, skillFileName)); err != nil {
				t.Fatalf("%s: skill %s not copied: %v", dir, name, err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, skillsInjectedMarker)); err != nil {
			t.Fatalf("%s: marker missing: %v", dir, err)
		}
	}

	// 客户端自有文件：注入/踢出都不得触碰
	own := filepath.Join(dirA, "user-own-skill")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, skillFileName), []byte("own"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 踢出：副本与清单被移除，自有文件保留
	res = BulkSyncSkills(false)
	for _, r := range res {
		if r.ID == "bulk-x" && !r.OK {
			t.Fatalf("bulk-x undo: %+v", r)
		}
	}
	for _, dir := range []string{dirA, dirB} {
		for _, name := range []string{"bulk-skill-a", "bulk-skill-b"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatalf("%s: copy %s still present", dir, name)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, skillsInjectedMarker)); !os.IsNotExist(err) {
			t.Fatalf("%s: marker still present", dir)
		}
	}
	if _, err := os.Stat(filepath.Join(own, skillFileName)); err != nil {
		t.Fatal("client's own skill was removed")
	}
}
