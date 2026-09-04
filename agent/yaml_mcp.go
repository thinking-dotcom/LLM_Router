package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Hermes Agent 的 MCP 配置位于主配置 config.yaml 的顶层 mcp_servers 键下
//（Claude 风格 mcpServers 的 YAML 形态；stdio 用 command/args，远程用 url）。
// 为保留用户配置中的注释与其它段落，采用文本级段落手术：仅摘除既有块中
// 本次注入同名（或全量清除时全部）的条目——用户自有的其它条目原样保留
//（合并语义，与 JSON 路径 mergeMcpServers 一致）——再追加本次注入的服务定义。

// 顶层的 mcp_servers 键行（无缩进；允许可选引号包裹键名，避免带引号写法
// 摘除失败导致重复注入）。块形态要求冒号后跟空白或无内容：`mcp_servers:#x`
// 这类紧贴 # 的写法视为普通标量行原样保留，不被误判为块头而吞后续缩进行。
var yamlMcpKeyRe = regexp.MustCompile(`^['"]?mcp_servers['"]?:\s+(.*)$|^['"]?mcp_servers['"]?:$`)

// 条目键名提取：块内条目行形如 `  name:` 或 `  "name":`（RE2 不支持反向引用，
// 引号在代码里统一 Trim 掉）。
var yamlEntryNameRe = regexp.MustCompile(`^\s*(['"]?)([^:#]+?)['"]?:`)

// injectYamlMcpServers 把 servers 合并进 yamlFile 的顶层 mcp_servers 块，
// 文件其余内容（含注释、其它段落与用户自有的其它条目）原样保留；
// 文件不存在时按空配置创建。servers 为空时摘除整个 mcp_servers 块
// （全量清除语义，供撤销使用）。
func injectYamlMcpServers(yamlFile string, servers []MCPServer) error {
	// 读-改-写全程持锁：与 SaveTargetConfig 等并发时避免后写覆盖先写
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	b, err := os.ReadFile(yamlFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// 首次注入前备份原始状态（已有备份则保留首次），供手动回退
	if _, err := backupIfAbsent(yamlFile); err != nil {
		return err
	}

	// 本次要替换的条目名集合
	drop := make(map[string]bool, len(servers))
	for _, s := range servers {
		drop[s.Name] = true
	}

	lines := strings.Split(string(b), "\n")
	var kept []string
	var keptSub []string // 保留的既有条目行（缩进原样）
	seenBlk := false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		m := yamlMcpKeyRe.FindStringSubmatch(line)
		if m == nil {
			kept = append(kept, lines[i])
			continue
		}
		inline := strings.TrimSpace(stripYamlComment(m[1]))
		if inline != "" && !strings.HasPrefix(inline, "{") {
			// 标量等其它取值：非本工具管理的块形态，原样保留
			kept = append(kept, lines[i])
			continue
		}
		// 块形态（含 `mcp_servers: {}` 内联空对象）：收集缩进子块并按名过滤。
		// 同一文件出现多个顶层 mcp_servers 块时按合并只会互相覆盖导致丢条目，
		// 直接拒绝写入（有备份可回退），不再静默删减。
		if seenBlk {
			return fmt.Errorf("%s 包含多个顶层 mcp_servers 块，已拒绝写入以免丢失配置", yamlFile)
		}
		seenBlk = true
		i++
		var sub []string
		for ; i < len(lines); i++ {
			t := strings.TrimSuffix(lines[i], "\r")
			if t != "" && !strings.HasPrefix(t, " ") && !strings.HasPrefix(t, "\t") {
				break // 下一个顶层键：块结束
			}
			sub = append(sub, lines[i])
		}
		i-- // 外层 for 会再 ++，回退到块结束行
		keptSub = filterYamlMcpEntries(sub, drop)
		// 块之后的剩余行由后续循环按普通行处理（此处不跳出）
	}

	text := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	if len(servers) > 0 {
		var blk strings.Builder
		blk.WriteString("mcp_servers:")
		for _, l := range keptSub {
			blk.WriteString("\n" + strings.TrimSuffix(l, "\r"))
		}
		for _, s := range servers {
			if e := renderYamlMcpEntry(s); e != "" {
				blk.WriteString("\n" + e)
			}
		}
		text += "\n\n" + blk.String()
	}
	text += "\n"

	// 冒烟校验：拼接后的整体必须仍是合法 YAML，否则拒绝写入，
	// 避免缩进误判/键名转义出错导致客户端下次启动配置损坏。
	var probe any
	if err := yaml.Unmarshal([]byte(text), &probe); err != nil {
		return fmt.Errorf("生成的 YAML 配置无法解析，已拒绝写入 %s: %w", yamlFile, err)
	}

	if err := os.MkdirAll(filepath.Dir(yamlFile), 0o755); err != nil {
		return err
	}
	// config.yaml 含 api_key 等凭据，落盘权限收紧为 0600
	return writeFileAtomic(yamlFile, []byte(text), 0o600)
}

// stripYamlComment 去掉行尾注释（不在引号内的 # 之后内容）。
func stripYamlComment(s string) string {
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			if !inD {
				inS = !inS
			}
		case '"':
			if !inS {
				inD = !inD
			}
		case '#':
			if !inS && !inD && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
				return s[:i]
			}
		}
	}
	return s
}

// filterYamlMcpEntries 按条目切分 mcp_servers 子块（块内最小缩进的行 = 条目
// 起点，更大缩进/空行归属当前条目），仅移除名字命中 drop 的条目，其余原样保留。
func filterYamlMcpEntries(sub []string, drop map[string]bool) []string {
	if len(drop) == 0 {
		return nil // 全量清除：整个子块不保留
	}
	minInd := -1
	for _, l := range sub {
		t := strings.TrimSuffix(l, "\r")
		if strings.TrimSpace(t) == "" {
			continue
		}
		ind := len(t) - len(strings.TrimLeft(t, " \t"))
		if minInd < 0 || ind < minInd {
			minInd = ind
		}
	}
	var out []string
	curDrop := false
	for _, l := range sub {
		t := strings.TrimSuffix(l, "\r")
		if strings.TrimSpace(t) == "" {
			if !curDrop {
				out = append(out, l)
			}
			continue
		}
		ind := len(t) - len(strings.TrimLeft(t, " \t"))
		if ind == minInd {
			name := ""
			if mm := yamlEntryNameRe.FindStringSubmatch(strings.TrimSpace(t)); mm != nil {
				name = strings.Trim(strings.TrimSpace(mm[2]), `"'`)
			}
			curDrop = drop[name]
		}
		if !curDrop {
			out = append(out, l)
		}
	}
	return out
}

// renderYamlMcpEntry 渲染单个服务的缩进条目（挂在 mcp_servers: 之下）。
func renderYamlMcpEntry(s MCPServer) string {
	doc := map[string]any{}
	switch strings.ToLower(s.Type) {
	case "stdio":
		entry := map[string]any{"command": s.Command}
		if len(s.Args) > 0 {
			entry["args"] = s.Args
		}
		doc[s.Name] = entry
	default: // sse / http
		doc[s.Name] = map[string]any{"url": s.URL}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return ""
	}
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out.WriteString("  ")
		out.WriteString(line)
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}
