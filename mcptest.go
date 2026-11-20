package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	agentcore "llm-router-go/agent/core"
)

// mcpTestTimeout MCP 启动探测总超时：stdio 型首次 npx 拉取可能较慢，给 30s。
const mcpTestTimeout = 30 * time.Second

// mcpInitRequest MCP 握手请求（JSON-RPC initialize），成功响应即视为服务可启动。
var mcpInitRequest = []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"LLM Router","version":"1.0.0"}}}` + "\n")

// TestMcpServer 测试指定 MCP 服务能否正常启动并响应 MCP 握手。
// 返回 {ok, error, ms, detail}；detail 为服务返回的简短信息。
func (a *App) TestMcpServer(name string) map[string]any {
	_, servers := agentcore.GetMcps()
	for _, s := range servers {
		if s.Name != name {
			continue
		}
		start := time.Now()
		err := testMcpServer(s)
		res := map[string]any{"ok": err == nil, "ms": time.Since(start).Milliseconds()}
		if err != nil {
			res["error"] = err.Error()
		}
		return res
	}
	return map[string]any{"ok": false, "error": "MCP 服务不存在: " + name}
}

// testMcpServer 按类型分派探测。
func testMcpServer(s agentcore.MCPServer) error {
	switch strings.ToLower(strings.TrimSpace(s.Type)) {
	case "stdio":
		return testMcpStdio(s)
	case "sse", "http":
		return testMcpHTTP(s)
	default:
		return fmt.Errorf("未知的 MCP 类型: %s", s.Type)
	}
}

// testMcpStdio 启动 stdio 命令，写入 initialize 并等待合法 JSON-RPC 响应。
func testMcpStdio(s agentcore.MCPServer) error {
	if strings.TrimSpace(s.Command) == "" {
		return fmt.Errorf("未配置启动命令")
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpTestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	hideConsole(cmd)
	cmd.Env = append(os.Environ(), mcpExtraEnv(s)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动失败: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	if _, err := stdin.Write(mcpInitRequest); err != nil {
		return fmt.Errorf("发送握手失败: %w", err)
	}
	_ = stdin.Close()

	// 逐行读取，跳过非 JSON 日志行；找到 id=1 的响应即成功。
	lineCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			select {
			case lineCh <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
		close(lineCh)
	}()
	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				return fmt.Errorf("进程退出但未返回握手响应")
			}
			if msg := mcpInitResponse(line); msg != "" {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("启动超时（%s），未收到握手响应", mcpTestTimeout)
		}
	}
}

// mcpInitResponse 判断一行是否为 initialize 的成功响应，返回简短描述。
func mcpInitResponse(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return ""
	}
	var v map[string]any
	if json.Unmarshal([]byte(line), &v) != nil {
		return ""
	}
	if v["error"] != nil {
		return ""
	}
	if id, ok := v["id"].(float64); !ok || int(id) != 1 {
		return ""
	}
	result, ok := v["result"].(map[string]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, 2)
	if info, ok := result["serverInfo"].(map[string]any); ok {
		if n, _ := info["name"].(string); n != "" {
			parts = append(parts, n)
		}
	}
	if v, _ := result["protocolVersion"].(string); v != "" {
		parts = append(parts, v)
	}
	if len(parts) == 0 {
		return "ok"
	}
	return strings.Join(parts, " ")
}

// testMcpHTTP 对 sse/http 型服务发 initialize POST；2xx 且返回 JSON-RPC 响应即成功。
func testMcpHTTP(s agentcore.MCPServer) error {
	if strings.TrimSpace(s.URL) == "" {
		return fmt.Errorf("未配置服务地址")
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpTestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(mcpInitRequest[:len(mcpInitRequest)-1]))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range mcpExtraHeaders(s) {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bytes.TrimSpace(body)))
	}
	// SSE 响应首行可能是 event:/data:，提取 data 行再判断
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
		if mcpInitResponse(line) != "" {
			return nil
		}
	}
	return fmt.Errorf("已连接但未返回合法握手响应: %s", string(bytes.TrimSpace(body)))
}

// mcpExtraEnv 从 Extra 里读 env 对象，转成 KEY=VALUE 环境变量列表。
func mcpExtraEnv(s agentcore.MCPServer) []string {
	raw, ok := s.Extra["env"]
	if !ok {
		return nil
	}
	var env map[string]any
	if json.Unmarshal(raw, &env) != nil {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+fmt.Sprint(v))
	}
	return out
}

// mcpExtraHeaders 从 Extra 里读 headers 对象，转成 HTTP 请求头。
func mcpExtraHeaders(s agentcore.MCPServer) map[string]string {
	out := map[string]string{}
	raw, ok := s.Extra["headers"]
	if !ok {
		return out
	}
	var headers map[string]any
	if json.Unmarshal(raw, &headers) != nil {
		return out
	}
	for k, v := range headers {
		out[k] = fmt.Sprint(v)
	}
	return out
}
