package clients

// 从客户端 MCP 配置文件中剔除单个服务（「踢出」按钮）：
// JSON/JSONC 走 mcpServers / mcp 键的定点删除（其余服务与文件其它键原样保留）；
// TOML 主配置走段落手术（见 toml_mcp.go 的 RemoveTomlMcpServer）。
// 与注入同属「整体替换法」的写盘口径：读现值 → 剔除目标 → 原子写回。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"llm-router-go/agent/core"
)

// RemoveMcpFromClient 从指定客户端的 MCP 配置文件中剔除单个服务。
func RemoveMcpFromClient(clientID, serverName string) error {
	if strings.TrimSpace(serverName) == "" {
		return errors.New("服务名为空")
	}
	path, err := core.ClientMcpPath(clientID)
	if err != nil {
		return err
	}
	return removeMcpFromFile(path, serverName)
}

// removeMcpFromFile 按 JSON/JSONC 或 TOML 分派到对应剔除实现。
func removeMcpFromFile(path, serverName string) error {
	if strings.EqualFold(filepath.Ext(path), ".toml") {
		return RemoveTomlMcpServer(path, serverName)
	}
	return removeMcpJSONFile(path, serverName)
}

// removeMcpJSONFile 在 JSON/JSONC 配置的 mcpServers / mcp 键下定点删除单个服务。
func removeMcpJSONFile(path, serverName string) error {
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return errors.New("该客户端的 MCP 配置文件不存在")
		}
		return rerr
	}
	doc := core.StripBOM(b)
	if len(bytes.TrimSpace(doc)) == 0 {
		return errors.New("配置为空")
	}
	// 与其它写盘路径一致：首次改动前留一份 .bak。JSONC 落盘会退化成严格
	// JSON（注释与尾逗号丢失），没有备份就永久找不回来；备份失败即中止。
	if _, berr := core.BackupIfAbsent(path); berr != nil {
		return berr
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(doc, &top); err != nil {
		// JSONC（含注释）：剥离后按严格 JSON 处理，其余键值语义保留
		strict, cerr := core.JSONCToJSON(doc)
		if cerr != nil || json.Unmarshal(strict, &top) != nil {
			return fmt.Errorf("解析 %s 失败，已拒绝覆写: %w", path, err)
		}
		doc = strict
	}
	var errNotInFile = errors.New("该客户端配置中没有此服务")
	for _, key := range []string{"mcpServers", "mcp"} {
		if _, ok := top[key]; !ok {
			continue
		}
		newDoc, uerr := core.UpsertJSONObjectKey(doc, key, func(existing json.RawMessage) (json.RawMessage, error) {
			m := map[string]json.RawMessage{}
			if len(bytes.TrimSpace(existing)) > 0 {
				if err := json.Unmarshal(existing, &m); err != nil {
					m = map[string]json.RawMessage{}
				}
			}
			if _, ok := m[serverName]; !ok {
				return nil, errNotInFile
			}
			delete(m, serverName)
			return json.Marshal(m)
		})
		if uerr != nil {
			return uerr
		}
		return core.WriteFileAtomic(path, newDoc, 0o600)
	}
	return errors.New("未找到 mcpServers / mcp 配置键")
}

// EjectAllMcp 一键踢出：清空所有客户端 MCP 配置文件里的全部服务（整体替换法——
// JSON 的 mcpServers / mcp 键整体替换为空对象、TOML 摘除全部 [mcp_servers.*] 段；
// 文件其它内容原样保留）。文件不存在的客户端视为无可踢出，跳过。
// 返回（实际清空的文件数, 首个错误描述——空串为全部成功）。
func EjectAllMcp() (int, string) {
	n := 0
	firstErr := ""
	for _, id := range core.McpCapableTargetIDs() {
		path, err := core.ClientMcpPath(id)
		if err != nil {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue // 配置文件不存在：未注入过，跳过
		}
		if err := clearMcpFile(path); err != nil {
			if firstErr == "" {
				firstErr = id + ": " + err.Error()
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// clearMcpFile 清空单个配置文件里的全部 MCP 服务。
func clearMcpFile(path string) error {
	if strings.EqualFold(filepath.Ext(path), ".toml") {
		return RemoveTomlMcpServer(path, "")
	}
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc := core.StripBOM(b)
	if len(bytes.TrimSpace(doc)) == 0 {
		return errors.New("配置为空")
	}
	// 清空同样是破坏性写入：先留首次备份（JSONC 会退化成严格 JSON）
	if _, berr := core.BackupIfAbsent(path); berr != nil {
		return berr
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(doc, &top); err != nil {
		strict, cerr := core.JSONCToJSON(doc)
		if cerr != nil || json.Unmarshal(strict, &top) != nil {
			return fmt.Errorf("解析 %s 失败，已拒绝覆写: %w", path, err)
		}
		doc = strict
	}
	for _, key := range []string{"mcpServers", "mcp"} {
		if _, ok := top[key]; !ok {
			continue
		}
		newDoc, uerr := core.UpsertJSONObjectKey(doc, key, func(json.RawMessage) (json.RawMessage, error) {
			return json.Marshal(map[string]json.RawMessage{})
		})
		if uerr != nil {
			return uerr
		}
		return core.WriteFileAtomic(path, newDoc, 0o600)
	}
	return nil // 没有 mcp 配置键：视为已空，无需写盘
}
