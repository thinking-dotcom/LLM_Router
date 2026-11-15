package core

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

// 键名含转义字符时，键起点必须按字面长度回退（此前用解码后长度 → 区间算错、
// 会误伤相邻键）。此处构造 "a\"b" 作为被删键：解码后长度 4，字面长度 5。
func TestDeleteJSONObjectKeyEscapedName(t *testing.T) {
	doc := []byte("{\n  \"keep1\": 1,\n  \"a\\\"b\": \"x\",\n  \"b\": 2\n}\n")
	out, removed, err := deleteJSONObjectKey(doc, `a"b`)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected escaped-name key removed")
	}
	s := string(out)
	if !json.Valid(out) {
		t.Fatalf("result not valid JSON:\n%s", s)
	}
	// 相邻键与格式必须完好
	for _, want := range []string{`"keep1": 1`, `"b": 2`} {
		if !strings.Contains(s, want) {
			t.Errorf("neighbor key damaged, missing %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"a\"b"`) || strings.Contains(s, `a\"b`) {
		t.Errorf("target key not removed:\n%s", s)
	}
}

// jsonStringLiteralLen：正确定位含转义的字符串字面量长度。位置取自真实的
// json.Decoder.InputOffset()，与 deleteJSONObjectKey 的调用方式一致。
func TestJSONStringLiteralLen(t *testing.T) {
	cases := []struct {
		doc  string
		want int // 引号内字面字节长度
	}{
		{`{"model": 1}`, 5},    // "model"
		{`{"a\"b": 1}`, 4},     // "a\"b" → 字面 a\"b（4 字节；解码后 a"b 仅 3 字节）
		{`{"a\\b": 1}`, 4},     // "a\\b" → 字面 a\\b（4 字节；解码后 a\b 仅 3 字节）
		{`{"": 1}`, 0},         // 空键
		{`{"a\u0041b": 1}`, 8}, // "a\u0041b" 字面 8 字节，解码后 3 字节
	}
	for _, c := range cases {
		dec := json.NewDecoder(strings.NewReader(c.doc))
		if _, err := dec.Token(); err != nil { // 消费 '{'
			t.Fatalf("%s: %v", c.doc, err)
		}
		if _, err := dec.Token(); err != nil { // 消费键 token
			t.Fatalf("%s: %v", c.doc, err)
		}
		got, err := jsonStringLiteralLen([]byte(c.doc), int(dec.InputOffset()))
		if err != nil {
			t.Fatalf("%s: %v", c.doc, err)
		}
		if got != c.want {
			t.Errorf("%s: jsonStringLiteralLen = %d, want %d", c.doc, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// JSONC 输入先剥注释再定点写入（带注释文件不再被拒写）
// ---------------------------------------------------------------------------

// 注释剥离后应能定点写入，且只动目标键、其余键（含转义键名）与格式不变。
func TestSaveSettingsAcceptsJSONCInput(t *testing.T) {
	in := []byte("{\n  // 用户手写注释\n  \"theme\": \"github\",\n  \"model\": \"old\"\n}\n")
	stripped, err := jsoncToJSON(in)
	if err != nil {
		t.Fatalf("jsoncToJSON: %v", err)
	}
	if !json.Valid(stripped) {
		t.Fatal("stripped doc should be valid JSON")
	}
	out, err := upsertJSONObjectKey(stripped, "model", func(json.RawMessage) (json.RawMessage, error) {
		return json.Marshal("new")
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"model": "new"`) {
		t.Errorf("target key not updated:\n%s", s)
	}
	if !strings.Contains(s, `"theme": "github"`) {
		t.Errorf("other key lost:\n%s", s)
	}
	if !json.Valid(out) {
		t.Errorf("result not valid JSON:\n%s", s)
	}
}

// 语法损坏（剥离注释后仍非法）必须仍被拒绝，避免整体重排用户文件。
func TestJSONCInputStillRejectedWhenBroken(t *testing.T) {
	broken := []byte("{\n  \"model\": \"x\",\n  // 注释\n  \"tail\": \n}\n")
	stripped, err := jsoncToJSON(broken)
	if err == nil && json.Valid(stripped) {
		t.Fatal("broken doc should not become valid after comment stripping")
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
