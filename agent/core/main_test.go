package core

import (
	"os"
	"testing"
)

// TestMain 把所有测试的 data 目录指向临时目录。
// SkillsDir/McpDir 现已落在用户配置目录（%APPDATA%\LLMRouter\data），测试若继续
// 用默认值会读写真实用户数据；LLMROUTER_DATA 优先级最高，正好用来隔离。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "llmrouter-test-data-")
	if err != nil {
		os.Exit(1)
	}
	_ = os.Setenv("LLMROUTER_DATA", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
