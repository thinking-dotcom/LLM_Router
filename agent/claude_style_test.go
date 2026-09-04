package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claude-code / claude-desktop 形态：无 configFile（参考模版），编辑源即真实 McpFile。
// GetTargetConfig 应回退读 McpFile；SaveTargetConfig 应备份后写回。
// 用临时注册的 fileMcpTarget 模拟该形态，不触碰真实 ~/.claude。
func TestClaudeStyleTargetReadWriteFallback(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "settings.json")
	original := `{"model":"claude","permissions":{"defaultMode":"default"},"env":{}}`
	if err := os.WriteFile(real, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	// 模拟 claude-code：无 configFile，仅 mcpFile
	RegisterTarget(&fileMcpTarget{id: "test-claude-style", name: "Claude Style", apiFormat: "/aai", mcpFile: real})
	defer delete(registry, "test-claude-style")

	// 读取：ConfigFile 为空 → 回退 McpFile
	content, exists, fp, errMsg := GetTargetConfig("test-claude-style")
	if errMsg != "" || !exists {
		t.Fatalf("GetTargetConfig: exists=%v err=%q", exists, errMsg)
	}
	if fp != real {
		t.Fatalf("filePath mismatch: %s != %s", fp, real)
	}
	if !strings.Contains(content, "permissions") {
		t.Fatalf("content mismatch: %s", content)
	}

	// 保存：备份 + 写回同一文件
	if errMsg := SaveTargetConfig("test-claude-style", `{"model":"claude","env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:3000/aai"}}`); errMsg != "" {
		t.Fatalf("SaveTargetConfig: %s", errMsg)
	}
	b, _ := os.ReadFile(real)
	if !strings.Contains(string(b), "ANTHROPIC_BASE_URL") {
		t.Fatalf("save did not write: %s", b)
	}
	// 备份文件存在
	if _, err := os.Stat(real + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	// 非法 JSON 拒绝且不改文件
	before, _ := os.ReadFile(real)
	if errMsg := SaveTargetConfig("test-claude-style", "{bad"); errMsg == "" {
		t.Fatal("invalid JSON should reject")
	}
	after, _ := os.ReadFile(real)
	if string(after) != string(before) {
		t.Fatalf("invalid save modified file: %s", after)
	}
}
