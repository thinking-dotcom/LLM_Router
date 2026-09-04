package agent

import (
	"strings"
)

// Gemini CLI 的 .env 环境变量文件：直接读写真实文件 ~/.gemini/.env（无模板中转）。
// 保存时校验 KEY=VALUE 格式后原样写入。真实文件不存在时无预设（仓库中
// .env 模板默认不存在），返回 exists=false。

// GetGeminiDotEnv 读取真实 .env 内容；不存在返回空串与 exists=false。
func GetGeminiDotEnv() (content string, exists bool, filePath string, errMsg string) {
	return getRealText(geminiDotEnvRealFile(), "")
}

// SaveGeminiDotEnv 原样保存 .env：直写真实路径 ~/.gemini/.env。
func SaveGeminiDotEnv(content string) string {
	if err := ValidateDotEnv(content); err != nil {
		return ".env 格式错误：" + err.Error()
	}
	out := []byte(content)
	if strings.TrimSpace(content) == "" {
		out = nil
	} else if !strings.HasSuffix(content, "\n") {
		out = append(out, '\n')
	}
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	if err := writeGeminiDeploy(geminiDotEnvRealFile(), out); err != nil {
		return err.Error()
	}
	return ""
}
