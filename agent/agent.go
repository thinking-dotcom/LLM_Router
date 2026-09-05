// Package agent 是 LLM Router 的多目标 Agent 配置适配器（模块化新功能）。
//
// 它负责管理可执行文件同目录 data/ 下的 skills 与 mcp 全局配置，并把它们
// 手动注入（或撤销）到各 Agent CLI 客户端的本地配置目录（Claude Code、
// OpenCode、Gemini CLI、pi、Grok Build、Codex、Cline、
// Hermes、Command Code、Kimi Code、Copilot CLI、Kilo），使这些客户端能直接使用本路由器。
//
// 本包完全独立于 config/format/router/core：不修改任何既有配置结构与转发
// 逻辑，skills/mcp 仅存储与管理，不注入上游请求。data 目录靠懒加载自动创建。
package agent

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	dataDirOnce sync.Once
	dataDirVal  string
)

// DataDir 返回 data 根目录（可执行文件同目录下的 data/；环境变量
// LLMROUTER_DATA 优先）。懒加载并自动创建 data/skills 与 data/mcp 子目录，
// 符合"不存在则自动创建"且对主程序启动无副作用。
func DataDir() string {
	dataDirOnce.Do(func() {
		dir := os.Getenv("LLMROUTER_DATA")
		if dir == "" {
			exe, err := os.Executable()
			if err == nil && exe != "" {
				dir = filepath.Join(filepath.Dir(exe), "data")
			} else {
				dir = filepath.Join("data")
			}
		}
		dataDirVal = dir
		_ = os.MkdirAll(filepath.Join(dir, "skills"), 0o755)
		_ = os.MkdirAll(filepath.Join(dir, "mcp"), 0o755)
	})
	return dataDirVal
}

// SkillsDir 返回 skills 数据目录。
func SkillsDir() string { return filepath.Join(DataDir(), "skills") }

// McpDir 返回 mcp 数据目录。
func McpDir() string { return filepath.Join(DataDir(), "mcp") }

// HomeDir 返回当前用户主目录（用于定位 ~/.claude、~/.agent 等）。
func HomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		h = os.Getenv("USERPROFILE")
	}
	return h
}
