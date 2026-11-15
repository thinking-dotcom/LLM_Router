package clients

import (
	"os"
	"strings"
	"testing"
)

// commandcode 注册形态校验：非占位、JSON 格式、ConfigFile 指向真实主配置
// （~/.commandcode/config.json）、MCP 走独立真实文件（~/.commandcode/mcp.json）、
// 模板仅作首次初始值（PresetFile）。
func TestCommandcodeTargetRegistered(t *testing.T) {
	info := targetInfoOf(t, "commandcode")
	if info.Placeholder {
		t.Fatal("commandcode still placeholder")
	}
	if info.ConfigFormat != "json" {
		t.Errorf("ConfigFormat = %q, want json", info.ConfigFormat)
	}
	if info.ConfigFile != commandcodeRealFile() {
		t.Errorf("ConfigFile = %q, want real path %q", info.ConfigFile, commandcodeRealFile())
	}
	if info.PresetFile != commandcodeTemplateFile() {
		t.Errorf("PresetFile = %q, want template path %q", info.PresetFile, commandcodeTemplateFile())
	}
	if info.McpFile != commandcodeMcpRealFile() || !strings.HasSuffix(info.McpFile, "mcp.json") {
		t.Errorf("McpFile = %q, want ~/.commandcode/mcp.json", info.McpFile)
	}
	if !info.McpSupported {
		t.Error("McpSupported = false, want true")
	}
	b, err := os.ReadFile(commandcodeTemplateFile())
	if err != nil {
		t.Fatalf("template missing: %v", err)
	}
	if !strings.Contains(string(b), `"reasoningEffort"`) {
		t.Errorf("template missing reasoningEffort key: %s", b)
	}
}

// settings.json / providers.json 读写：JSON 校验拒绝非法内容；合法内容写回模版
// 并部署到真实路径（本测试通过 LLMROUTER_JSON_MODLES_DIR / 临时 client-paths
// 不可行，真实路径指向 ~/.commandcode，仅校验模版侧回写内容）。
func TestCommandcodeSettingsProvidersValidation(t *testing.T) {
	if errMsg := SaveCommandcodeSettings("{bad json"); errMsg == "" {
		t.Error("SaveCommandcodeSettings accepted invalid JSON")
	}
	if errMsg := SaveCommandcodeProviders("{bad json"); errMsg == "" {
		t.Error("SaveCommandcodeProviders accepted invalid JSON")
	}
	// 模版文件不应被非法内容破坏
	if b, err := os.ReadFile(commandcodeSettingsTemplateFile()); err == nil && strings.TrimSpace(string(b)) == "{bad json" {
		t.Error("invalid content leaked into settings template")
	}
}
