package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill 表示一个技能（data/skills/<name>/SKILL.md）。
type Skill struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Desc    string `json:"desc"`
	Content string `json:"content,omitempty"`
}

// skillFileName 是 Claude Code 兼容的技能定义文件名。
const skillFileName = "SKILL.md"

// validSkillName 判断技能名是否可安全用作目录/文件名（与 SaveSkill/DeleteSkill
// 的校验一致）：非空、不含路径分隔符、且不是相对路径成分。扫描与注入清单
// 等来源都经此校验，防止异常文件名（如 "...md" 解析出的 ".."）越出技能目录。
func validSkillName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return false
	}
	return true
}

// ListSkills 扫描 data/skills/ 下所有技能（每个子目录含 SKILL.md 才算一个），
// 返回按名称排序的列表，并提取首行描述（去掉 YAML front-matter）。
func ListSkills() []Skill {
	return readSkillsDir(SkillsDir())
}

// readSkillsDir 扫描指定目录下所有技能并返回按名称排序的列表。
// 支持的存放方式（更宽松，便于把技能直接放进 data/skills）：
//  1. 子目录 / SKILL.md（Claude Code 约定）；
//  2. 根目录下直接散放的 .md 文件（以去后缀文件名作为技能名）。
func readSkillsDir(root string) []Skill {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []Skill{}
	}
	out := []Skill{}
	for _, e := range entries {
		if e.IsDir() {
			name := e.Name()
			if !validSkillName(name) {
				continue
			}
			dir := filepath.Join(root, name)
			content, err := os.ReadFile(filepath.Join(dir, skillFileName))
			if err != nil {
				continue
			}
			out = append(out, Skill{
				Name:    name,
				Path:    dir,
				Desc:    skillDesc(string(content)),
				Content: "",
			})
			continue
		}
		// 根目录直接散放的 .md 文件
		if ext := strings.ToLower(filepath.Ext(e.Name())); ext == ".md" {
			base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			if !validSkillName(base) {
				continue
			}
			fp := filepath.Join(root, e.Name())
			content, err := os.ReadFile(fp)
			if err != nil {
				continue
			}
			out = append(out, Skill{
				Name:    base,
				Path:    fp,
				Desc:    skillDesc(string(content)),
				Content: "",
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// GetSkillContent 返回指定技能的 SKILL.md 内容；不存在返回空。
// name 需为合法目录名（与 SaveSkill/DeleteSkill 相同校验，防路径穿越）。
func GetSkillContent(name string) string {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return ""
	}
	dir := filepath.Join(SkillsDir(), name)
	b, err := os.ReadFile(filepath.Join(dir, skillFileName))
	if err != nil {
		return ""
	}
	return string(b)
}

// SaveSkill 新建或更新技能。name 需为合法目录名（去掉 / \ 与路径穿越）。
// 返回错误字符串（空 = 成功）。
func SaveSkill(name, content string) string {
	if name == "" {
		return "技能名称不能为空"
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "技能名称不合法"
	}
	dir := filepath.Join(SkillsDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "创建技能目录失败: " + err.Error()
	}
	// 原子写：与其它落盘路径一致，避免中途崩溃留下半截 SKILL.md
	if err := writeFileAtomic(filepath.Join(dir, skillFileName), []byte(content), 0o644); err != nil {
		return "写入技能失败: " + err.Error()
	}
	return ""
}

// DeleteSkill 删除指定技能目录。返回错误字符串（空 = 成功）。
func DeleteSkill(name string) string {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "技能名称不合法"
	}
	dir := filepath.Join(SkillsDir(), name)
	if err := os.RemoveAll(dir); err != nil {
		return "删除技能失败: " + err.Error()
	}
	return ""
}

// AgentSkills 表示一个 Agent 在其系统用户目录技能文件夹里拥有的技能列表。
type AgentSkills struct {
	Name   string  `json:"name"`
	Path   string  `json:"path"`
	Skills []Skill `json:"skills"`
}

// ListAgentSkills 扫描各 Agent 客户端在系统用户主目录下的技能目录（路径来自
// client-paths.json 的 skills 项），返回按 Agent 分组（AgentSkills）的列表。
// 目录不存在或为空时 Skills 为空切片；未安装的 Agent 对应目录不存在即显示为空。
func ListAgentSkills() []AgentSkills {
	agents := []struct{ name, id string }{
		{"Claude Code", "claude-code"},
		{"Codex", "codex"},
		{"Gemini", "gemini"},
		{"Antigravity CLI", "agy"},
		{"Grok Build", "grok-build"},
		{"Kimi Code", "kimi"},
		{"OpenCode", "opencode"},
		{"Hermes", "hermes"},
		{"pi", "pi"},
	}
	out := make([]AgentSkills, 0, len(agents))
	for _, a := range agents {
		dir := clientPath(a.id, "skills")
		if dir == "" {
			continue
		}
		out = append(out, AgentSkills{
			Name:   a.name,
			Path:   dir,
			Skills: readSkillsDir(dir),
		})
	}
	return out
}

// skillDesc 从 SKILL.md 中提取一行简述：优先取 front-matter 的 description，
// 否则取首个非空正文行的前 80 字符。
func skillDesc(content string) string {
	// YAML front-matter：---\n...\n---
	if strings.HasPrefix(content, "---") {
		if rest, ok := strings.CutPrefix(content, "---"); ok {
			if idx := strings.Index(rest, "---"); idx >= 0 {
				fm := rest[:idx]
				for _, line := range strings.Split(fm, "\n") {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "description:") {
						return strings.TrimSpace(strings.TrimPrefix(line, "description:"))
					}
				}
			}
		}
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
			r := []rune(line)
			if len(r) > 80 {
				r = r[:80]
			}
			return string(r)
		}
	}
	return ""
}
