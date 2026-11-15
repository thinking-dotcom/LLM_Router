package core

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestFile 在 dir/rel 写文件（自动建父目录）。
func writeTestFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestMigrateLegacyData 旧版 exe 同目录 data 首次启动搬到新目录，且只搬一次。
func TestMigrateLegacyData(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "bin", "data")
	target := filepath.Join(root, "appdata", "LLMRouter", "data")
	writeTestFile(t, legacy, "skills/s1/SKILL.md", "# s1")
	writeTestFile(t, legacy, "mcp/one.json", `{"name":"one"}`)

	if err := migrateLegacyData(legacy, target); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, rel := range []string{"skills/s1/SKILL.md", "mcp/one.json"} {
		b, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s 未迁移: %v", rel, err)
		}
		if len(b) == 0 {
			t.Fatalf("%s 内容为空", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(target, dataDirMarker)); err != nil {
		t.Fatalf("缺迁移标记: %v", err)
	}
	// 标记存在后再次调用：不报错、不改动目标内容
	writeTestFile(t, target, "skills/keep.md", "keep")
	if err := migrateLegacyData(legacy, target); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "keep.md")); err != nil {
		t.Fatal("重复迁移覆盖了新目录内容")
	}
	if _, err := os.Stat(filepath.Join(target, ".migrating")); err == nil {
		t.Fatal("残留临时目录")
	}
}

// TestMigrateSkipsWhenTargetHasData 新目录已有数据时不搬（以新目录为准）。
func TestMigrateSkipsWhenTargetHasData(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "old")
	target := filepath.Join(root, "new")
	writeTestFile(t, legacy, "skills/old.md", "old")
	writeTestFile(t, target, "skills/new.md", "new")

	if err := migrateLegacyData(legacy, target); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "old.md")); err == nil {
		t.Fatal("不该把旧文件合并进已有数据目录")
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "new.md")); err != nil {
		t.Fatal("新目录原有内容被破坏")
	}
}

// TestMigrateNoLegacy 旧目录不存在（全新安装）时不做任何事。
func TestMigrateNoLegacy(t *testing.T) {
	root := t.TempDir()
	if err := migrateLegacyData(filepath.Join(root, "nope"), filepath.Join(root, "new")); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new")); err == nil {
		t.Fatal("无旧数据时不该创建新目录")
	}
}

// TestUserDataDir 新目录位于用户配置目录下的 LLMRouter/data。
func TestUserDataDir(t *testing.T) {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		t.Skip("无用户配置目录")
	}
	want := filepath.Join(base, "LLMRouter", "data")
	if got := userDataDir(); got != want {
		t.Fatalf("userDataDir = %q, want %q", got, want)
	}
}
