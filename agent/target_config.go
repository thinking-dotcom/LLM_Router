package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// GetTargetConfig 读取目标客户端的真实主配置文件内容。
// 文件不存在返回空字符串与 exists=false，由前端提示“将新建”；
// 若该客户端带有首次初始值模板（PresetFile），则以模板内容作为初始值返回
// （exists 仍为 false，filePath 为真实路径）——模板仅作初始值来源，不是编辑/同步桥梁。
func GetTargetConfig(targetID string) (content string, exists bool, filePath string, errMsg string) {
	a, ok := registry[targetID]
	if !ok {
		return "", false, "", "未注册的目标客户端"
	}
	info := a.Info()
	fp := info.ConfigFile
	if fp == "" {
		fp = info.McpFile
	}
	if fp == "" {
		return "", false, "", "该客户端暂无可编辑的配置文件路径"
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		if os.IsNotExist(err) {
			if info.PresetFile != "" {
				if pb, perr := os.ReadFile(info.PresetFile); perr == nil && len(pb) > 0 {
					return string(pb), false, fp, ""
				}
			}
			return "", false, fp, ""
		}
		return "", false, fp, err.Error()
	}
	return string(b), true, fp, ""
}

// SaveTargetConfig 校验并保存目标客户端的真实主配置文件内容（ConfigFile 即真实
// 路径，不存在模板中转：表单/源码与真实文件直接双向同步）。
// JSON 配置（默认）校验通过后按原文直写，保留用户的键序与缩进；
// TOML / YAML 配置（Grok Build、Codex、Hermes）格式校验通过后直接保存原文。
// 全程持锁（与 MCP 注入等读-改-写路径互斥，避免并发丢失更新）。
func SaveTargetConfig(targetID, content string) string {
	a, ok := registry[targetID]
	if !ok {
		return "未注册的目标客户端"
	}
	info := a.Info()
	if info.Placeholder {
		return "该客户端尚未接入（占位），暂不支持编辑"
	}
	fp := info.ConfigFile
	if fp == "" {
		fp = info.McpFile
	}
	if fp == "" {
		return "该客户端暂无可编辑的配置文件路径"
	}
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	// TOML / YAML 配置以原文保存（不做 JSON 校验/重排），但保存前做格式校验
	if info.ConfigFormat == "toml" || info.ConfigFormat == "yaml" {
		if content != "" {
			var formatErr error
			if info.ConfigFormat == "toml" {
				// 先折叠跨行内联表（Grok 文档风格写法）再校验；通过后保存折叠结果，
				// 保证写盘内容是严格 TOML（客户端可直接解析）
				content = foldTomlInlineTables(content)
				formatErr = ValidateToml(content)
			} else {
				formatErr = ValidateYaml(content)
			}
			if formatErr != nil {
				return strings.ToUpper(info.ConfigFormat) + " 格式错误：" + formatErr.Error()
			}
		}
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			return err.Error()
		}
		if _, err := backupIfAbsent(fp); err != nil {
			return err.Error()
		}
		if err := writeFileAtomic(fp, []byte(content), 0o600); err != nil {
			return err.Error()
		}
		return ""
	}
	if content != "" {
		// 校验并给出具体出错位置；容忍文件头 BOM（部分编辑器会写入）
		payload := []byte(strings.TrimPrefix(content, "\uFEFF"))
		if info.ConfigFormat == "jsonc" {
			// JSONC（JSON with Comments，Kilo 等配置允许 // 与 /* */ 注释和尾逗号）：
			// 剥离注释/尾逗号后按严格 JSON 校验，顶层必须是对象。
			clean, jerr := jsoncToJSON(payload)
			if jerr != nil {
				return "JSONC 格式错误：" + jerr.Error()
			}
			var probe any
			if err := json.Unmarshal(clean, &probe); err != nil {
				return "JSONC 格式错误：" + err.Error()
			}
			if _, ok := probe.(map[string]any); !ok {
				return "JSONC 顶层必须是对象（{...}）"
			}
		} else {
			var probe any
			if err := json.Unmarshal(payload, &probe); err != nil {
				return "JSON 格式错误：" + err.Error()
			}
			// 顶层必须是对象：数组/标量虽是合法 JSON，但写入客户端主配置会让客户端启动解析失败
			if _, ok := probe.(map[string]any); !ok {
				return "JSON 顶层必须是对象（{...}）"
			}
		}
	}
	// 原文直写：前端表单联动已生成规范文本，此处不再重排；空内容写空对象。
	// 落盘前去掉 BOM（校验时已剥离容忍），部分客户端拒读带 BOM 的配置
	out := []byte(strings.TrimPrefix(content, "\uFEFF"))
	if content == "" {
		out = []byte("{}\n")
	}
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err.Error()
	}
	// 保存前备份：首次 .bak 保留最初状态。
	if _, err := backupIfAbsent(fp); err != nil {
		return err.Error()
	}
	if err := writeFileAtomic(fp, out, 0o600); err != nil {
		return err.Error()
	}
	return ""
}
