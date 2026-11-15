package clients

import (
	"llm-router-go/agent/core"

	"os"
	"path/filepath"
)

// OpenCode 双文件模型（与 gemini 一致）：
//   - 参考模版 agent/Json_modles/opencode/opencode.json 是「编辑源」：
//     编辑窗口的表单与 JSON 源码页签读写它；
//   - 保存时把内容清空覆写真实路径 ~/.config/opencode/opencode.json；
//   - 外部通道直接修改真实文件（如 MCP 注入）后，需把真实文件回写模版保持同步。

// opencodeTemplateFile 返回参考模版文件路径。
func opencodeTemplateFile() string {
	return filepath.Join(core.JSONModelsDir("opencode"), "opencode.json")
}

// opencodeRealFile 返回真实配置文件路径（client-paths.json：opencode/config）。
func opencodeRealFile() string {
	return core.ClientPath("opencode", "config")
}

// writeOpencodeDeploy 原子覆写目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。
func writeOpencodeDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return core.WriteFileAtomic(fp, out, 0o600)
}
