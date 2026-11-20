package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MCPServer 表示一个 MCP 服务项配置。
type MCPServer struct {
	Name    string   `json:"name"`
	Desc    string   `json:"desc,omitempty"`    // 描述（显示在列表）
	Type    string   `json:"type"`              // stdio / sse / http
	Command string   `json:"command,omitempty"` // stdio 命令行程序
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"` // sse / http 地址
	Enabled bool     `json:"enabled"`       // 是否参与注入（缺省视为 true）
	// Extra 保存配置文件里不被上述字段管制的自定义键（env/headers/transport 等）。
	// 非空时逐字保留，保证列表改名/删除等整体持久化不再抹掉用户手写字段。
	Extra map[string]json.RawMessage `json:"-"`
}

// mcpManagedKeys 是 MCPServer 受管字段（Extra 之外），序列化/反序列化时跳过。
var mcpManagedKeys = map[string]bool{
	"name": true, "desc": true, "type": true,
	"command": true, "args": true, "url": true, "enabled": true,
}

// McpManagedKey 判断键是否由 MCPServer 的受管字段承载（供 main 绑定层识别
// 前端提交里的自定义键，将其收进 Extra 保留）。
func McpManagedKey(k string) bool { return mcpManagedKeys[k] }

// UnmarshalJSON 反序列化受管字段，并把其余自定义键原样收进 Extra。
func (s *MCPServer) UnmarshalJSON(b []byte) error {
	type plain MCPServer
	var p plain
	if err := json.Unmarshal(b, (*plain)(&p)); err != nil {
		return err
	}
	*s = MCPServer(p)
	var all map[string]json.RawMessage
	if json.Unmarshal(b, &all) == nil {
		// 旧配置没有 enabled 字段：缺省视为启用
		if _, ok := all["enabled"]; !ok {
			s.Enabled = true
		}
		for k, v := range all {
			if mcpManagedKeys[k] {
				continue
			}
			if s.Extra == nil {
				s.Extra = map[string]json.RawMessage{}
			}
			s.Extra[k] = v
		}
	}
	return nil
}

// MarshalJSON 输出受管字段 + 自定义键（顶层平铺），保证往返不丢字段。
func (s MCPServer) MarshalJSON() ([]byte, error) {
	m := map[string]any{"name": s.Name, "type": s.Type, "enabled": s.Enabled}
	if s.Desc != "" {
		m["desc"] = s.Desc
	}
	if s.Command != "" {
		m["command"] = s.Command
	}
	if len(s.Args) > 0 {
		m["args"] = s.Args
	}
	if s.URL != "" {
		m["url"] = s.URL
	}
	for k, v := range s.Extra {
		if mcpManagedKeys[k] {
			continue
		}
		m[k] = json.RawMessage(v)
	}
	return json.Marshal(m)
}

// 存储模型：data/mcp 目录下每个 *.json 文件即一个 MCP 服务配置（文件内容为
// MCPServer 对象）；mcps.json 仅保存路由器接入地址 baseUrl。
// 兼容旧版：若 mcps.json 里仍有内嵌 servers 列表，首次读取时自动拆分为独立文件。

// McpConfig 是旧版 mcps.json 的结构（现仅用其 baseUrl 与迁移）。
type McpConfig struct {
	BaseURL string      `json:"baseUrl"`
	Servers []MCPServer `json:"servers,omitempty"`
}

func mcpPath() string { return filepath.Join(McpDir(), "mcps.json") }

// winReservedNames 是 Windows 保留设备名（带任意扩展名仍无效），文件名避让。
var winReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// McpFileName 由服务名生成配置文件名（过滤 Windows 非法字符、结尾点/空格
// 与保留设备名：结尾的点/空格会被文件系统吞掉造成重名覆盖，con/nul 等
// 保留名即使带 .json 扩展名也无法创建）。
func McpFileName(name string) string {
	rep := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	n := strings.TrimSpace(rep.Replace(name))
	n = strings.TrimRight(n, ". ")
	if isWinReserved(strings.TrimSuffix(n, filepath.Ext(n))) {
		n = "_" + n
	}
	if n == "" || n == ".json" {
		n = "mcp"
	}
	return n + ".json"
}

// isWinReserved 判断（不含扩展名的）文件名是否为 Windows 保留设备名（大小写不敏感）。
func isWinReserved(base string) bool {
	return winReservedNames[strings.ToLower(strings.TrimSpace(base))]
}

// WriteMcpFile 把文本内容原样写入 data/mcp 下指定配置文件。
func WriteMcpFile(fileName, content string) error {
	// 内置路径清洗：不接受路径分隔符与相对路径成分，防止 `../` 穿越出 data/mcp
	name := strings.TrimSpace(fileName)
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return fmt.Errorf("非法的 MCP 配置文件名: %q", fileName)
	}
	if err := os.MkdirAll(McpDir(), 0o755); err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(name), ".json") {
		name += ".json"
	}
	return writeFileAtomic(filepath.Join(McpDir(), name), []byte(content), 0o644)
}

// loadMcpBaseURL 读取 mcps.json 中的路由器接入地址。
func loadMcpBaseURL() string {
	b, err := os.ReadFile(mcpPath())
	if err != nil {
		return ""
	}
	var c McpConfig
	if json.Unmarshal(b, &c) != nil {
		return ""
	}
	return c.BaseURL
}

// saveMcpBaseURL 仅写入路由器接入地址。
func saveMcpBaseURL(base string) error {
	b, err := json.MarshalIndent(McpConfig{BaseURL: base}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(McpDir(), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(mcpPath(), b, 0o644)
}

// writeMcpServerFile 把单个 MCP 服务写成独立配置文件。
func writeMcpServerFile(s MCPServer) error {
	if err := os.MkdirAll(McpDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(McpDir(), McpFileName(s.Name)), b, 0o644)
}

// ensureMcpLayout 旧版迁移：mcps.json 内嵌 servers 时拆分为独立文件，
// 并把 mcps.json 重写为只含 baseUrl。无内嵌列表时不做任何事。
func ensureMcpLayout() {
	b, err := os.ReadFile(mcpPath())
	if err != nil {
		return
	}
	var c McpConfig
	if json.Unmarshal(b, &c) != nil || len(c.Servers) == 0 {
		return
	}
	for _, s := range c.Servers {
		_ = writeMcpServerFile(s)
	}
	_ = saveMcpBaseURL(c.BaseURL)
}

// readMcpDirServers 扫描 data/mcp 下所有 *.json 配置文件（mcps.json 除外），
// 解析为 MCP 服务列表，按文件名排序。无法解析的文件跳过；
// 文件内 name 为空时以文件名（去扩展名）作为服务名。
func readMcpDirServers() []MCPServer {
	entries, err := os.ReadDir(McpDir())
	if err != nil {
		return []MCPServer{}
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		if strings.EqualFold(e.Name(), "mcps.json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	servers := make([]MCPServer, 0, len(names))
	for _, fn := range names {
		base := strings.TrimSuffix(fn, filepath.Ext(fn))
		b, err := os.ReadFile(filepath.Join(McpDir(), fn))
		if err != nil || len(strings.TrimSpace(string(b))) == 0 {
			// 空文件：仍显示为条目（以文件名为服务名），等待用户编辑补全
			servers = append(servers, MCPServer{Name: base})
			continue
		}
		var s MCPServer
		if json.Unmarshal(b, &s) != nil {
			servers = append(servers, MCPServer{Name: base})
			continue
		}
		if s.Type == "" && s.URL == "" && s.Command == "" {
			// 可能是 {servers:[…]} 包装形式：把全部服务展开为独立条目，
			// 只取第一个会导致其余服务在随后 SaveMcps 重写时被永久删除
			var w struct {
				Servers []MCPServer `json:"servers"`
			}
			if json.Unmarshal(b, &w) == nil && len(w.Servers) > 0 {
				for _, ws := range w.Servers {
					ss := ws
					if strings.TrimSpace(ss.Name) == "" {
						ss.Name = base
					}
					servers = append(servers, ss)
				}
				continue
			}
			servers = append(servers, MCPServer{Name: base})
			continue
		}
		if strings.TrimSpace(s.Name) == "" {
			s.Name = base
		}
		servers = append(servers, s)
	}
	return servers
}

// GetMcps 返回当前全部 MCP 配置（目录内所有配置文件 + 路由器地址）。
func GetMcps() (string, []MCPServer) {
	// 与 SaveMcps 互斥：避免读到写入到一半的目录状态
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	ensureMcpLayout()
	base := loadMcpBaseURL()
	servers := readMcpDirServers()
	return base, servers
}

// EnabledMcps 返回参与注入的 MCP 服务（禁用项跳过）。
func EnabledMcps() []MCPServer {
	_, all := GetMcps()
	out := make([]MCPServer, 0, len(all))
	for _, s := range all {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

// SaveMcps 保存 MCP 配置：baseUrl 写入 mcps.json；每个服务写成独立
// 配置文件，并删除列表中已不存在的服务文件。返回错误字符串。
func SaveMcps(baseURL string, servers []MCPServer) string {
	// 写服务文件→列目录→删除 keep 之外文件全程持锁：并发的 SaveMcps
	//（或与 MCP 注入）会在列目录与删除之间的窗口期误删刚写入的服务文件
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	if servers == nil {
		servers = []MCPServer{}
	}
	if err := saveMcpBaseURL(baseURL); err != nil {
		return "写入 MCP 配置失败: " + err.Error()
	}
	keep := map[string]bool{"mcps.json": true}
	used := map[string]bool{}
	for _, s := range servers {
		fn := McpFileName(strings.TrimSpace(s.Name))
		base := strings.TrimSuffix(fn, ".json")
		// 重名/保留名探测：mcps.json（不区分大小写）专存 baseUrl，服务文件
		// 占用它会导致配置永不被读出，视同重名顺延序号改名
		candidate, seq := fn, 1
		for used[strings.ToLower(candidate)] || strings.EqualFold(candidate, "mcps.json") {
			seq++
			candidate = base + "-" + itoa(seq) + ".json"
		}
		used[strings.ToLower(candidate)] = true
		fn = candidate
		keep[strings.ToLower(fn)] = true
		item := s
		item.Name = strings.TrimSpace(item.Name)
		if err := writeFileAtomic(filepath.Join(McpDir(), fn), mustJSON(item), 0o644); err != nil {
			return "写入 MCP 配置失败: " + err.Error()
		}
	}
	// 清理已删除服务的残留文件
	entries, _ := os.ReadDir(McpDir())
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		if !keep[strings.ToLower(e.Name())] {
			_ = os.Remove(filepath.Join(McpDir(), e.Name()))
		}
	}
	return ""
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
