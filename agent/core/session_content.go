package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// ---- 会话内容阅读（阅读窗口用）----
// 从会话条目（文件或目录）提取对话消息列表，形状解析与统计口径（sessionUsageStats /
// userTextFromRecord）保持一致，覆盖各客户端已知的 JSON 形状。

// SessionMessage 阅读窗口展示的一条对话消息。
type SessionMessage struct {
	Role string `json:"role"` // user | assistant
	Text string `json:"text"`
}

const (
	sessionContentMaxFile = 32 << 20 // 单文件上限：超过视为异常产物不解析
	sessionContentMaxMsgs = 4000     // 消息条数上限，防止超大 会话 撑爆前端
	sessionContentLineCap = 8 << 20  // 单行上限：带长工具输出的行可能很大
)

// SessionContent 返回会话的对话消息列表。文件型条目直接解析该文件；目录型
// 条目取目录内对话记录（grok-build 优先 chat_history.jsonl，其余取第一个
// *.jsonl）。识别不到可读内容时 ok=false，前端显示空态而非报错。
func SessionContent(clientID, path string) ([]SessionMessage, bool) {
	// 库型存储（MiMo Code）的会话在 SQLite 库里，没有可解析的转写文件
	if st, ok := sessionStores[clientID]; ok && st.CLI != nil {
		return nil, false
	}
	p := path
	if fi, err := os.Stat(p); err != nil {
		return nil, false
	} else if fi.IsDir() {
		p = dirTranscriptFile(clientID, p)
		if p == "" {
			return nil, false
		}
	}
	return parseTranscriptJSONL(p)
}

// dirTranscriptFile 目录型会话里挑对话记录文件：grok-build 固定 chat_history.jsonl，
// 其余客户端取目录内第一个 *.jsonl（与 dirSessionMeta 的兜底口径一致）。
func dirTranscriptFile(clientID, dir string) string {
	if clientID == "grok-build" {
		p := dir + string(os.PathSeparator) + "chat_history.jsonl"
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() <= sessionContentMaxFile {
			return p
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jsonl") {
			continue
		}
		p := dir + string(os.PathSeparator) + e.Name()
		if fi, ferr := e.Info(); ferr == nil && fi.Size() <= sessionContentMaxFile {
			return p
		}
	}
	return ""
}

// parseTranscriptJSONL 逐行解析会话 jsonl，提取 user / assistant 消息。
func parseTranscriptJSONL(path string) ([]SessionMessage, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), sessionContentLineCap)
	var msgs []SessionMessage
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var rec map[string]any
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		role, text := transcriptFromRecord(rec)
		if text == "" {
			continue
		}
		msgs = append(msgs, SessionMessage{Role: role, Text: text})
		if len(msgs) >= sessionContentMaxMsgs {
			break
		}
	}
	return msgs, len(msgs) > 0
}

// transcriptFromRecord 从一条会话记录提取 (role, text)，形状与 sessionRecordIsMessage
// 对齐（能被统计为消息的记录这里就能读出内容；读不出文本的统计消息返回空跳过）：
//
//	Claude 系 / grok  {"type":"user"|"assistant","message":{"content":…}} 或 {"type":…,"content":…}
//	通用              {"role":…,"content":…} / {"message":{"role":…,"content":…}}
//	Codex rollout     {"payload":{"type":"user_message"|"agent_message","message":…}}
//	                  {"payload":{"type":"message","role":…,"content":…}}
func transcriptFromRecord(rec map[string]any) (role, text string) {
	if s, _ := rec["type"].(string); s == "user" || s == "assistant" {
		role = s
		if m, ok := rec["message"].(map[string]any); ok {
			if text = transcriptText(m["content"]); text != "" {
				return role, text
			}
		}
		return role, transcriptText(rec["content"])
	}
	if p, ok := rec["payload"].(map[string]any); ok {
		switch s, _ := p["type"].(string); s {
		case "user_message":
			return "user", transcriptText(p["message"]) // codex 旧：文本在 payload.message
		case "agent_message":
			return "assistant", transcriptText(p["message"])
		case "message":
			if r, _ := p["role"].(string); r == "user" || r == "assistant" {
				return r, transcriptText(p["content"])
			}
		}
	}
	if m, ok := rec["message"].(map[string]any); ok {
		if r, _ := m["role"].(string); r == "user" || r == "assistant" {
			return r, transcriptText(m["content"])
		}
	}
	if r, _ := rec["role"].(string); r == "user" || r == "assistant" {
		return r, transcriptText(rec["content"])
	}
	return "", ""
}

// transcriptText 取消息文本：字符串原样；数组取文本项（type 缺省视为文本）拼合。
// 与标题提取的 cleanUserText 不同，这里保留原文换行（阅读需要完整排版），只剔除
// 注入型噪声块（<system-reminder> 等，Claude 会话高频出现）。
func transcriptText(v any) string {
	switch c := v.(type) {
	case string:
		return transcriptClean(c)
	case []any:
		var parts []string
		for _, it := range c {
			mm, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if t, hasType := mm["type"].(string); hasType && t != "text" && t != "input_text" {
				continue
			}
			if s, _ := mm["text"].(string); s != "" {
				if s = transcriptClean(s); s != "" {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}

// transcriptClean 处理标签包装：
//   - 剥掉噪声块（<system-reminder>…</system-reminder>、grok 的 <user_info> 环境
//     块等，常与用户正文混在同一段文本里，按块删除而非整段丢弃）；
//   - <user_query>…</user_query> 是 grok 的用户正文包装，剥壳保留内容。
//
// 剩余文本原样返回（仅去首尾空白）。
func transcriptClean(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, tag := range []string{"system-reminder", "command-message", "local-command-stdout", "user_info", "rules"} {
		open, close := "<"+tag+">", "</"+tag+">"
		for {
			i := strings.Index(s, open)
			if i < 0 {
				break
			}
			j := strings.Index(s[i:], close)
			if j < 0 { // 未闭合：视为纯噪声块，整段丢弃
				return strings.TrimSpace(s[:i])
			}
			s = strings.TrimSpace(s[:i] + s[i+j+len(close):])
		}
	}
	if i := strings.Index(s, "<user_query>"); i >= 0 {
		if j := strings.Index(s[i:], "</user_query>"); j >= 0 {
			inner := s[i+len("<user_query>") : i+j]
			s = strings.TrimSpace(s[:i] + inner + s[i+j+len("</user_query>"):])
		}
	}
	return s
}
