package format

import (
	"encoding/json"
	"io"
)

// UpstreamErrorToOpenai reads an upstream error response body and converts it
// into an OpenAI-compatible error shape. It returns the HTTP status code and a
// map suitable for JSON serialization.
func UpstreamErrorToOpenai(ptype string, body io.ReadCloser, statusCode int) (int, map[string]any) {
	// 错误体通常只有几百字节；限制读取上限，避免异常上游返回超大错误体时
	// 一次性读入导致内存膨胀（body 大小不可信）。
	data, _ := io.ReadAll(io.LimitReader(body, 64<<10))
	text := string(data)
	var j map[string]any
	json.Unmarshal(data, &j)

	message := text
	if len(message) > 500 {
		// 按 rune 截断，避免切断多字节字符产生乱码
		runes := []rune(message)
		if len(runes) > 500 {
			message = string(runes[:500])
		}
	}
	typeName := "upstream_error"
	var code any

	if j != nil {
		switch e := j["error"].(type) {
		case map[string]any:
			if ptype == "anthropic" {
				if m, ok := e["message"].(string); ok {
					message = m
				}
				if t, ok := e["type"].(string); ok {
					typeName = t
				}
			} else {
				if m, ok := e["message"].(string); ok {
					message = m
				}
				if t, ok := e["type"].(string); ok {
					typeName = t
				} else if s, ok := e["status"].(string); ok {
					typeName = s
				}
				code = e["code"]
			}
		case string:
			// 部分上游常返回 {"error": "..."} 的纯字符串错误
			if e != "" {
				message = e
				typeName = "upstream_error"
			}
		}
	}

	return statusCode, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    typeName,
			"param":   nil,
			"code":    code,
		},
	}
}

// UpstreamErrorForClient 按客户端接入格式转换上游错误体。跨型路径此前一律
// 返回 OpenAI 形错误，anthropic 客户端（期望 {"type":"error",error:{...}}）与
// gemini 客户端（期望 error.code/status 字段）的严格 SDK 会反序列化失败，
// 用户看到的是客户端内部错误而非真实上游错误。
func UpstreamErrorForClient(inboundFormat, ptype string, body io.ReadCloser, statusCode int) (int, map[string]any) {
	status, oe := UpstreamErrorToOpenai(ptype, body, statusCode)
	em, _ := oe["error"].(map[string]any)
	if em == nil {
		return status, oe
	}
	msg, _ := em["message"].(string)
	typ, _ := em["type"].(string)
	switch inboundFormat {
	case "anthropic":
		return status, map[string]any{
			"type":  "error",
			"error": map[string]any{"type": typ, "message": msg},
		}
	case "gemini":
		return status, map[string]any{
			"error": map[string]any{
				"code":    status,
				"message": msg,
				"status":  typ,
			},
		}
	default: // openai / responses：OpenAI 形错误即为客户端原生形状
		return status, oe
	}
}
