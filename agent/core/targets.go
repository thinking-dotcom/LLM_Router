package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ---------- 目标适配器抽象 ----------

// TargetInfo 描述一个目标 Agent 客户端及其支持能力，供前端展示与勾选。
type TargetInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	APIFormat       string `json:"apiFormat"`       // 协议亲和标签 /oai /aai /gai（用于模型过滤；接入地址统一为 /router）
	ConfigFormat    string `json:"configFormat"`    // 主配置文件格式：json / toml（默认 json）
	SkillsSupported bool   `json:"skillsSupported"` // 是否支持注入 skills
	McpSupported    bool   `json:"mcpSupported"`    // 是否支持注入 mcp
	SkillsDir       string `json:"skillsDir,omitempty"`
	McpFile         string `json:"mcpFile,omitempty"`
	ConfigFile      string `json:"configFile,omitempty"`  // 主配置文件路径（编辑弹窗用，真实文件）
	PresetFile      string `json:"presetFile,omitempty"`  // 首次初始值模板（仅真实文件不存在时作为编辑初始内容，非同步桥梁）
	Placeholder     bool   `json:"placeholder,omitempty"` // 尚未接入(占位)

	// CLI 安装信息（客户端页卡片下半区「安装 / 更新 / 版本号」用）：
	// Installed/Version 为本机探测结果（读 npm 全局包或 PATH 查找，不起进程）
	Bin          string `json:"bin,omitempty"`
	Pkg          string `json:"pkg,omitempty"`     // npm 全局包名；空 = 非 npm 客户端
	Installed    bool   `json:"installed"`         // 本机是否已安装该 CLI
	Version      string `json:"version,omitempty"` // 已知版本号（可能为空，前端可再按需探测）
	CanInstall   bool   `json:"canInstall"`        // 是否支持一键安装/更新（npm 或已登记命令）
	CanUninstall bool   `json:"canUninstall"`      // 是否支持自动卸载（npm 包，或登记了二进制路径如 agy）
	// 命令通道客户端卸载时会删除的二进制路径（tooltip/弹窗展示用），npm 通道为空
	UninstallPaths string `json:"uninstallPaths,omitempty"`
	// 将执行的安装/更新命令（tooltip 透明展示）：npm 通道为 `npm install -g <pkg>[@latest]`，
	// 命令通道为登记的 argv（如 PowerShell 官方安装脚本、`agy update`）
	InstallCmd string `json:"installCmd,omitempty"`
	UpdateCmd  string `json:"updateCmd,omitempty"`
}

// TargetAdapter 是单个客户端适配器，负责将集中配置注入/撤销到该客户端。
type TargetAdapter interface {
	Info() TargetInfo
	// InjectSkills 把 data/skills 注入到该客户端 skills 目录（routerBase 为接入地址）。
	InjectSkills(routerBase string, skills []Skill) error
	// UndoSkills 撤销之前注入的 skills。
	UndoSkills() error
	// InjectMcp 把 MCP server 列表合并进该客户端 mcp 配置（routerBase 为接入地址）。
	InjectMcp(routerBase string, servers []MCPServer) error
}

// registry 目标适配器注册表。
var registry = map[string]TargetAdapter{}

// RegisterTarget 注册一个客户端适配器。
func RegisterTarget(a TargetAdapter) { registry[a.Info().ID] = a }

// ListTargets 返回全部已注册目标，按名称排序（附带 CLI 安装/版本探测结果）。
func ListTargets() []TargetInfo {
	out := make([]TargetInfo, 0, len(registry))
	for _, a := range registry {
		ti := a.Info()
		decorateInstallInfo(&ti)
		out = append(out, ti)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// clientNameOf 从注册表取某客户端的显示名（不带安装探测，纯查表）；未注册返回空串。
// 展示名以注册表为唯一来源，避免各页面（客户端卡片 / 会话管理 / 技能管理）各自维护一份名字。
func clientNameOf(id string) string {
	if a, ok := registry[id]; ok {
		return a.Info().Name
	}
	return ""
}

// ---------- 通用 skills/mcp 注入与撤销实现 ----------

// skillsInjectedMarker 记录注入清单的文件名（目标 skills 目录下）：
// 撤销时只删除本程序注入过的技能，避免误删用户自建的同名技能。
const skillsInjectedMarker = ".llmrouter-injected-skills.json"

// injectedSkillsManifest 记录注入到某目标 skills 目录的技能名。
type injectedSkillsManifest struct {
	Skills []string `json:"skills"`
	Time   string   `json:"time,omitempty"`
}

// loadInjectedSkillNames 读取注入清单中的技能名集合；清单缺失或损坏返回 nil。
func loadInjectedSkillNames(skillsDir string) map[string]bool {
	b, err := os.ReadFile(filepath.Join(skillsDir, skillsInjectedMarker))
	if err != nil {
		return nil
	}
	var m injectedSkillsManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	out := make(map[string]bool, len(m.Skills))
	for _, s := range m.Skills {
		out[s] = true
	}
	return out
}

// saveInjectedSkillNames 原子写注入清单。
func saveInjectedSkillNames(skillsDir string, names map[string]bool) error {
	list := make([]string, 0, len(names))
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	m := injectedSkillsManifest{Skills: list, Time: time.Now().Format(time.RFC3339)}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(skillsDir, skillsInjectedMarker), append(b, '\n'), 0o644)
}

// injectSkills 是注入 skills 的通用实现：把每个 data/skills/<name> 复制到
// 目标的 skills 目录下。
func injectSkills(skillsDir string, skills []Skill) error {
	if skillsDir == "" {
		return nil
	}
	var injected []string
	for _, s := range skills {
		if !validSkillName(s.Name) {
			continue
		}
		// 源路径优先取扫描结果里的真实路径（根目录散放 .md 技能带扩展名）
		src := s.Path
		if src == "" {
			src = filepath.Join(SkillsDir(), s.Name)
		}
		fi, statErr := os.Stat(src)
		if statErr != nil && os.IsNotExist(statErr) {
			continue
		}
		// 单文件技能（根目录散放的 .md）目标写成 <技能名>.md 保留扩展名；
		// 目录型技能仍复制为同名子目录
		dst := filepath.Join(skillsDir, s.Name)
		if statErr == nil && fi.Mode().IsRegular() {
			dst += ".md"
		}
		if err := copyDirRecursive(src, dst); err != nil {
			return err
		}
		injected = append(injected, s.Name)
	}
	if len(injected) == 0 {
		return nil
	}
	// 记录注入清单（合并已有条目）：撤销时只删清单内的技能副本，
	// 避免误删用户自建的同名文件；清单写失败仅致撤销回退旧逻辑
	names := loadInjectedSkillNames(skillsDir)
	if names == nil {
		names = map[string]bool{}
	}
	for _, n := range injected {
		names[n] = true
	}
	_ = saveInjectedSkillNames(skillsDir, names)
	return nil
}

// undoSkills 是撤销 skills 的通用实现：按注入清单删除目标 skills 目录下
// 本程序注入的技能副本（目录型删 <name>，单文件技能删 <name>.md）；
// 无清单（老版本注入或清单损坏）时回退旧逻辑：删除与 data/skills 同名的条目。
func undoSkills(skillsDir string) error {
	if skillsDir == "" {
		return nil
	}
	// 兜底：目标目录若解析到本程序的技能源目录（data/skills），一律不删——
	// 撤销注入只该清理客户端侧的副本，误把源目录当目标会把全部技能删光。
	if a, err1 := filepath.Abs(skillsDir); err1 == nil {
		if b, err2 := filepath.Abs(SkillsDir()); err2 == nil && a == b {
			return nil
		}
	}
	names := loadInjectedSkillNames(skillsDir)
	if names == nil {
		for _, s := range ListSkills() {
			if !validSkillName(s.Name) {
				continue
			}
			if err := removeIfExists(filepath.Join(skillsDir, s.Name)); err != nil {
				return err
			}
			if err := removeIfExists(filepath.Join(skillsDir, s.Name+".md")); err != nil {
				return err
			}
		}
		return nil
	}
	for n := range names {
		// 清单条目同样做路径校验：被篡改/损坏的清单不得让删除越出技能目录
		if !validSkillName(n) {
			continue
		}
		if err := removeIfExists(filepath.Join(skillsDir, n)); err != nil {
			return err
		}
		if err := removeIfExists(filepath.Join(skillsDir, n+".md")); err != nil {
			return err
		}
	}
	// 撤销完成，移除清单
	return os.Remove(filepath.Join(skillsDir, skillsInjectedMarker))
}

// injectMcp 是注入 mcp 的通用实现：把 server 列表合并进目标文件的
// mcpServers 字段（保留该文件其它配置）。
func injectMcp(mcpFile, routerBase string, servers []MCPServer) error {
	if mcpFile == "" {
		return nil
	}
	return mergeMcpServers(mcpFile, servers, routerBase)
}

// mergeMcpServers 把 servers 写入目标 JSON 文件的 mcpServers 字段。
// 仅支持对象形态 { mcpServers: { name: {…} } }（Claude Code
// 常用）；若现有 mcpServers 是数组或其它类型，会被丢弃并重建为空对象再合并。
//
// 采用字节级定点替换（upsertJSONObjectKey）：只改动 mcpServers 键的值，
// 其它键的键序 / 缩进 / 数字格式 / 行尾原样保留，不把用户手写的真实配置
// 整体重排。文件不存在或内容为空时按空对象初始化；文件存在但不是严格 JSON
// 或顶层不是对象时直接报错拒绝写入（部分主配置允许 JSON5 注释/尾逗号，
// ~/.claude.json 为活文件，盲目覆写会把用户配置整体抹掉）。首次注入前备份
// 原始文件（<file>.bak），供手动回退。
func mergeMcpServers(file string, servers []MCPServer, _ string) error {
	// 读-改-写全程持锁：与 SaveTargetConfig 等并发时避免后写覆盖先写
	muCfgFile.Lock()
	defer muCfgFile.Unlock()
	b, rerr := os.ReadFile(file)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rerr
	}
	// 首次注入前备份原始状态（已有备份则保留首次），供手动回退
	if _, err := backupIfAbsent(file); err != nil {
		return err
	}
	doc := b
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}")
	}
	newDoc, err := upsertJSONObjectKey(doc, "mcpServers", func(existing json.RawMessage) (json.RawMessage, error) {
		// 替换语义：mcpServers 的值整体替换为本次所选服务（旧条目全部清掉），
		// 客户端 MCP 配置与「MCP 页所配内容」保持一致；文件其余键原样保留。
		obj := map[string]any{}
		for _, s := range servers {
			obj[s.Name] = mcpServerToAny(s)
		}
		return json.Marshal(obj)
	})
	if err != nil {
		return fmt.Errorf("解析 %s 失败，已拒绝覆写以免丢失现有配置: %w", file, err)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	// mcpServers 可能携带用户凭据，落盘权限收紧为 0600
	return writeFileAtomic(file, newDoc, 0o600)
}

// mcpServerToAny 把 MCPServer 转成 Claude Code 兼容的 mcpServer 配置对象。
func mcpServerToAny(s MCPServer) map[string]any {
	m := map[string]any{"type": "sse"}
	switch s.Type {
	case "stdio":
		m = map[string]any{"type": "stdio", "command": s.Command}
		if len(s.Args) > 0 {
			m["args"] = s.Args
		}
	case "http":
		m = map[string]any{"type": "http", "url": s.URL}
	default: // sse
		m = map[string]any{"type": "sse", "url": s.URL}
	}
	return m
}
