package agent

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
			res = append(res, TargetResult{TargetID: op.TargetID, Name: op.TargetID, Error: "未注册的目标客户端"})
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
				_, servers := GetMcps()
				r.InjectedMcp = len(servers)
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
		_, servers := GetMcps()
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

// SyncMcpMapping 按「客户端 ↔ MCP 服务」映射执行选择性 MCP 注入：
// 仅把 serverNames 中列出的服务注入 targetIDs 中支持 mcp 的客户端。
// 忽略未注册/占位/不支持 mcp 的目标；返回实际处理的目标结果。
func SyncMcpMapping(targetIDs []string, serverNames []string, routerBase string) []TargetResult {
	wantServer := make(map[string]bool, len(serverNames))
	for _, n := range serverNames {
		wantServer[n] = true
	}
	_, servers := GetMcps()
	filtered := make([]MCPServer, 0, len(servers))
	for _, s := range servers {
		if wantServer[s.Name] {
			filtered = append(filtered, s)
		}
	}

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
