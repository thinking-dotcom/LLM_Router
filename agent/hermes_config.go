package agent

import (
	"os"
	"path/filepath"
)

// hermes 参考模版文件（agent/Json_modles/hermes/config.yaml）：
// Hermes Agent 主配置为 YAML 文本，保存前经 SaveTargetConfig 做 YAML 格式校验，原文直存。
// 模版文件是「编辑源」：加载时从它读取，保存时把内容写回它并清空覆写到真实路径。

// hermesTemplatesDir 返回 hermes 参考模版文件所在目录（agent/Json_modles/hermes）。
func hermesTemplatesDir() string { return jsonModelsDir("hermes") }

// hermesTemplateFile 返回 Hermes Agent 主配置参考模版路径。
func hermesTemplateFile() string { return filepath.Join(hermesTemplatesDir(), "config.yaml") }

// hermesRealFile 返回 Hermes Agent 真实配置路径（client-paths.json：hermes/config）。
func hermesRealFile() string { return clientPath("hermes", "config") }

// writeHermesDeploy 原子覆写目标文件（写临时文件后 rename，避免半截文件），
// 自动建目录，覆盖前不做备份。
func writeHermesDeploy(fp string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(fp, out, 0o600)
}
