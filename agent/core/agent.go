// Package core 是「多目标 Agent 配置适配器」的共享内核。
//
// 职责边界（与 agent/clients 的分工，2026-09 拆分）：
//
//   - 本包只放与具体客户端无关的机制：目标注册表与适配器接口（targets.go）、
//     通用注入原语（skills / mcp 合并）、配置读写与备份、参考模版目录解析、
//     client-paths.json 路径解析、MCP / skills 存储、会话管理、CLI 安装与
//     版本探测、JSON / JSONC / TOML / YAML 校验与定点编辑、平台差异实现。
//   - 具体客户端（Claude Code、Codex、pi、Grok Build、OpenCode、
//     Command Code、Kimi Code、Copilot CLI、MimoCode、
//     Antigravity CLI、CodeBuddy CLI）的路径、注入算法与注册全在 agent/clients。
//   - 依赖方向单向：agent/clients → agent/core；本包不反向依赖任何客户端。
//     跨包只走 api.go 导出的原语与 Targets 相关导出类型。
//
// 它负责管理用户数据目录（Windows 为 %APPDATA%\LLMRouter\data）下的 skills 与
// mcp 全局配置，并把它们手动注入（或撤销）到各 Agent CLI 客户端的本地配置目录
// （Claude Code、OpenCode、pi、Grok Build、Codex、Command Code、Kimi Code、
// Copilot CLI），使这些客户端能直接使用本路由器。旧版把 data 放在可执行文件
// 同目录，首次启动会自动迁移（见 DataDir）。
//
// 本包完全独立于 config/format/router/core：不修改任何既有配置结构与转发
// 逻辑，skills/mcp 仅存储与管理，不注入上游请求。data 目录靠懒加载自动创建。
package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	dataDirOnce sync.Once
	dataDirVal  string
)

// dataDirMarker 迁移完成标记：已从旧版「可执行文件同目录 data」搬过数据，
// 之后不再重复搬运（也避免旧目录残留导致每次都覆盖新数据）。
const dataDirMarker = ".migrated-from-exe-dir"

// DataDir 返回 data 根目录。优先级：
//
//	LLMROUTER_DATA 环境变量（测试/便携用）
//	→ 用户配置目录 %APPDATA%\LLMRouter\data（推荐；安装目录通常不可写）
//	→ 可执行文件同目录 data/（兜底，例如读不到 UserConfigDir）
//
// 首次启动且新目录还没有内容时，自动把旧版 exe 同目录 data 整体迁移过来；
// 迁移失败继续用旧目录，保证技能/MCP 不会因为换位置而“消失”。
// 懒加载并自动创建 data/skills 与 data/mcp 子目录。
func DataDir() string {
	dataDirOnce.Do(func() {
		if dir := strings.TrimSpace(os.Getenv("LLMROUTER_DATA")); dir != "" {
			dataDirVal = dir
		} else {
			dataDirVal = resolveDataDir()
		}
		_ = os.MkdirAll(filepath.Join(dataDirVal, "skills"), 0o755)
		_ = os.MkdirAll(filepath.Join(dataDirVal, "mcp"), 0o755)
	})
	return dataDirVal
}

// resolveDataDir 选定数据根目录，并在需要时完成一次性迁移。
func resolveDataDir() string {
	legacy := legacyDataDir()
	target := userDataDir()
	if target == "" {
		return legacy
	}
	if err := migrateLegacyData(legacy, target); err != nil {
		return legacy // 迁移失败：保持旧位置可用，绝不丢数据
	}
	return target
}

// legacyDataDir 旧版位置：可执行文件同目录的 data/（构建产物目录下）。
func legacyDataDir() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "data"
	}
	return filepath.Join(filepath.Dir(exe), "data")
}

// userDataDir 用户级数据目录（Windows: %APPDATA%\LLMRouter\data）。
func userDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		return ""
	}
	return filepath.Join(base, "LLMRouter", "data")
}

// migrateLegacyData 把旧目录内容一次性搬到新目录：
// 目标已有内容（或已带标记）时直接跳过；搬运先落到 <target>.migrating 再
// 原子改名，避免中途失败留下“半套数据 + 有内容”的坏状态。
func migrateLegacyData(legacy, target string) error {
	legacy = filepath.Clean(legacy)
	target = filepath.Clean(target)
	if legacy == "" || target == "" || legacy == target {
		return nil
	}
	if fi, err := os.Stat(legacy); err != nil || !fi.IsDir() {
		return nil // 旧目录不存在：全新安装
	}
	if _, err := os.Stat(filepath.Join(target, dataDirMarker)); err == nil {
		return nil
	}
	if hasRegularFile(target) {
		return nil // 新目录已有数据：以它为准
	}
	tmp := target + ".migrating"
	_ = os.RemoveAll(tmp)
	if err := copyTree(legacy, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	_ = os.RemoveAll(target)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	note := "migrated from " + legacy + " at " + time.Now().Format("2006-01-02 15:04:05") + "\n"
	return os.WriteFile(filepath.Join(target, dataDirMarker), []byte(note), 0o644)
}

// hasRegularFile 判断目录下（含子目录）是否存在普通文件：只看文件，
// 自己预建的空 skills/ 与 mcp/ 不算“已有数据”。
func hasRegularFile(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		found = true
		return errors.New("stop")
	})
	return found
}

// copyTree 递归复制目录（保留权限位），目标目录不存在时自动创建。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		b, rerr2 := os.ReadFile(p)
		if rerr2 != nil {
			return rerr2
		}
		if werr := os.WriteFile(out, b, info.Mode().Perm()); werr != nil {
			return werr
		}
		return nil
	})
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
