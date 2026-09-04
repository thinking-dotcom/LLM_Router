package router

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"llm-router-go/types"
)

// ErrorResponse builds a minimal *http.Response with a JSON error body in
// OpenAI-compatible shape.
func ErrorResponse(status int, message, etype string) *http.Response {
	body := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    etype,
			"param":   nil,
			"code":    nil,
		},
	}
	b, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:          io.NopCloser(bytes.NewReader(b)),
		ContentLength: int64(len(b)),
	}
}

// CheckAuth validates the incoming request against the configured auth token.
// Returns true when no token is configured or when a valid credential is
// presented via any of these locations (checked in order):
//
//   - Authorization: Bearer <token>
//   - x-api-key header
//   - x-goog-api-key header
//   - ?key=<token> query parameter
func CheckAuth(r *http.Request, config types.Config) bool {
	if config.AuthToken == "" {
		return true
	}
	token := config.AuthToken
	// Authorization 的 scheme 按 HTTP 规范大小写不敏感（Bearer/bearer 均可），
	// 与核心管理接口 adminAllowed 的校验保持一致；token 比较用常量时间算法，
	// 避免逐字节短路造成的时序侧信道泄漏。
	const bearerScheme = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) > len(bearerScheme) && strings.EqualFold(auth[:len(bearerScheme)], bearerScheme) &&
		subtle.ConstantTimeCompare([]byte(auth[len(bearerScheme):]), []byte(token)) == 1 {
		return true
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("x-api-key")), []byte(token)) == 1 {
		return true
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("x-goog-api-key")), []byte(token)) == 1 {
		return true
	}
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("key")), []byte(token)) == 1 {
		return true
	}
	return false
}
