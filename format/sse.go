package format

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"strings"
)

// SSEEvent holds a parsed SSE event with its type and data payload.
type SSEEvent struct {
	Event string
	Data  map[string]any
}

// IterSSE reads an io.ReadCloser line by line and yields parsed SSE events.
// Each event is delimited by a blank line. Lines starting with "data:" are
// collected (with the "data:" prefix stripped). On a blank line the collected
// payload is JSON-unmarshalled and emitted.
func IterSSE(body io.ReadCloser) <-chan SSEEvent {
	ch := make(chan SSEEvent, 16)
	go func() {
		defer close(ch)
		defer body.Close()
		// panic 时直接返回，close(ch) 由外层 defer 执行；切勿在此 drain，否则死锁
		defer func() {
			_ = recover()
		}()
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 64*1024), 8*1024*1024) // 单行最大 8MB，覆盖大 JSON/base64 图片
		var dataLines []string
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if len(dataLines) > 0 {
					payload := strings.Join(dataLines, "\n")
					dataLines = nil
					var j map[string]any
					if err := json.Unmarshal([]byte(payload), &j); err == nil {
						event := ""
						if t, ok := j["type"].(string); ok {
							event = t
						}
						ch <- SSEEvent{Event: event, Data: j}
					} else {
						// 解析失败不能静默丢弃：那是客户端「流无声截断、无错误可查」
						// 的元凶。记日志留痕（事件本身无法安全重建，仍跳过）。
						log.Printf("[sse] upstream data 行 JSON 解析失败，事件已跳过: %v | payload: %.200s", err, payload)
					}
				}
			} else if strings.HasPrefix(line, "data:") {
				payload := strings.TrimSpace(line[5:])
				// OpenAI 兼容上游的流终止标记不是 JSON，直接跳过，
				// 避免每个成功的流式请求都刷一条「解析失败」错误日志
				if payload == "[DONE]" {
					dataLines = nil
					continue
				}
				dataLines = append(dataLines, payload)
			}
		}
		// Flush any remaining lines that were not terminated by a blank line.
		if len(dataLines) > 0 {
			payload := strings.Join(dataLines, "\n")
			var j map[string]any
			if err := json.Unmarshal([]byte(payload), &j); err == nil {
				event := ""
				if t, ok := j["type"].(string); ok {
					event = t
				}
				ch <- SSEEvent{Event: event, Data: j}
			} else {
				log.Printf("[sse] upstream 尾部 data 行 JSON 解析失败，事件已跳过: %v | payload: %.200s", err, payload)
			}
		}
		// scanner 异常（如单行超长）必须向上游报错，而不是静默截断剩余事件。
		// OpenaiStreamToChunks 会识别 error 事件并透传错误块；其它转换忽略该事件，
		// 但至少不会让流卡死在半截响应上。
		if err := scanner.Err(); err != nil {
			ch <- SSEEvent{Event: "error", Data: map[string]any{"error": map[string]any{"message": "upstream SSE read failed: " + err.Error()}}}
		}
	}()
	return ch
}
