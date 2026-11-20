package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// McpCapableTargetIDs 返回所有支持 MCP 注入的客户端 id（注册表顺序），
// 供「一键踢出」遍历所有客户端的 MCP 配置文件。
func McpCapableTargetIDs() []string {
	var ids []string
	for _, a := range registry {
		info := a.Info()
		if info.McpSupported && !info.Placeholder {
			ids = append(ids, info.ID)
		}
	}
	return ids
}

// ClientMcpPath 返回指定客户端登记的 MCP 配置文件路径（已展开 ~），
// 供前端「点击客户端图标打开文件位置」使用。
func ClientMcpPath(id string) (string, error) {
	a, ok := registry[id]
	if !ok {
		return "", errors.New("未注册的智能体: " + id)
	}
	info := a.Info()
	if !info.McpSupported || info.McpFile == "" {
		return "", errors.New("该智能体未登记 MCP 配置文件")
	}
	return info.McpFile, nil
}

// saveMergedMcpFile 把所选服务组装成标准 {"mcpServers": {...}} 文档，落盘为
// 「配置好的 MCP 合并文件」（data/mcp-merged.json）。注入以它为源替换各客户端
// 的 MCP 配置；文件本身也可手工复制给任何支持 mcpServers 格式的程序使用。
// 写失败只影响这份留档（不影响注入），静默忽略。
func saveMergedMcpFile(servers []MCPServer) {
	obj := make(map[string]any, len(servers))
	for _, s := range servers {
		obj[s.Name] = mcpServerToAny(s)
	}
	b, err := json.MarshalIndent(map[string]any{"mcpServers": obj}, "", "  ")
	if err != nil {
		return
	}
	_ = writeFileAtomic(filepath.Join(filepath.Dir(McpDir()), "mcp-merged.json"), b, 0o600)
}

// McpInjectedTargets 扫描各客户端的 MCP 配置文件（路径取自注册表），提取其
// 当前配置的服务名，返回 {服务名: [客户端id…]}——供前端在 MCP 列表标注
// 「已注入到哪些客户端」。文件缺失/解析失败按未注入处理，不报错。
func McpInjectedTargets() map[string][]string {
	out := map[string][]string{}
	for _, a := range registry {
		info := a.Info()
		if !info.McpSupported || info.Placeholder || info.McpFile == "" {
			continue
		}
		for _, name := range mcpNamesInFile(info.McpFile) {
			out[name] = append(out[name], info.ID)
		}
	}
	return out
}

// mcpNamesInFile 提取一个客户端 MCP 配置文件里的服务名：JSON/JSONC 读顶层
// mcpServers 键（mimocode 用 mcp 键，两者都试）；TOML 读 [mcp_servers.*] 段名。
func mcpNamesInFile(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if strings.EqualFold(filepath.Ext(path), ".toml") {
		var doc map[string]any
		if _, err := toml.Decode(string(b), &doc); err != nil {
			return nil
		}
		t, _ := doc["mcp_servers"].(map[string]any)
		names := make([]string, 0, len(t))
		for k := range t {
			names = append(names, k)
		}
		return names
	}
	strict, err := JSONCToJSON(StripBOM(b))
	if err != nil {
		return nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(strict, &doc); err != nil {
		return nil
	}
	for _, key := range []string{"mcpServers", "mcp"} {
		raw, ok := doc[key]
		if !ok {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err == nil {
			names := make([]string, 0, len(m))
			for k := range m {
				names = append(names, k)
			}
			return names
		}
	}
	return nil
}

// TargetOp 是一次注入/撤销操作：对指定目标客户端执行注入或撤销。
type TargetOp struct {
	TargetID string `json:"targetId"`
	Action   string `json:"action"` // inject / undo
}

// TargetResult 是单目标执行后的结果，供前端展示/toast。
type TargetResult struct {
	TargetID       string `json:"targetId"`
	Name           string `json:"name"`
	InjectedSkills int    `json:"injectedSkills"`
	InjectedMcp    int    `json:"injectedMcp"`
	Error          string `json:"error,omitempty"`
}

// SyncTargets 对一批目标客户端执行注入或撤销（手动触发）。
// routerBase 为本路由器接入地址；skills/mcp 数据来自 data 目录。
// 返回每个目标的执行结果。单个目标出错不影响其余目标。
func SyncTargets(ops []TargetOp, routerBase string) []TargetResult {
	res := make([]TargetResult, 0, len(ops))
	for _, op := range ops {
		a, ok := registry[op.TargetID]
		if !ok {
			res = append(res, TargetResult{TargetID: op.TargetID, Name: op.TargetID, Error: "未注册的目标智能体"})
			continue
		}
		info := a.Info()
		r := TargetResult{TargetID: info.ID, Name: info.Name}
		if info.Placeholder {
			r.Error = "该客户端尚未接入（占位）"
			res = append(res, r)
			continue
		}
		switch op.Action {
		case "undo":
			if err := undoTarget(a); err != nil {
				r.Error = err.Error()
			}
		default: // inject
			// 注入失败时不再上报成功数量，仅统计真正成功的注入
			if err := injectTarget(a, routerBase); err != nil {
				r.Error = err.Error()
			} else {
				skills := ListSkills()
				r.InjectedSkills = len(skills)
				r.InjectedMcp = len(EnabledMcps())
			}
		}
		res = append(res, r)
	}
	return res
}

// injectTarget 对一个目标执行 skills+mcp 的完整注入。
func injectTarget(a TargetAdapter, routerBase string) error {
	info := a.Info()
	if info.SkillsSupported {
		if err := a.InjectSkills(routerBase, ListSkills()); err != nil {
			return err
		}
	}
	if info.McpSupported {
		// 仅注入启用状态的 MCP 服务（禁用项跳过）
		servers := EnabledMcps()
		// 无 MCP 服务时跳过注入：TOML/YAML 注入器把空列表当「全量清除」，
		// 注入路径不得清空用户自有的 MCP 配置（与 JSON 合并语义一致）
		if len(servers) > 0 {
			if err := a.InjectMcp(routerBase, servers); err != nil {
				return err
			}
		}
	}
	return nil
}

// undoTarget 对一个目标执行 skills 撤销。
// （MCP 注入已无撤销机制：注入为合并式写入，撤销需恢复注入前备份，已移除。）
func undoTarget(a TargetAdapter) error {
	info := a.Info()
	if info.SkillsSupported {
		if err := a.UndoSkills(); err != nil {
			return err
		}
	}
	return nil
}

// SyncMcpMapping 按「客户端 ↔ MCP 服务」映射执行**替换式** MCP 注入：
// 把 serverNames 中列出的服务注入 targetIDs 中支持 mcp 的客户端——
// 客户端的 MCP 配置区被整体替换为所选服务（旧条目清除，不再合并追加）。
// 注入前先把所选服务组装成标准 mcpServers 文档落盘为「配置好的 MCP 合并文件」
// （data/mcp-merged.json，各客户端 MCP 文件路径登记于 Json_modles/client-paths.json）。
// 忽略未注册/占位/不支持 mcp 的目标；返回实际处理的目标结果。
func SyncMcpMapping(targetIDs []string, serverNames []string, routerBase string) []TargetResult {
	wantServer := make(map[string]bool, len(serverNames))
	for _, n := range serverNames {
		wantServer[n] = true
	}
	servers := EnabledMcps()
	filtered := make([]MCPServer, 0, len(servers))
	for _, s := range servers {
		if wantServer[s.Name] {
			filtered = append(filtered, s)
		}
	}
	saveMergedMcpFile(filtered)

	wantTarget := make(map[string]bool, len(targetIDs))
	for _, id := range targetIDs {
		wantTarget[id] = true
	}
	res := make([]TargetResult, 0, len(targetIDs))
	for _, a := range registry {
		info := a.Info()
		if !wantTarget[info.ID] || !info.McpSupported || info.Placeholder {
			continue
		}
		r := TargetResult{TargetID: info.ID, Name: info.Name}
		if len(filtered) > 0 {
			if err := a.InjectMcp(routerBase, filtered); err != nil {
				r.Error = err.Error()
			} else {
				r.InjectedMcp = len(filtered)
			}
		}
		res = append(res, r)
	}
	return res
}
