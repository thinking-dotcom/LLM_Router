package clients

import (
	"encoding/json"
	"strings"
	"testing"
)

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
// applyCopilotNestedBoolToDoc 沙箱/IDE 嵌套读写与空对象清理
// ---------------------------------------------------------------------------

func TestApplyCopilotSandboxSet(t *testing.T) {
	doc := []byte("{\n  \"model\": \"x\"\n}\n")
	out, err := applyCopilotNestedBoolToDoc(doc, CopilotKeySandbox, copilotSandboxFields, map[string]any{
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
	out, err := applyCopilotNestedBoolToDoc(doc, CopilotKeySandbox, copilotSandboxFields, map[string]any{
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
