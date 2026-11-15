package core

// JSONC 与 JSON 定点删除的通用实现。
//
// 这两件事都不属于任何单个客户端：多处主配置（Copilot settings.json、
// MimoCode 的 .jsonc）都允许 JSONC 写法，保存校验（SaveTargetConfig）也要按
// JSONC 口径校验，因此它们的实现放在内核，客户端通过 core.JSONCToJSON /
// core.DeleteJSONObjectKey 调用。

import (
	"bytes"
	"encoding/json"
	"errors"
)

// deleteJSONObjectKey 删除顶层 JSON 对象中的指定键（含其值），其余内容
// （键序 / 缩进 / 行尾）原样保留，返回 (新文档, 是否删除, 错误)。
// 非最后一项：切除 [键起点, 下一键起点) 区间（该区间含值、尾逗号与缩进，
// 前驱键的逗号得以保留）；最后一项：切除 [键起点, 值结束)，并去掉前驱键的
// 尾逗号。仅支持严格 JSON（与 upsertJSONObjectKey 一致：文件含注释时解析
// 报错，调用方应拒绝覆写）。
func deleteJSONObjectKey(doc []byte, key string) ([]byte, bool, error) {
	if len(bytes.TrimSpace(doc)) == 0 {
		return doc, false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	tk, err := dec.Token()
	if err != nil {
		return nil, false, err
	}
	dl, ok := tk.(json.Delim)
	if !ok || dl != '{' {
		return nil, false, errors.New("顶层必须是 JSON 对象")
	}
	type kvEnt struct {
		keyStart int
		valEnd   int
	}
	var kvs []kvEnt
	found := -1
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, false, err
		}
		k, _ := kt.(string)
		// 键名起始（含两端引号）：回退 len(k)+2 只对「无转义键名」成立
		//（如 "a\"b" 解码后长度比字面短）。此处先从键结尾向左做一次
		// 反斜杠计数，得到引号内真实字节长度，避免转义键名把区间算错、
		// 误伤相邻键。键名均无转义时结果与原实现完全一致。
		litLen, serr := jsonStringLiteralLen(doc, int(dec.InputOffset()))
		if serr != nil {
			return nil, false, serr
		}
		keyStart := int(dec.InputOffset()) - litLen - 2 // 键名起始（含两端引号）
		keyEnd := int(dec.InputOffset())
		colon := bytes.IndexByte(doc[keyEnd:], ':')
		if colon < 0 {
			return nil, false, errors.New("缺少冒号")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, false, err
		}
		valEnd := int(dec.InputOffset())
		if len(kvs) > 0 && keyStart <= kvs[len(kvs)-1].valEnd {
			return nil, false, errors.New("键区间重叠，文档结构异常")
		}
		if k == key && found < 0 {
			found = len(kvs)
		}
		kvs = append(kvs, kvEnt{keyStart: keyStart, valEnd: valEnd})
	}
	if found < 0 {
		return doc, false, nil
	}
	if found < len(kvs)-1 {
		// 非最后一项：前驱逗号保留，本项（值+尾逗号+缩进）整体切除
		var out bytes.Buffer
		out.Write(doc[:kvs[found].keyStart])
		out.Write(doc[kvs[found+1].keyStart:])
		return out.Bytes(), true, nil
	}
	// 最后一项：切除 [键起点, 值结束)，并去掉前驱键的尾逗号
	bs, be := kvs[found].keyStart, kvs[found].valEnd
	end := bs
	for end > 0 && isJSONSpace(doc[end-1]) {
		end--
	}
	if end > 0 && doc[end-1] == ',' {
		end--
	}
	if len(kvs) == 1 {
		return []byte("{}"), true, nil // 唯一键被删后对象为空
	}
	var out bytes.Buffer
	out.Write(doc[:end])
	out.Write(doc[be:])
	return out.Bytes(), true, nil
}

// jsoncToJSON 把 JSONC（JSON with Comments）文本剥离 // 行注释、/* */ 块注释
// 与字符串字面量外的尾逗号，返回严格 JSON 字节。字符串内的注释符号与换行
// 不做处理；块注释跨行时保留换行以维持行号。剥离结果仍非合法 JSON 时返回
// 错误。空文档按空对象处理。
func jsoncToJSON(doc []byte) ([]byte, error) {
	var out bytes.Buffer
	inStr, esc := false, false
	i := 0
	for i < len(doc) {
		c := doc[i]
		if inStr {
			out.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte(c)
			i++
		case c == '/' && i+1 < len(doc) && doc[i+1] == '/':
			for i < len(doc) && doc[i] != '\n' {
				i++
			}
			// 保留换行（若到文件尾无换行则自然结束），维持后面内容的行号
			if i < len(doc) {
				out.WriteByte('\n')
				i++
			}
		case c == '/' && i+1 < len(doc) && doc[i+1] == '*':
			i += 2
			for i+1 < len(doc) && !(doc[i] == '*' && doc[i+1] == '/') {
				if doc[i] == '\n' {
					out.WriteByte('\n') // 块注释跨行：保留换行保持行号
				}
				i++
			}
			if i+1 < len(doc) {
				i += 2
			} else {
				i = len(doc)
			}
		case c == ',':
			// 尾逗号：跳过其后空白/注释，若遇 } 或 ] 则丢弃该逗号
			j := i + 1
			for j < len(doc) {
				// 跳过空白
				for j < len(doc) && isJSONSpace(doc[j]) {
					j++
				}
				// 跨过行注释与块注释后再判断闭合符
				if j+1 < len(doc) && doc[j] == '/' && doc[j+1] == '/' {
					for j < len(doc) && doc[j] != '\n' {
						j++
					}
					continue
				}
				if j+1 < len(doc) && doc[j] == '/' && doc[j+1] == '*' {
					j += 2
					for j+1 < len(doc) && !(doc[j] == '*' && doc[j+1] == '/') {
						j++
					}
					if j+1 < len(doc) {
						j += 2
					} else {
						j = len(doc)
					}
					continue
				}
				break
			}
			if j < len(doc) && (doc[j] == '}' || doc[j] == ']') {
				i = j
			} else {
				out.WriteByte(c)
				i++
			}
		default:
			out.WriteByte(c)
			i++
		}
	}
	if inStr {
		return nil, errors.New("JSONC 字符串未闭合")
	}
	if len(bytes.TrimSpace(out.Bytes())) == 0 {
		return []byte("{}"), nil
	}
	if !json.Valid(out.Bytes()) {
		return nil, errors.New("JSONC 剥离后仍不是合法 JSON")
	}
	return out.Bytes(), nil
}
