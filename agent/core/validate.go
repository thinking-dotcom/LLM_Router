package core

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// ---------- 非文本类配置的格式校验 ----------
// 与 JSON 配置的保存前校验对应：格式非法时拒绝写入，避免部署损坏的客户端配置。

// ValidateToml 校验 TOML 内容；空内容视为合法（等价清空配置）。
// 顶层必须是表/键值对映射（任意客户端配置均为映射），标量/数组等
// 顶层视为非法，避免保存后客户端启动时解析失败。
// 校验前先折叠跨行内联表（foldTomlInlineTables），使 Grok Build 文档风格的
// 多行内联表写法也能通过严格 TOML 解析。
func ValidateToml(content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	var v map[string]any
	if err := toml.Unmarshal([]byte(foldTomlInlineTables(content)), &v); err != nil {
		return err
	}
	return nil
}

// foldTomlInlineTables 把 TOML 内联表 {...} 内部的换行折叠为空格。
//
// 背景：严格 TOML 规范不允许内联表跨行（解析器报
// "newlines not allowed within inline tables"），但 Grok Build 官方文档示例
// 与部分工具写出的配置正是跨行内联表形态，如：
//
//	[permission]
//	rules = [
//	  { tool = "bash",
//	    action = "allow" },
//	]
//
// 折叠后即成合法单行内联表，语义不变。逐字符扫描并维护三类状态：
//   - 双/单引号字符串（含 """ 与 ”' 多行字符串，\" 等转义不结束字符串）：
//     字符串内的换行原样保留（多行字符串合法，折叠会破坏其内容）
//   - # 注释：从 # 到行尾原样保留（注释里的 { } 不影响深度）
//   - 花括号深度：深度 > 0（处于内联表内）时把换行替换为空格
func foldTomlInlineTables(content string) string {
	if !strings.ContainsAny(content, "{}") {
		return content
	}
	var b strings.Builder
	b.Grow(len(content))
	depth := 0
	inDq, inSq := false, false   // "…" / '…'
	inTdq, inTsq := false, false // """…""" / '''…'''
	for i := 0; i < len(content); i++ {
		c := content[i]
		switch {
		case inTdq || inTsq:
			b.WriteByte(c)
			if c == '\\' && inTdq && i+1 < len(content) { // 多行基本字符串内的转义
				i++
				b.WriteByte(content[i])
				continue
			}
			if c == '"' && inTdq && strings.HasPrefix(content[i+1:], `""`) {
				b.WriteString(`""`)
				i += 2
				inTdq = false
			} else if c == '\'' && inTsq && strings.HasPrefix(content[i+1:], `''`) {
				b.WriteString(`''`)
				i += 2
				inTsq = false
			}
		case inDq || inSq:
			b.WriteByte(c)
			if c == '\\' && inDq && i+1 < len(content) { // 基本字符串内的转义
				i++
				b.WriteByte(content[i])
				continue
			}
			if (c == '"' && inDq) || (c == '\'' && inSq) {
				inDq, inSq = false, false
			}
		default:
			switch {
			case strings.HasPrefix(content[i:], `"""`):
				inTdq = true
				b.WriteString(`"""`)
				i += 2
			case strings.HasPrefix(content[i:], `'''`):
				inTsq = true
				b.WriteString(`'''`)
				i += 2
			case c == '"':
				inDq = true
				b.WriteByte(c)
			case c == '\'':
				inSq = true
				b.WriteByte(c)
			case c == '#':
				// 注释直到行尾原样保留，注释里的 { } 不改变深度
				if j := strings.IndexByte(content[i:], '\n'); j >= 0 {
					b.WriteString(content[i : i+j])
					i += j - 1 // for 循环 i++ 后正好指向换行符
				} else {
					b.WriteString(content[i:])
					i = len(content)
				}
			case c == '{':
				depth++
				b.WriteByte(c)
			case c == '}':
				if depth > 0 {
					depth--
				}
				b.WriteByte(c)
			case (c == '\n' || c == '\r') && depth > 0:
				if c == '\r' && i+1 < len(content) && content[i+1] == '\n' {
					i++
				}
				b.WriteByte(' ')
			default:
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}

// ValidateYaml 校验 YAML 内容；空内容视为合法。
// 顶层必须是映射（各客户端主配置均为 mapping），标量/数组顶层视为非法。
func ValidateYaml(content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	var v map[string]any
	if err := yaml.Unmarshal([]byte(content), &v); err != nil {
		return fmt.Errorf("YAML 顶层必须是对象: %w", err)
	}
	return nil
}

// ValidateDotEnv 校验 .env 内容：非注释行必须是 KEY=VALUE 形式，
// KEY 不能为空、不能含 '=' 以外的空白字符。空行与 # 注释行跳过。
func ValidateDotEnv(content string) error {
	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		k, _, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) == "" || strings.ContainsAny(k, " \t") {
			return fmt.Errorf("第 %d 行不是有效的 KEY=VALUE 格式", i+1)
		}
	}
	return nil
}

// ValidateText 校验纯文本（Markdown 等）为合法 UTF-8 且不含 NUL 等控制字符。
func ValidateText(content string) error {
	if !utf8.ValidString(content) {
		return fmt.Errorf("内容不是有效的 UTF-8 文本")
	}
	if strings.ContainsRune(content, '\x00') {
		return fmt.Errorf("内容包含非法控制字符")
	}
	return nil
}
