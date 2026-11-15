package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// upsertJSONObjectKey 定点替换
// ---------------------------------------------------------------------------

func TestUpsertJSONObjectKeyReplace(t *testing.T) {
	doc := []byte("{\n  \"temperature\": 1.0,\n  \"mcpServers\": {\"old\": {\"type\": \"sse\", \"url\": \"http://old\"}},\n  \"nested\": {\"a\": [1, 2], \"b\": true},\n  \"z\": \"tail\"\n}\n")
	out, err := upsertJSONObjectKey(doc, "mcpServers", func(existing json.RawMessage) (json.RawMessage, error) {
		if !strings.Contains(string(existing), "old") {
			t.Errorf("existing raw not passed: %s", existing)
		}
		return json.Marshal(map[string]any{"new": map[string]any{"type": "stdio", "command": "npx"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// 其余键原样：数字格式 1.0、键序、缩进、行尾均不被重排
	if !strings.Contains(s, `"temperature": 1.0`) {
		t.Errorf("temperature float format not preserved:\n%s", s)
	}
	if !strings.Contains(s, `"nested": {"a": [1, 2], "b": true}`) {
		t.Errorf("nested key format not preserved:\n%s", s)
	}
	if !strings.Contains(s, `"z": "tail"`) {
		t.Errorf("tail key lost:\n%s", s)
	}
	// 新值按文件外层缩进对齐（2 空格）
	if !strings.Contains(s, "  \"mcpServers\": {\n    \"new\"") {
		t.Errorf("injected value not indented to file style:\n%s", s)
	}
	// 结果必须是合法 JSON
	if !json.Valid(out) {
		t.Errorf("result not valid JSON:\n%s", s)
	}
}

func TestUpsertJSONObjectKeyInsert(t *testing.T) {
	doc := []byte("{\n  \"a\": 1\n}\n")
	out, err := upsertJSONObjectKey(doc, "mcpServers", func(existing json.RawMessage) (json.RawMessage, error) {
		if len(existing) != 0 {
			t.Errorf("expected nil existing on insert, got %s", existing)
		}
		return json.Marshal(map[string]any{"x": map[string]any{"type": "sse", "url": "u"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"a": 1`) {
		t.Errorf("existing key lost:\n%s", s)
	}
	if !strings.Contains(s, `"mcpServers"`) {
		t.Errorf("mcpServers not inserted:\n%s", s)
	}
	if !json.Valid(out) {
		t.Errorf("result not valid JSON:\n%s", s)
	}
}

func TestUpsertJSONObjectKeyEmptyObject(t *testing.T) {
	out, err := upsertJSONObjectKey([]byte("{}\n"), "mcpServers", func(json.RawMessage) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"y": 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Errorf("result not valid JSON:\n%s", out)
	}
	if !strings.Contains(string(out), `"mcpServers"`) {
		t.Errorf("mcpServers not inserted:\n%s", out)
	}
}

func TestUpsertJSONObjectKeyNotObject(t *testing.T) {
	if _, err := upsertJSONObjectKey([]byte(`[1,2]`), "mcpServers", func(json.RawMessage) (json.RawMessage, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("expected error for non-object top level")
	}
}

func TestUpsertJSONObjectKeyInvalidJSON(t *testing.T) {
	if _, err := upsertJSONObjectKey([]byte(`{"a": 1,}`), "mcpServers", func(json.RawMessage) (json.RawMessage, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("expected error for invalid JSON (trailing comma)")
	}
}

// ---------------------------------------------------------------------------
// mergeMcpServers：格式保留 + 合并语义 + 首次备份
// ---------------------------------------------------------------------------

func TestMergeMcpServersPreservesFormatAndBackup(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "settings.json")
	original := "{\n  \"temperature\": 1.0,\n  \"model\": \"claude\",\n  \"mcpServers\": {\"keep\": {\"type\": \"sse\", \"url\": \"http://old\"}}\n}\n"
	if err := os.WriteFile(fp, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mergeMcpServers(fp, []MCPServer{{Name: "fs", Type: "stdio", Command: "npx"}}, ""); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(fp)
	s := string(b)
	// 替换：旧 server 清掉，仅剩本次注入的 server
	if strings.Contains(s, `"keep"`) {
		t.Errorf("old server should be replaced:\n%s", s)
	}
	if !strings.Contains(s, `"command": "npx"`) {
		t.Errorf("new server missing:\n%s", s)
	}
	// 其余键格式原样
	if !strings.Contains(s, `"temperature": 1.0`) {
		t.Errorf("float format not preserved:\n%s", s)
	}
	if !json.Valid(b) {
		t.Errorf("result not valid JSON:\n%s", s)
	}
	// 首次备份 = 原内容
	bb, err := os.ReadFile(fp + ".bak")
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(bb) != original {
		t.Errorf("backup mismatch:\n got: %q\nwant: %q", bb, original)
	}
}

func TestMergeMcpServersArrayExistingDropped(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "s.json")
	if err := os.WriteFile(fp, []byte(`{"mcpServers":["not-an-object"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mergeMcpServers(fp, []MCPServer{{Name: "fs", Type: "stdio", Command: "npx"}}, ""); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(fp)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	obj, ok := doc["mcpServers"].(map[string]any)
	if !ok || obj["fs"] == nil {
		t.Fatalf("mcpServers not rebuilt as object: %s", b)
	}
}
