package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Gemini CLI 的 MCP 配置位于主配置 settings.json 顶层的 mcpServers 键
//（Claude 风格；stdio 用 command/args，SSE 用 url，HTTP 用 httpUrl）。
// 采用字节级定点替换（upsertJSONObjectKey）：只改动 mcpServers 键的值，
// 其余键的键序 / 缩进 / 数字格式原样保留，不把用户手写的 settings.json
// 整体重排。settings.json 为严格 JSON；文件非严格 JSON（含注释等）时
// 拒绝覆写，避免把写到一半的活文件整体抹掉。首次注入前备份原始文件。

// mergeGeminiMcp 把 servers 合并进 settings.json 的 mcpServers 键，
// 文件其余内容原样保留；文件不存在或为空时按空对象初始化。
func mergeGeminiMcp(settingsFile string, servers []MCPServer) error {
	// 读-改-写全程持锁：与 SaveTargetConfig 等并发时避免后写覆盖先写
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	if settingsFile == "" {
		return nil
	}
	b, rerr := os.ReadFile(settingsFile)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rerr
	}
	// 首次注入前备份原始状态（已有备份则保留首次），供手动回退
	if _, err := backupIfAbsent(settingsFile); err != nil {
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
			obj[s.Name] = geminiServerToAny(s)
		}
		return json.Marshal(obj)
	})
	if err != nil {
		return fmt.Errorf("解析 %s 失败，已拒绝覆写以免丢失现有配置: %w", settingsFile, err)
	}
	if err := os.MkdirAll(filepath.Dir(settingsFile), 0o755); err != nil {
		return err
	}
	// settings.json 可能含 apiKey 等凭据，落盘权限收紧为 0600
	return writeFileAtomic(settingsFile, newDoc, 0o600)
}

// geminiServerToAny 转换单个服务为 Gemini CLI 的配置对象。
func geminiServerToAny(s MCPServer) map[string]any {
	switch strings.ToLower(s.Type) {
	case "stdio":
		m := map[string]any{"command": s.Command}
		if len(s.Args) > 0 {
			m["args"] = s.Args
		}
		return m
	case "http":
		return map[string]any{"httpUrl": s.URL}
	default: // sse
		return map[string]any{"url": s.URL}
	}
}
