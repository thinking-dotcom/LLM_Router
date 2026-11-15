package clients

import (
	"llm-router-go/agent/core"

	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Antigravity CLI 的 MCP 配置位于全局 ~/.gemini/config/mcp_config.json 顶层的
// mcpServers 键（官方约定；stdio 用 command/args，远程（SSE/HTTP）用 serverUrl）。
// 采用字节级定点替换（upsertJSONObjectKey）：只改动 mcpServers 键的值，
// 其余键的键序 / 缩进 / 数字格式原样保留，不把用户手写的配置文件整体重排。
// 文件为空或不存在时按空对象初始化；首次注入前备份原始文件。

// mergeAgyMcp 把 servers 合并进 mcp_config.json 的 mcpServers 键，
// 文件其余内容原样保留。
func mergeAgyMcp(mcpFile string, servers []core.MCPServer) error {
	// 读-改-写全程持锁：与 SaveTargetConfig 等并发时避免后写覆盖先写
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	if mcpFile == "" {
		return nil
	}
	b, rerr := os.ReadFile(mcpFile)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rerr
	}
	// 首次注入前备份原始状态（已有备份则保留首次），供手动回退
	if _, err := core.BackupIfAbsent(mcpFile); err != nil {
		return err
	}
	doc := core.StripBOM(b)
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}")
	}
	newDoc, err := core.UpsertJSONObjectKey(doc, "mcpServers", func(existing json.RawMessage) (json.RawMessage, error) {
		// 替换语义：值整体替换为本次所选服务（旧条目清掉），与 MCP 页所配一致
		obj := map[string]any{}
		for _, s := range servers {
			obj[s.Name] = agyServerToAny(s)
		}
		return json.Marshal(obj)
	})
	if err != nil {
		return fmt.Errorf("解析 %s 失败，已拒绝覆写以免丢失现有配置: %w", mcpFile, err)
	}
	if err := os.MkdirAll(filepath.Dir(mcpFile), 0o755); err != nil {
		return err
	}
	return core.WriteFileAtomic(mcpFile, newDoc, 0o600)
}

// agyServerToAny 转换单个服务为 Antigravity CLI 的 mcpServer 配置对象。
func agyServerToAny(s core.MCPServer) map[string]any {
	switch strings.ToLower(s.Type) {
	case "stdio":
		m := map[string]any{"command": s.Command}
		if len(s.Args) > 0 {
			m["args"] = s.Args
		}
		return m
	default: // sse / http
		return map[string]any{"serverUrl": s.URL}
	}
}

// GetAgyMcp 读取真实 mcp_config.json（~/.gemini/config/mcp_config.json）；
// 真实文件缺失时返回空对象作为初始内容（exists=false，filePath 为真实路径）。
func GetAgyMcp() (content string, exists bool, filePath string, errMsg string) {
	fp := agyMcpRealFile()
	if fp == "" {
		return "", false, "", "未配置 agy 的 mcp 路径"
	}
	return core.GetRealText(fp, "")
}

// SaveAgyMcp 校验并保存 agy MCP 配置：直写真实路径 ~/.gemini/config/mcp_config.json。
// 按原文落盘（保留键序/精度），持锁串行化。
func SaveAgyMcp(content string) string {
	out, errStr := core.NormalizeJSONSave(content)
	if errStr != "" {
		return errStr
	}
	core.LockConfigFile()
	defer core.UnlockConfigFile()
	if err := writeAgyDeploy(agyMcpRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
