package format

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

func jsonDumps(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func randID(prefix string) string {
	b := make([]byte, 6)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

type DataURL struct {
	Mime string
	Data string
}

func parseDataURL(url string) *DataURL {
	if url == "" {
		return nil
	}
	// data:<mime>;base64,<data>
	re1 := regexp.MustCompile(`^data:([^;,]+);base64,(.+)$`)
	if m := re1.FindStringSubmatch(url); m != nil {
		return &DataURL{Mime: m[1], Data: m[2]}
	}
	// data:<mime>,<urlencoded data>
	re2 := regexp.MustCompile(`^data:([^;,]+);[^,]+,(.+)$`)
	if m := re2.FindStringSubmatch(url); m != nil {
		decoded, err := urlDecode(m[2])
		if err == nil {
			return &DataURL{Mime: m[1], Data: base64.StdEncoding.EncodeToString([]byte(decoded))}
		}
	}
	return nil
}

func urlDecode(s string) (string, error) {
	// Simple percent-decoding
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			high, low := s[i+1], s[i+2]
			h := unhex(high)
			l := unhex(low)
			if h >= 0 && l >= 0 {
				b.WriteByte(byte(h<<4 | l))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

func unhex(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c - 'a' + 10)
	case 'A' <= c && c <= 'F':
		return int(c - 'A' + 10)
	}
	return -1
}

// Safe parse: return fb on failure
func safeParse(s any, fb any) any {
	str, ok := s.(string)
	if !ok {
		return fb
	}
	var v any
	if json.Unmarshal([]byte(str), &v) == nil {
		return v
	}
	return fb
}

func toList(x any) []any {
	if arr, ok := x.([]any); ok {
		return arr
	}
	if x == nil {
		return []any{}
	}
	return []any{x}
}

func partsToText(parts any) string {
	if s, ok := parts.(string); ok {
		return s
	}
	arr, ok := parts.([]any)
	if !ok {
		return ""
	}
	var out strings.Builder
	for _, p := range arr {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := m["type"].(string); t == "text" {
			if text, ok := m["text"].(string); ok {
				out.WriteString(text)
			}
		} else if text, ok := m["text"].(string); ok {
			out.WriteString(text)
		}
	}
	return out.String()
}

func openaiErrorChunk(message string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "upstream_stream_error",
			"param":   nil,
			"code":    nil,
		},
	}
}

// normalizeBaseURL strips known API endpoint suffixes from a base URL
// so the system can cleanly re-append the correct path for the selected type.
// This is a universal pre-processing step, used for all provider types.
func NormalizeBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	base = strings.TrimSuffix(base, "/chat/completions")
	base = strings.TrimSuffix(base, "/messages")
	base = strings.TrimSuffix(base, "/responses")
	base = strings.TrimSuffix(base, "/models")
	for strings.HasSuffix(base, "/v1beta/openai") {
		base = base[:len(base)-len("/v1beta/openai")]
	}
	for strings.HasSuffix(base, "/v1") {
		base = base[:len(base)-len("/v1")]
	}
	for strings.HasSuffix(base, "/v1beta") {
		base = base[:len(base)-len("/v1beta")]
	}
	for strings.HasSuffix(base, "/api") {
		base = base[:len(base)-len("/api")]
	}
	return base
}
