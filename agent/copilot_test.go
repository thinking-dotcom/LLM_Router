package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// deleteJSONObjectKey 定点删除
// ---------------------------------------------------------------------------

func TestDeleteJSONObjectKeyMiddle(t *testing.T) {
	doc := []byte("{\n  \"a\": 1,\n  \"model\": \"x\",\n  \"b\": 2\n}\n")
	out, removed, err := deleteJSONObjectKey(doc, "model")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected key removed")
	}
	s := string(out)
	// 前驱键尾逗号保留、目标项整体切除、后继键原样
	if !json.Valid(out) || s != "{\n  \"a\": 1,\n  \"b\": 2\n}\n" {
		t.Errorf("unexpected result:\n%s", s)
	}
}

func TestDeleteJSONObjectKeyLast(t *testing.T) {
	doc := []byte("{\n  \"a\": 1,\n  \"model\": \"x\"\n}\n")
	out, removed, err := deleteJSONObjectKey(doc, "model")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected key removed")
	}
	s := string(out)
	// 前驱键的尾逗号被剥掉，结果仍为合法 JSON
	if !json.Valid(out) || s != "{\n  \"a\": 1\n}\n" {
		t.Errorf("unexpected result:\n%s", s)
	}
}

func TestDeleteJSONObjectKeyFirst(t *testing.T) {
	doc := []byte("{\n  \"model\": \"x\",\n  \"a\": 1\n}\n")
	out, removed, err := deleteJSONObjectKey(doc, "model")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected key removed")
	}
	s := string(out)
	if !json.Valid(out) || s != "{\n  \"a\": 1\n}\n" {
		t.Errorf("unexpected result:\n%s", s)
	}
}

func TestDeleteJSONObjectKeyOnlyOne(t *testing.T) {
	for _, doc := range [][]byte{
		[]byte("{\n  \"model\": \"x\"\n}\n"),
		[]byte(`{"model": "x"}`),
	} {
		out, removed, err := deleteJSONObjectKey(doc, "model")
		if err != nil {
			t.Fatal(err)
		}
		if !removed {
			t.Fatal("expected key removed")
		}
		s := string(out)
		if !json.Valid(out) || s != "{}" {
			t.Errorf("expected empty object, got:\n%s", s)
		}
	}
}

func TestDeleteJSONObjectKeyMissing(t *testing.T) {
	doc := []byte("{\n  \"a\": 1\n}\n")
	out, removed, err := deleteJSONObjectKey(doc, "model")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("expected no removal for missing key")
	}
	if string(out) != string(doc) {
		t.Errorf("document changed:\n%s", out)
	}
}

func TestDeleteJSONObjectKeyNonObject(t *testing.T) {
	if _, _, err := deleteJSONObjectKey([]byte(`[1, 2]`), "model"); err == nil {
		t.Fatal("expected error for non-object top level")
	}
}

// ---------------------------------------------------------------------------
// jsoncToJSON JSONC 剥离（读取侧宽容解析）
// ---------------------------------------------------------------------------

func TestJSONCToJSONStripsCommentsAndTrailingCommas(t *testing.T) {
	doc := []byte(`{
  // 行注释
  "theme": "github", // 行尾注释
  "model": "gpt-5.2",
  "nested": { /* 块注释 */ "a": [1, 2,], },
  "str": "含 // 和 /* */ 的字符串不剥离",
}
`)
	out, err := jsoncToJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("result not valid JSON:\n%s", out)
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["model"] != "gpt-5.2" {
		t.Errorf("model not parsed: %v", obj["model"])
	}
	if obj["str"] != "含 // 和 /* */ 的字符串不剥离" {
		t.Errorf("string content damaged: %v", obj["str"])
	}
}

func TestJSONCToJSONEmpty(t *testing.T) {
	for _, doc := range [][]byte{[]byte(""), []byte("  \n"), []byte("// 只有注释\n")} {
		out, err := jsoncToJSON(doc)
		if err != nil {
			t.Fatalf("expected ok for %q: %v", doc, err)
		}
		if !json.Valid(out) {
			t.Errorf("result not valid JSON: %s", out)
		}
	}
}

func TestJSONCToJSONUnclosedString(t *testing.T) {
	if _, err := jsoncToJSON([]byte(`{"a": "unclosed}`)); err == nil {
		t.Fatal("expected error for unclosed string")
	}
}

// ---------------------------------------------------------------------------
// deleteCopilotBypassPerm 嵌套 permissions.disableBypassPermissionsMode 删除
// ---------------------------------------------------------------------------

func TestDeleteCopilotBypassPermKeepsOtherPermKeys(t *testing.T) {
	doc := []byte("{\n  \"model\": \"x\",\n  \"permissions\": {\"disableBypassPermissionsMode\": \"disable\", \"ok\": 1}\n}\n")
	out, err := deleteCopilotBypassPerm(doc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !json.Valid(out) {
		t.Fatalf("result not valid JSON:\n%s", s)
	}
	// 嵌套内移除目标键，其它嵌套成员保留，顶层 permissions 保留
	if !strings.Contains(s, `"ok": 1`) || strings.Contains(s, "disableBypassPermissionsMode") {
		t.Errorf("unexpected result:\n%s", s)
	}
}

func TestDeleteCopilotBypassPermRemovesEmptyParent(t *testing.T) {
	doc := []byte("{\n  \"model\": \"x\",\n  \"permissions\": {\"disableBypassPermissionsMode\": \"disable\"}\n}\n")
	out, err := deleteCopilotBypassPerm(doc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !json.Valid(out) {
		t.Fatalf("result not valid JSON:\n%s", s)
	}
	// permissions 仅含目标键 → 变空后整体删除顶层 permissions
	if strings.Contains(s, "permissions") || s != "{\n  \"model\": \"x\"\n}\n" {
		t.Errorf("expected empty permissions removed:\n%s", s)
	}
}

func TestDeleteCopilotBypassPermWhenMissing(t *testing.T) {
	doc := []byte("{}\n")
	out, err := deleteCopilotBypassPerm(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "{}\n" {
		t.Errorf("expected document unchanged:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// applyCopilotSandboxToDoc 沙箱嵌套读写与空对象清理
// ---------------------------------------------------------------------------

func TestApplyCopilotSandboxSet(t *testing.T) {
	doc := []byte("{\n  \"model\": \"x\"\n}\n")
	out, err := applyCopilotSandboxToDoc(doc, map[string]any{
		CopilotFieldSandboxEna:    "true",
		CopilotFieldSandboxBypass: "false",
		CopilotFieldSandboxLocal:  "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("result not valid JSON:\n%s", out)
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	sb, ok := obj["sandbox"].(map[string]any)
	if !ok {
		t.Fatalf("sandbox not present:\n%s", out)
	}
	if sb["enabled"] != true || sb["allowBypass"] != false {
		t.Errorf("unexpected sandbox booleans: %v", sb)
	}
	up, _ := sb["userPolicy"].(map[string]any)
	net, _ := up["network"].(map[string]any)
	if net["allowLocalNetwork"] != true {
		t.Errorf("unexpected allowLocalNetwork: %v", net)
	}
}

func TestApplyCopilotSandboxRemoveAll(t *testing.T) {
	doc := []byte("{\n  \"model\": \"x\",\n  \"sandbox\": {\"enabled\": true, \"userPolicy\": {\"network\": {\"allowLocalNetwork\": false}}}\n}\n")
	out, err := applyCopilotSandboxToDoc(doc, map[string]any{
		CopilotFieldSandboxEna:    "",
		CopilotFieldSandboxBypass: "",
		CopilotFieldSandboxLocal:  "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("result not valid JSON:\n%s", out)
	}
	if strings.Contains(string(out), "sandbox") {
		t.Errorf("expected empty sandbox removed:\n%s", out)
	}
}

func TestDeleteEmptyJSONKey(t *testing.T) {
	// 值为空对象 → 删除顶层键
	doc := []byte("{\n  \"permissions\": {},\n  \"a\": 1\n}\n")
	out, err := deleteEmptyJSONKey(doc, "permissions")
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) || strings.Contains(string(out), "permissions") {
		t.Errorf("expected empty key removed:\n%s", out)
	}
	// 值非空对象 → 原样保留
	doc2 := []byte("{\n  \"permissions\": {\"ok\": 1}\n}\n")
	out2, err := deleteEmptyJSONKey(doc2, "permissions")
	if err != nil {
		t.Fatal(err)
	}
	if string(out2) != string(doc2) {
		t.Errorf("expected non-empty key kept:\n%s", out2)
	}
	// 键不存在 → 原样返回
	out3, err := deleteEmptyJSONKey(doc2, "model")
	if err != nil {
		t.Fatal(err)
	}
	if string(out3) != string(doc2) {
		t.Errorf("expected unchanged when key missing:\n%s", out3)
	}
}
