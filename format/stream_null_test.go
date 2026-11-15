package format

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// 回归：上游 SSE 里的 `data: null` 解出来是 nil map（Unmarshal 成功但无内容），
// 三个 writer 之前都会对它做 map 写入（chunk["model"] = model），直接 panic。
func TestWritersSkipNilChunk(t *testing.T) {
	mk := func() <-chan map[string]any {
		ch := make(chan map[string]any, 3)
		ch <- nil // data: null
		ch <- map[string]any{
			"id":      "chatcmpl-1",
			"object":  "chat.completion.chunk",
			"model":   "m1",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "hi"}}},
		}
		close(ch)
		return ch
	}

	t.Run("openai", func(t *testing.T) {
		var buf bytes.Buffer
		if _, err := WriteChunksToOpenai(&buf, mk(), "m1"); err != nil {
			t.Fatalf("WriteChunksToOpenai: %v", err)
		}
		if !strings.Contains(buf.String(), "chatcmpl-1") {
			t.Fatalf("正常 chunk 应被写出:\n%s", buf.String())
		}
	})

	t.Run("anthropic", func(t *testing.T) {
		var buf bytes.Buffer
		if _, err := WriteChunksToAnthropic(&buf, mk(), "m1"); err != nil {
			t.Fatalf("WriteChunksToAnthropic: %v", err)
		}
		if buf.Len() == 0 {
			t.Fatal("应写出至少一条事件")
		}
	})

	t.Run("gemini", func(t *testing.T) {
		var buf bytes.Buffer
		if _, err := WriteChunksToGemini(&buf, mk(), "m1"); err != nil {
			t.Fatalf("WriteChunksToGemini: %v", err)
		}
	})
}

// 回归：格式互译产生的增量 chunk 只带 choices（无 id/object/created），原样写出会让
// 严格反序列化的 OpenAI 客户端（grokcli 等 serde 实现）报 missing field `id`。
func TestWriteChunksToOpenaiFillsRequiredFields(t *testing.T) {
	ch := make(chan map[string]any, 3)
	ch <- map[string]any{
		"object": "chat.completion.chunk", "model": "m1",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}}},
	}
	ch <- map[string]any{
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "hi"}}},
	}
	ch <- map[string]any{"error": map[string]any{"message": "boom"}}
	close(ch)

	var buf bytes.Buffer
	if _, err := WriteChunksToOpenai(&buf, ch, "m1"); err != nil {
		t.Fatalf("WriteChunksToOpenai: %v", err)
	}

	var ids []string
	for _, line := range strings.Split(buf.String(), "\n") {
		data, ok := strings.CutPrefix(strings.TrimSpace(line), "data: ")
		if !ok || data == "[DONE]" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			t.Fatalf("写出非法 JSON: %v (%s)", err, data)
		}
		if _, ok := m["error"]; ok {
			continue // 错误块保持原样
		}
		id, _ := m["id"].(string)
		if id == "" || m["object"] != "chat.completion.chunk" || m["created"] == nil {
			t.Fatalf("chunk 缺少必需字段: %s", data)
		}
		ids = append(ids, id)
	}
	if len(ids) != 2 {
		t.Fatalf("应写出 2 条 chunk，实得 %d: %s", len(ids), buf.String())
	}
	if ids[0] != ids[1] {
		t.Fatalf("同一响应的分片 id 必须一致: %v", ids)
	}
}
