package clients

import (
	"errors"
	"llm-router-go/agent/core"

	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Grok Build 的 MCP 配置位于主配置 config.toml 的 [mcp_servers.<name>] 段。
// 为保留用户配置中的注释与其它段落，采用文本级段落手术而非整体反序列化重排：
// 仅摘除本次注入同名（或全量清除时全部）的 [mcp_servers.*] 段——用户自有的
// 其它 server 段原样保留（合并语义，与 JSON 路径 mergeMcpServers 一致）——
// 再在文件末尾追加本次注入的服务定义。

// 匹配 [mcp_servers] 或 [mcp_servers.<key>] 段头（不匹配 [mcp] 等其它段）。
var tomlMcpHeaderRe = regexp.MustCompile(`^\s*\[mcp_servers(?:\.[^\[\]]*)?\]\s*(#.*)?$`)

// 提取 [mcp_servers.<key>] 段头中的 <key>（裸 [mcp_servers] 表头捕获为空）。
var tomlMcpNameRe = regexp.MustCompile(`^\s*\[mcp_servers(?:\.([^\[\]]+))?\]\s*(?:#.*)?$`)

// 段头通用形态：方括号内不含嵌套方括号即视为表头（用于判定当前段是否结束）。
// 必须同时认「普通表 [x]」和「数组表 [[x]]」——旧写法只允许一个右方括号，
// `[[hooks]]` 这类数组表头识别不出来，其后整段会被当成 mcp 段内容删掉。
var tomlAnyHeaderRe = regexp.MustCompile(`^\s*\[\[?[^\[\]]+\]\]?\s*(#.*)?$`)

// 裸键允许字符；不满足时用双引号基本字符串包裹并转义。
var tomlBareKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// tomlMcpServerName 从 [mcp_servers.<key>] 段头提取 server 名：
// 带引号的键整体为名（可含点）；裸键允许点分子表（[mcp_servers.fs.sub]），
// 取首段为名；裸 [mcp_servers] 表头返回空串。
func tomlMcpServerName(header string) string {
	m := tomlMcpNameRe.FindStringSubmatch(header)
	if m == nil || m[1] == "" {
		return ""
	}
	raw := strings.TrimSpace(m[1])
	if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') {
		return tomlUnquoteKey(raw)
	}
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		return raw[:i]
	}
	return raw
}

// tomlMcpServerFullName 提取 [mcp_servers.<key>] 段头的完整键名（去引号）。
// 补充 tomlMcpServerName 覆盖不到的情形：重注入用带引号名（可含点），
// 而旧文件可能是裸点分键 [mcp_servers.fs.sub]，首段截断后无法与注入名匹配，
// 导致旧段清不掉、重复注入。
func tomlMcpServerFullName(header string) string {
	m := tomlMcpNameRe.FindStringSubmatch(header)
	if m == nil || m[1] == "" {
		return ""
	}
	return tomlUnquoteKey(strings.TrimSpace(m[1]))
}

// tomlUnquoteKey 还原 TOML 键名：去引号并处理基本字符串转义。
func tomlUnquoteKey(k string) string {
	k = strings.TrimSpace(k)
	if len(k) >= 2 {
		if k[0] == '"' && k[len(k)-1] == '"' {
			if u, err := strconv.Unquote(k); err == nil {
				return u
			}
			return strings.ReplaceAll(k[1:len(k)-1], `\"`, `"`)
		}
		if k[0] == '\'' && k[len(k)-1] == '\'' {
			return k[1 : len(k)-1]
		}
	}
	return k
}

// injectTomlMcpServers 把 servers 写入 tomlFile 的 [mcp_servers.*] 段：
// 既有 mcp 段整段摘除后重写本次所选服务（替换语义），客户端 MCP 段与
// 「MCP 页所配内容」保持一致；文件其余内容（注释、其它段落）原样保留；
// 文件不存在时按空配置创建。servers 为空时摘除全部 [mcp_servers.*] 段
// （全量清除语义，供撤销使用）。
func injectTomlMcpServers(tomlFile string, servers []core.MCPServer) error {
	// 读-改-写全程持锁：与 SaveTargetConfig 等并发时避免后写覆盖先写
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	b, err := os.ReadFile(tomlFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// 首次注入前备份原始状态（已有备份则保留首次），供手动回退
	if _, err := core.BackupIfAbsent(tomlFile); err != nil {
		return err
	}

	// 替换语义：既有 [mcp_servers.*] 段全部摘除（从段头到下一个段头之前），
	// 再写入本次所选服务——客户端 MCP 段与「MCP 页所配内容」保持一致；
	// servers 为空即全部清除。文件其余段落与注释原样保留。
	var kept []string
	inMcp := false
	inDq, inSq := false, false // 是否处于 """…""" / '''…''' 多行字符串内部
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !inDq && !inSq && tomlAnyHeaderRe.MatchString(trimmed) {
			inMcp = tomlMcpHeaderRe.MatchString(trimmed)
		}
		if !inMcp {
			kept = append(kept, line)
		}
		// 多行字符串可跨行，段内形如 [xxx] 的行不是段头：按出现次数奇偶翻转状态。
		// 纯注释行（不在多行字符串内）不算字符串边界：注释里出现奇数个 """
		// 会把状态机翻转失步，其后所有段落被误判为 mcp 段而整段丢弃。
		if !inDq && !inSq && strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Count(trimmed, `"""`)%2 == 1 {
			inDq = !inDq
		}
		if strings.Count(trimmed, `'''`)%2 == 1 {
			inSq = !inSq
		}
	}

	text := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	for _, s := range servers {
		if block := renderTomlMcpServer(s); block != "" {
			text += "\n\n" + block
		}
	}
	text += "\n"

	// 冒烟校验：拼接后的整体必须仍是合法 TOML，否则拒绝写入，
	// 避免段头正则/键名转义出错导致客户端下次启动配置损坏。
	// 校验前折叠跨行内联表（既有文件可能是 Grok 文档风格的多行内联表），
	// 并把折叠结果写盘，保证最终文件可被严格 TOML 解析。
	text = core.FoldTomlInlineTables(text)
	var probe any
	if _, err := toml.Decode(text, &probe); err != nil {
		return fmt.Errorf("生成的 TOML 配置无法解析，已拒绝写入 %s: %w", tomlFile, err)
	}

	if err := os.MkdirAll(filepath.Dir(tomlFile), 0o755); err != nil {
		return err
	}
	// config.toml 含 api_key 等凭据，落盘权限收紧为 0600
	return core.WriteFileAtomic(tomlFile, []byte(text), 0o600)
}

// renderTomlMcpServer 渲染单个服务的 [mcp_servers.<name>] 段。
// stdio → command/args；sse/http 均为远程 url 形态（grok 按自动识别传输）。
// 字段编码交给 toml.Marshal，保证转义正确。
func renderTomlMcpServer(s core.MCPServer) string {
	var sv struct {
		Command string   `toml:"command,omitempty"`
		Args    []string `toml:"args,omitempty"`
		URL     string   `toml:"url,omitempty"`
		Enabled bool     `toml:"enabled"`
	}
	switch strings.ToLower(s.Type) {
	case "stdio":
		sv.Command = s.Command
		sv.Args = s.Args
	default: // sse / http
		sv.URL = s.URL
	}
	sv.Enabled = true

	body, err := toml.Marshal(sv)
	if err != nil || strings.TrimSpace(string(body)) == "" {
		return ""
	}
	key := s.Name
	if !tomlBareKeyRe.MatchString(key) {
		// TOML 字面量字符串（单引号）不支持 \' 转义，含引号的键会产出非法 TOML；
		// 改用双引号基本字符串并转义反斜杠与双引号
		key = `"` + strings.ReplaceAll(strings.ReplaceAll(key, `\`, `\\`), `"`, `\"`) + `"`
	}
	return "[mcp_servers." + key + "]\n" + strings.TrimRight(string(body), "\n")
}

// RemoveTomlMcpServer 从 TOML 主配置中摘除单个 [mcp_servers.<name>] 段（「踢出」：
// 该段整段移除，其余段落、注释与其它服务段原样保留），产物仍过 TOML 合法性校验。
func RemoveTomlMcpServer(tomlFile, name string) error {
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	b, err := os.ReadFile(tomlFile)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("该智能体的 MCP 配置文件不存在")
		}
		return err
	}
	if _, err := core.BackupIfAbsent(tomlFile); err != nil {
		return err
	}
	var kept []string
	inMcp := false
	dropCur := false
	found := false
	inDq, inSq := false, false
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !inDq && !inSq && tomlAnyHeaderRe.MatchString(trimmed) {
			if inMcp = tomlMcpHeaderRe.MatchString(trimmed); inMcp {
				n := tomlMcpServerName(trimmed)
				full := tomlMcpServerFullName(trimmed)
				dropCur = name == "" || n == name || full == name // name 为空 = 全量摘除（一键踢出）
				if dropCur {
					found = true
				}
			}
		}
		if !inMcp || !dropCur {
			kept = append(kept, line)
		}
		if strings.Count(trimmed, `"""`)%2 == 1 {
			inDq = !inDq
		}
		if strings.Count(trimmed, `'''`)%2 == 1 {
			inSq = !inSq
		}
	}
	if !found {
		return errors.New("未找到该服务的配置段")
	}
	text := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"
	text = core.FoldTomlInlineTables(text)
	var probe any
	if _, err := toml.Decode(text, &probe); err != nil {
		return fmt.Errorf("剔除后的 TOML 无法解析，已拒绝写入 %s: %w", tomlFile, err)
	}
	return core.WriteFileAtomic(tomlFile, []byte(text), 0o600)
}
