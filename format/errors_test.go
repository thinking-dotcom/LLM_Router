package format

import (
	"io"
	"strings"
	"testing"
)

// countingReadCloser 统计实际读取的字节数,用于断言读取被限制。
type countingReadCloser struct {
	r    *strings.Reader
	read int
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += n
	return n, err
}
func (c *countingReadCloser) Close() error { return nil }

// 上游错误体读取必须受限:异常上游返回超大错误体时,不能一次性读入导致内存膨胀。
func TestUpstreamErrorToOpenaiBodyLimit(t *testing.T) {
	// 10MB 的超大错误体
	big := strings.Repeat("x", 10<<20)
	cr := &countingReadCloser{r: strings.NewReader(big)}

	status, out := UpstreamErrorToOpenai("openai", cr, 502)
	if status != 502 {
		t.Errorf("status = %d, want 502", status)
	}
	if cr.read > 64<<10 {
		t.Errorf("读取了 %d 字节,超过 64KB 上限", cr.read)
	}
	em := out["error"].(map[string]any)
	msg, _ := em["message"].(string)
	if len(msg) > 500 {
		t.Errorf("message 长度 %d > 500", len(msg))
	}
}

// 正常的小错误体原样解析出 message/type。
func TestUpstreamErrorToOpenaiParsing(t *testing.T) {
	body := io.NopCloser(strings.NewReader(`{"error":{"message":"boom","type":"rate_limit_error","code":429}}`))
	status, out := UpstreamErrorToOpenai("openai", body, 429)
	if status != 429 {
		t.Errorf("status = %d, want 429", status)
	}
	em := out["error"].(map[string]any)
	if em["message"] != "boom" || em["type"] != "rate_limit_error" {
		t.Errorf("parsed error = %v", em)
	}

	// 部分上游常见 {"error": "string"} 形式
	body2 := io.NopCloser(strings.NewReader(`{"error":"model not found"}`))
	_, out2 := UpstreamErrorToOpenai("openai", body2, 404)
	em2 := out2["error"].(map[string]any)
	if em2["message"] != "model not found" {
		t.Errorf("string error message = %v", em2["message"])
	}
}
