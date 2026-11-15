package core

// 通用目标适配器形态 + 参考模版根目录解析。
//
// 这些「形态」不含任何单个客户端的业务知识，只描述注入能力的组合方式，
// 由 agent/clients 里的各客户端用具体路径实例化后注册：
//
//   - FileMcpTarget   仅注入 mcp（+ 编辑主配置）
//   - SkillsMcpTarget skills + mcp 都注入（+ 编辑主配置）
//
// 只注入 skills 的形态（dirTarget）与仅编辑配置的形态（editTarget）当前没有
// 客户端使用，保留类型以备后续接入（Go 允许未使用的包级类型）。

import (
	"os"
	"path/filepath"
)

// jsonModelsDir 返回参考模版根目录 agent/Json_modles/<sub>。
// 优先级：LLMROUTER_JSON_MODLES_DIR 环境变量 > 从当前工作目录逐级向上查找 > 可执行文件同目录。
func jsonModelsDir(sub string) string {
	if d := os.Getenv("LLMROUTER_JSON_MODLES_DIR"); d != "" {
		return filepath.Join(d, sub)
	}
	if wd, err := os.Getwd(); err == nil {
		c := wd
		for i := 0; i < 9; i++ {
			cand := filepath.Join(c, "agent", "Json_modles", sub)
			if st, err := os.Stat(cand); err == nil && st.IsDir() {
				return cand
			}
			p := filepath.Dir(c)
			if p == c {
				break
			}
			c = p
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "agent", "Json_modles", sub)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
		return dir
	}
	return filepath.Join("agent", "Json_modles", sub)
}

// editTarget 仅提供「编辑配置」的客户端：主配置文件路径已知、但 skills/mcp 注入细节
// 尚未确认（或无需注入）。注入相关方法为空操作，编辑走 ConfigFile。
type editTarget struct {
	id         string
	name       string
	apiFormat  string
	configFile string
	format     string // json / toml
}

func (e *editTarget) Info() TargetInfo {
	return TargetInfo{ID: e.id, Name: e.name, APIFormat: e.apiFormat, ConfigFormat: e.format, ConfigFile: e.configFile}
}
func (e *editTarget) InjectSkills(string, []Skill) error  { return nil }
func (e *editTarget) UndoSkills() error                   { return nil }
func (e *editTarget) InjectMcp(string, []MCPServer) error { return nil }

// dirTarget 表示一个只注入 skills 的目标。
type dirTarget struct {
	id          string
	name        string
	apiFormat   string
	skillsDir   string
	configFile  string
	skillsFiles []string // 保留字段：当前未被注入逻辑使用，注册时一律留空
}

func (d *dirTarget) Info() TargetInfo {
	return TargetInfo{ID: d.id, Name: d.name, APIFormat: d.apiFormat, SkillsSupported: true, SkillsDir: d.skillsDir, ConfigFile: d.configFile}
}
func (d *dirTarget) InjectSkills(_ string, skills []Skill) error {
	return injectSkills(d.skillsDir, skills)
}
func (d *dirTarget) UndoSkills() error                       { return undoSkills(d.skillsDir) }
func (d *dirTarget) InjectMcp(_ string, _ []MCPServer) error { return nil }

// FileMcpTarget 表示只注入 mcp 的目标（MCP 声明在独立文件里，如 pi 的 mcp.json /
// Claude / Copilot 的 mcpServers 对象文件），主配置编辑走 ConfigFile。
type FileMcpTarget struct {
	ID         string
	Name       string
	APIFormat  string
	McpFile    string
	ConfigFile string // 真实主配置路径（空则回退 McpFile）
	PresetFile string // 首次初始值模板（仅真实文件不存在时用，非同步桥梁）
	Format     string // 主配置格式（json 等），空表示默认
}

func (f *FileMcpTarget) Info() TargetInfo {
	cf := f.ConfigFile
	if cf == "" {
		cf = f.McpFile
	}
	return TargetInfo{ID: f.ID, Name: f.Name, APIFormat: f.APIFormat, ConfigFormat: f.Format,
		McpSupported: true, McpFile: f.McpFile, ConfigFile: cf, PresetFile: f.PresetFile}
}
func (f *FileMcpTarget) InjectSkills(_ string, _ []Skill) error { return nil }
func (f *FileMcpTarget) UndoSkills() error                      { return nil }
func (f *FileMcpTarget) InjectMcp(_ string, servers []MCPServer) error {
	return injectMcp(f.McpFile, "", servers)
}

// SkillsMcpTarget 表示 skills 与 mcp 都注入的目标（Claude Code、OpenCode 等）。
type SkillsMcpTarget struct {
	ID         string
	Name       string
	APIFormat  string
	SkillsDir  string
	McpFile    string
	ConfigFile string // 真实主配置路径（空则回退 McpFile）
	PresetFile string // 首次初始值模板（仅真实文件不存在时用，非同步桥梁）
	Format     string // 主配置格式（json 等），空表示默认
}

func (s *SkillsMcpTarget) Info() TargetInfo {
	cf := s.ConfigFile
	if cf == "" {
		cf = s.McpFile
	}
	return TargetInfo{ID: s.ID, Name: s.Name, APIFormat: s.APIFormat, ConfigFormat: s.Format,
		SkillsSupported: true, McpSupported: true, SkillsDir: s.SkillsDir, McpFile: s.McpFile, ConfigFile: cf, PresetFile: s.PresetFile}
}
func (s *SkillsMcpTarget) InjectSkills(_ string, skills []Skill) error {
	return injectSkills(s.SkillsDir, skills)
}
func (s *SkillsMcpTarget) UndoSkills() error { return undoSkills(s.SkillsDir) }
func (s *SkillsMcpTarget) InjectMcp(_ string, servers []MCPServer) error {
	return injectMcp(s.McpFile, "", servers)
}
