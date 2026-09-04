package agent

import (
	"bytes"
	"encoding/json"
	"errors"
)

// upsertJSONObjectKey 在顶层 JSON 对象中替换 key 的 value（fn 接收现有值、
// 返回新值），其余字节（键序 / 缩进 / 数字格式 / 行尾）原样保留，
// 避免把用户手写的真实配置整体重排。
//
// - key 不存在时在顶层对象末尾插入（fn 收到 nil existing）。
// - 顶层不是对象、或文件不是严格 JSON 时返回错误（调用方应拒绝覆写）。
//
// 现有值类型不对（如 mcpServers 是数组）时由 fn 自行决定丢弃或重建。
func upsertJSONObjectKey(doc []byte, key string, fn func(existing json.RawMessage) (json.RawMessage, error)) ([]byte, error) {
	if len(bytes.TrimSpace(doc)) == 0 {
		return nil, errors.New("空文档无法定点更新")
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	tk, err := dec.Token()
	if err != nil {
		return nil, err
	}
	dl, ok := tk.(json.Delim)
	if !ok || dl != '{' {
		return nil, errors.New("顶层必须是 JSON 对象")
	}
	openEnd := int(dec.InputOffset()) // '{' 结束位置（空对象时用于定位闭合括号）

	outer := outerJSONIndent(doc)

	var out bytes.Buffer
	out.Write(doc[:openEnd]) // 对象起始括号原样写入
	last := openEnd          // 已扫描字节位置（上一值结束）
	found := false
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		k, _ := kt.(string)
		keyEnd := int(dec.InputOffset()) // key token 结束偏移
		colon := bytes.IndexByte(doc[keyEnd:], ':')
		if colon < 0 {
			return nil, errors.New("缺少冒号")
		}
		valStart := keyEnd + colon + 1
		for valStart < len(doc) && isJSONSpace(doc[valStart]) {
			valStart++
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		valEnd := int(dec.InputOffset()) // 值结束偏移

		// 前续内容（分隔符/缩进/原始 key/冒号后空白）原样保留
		out.Write(doc[last:valStart])
		if k == key {
			nv, rerr := fn(raw)
			if rerr != nil {
				return nil, rerr
			}
			rendered, rerr := renderJSONValue(nv, outer)
			if rerr != nil {
				return nil, rerr
			}
			out.Write(rendered)
			found = true
		} else {
			out.Write(doc[valStart:valEnd])
		}
		last = valEnd
	}

	// 顶层闭合括号：last 之后（空对象时从 '{' 之后）第一个非空白必须是 '}'
	searchFrom := last
	if searchFrom < openEnd {
		searchFrom = openEnd
	}
	closeAt := searchFrom
	for closeAt < len(doc) && isJSONSpace(doc[closeAt]) {
		closeAt++
	}
	if closeAt >= len(doc) || doc[closeAt] != '}' {
		return nil, errors.New("顶层对象未闭合")
	}

	if !found {
		nv, rerr := fn(nil)
		if rerr != nil {
			return nil, rerr
		}
		rendered, rerr := renderJSONValue(nv, outer)
		if rerr != nil {
			return nil, rerr
		}
		if isEmptyContainer(rendered) {
			return doc, nil // 键不存在且新值为空容器：无需插入，原样返回
		}
		// 空对象 { } 时不加逗号
		trimLast := bytes.TrimRight(doc[:last], " \t\r\n")
		if len(trimLast) > 0 && trimLast[len(trimLast)-1] != '{' {
			out.WriteByte(',')
		}
		out.WriteString("\n")
		out.WriteString(outer)
		out.WriteString(`"` + key + `": `)
		out.Write(rendered)
	}
	out.Write(doc[last:closeAt]) // 最后一个值后的原始空白/换行（替换分支同样保留）
	out.Write(doc[closeAt:])     // '}' 及尾部原样
	return out.Bytes(), nil
}

// renderJSONValue 把任意值渲染为对齐外层缩进的 JSON 字节。
// 标量值（数字/字符串/bool/null）原样紧凑输出；容器展开为多行、
// 嵌套缩进固定 2 空格，外层前缀取文件顶层键的缩进风格。
func renderJSONValue(v any, outer string) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw, nil
	}
	// 标量值（数字/字符串/bool/null）：无需展开
	if len(raw) == 0 || raw[0] != '{' && raw[0] != '[' {
		return raw, nil
	}
	// 以空前缀展开，随后手动给非首行加外层缩进：
	// 首行紧跟 `"key": ` 之后，不能带 prefix 造成多余空格。
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return nil, err
	}
	if outer == "" {
		return buf.Bytes(), nil
	}
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	var outB bytes.Buffer
	for i, ln := range lines {
		if i > 0 {
			outB.WriteString(outer)
		}
		outB.Write(ln)
		if i < len(lines)-1 {
			outB.WriteByte('\n')
		}
	}
	return outB.Bytes(), nil
}

// outerJSONIndent 推断顶层对象键的缩进（第一个换行后的前导空白），
// 用于让注入的键与文件现有缩进风格一致；单行文件返回空串。
func outerJSONIndent(doc []byte) string {
	nl := bytes.IndexByte(doc, '\n')
	if nl < 0 {
		return ""
	}
	i := nl + 1
	for i < len(doc) && (doc[i] == ' ' || doc[i] == '\t') {
		i++
	}
	return string(doc[nl+1 : i])
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// isEmptyContainer 判断渲染结果是否为空的 JSON 对象或数组。
func isEmptyContainer(rendered []byte) bool {
	t := bytes.TrimSpace(rendered)
	return bytes.Equal(t, []byte("{}")) || bytes.Equal(t, []byte("[]"))
}
