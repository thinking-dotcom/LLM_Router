package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Kimi Code 的 MCP 声明在独立文件 ~/.kimi-code/mcp.json（用户级；项目级为
// .kimi-code/mcp.json）顶层 mcpServers 键下，结构为 Claude 风格对象：
//   - stdio：command + args（含 command 字段即视为 stdio）；
//   - HTTP：url（不写 transport）；
//   - SSE：url + transport = "sse"。
//
// 与 Gemini 分支一样采用字节级定点替换（upsertJSONObjectKey）：只改动
// mcpServers 键的值，其余键序 / 缩进 / 格式原样保留；mcp.json 为严格 JSON，
// 文件非严格 JSON（含注释等）时拒绝覆写，避免抹掉写到一半的活文件。
// 首次注入前备份原始文件。

// mergeKimiMcp 把 servers 合并进 mcp.json 的 mcpServers 键，文件其余内容原样
// 保留；文件不存在或为空时按空对象初始化。
func mergeKimiMcp(mcpFile string, servers []MCPServer) error {
	// 读-改-写全程持锁：与 SaveTargetConfig 等并发时避免后写覆盖先写
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	if mcpFile == "" {
		return nil
	}
	b, rerr := os.ReadFile(mcpFile)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rerr
	}
	// 首次注入前备份原始状态（已有备份则保留首次），供手动回退
	if _, err := backupIfAbsent(mcpFile); err != nil {
		return err
	}
	doc := stripBOM(b)
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}")
	}
	newDoc, err := upsertJSONObjectKey(doc, "mcpServers", func(existing json.RawMessage) (json.RawMessage, error) {
		obj := map[string]any{}
		if len(bytes.TrimSpace(existing)) > 0 {
			if err := json.Unmarshal(existing, &obj); err != nil {
				obj = map[string]any{}
			}
		}
		for _, s := range servers {
			obj[s.Name] = kimiServerToAny(s)
		}
		return json.Marshal(obj)
	})
	if err != nil {
		return fmt.Errorf("解析 %s 失败，已拒绝覆写以免丢失现有配置: %w", mcpFile, err)
	}
	if err := os.MkdirAll(filepath.Dir(mcpFile), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(mcpFile, newDoc, 0o600)
}

// kimiServerToAny 转换单个服务为 Kimi Code 的 mcp.json 配置对象。
func kimiServerToAny(s MCPServer) map[string]any {
	switch strings.ToLower(s.Type) {
	case "stdio":
		m := map[string]any{"command": s.Command}
		if len(s.Args) > 0 {
			m["args"] = s.Args
		}
		return m
	case "http":
		return map[string]any{"url": s.URL}
	default: // sse
		return map[string]any{"transport": "sse", "url": s.URL}
	}
}
