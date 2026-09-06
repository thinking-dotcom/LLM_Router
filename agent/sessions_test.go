package agent

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

// 终端恢复命令组装：文件型按 stem 取 ID、cwd 从会话文件还原；目录型按名取 ID
func TestSessionResumePlan(t *testing.T) {
	// 文件型（Claude 风格）：Root/<分组>/<会话id>.jsonl，id=stem，目录还原自文件内 cwd
	rootFile := t.TempDir()
	cwdDir := filepath.Join(t.TempDir(), "orig-proj")
	if err := os.MkdirAll(cwdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mb, _ := json.Marshal(map[string]string{"sessionId": "ignored", "cwd": cwdDir}) // 反斜杠由 json.Marshal 转义
	writeFile(t, filepath.Join(rootFile, "proj1", "a.jsonl"), 10)
	if err := os.WriteFile(filepath.Join(rootFile, "proj1", "a.jsonl"), mb, 0o644); err != nil {
		t.Fatal(err)
	}
	withTestStore(t, "test-resume", SessionStore{Root: rootFile, Kind: "file", Pattern: "*.jsonl", Depth: 2,
		Resume: ResumeSpec{Bin: "claude", Args: []string{"claude", "--resume", "{id}"}, IDFrom: "stem", CwdFrom: "jsonlCwd"}})
	plan, err := BuildSessionResume("test-resume", filepath.Join(rootFile, "proj1", "a.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tokens) != 3 || plan.Tokens[0] != "claude" || plan.Tokens[1] != "--resume" || plan.Tokens[2] != "a" {
		t.Fatalf("tokens = %v", plan.Tokens)
	}
	if plan.Dir != cwdDir {
		t.Fatalf("dir = %q, want %q (从会话文件 cwd 还原)", plan.Dir, cwdDir)
	}

	// 目录型（grok 风格）：分组目录是 URL 编码的真实项目路径，解码后作为启动目录
	rootDir := t.TempDir()
	realGrokProj := filepath.Join(t.TempDir(), "real", "grok-proj")
	if err := os.MkdirAll(realGrokProj, 0o755); err != nil {
		t.Fatal(err)
	}
	enc := url.QueryEscape(realGrokProj)
	writeFile(t, filepath.Join(rootDir, enc, "01a00000-0000-0000-0000-000000000001", "x.jsonl"), 5)
	withTestStore(t, "test-grok", SessionStore{Root: rootDir, Kind: "dir", Depth: 2,
		Resume: ResumeSpec{Bin: "grok", Args: []string{"grok", "--resume", "{id}"}, IDFrom: "name", CwdFrom: "urlGroup"}})
	plan, err = BuildSessionResume("test-grok", filepath.Join(rootDir, enc, "01a00000-0000-0000-0000-000000000001"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tokens) != 3 || plan.Tokens[2] != "01a00000-0000-0000-0000-000000000001" {
		t.Fatalf("tokens = %v", plan.Tokens)
	}
	if plan.Dir != realGrokProj {
		t.Fatalf("dir = %q, want %q (URL 解码分组目录)", plan.Dir, realGrokProj)
	}

	// 按文件恢复（commandcode / gemini 风格）：{file} 占位替换为条目绝对路径
	withTestStore(t, "test-file2", SessionStore{Root: rootFile, Kind: "file", Pattern: "*.jsonl", Depth: 2,
		Resume: ResumeSpec{Bin: "commandcode", Args: []string{"commandcode", "--session", "{file}"}}})
	itemFile := filepath.Join(rootFile, "proj1", "a.jsonl")
	plan, err = BuildSessionResume("test-file2", itemFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tokens) != 3 || plan.Tokens[2] != itemFile {
		t.Fatalf("tokens = %v, want file placeholder replaced", plan.Tokens)
	}
}

// 不支持按会话恢复的客户端（Copilot）→ 报出原因
func TestSessionResumeUnsupported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "chat-uuid-1", "x.json"), 1)
	withTestStore(t, "test-copilot", SessionStore{Root: root, Kind: "dir", Depth: 1,
		Resume: ResumeSpec{Unsupported: "Copilot CLI 仅支持 --continue"}})
	_, err := BuildSessionResume("test-copilot", filepath.Join(root, "chat-uuid-1"))
	if err == nil || !strings.Contains(err.Error(), "--continue") {
		t.Fatalf("want unsupported error, got %v", err)
	}
	// 概览同样携带不支持标记
	clients := ListSessionClients()
	for _, c := range clients {
		if c.ID == "test-copilot" && (c.ResumeSupported || c.ResumeNote == "") {
			t.Fatal("copilot-style client should be marked unsupported with note")
		}
	}
}

// codex 风格：读 rollout 内 session_id（缺省回退文件名 stem）
func TestSessionResumeIDFromJSON(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "2026", "09", "07", "rollout-20260907-120000-x.jsonl")
	mb, _ := json.Marshal(map[string]string{"session_id": "uuid-abc-123", "cwd": filepath.Join(t.TempDir(), "not-exist-dir")}) // cwd 不存在 → 回退主目录
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, mb, 0o644); err != nil {
		t.Fatal(err)
	}
	withTestStore(t, "test-codex", SessionStore{Root: root, Kind: "file", Pattern: "*.jsonl", Depth: 4,
		Resume: ResumeSpec{Bin: "codex", Args: []string{"codex", "resume", "{id}"}, IDFrom: "jsonlSessionId", CwdFrom: "jsonlCwd"}})
	plan, err := BuildSessionResume("test-codex", f)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tokens[2] != "uuid-abc-123" {
		t.Fatalf("id = %q, want uuid-abc-123 (来自文件内 session_id)", plan.Tokens[2])
	}
	home, _ := os.UserHomeDir()
	if plan.Dir != home {
		t.Fatalf("dir = %q, want home fallback %q", plan.Dir, home)
	}
}

// 越界 / 分组层 / 未登记客户端 → 拒绝
func TestSessionResumeRejects(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proj1", "a.jsonl"), 10)
	withTestStore(t, "test-rej", SessionStore{Root: root, Kind: "file", Pattern: "*.jsonl", Depth: 2,
		Resume: ResumeSpec{Bin: "claude", Args: []string{"claude", "--resume", "{id}"}, IDFrom: "stem"}})
	if _, err := BuildSessionResume("test-rej", filepath.Join(t.TempDir(), "evil.jsonl")); err == nil {
		t.Fatal("outside path should be rejected")
	}
	if _, err := BuildSessionResume("test-rej", filepath.Join(root, "proj1")); err == nil {
		t.Fatal("group layer should be rejected")
	}
	if _, err := BuildSessionResume("no-such-client", filepath.Join(root, "proj1", "a.jsonl")); err == nil {
		t.Fatal("unknown client should be rejected")
	}
}

// 展示用会话 ID：口径与恢复一致（stem/name/trimSession/jsonlSessionId），
// 未登记 ID 提取规则（按文件恢复或不可恢复）时兜底去扩展名/目录名。
func TestSessionDisplayID(t *testing.T) {
	st := func(idFrom string) SessionStore {
		return SessionStore{Kind: "file", Resume: ResumeSpec{IDFrom: idFrom}}
	}
	cases := []struct {
		name string
		st   SessionStore
		it   SessionItem
		want string
	}{
		{"stem 去扩展名", st("stem"), SessionItem{Name: "01a07242-f649-7162-8523-28f7093ebab4.jsonl"}, "01a07242-f649-7162-8523-28f7093ebab4"},
		{"name 即 ID（目录型）", st("name"), SessionItem{Name: "01a06862-6071-7681-8704-edae8cdfe4b2", IsDir: true}, "01a06862-6071-7681-8704-edae8cdfe4b2"},
		{"trimSession 去前缀", st("trimSession"), SessionItem{Name: "session_143c66bf-ca43-46f3-bbe0-a8e4edc56602", IsDir: true}, "143c66bf-ca43-46f3-bbe0-a8e4edc56602"},
		{"jsonlSessionId 读文件内字段", st("jsonlSessionId"), SessionItem{Name: "rollout-x.jsonl"}, "uuid-from-json"},
		{"未登记规则兜底去扩展名", st(""), SessionItem{Name: "2026-08-21T10-59-13-506Z_01a023f9.jsonl"}, "2026-08-21T10-59-13-506Z_01a023f9"},
		{"未登记规则兜底目录名", st(""), SessionItem{Name: "6cf6381a-9b00-443a-8a00-a6b360c957b3", IsDir: true}, "6cf6381a-9b00-443a-8a00-a6b360c957b3"},
	}
	// 预建临时 jsonl 文件供 jsonlSessionId 用例读取
	dir := t.TempDir()
	mb, _ := json.Marshal(map[string]string{"session_id": "uuid-from-json"})
	jf := filepath.Join(dir, "rollout-x.jsonl")
	if err := os.WriteFile(jf, mb, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		it := c.it
		it.Path = jf // 覆盖 JSON 读取路径（其余分支不读文件，无害）
		got := sessionDisplayID(c.st, it)
		if got != c.want {
			t.Fatalf("%s: id = %q, want %q", c.name, got, c.want)
		}
	}
}

// 展示用恢复命令行：模板 {id}/{file} 替换、含空白 token 加引号；不支持时为空
func TestSessionResumeCmd(t *testing.T) {
	// 文件型 Claude 风格：id=stem → claude --resume <uuid>
	fileStore := SessionStore{Kind: "file", Resume: ResumeSpec{Bin: "claude", Args: []string{"claude", "--resume", "{id}"}, IDFrom: "stem"}}
	it := SessionItem{Name: "01a07242-f649-7162-8523-28f7093ebab4.jsonl", Path: `C:/Users/Me/Desktop/x/01a07242-f649-7162-8523-28f7093ebab4.jsonl`}
	it.ID = sessionDisplayID(fileStore, it)
	if got := sessionResumeCmd(fileStore, it); got != "claude --resume 01a07242-f649-7162-8523-28f7093ebab4" {
		t.Fatalf("cmd = %q", got)
	}
	// 按文件恢复 + 含空格路径 → {file} 替换并加引号
	gemStore := SessionStore{Kind: "file", Resume: ResumeSpec{Bin: "gemini", Args: []string{"gemini", "--session-file", "{file}"}}}
	spaceDir := filepath.Join(t.TempDir(), "My Docs") // 含空格的目录
	gIt := SessionItem{Name: "chat-a.jsonl", Path: filepath.Join(spaceDir, "chat-a.jsonl"), ID: "chat-a"}
	if got := sessionResumeCmd(gemStore, gIt); got != `gemini --session-file "`+gIt.Path+`"` {
		t.Fatalf("cmd = %q", got)
	}
	// 不支持恢复（Bin 空）→ 空
	unsup := SessionStore{Kind: "dir", Resume: ResumeSpec{Unsupported: "n/a"}}
	if got := sessionResumeCmd(unsup, SessionItem{Name: "chat-uuid"}); got != "" {
		t.Fatalf("unsupported cmd = %q, want empty", got)
	}
	// joinCmdline 引号转义
	if got := joinCmdline([]string{"a", `say "hi" there`, "b"}); got != `a "say \"hi\" there" b` {
		t.Fatalf("join = %q", got)
	}
}
