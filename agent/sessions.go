package agent

import (
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 会话管理：枚举/删除各 AI CLI 客户端落在本机磁盘上的历史会话记录。
// 各客户端的会话落盘结构互不相同（文件型 / 目录型、层级深浅不一），
// 这里用「根目录 + 条目类型 + 文件名通配 + 精确层级」的声明式注册表描述，
// 枚举与删除都基于同一份注册表，保证删除校验与展示口径一致。

// ResumeSpec 描述一家客户端「在终端中按会话恢复」的能力与命令模板。
// Bin 为空 = 该客户端不支持按指定会话恢复（Unsupported 给出原因）。
type ResumeSpec struct {
	Bin         string   // CLI 可执行名（在 PATH 中查找）；空 = 不支持按会话恢复
	Args        []string // 终端内执行的命令模板：{id}=会话 ID、{file}=会话条目绝对路径
	IDFrom      string   // {id} 的来源：stem=去扩展名 / name=条目名 / trimSession=去掉 session_ 前缀 / jsonlSessionId=读会话文件里的 session_id
	CwdFrom     string   // 恢复命令的启动目录来源：""=用户主目录 / jsonlCwd=读会话文件里的 cwd / urlGroup=URL 解码分组目录名（grok 项目目录）
	Unsupported string   // Bin 为空时展示给用户的不支持原因
}

// SessionStore 描述一个客户端的会话落盘位置：Root 下第 Depth 层、
// 名字匹配 Pattern 的文件（Kind="file"）或目录（Kind="dir"）视为一个会话。
type SessionStore struct {
	Root    string // 会话根目录（~ 开头主目录缩写，取值时展开）
	Kind    string // file / dir
	Pattern string // 条目名通配（空 = *，仅 file 类型使用）
	Depth   int    // 条目相对 Root 的精确层级（1 = Root 直接子项）
	Resume  ResumeSpec
}

// sessionStores 已接入客户端的会话目录注册表。结构随客户端版本演进可能
// 变化；目录不存在时计数为 0，不影响其它客户端。未知布局的客户端不注册。
// Resume 为各客户端「终端按会话恢复」的命令模板（各 CLI 恢复参数经本机 --help 核实）：
//
//	claude-code / codebuddy：文件名为会话 ID，claude --resume <id>（CD 还原原始项目目录，
//	 因官方文档明确旧版本按 ID 查找范围限于当前项目目录及其 worktrees）
//	commandcode：支持直接按会话文件恢复 commandcode --session <file>
//	gemini：无按 ID 恢复，但支持 --session-file <file> 按会话文件载入
//	codex / opencode / kimi / cline：按各自会话 ID 恢复；codex 读 rollout 内 session_id（旧版）
//	grok-build：会话目录名即 UUID，grok --resume <uuid>，分组目录是 URL 编码的真实项目路径
//	copilot：仅 --continue（恢复最近会话），无法按指定会话恢复；pi 未安装 → 均不登记恢复能力
var sessionStores = map[string]SessionStore{
	// Claude Code：~/.claude/projects/<项目编码>/<会话id>.jsonl
	"claude-code": {Root: "~/.claude/projects", Kind: "file", Pattern: "*.jsonl", Depth: 2,
		Resume: ResumeSpec{Bin: "claude", Args: []string{"claude", "--resume", "{id}"}, IDFrom: "stem", CwdFrom: "jsonlCwd"}},
	// OpenCode：~/.local/share/opencode/storage/session/<会话id>/（旧版；新版入库不落此目录）
	"opencode": {Root: "~/.local/share/opencode/storage/session", Kind: "dir", Depth: 1,
		Resume: ResumeSpec{Bin: "opencode", Args: []string{"opencode", "--session", "{id}"}, IDFrom: "name"}},
	// Gemini CLI：~/.gemini/tmp/<工作目录>/chats/<会话文件>
	"gemini": {Root: "~/.gemini/tmp", Kind: "file", Depth: 3,
		Resume: ResumeSpec{Bin: "gemini", Args: []string{"gemini", "--session-file", "{file}"}}},
	// pi：~/.pi/agent/sessions/<工作目录编码>/<时间戳_会话id>.jsonl（本机未安装 pi，无恢复能力登记）
	"pi": {Root: "~/.pi/agent/sessions", Kind: "file", Pattern: "*.jsonl", Depth: 2,
		Resume: ResumeSpec{Unsupported: "pi 未安装，且未登记其会话恢复命令"}},
	// Grok Build：~/.grok/sessions/<项目URL编码>/<会话uuid>/
	"grok-build": {Root: "~/.grok/sessions", Kind: "dir", Depth: 2,
		Resume: ResumeSpec{Bin: "grok", Args: []string{"grok", "--resume", "{id}"}, IDFrom: "name", CwdFrom: "urlGroup"}},
	// Codex：~/.codex/sessions/年/月/日/rollout-*.jsonl（旧版 rollout；新版会话入本地库）
	"codex": {Root: "~/.codex/sessions", Kind: "file", Pattern: "*.jsonl", Depth: 4,
		Resume: ResumeSpec{Bin: "codex", Args: []string{"codex", "resume", "{id}"}, IDFrom: "jsonlSessionId", CwdFrom: "jsonlCwd"}},
	// Cline：~/.cline/data/sessions/<会话id>/
	"cline": {Root: "~/.cline/data/sessions", Kind: "dir", Depth: 1,
		Resume: ResumeSpec{Bin: "cline", Args: []string{"cline", "--id", "{id}"}, IDFrom: "name"}},
	// Copilot CLI：~/.copilot/chats/<会话uuid>/（仅 --continue 恢复最近会话）
	"copilot": {Root: "~/.copilot/chats", Kind: "dir", Depth: 1,
		Resume: ResumeSpec{Unsupported: "Copilot CLI 仅支持 --continue 恢复最近一次会话，无法按指定会话恢复"}},
	// Command Code：~/.commandcode/projects/<项目编码>/<会话id>.jsonl（Claude 风格）
	"commandcode": {Root: "~/.commandcode/projects", Kind: "file", Pattern: "*.jsonl", Depth: 2,
		Resume: ResumeSpec{Bin: "commandcode", Args: []string{"commandcode", "--session", "{file}"}}},
	// Kimi Code：~/.kimi-code/sessions/<工作目录哈希>/session_<uuid>/
	"kimi": {Root: "~/.kimi-code/sessions", Kind: "dir", Pattern: "session_*", Depth: 2,
		Resume: ResumeSpec{Bin: "kimi", Args: []string{"kimi", "-S", "{id}"}, IDFrom: "trimSession"}},
	// CodeBuddy CLI：~/.codebuddy/projects/<项目编码>/<会话id>.jsonl
	"codebuddy": {Root: "~/.codebuddy/projects", Kind: "file", Pattern: "*.jsonl", Depth: 2,
		Resume: ResumeSpec{Bin: "codebuddy", Args: []string{"codebuddy", "--resume", "{id}"}, IDFrom: "stem", CwdFrom: "jsonlCwd"}},
}

// SessionItem 描述一个会话条目（文件或目录）。
type SessionItem struct {
	Path  string `json:"path"`  // 绝对路径
	Name  string `json:"name"`  // 条目名（文件/目录名）
	Group string `json:"group"` // 相对 Root 的父目录（项目/分组名，用于展示）
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`  // 字节数（目录为递归合计）
	Files int    `json:"files"` // 包含文件数（文件条目为 1）
	Mtime string `json:"mtime"` // 最后修改时间
}

// SessionClient 描述一个客户端的会话概览。
type SessionClient struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Root       string `json:"root"`       // 展开后的会话根目录
	RootExists bool   `json:"rootExists"` // 根目录是否存在
	Count      int    `json:"count"`      // 会话条目数
	TotalSize  int64  `json:"totalSize"`  // 会话总体积（字节）
	LastMod    string `json:"lastMod"`    // 最近会话修改时间（空 = 无会话）
	// 终端「按会话恢复」能力（false = 该客户端不支持，ResumeNote 给原因）
	ResumeSupported bool   `json:"resumeSupported"`
	ResumeNote      string `json:"resumeNote"`
}

// sessionClientName 从目标注册表解析客户端显示名，未注册时回退为 ID。
func sessionClientName(id string) string {
	for _, ti := range ListTargets() {
		if ti.ID == id {
			return ti.Name
		}
	}
	return id
}

// sessionStoreOf 取客户端的会话存储定义并展开 ~ 缩写；未注册返回 ok=false。
func sessionStoreOf(clientID string) (st SessionStore, rootAbs string, ok bool) {
	st, ok = sessionStores[clientID]
	if !ok {
		return st, "", false
	}
	return st, expandHome(st.Root), true
}

// walkSessionItems 遍历 Root 收集会话条目（文件/目录按注册表口径匹配）。
func walkSessionItems(st SessionStore, rootAbs string) ([]SessionItem, time.Time, error) {
	var items []SessionItem
	var lastMod time.Time
	pattern := st.Pattern
	if pattern == "" {
		pattern = "*"
	}
	err := filepath.WalkDir(rootAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 单个条目读不了（权限/占用）跳过，不影响整体
		}
		rel, rerr := filepath.Rel(rootAbs, p)
		if rerr != nil || rel == "." {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		if depth > st.Depth {
			return filepath.SkipDir // 深于目标层级（目录型会话的内部文件）
		}
		if depth < st.Depth {
			return nil // 分组层（项目/年月日等），继续下钻
		}
		if st.Kind == "dir" {
			if !d.IsDir() {
				return nil
			}
			if matched, _ := filepath.Match(pattern, d.Name()); !matched {
				return filepath.SkipDir // 不匹配的子目录整棵跳过
			}
			item, ok := buildSessionItem(p, rel, d, true)
			if ok {
				items = append(items, item)
				if mt, err := time.ParseInLocation("2006-01-02 15:04:05", item.Mtime, time.Local); err == nil && mt.After(lastMod) {
					lastMod = mt
				}
			}
			return filepath.SkipDir // 目录型会话不再深入
		}
		// 文件型
		if d.IsDir() {
			return nil
		}
		if matched, _ := filepath.Match(pattern, d.Name()); !matched {
			return nil
		}
		item, ok := buildSessionItem(p, rel, d, false)
		if ok {
			items = append(items, item)
			if mt, err := time.ParseInLocation("2006-01-02 15:04:05", item.Mtime, time.Local); err == nil && mt.After(lastMod) {
				lastMod = mt
			}
		}
		return nil
	})
	return items, lastMod, err
}

// buildSessionItem 组装单个会话条目（目录型递归统计体积与文件数）。
func buildSessionItem(p, rel string, d fs.DirEntry, isDir bool) (SessionItem, bool) {
	info, err := d.Info()
	if err != nil {
		return SessionItem{}, false
	}
	item := SessionItem{
		Path:  p,
		Name:  d.Name(),
		IsDir: isDir,
		Mtime: info.ModTime().Format("2006-01-02 15:04:05"),
	}
	// 分组 = 相对 Root 的父目录（项目/工作目录名）；Depth=1 时父目录是 "."，归一为空
	if g := filepath.Dir(rel); g != "." {
		item.Group = g
	}
	if isDir {
		filepath.WalkDir(p, func(_ string, fd fs.DirEntry, ferr error) error {
			if ferr != nil || fd.IsDir() {
				return nil
			}
			if fi, ie := fd.Info(); ie == nil {
				item.Size += fi.Size()
				item.Files++
			}
			return nil
		})
	} else {
		item.Size = info.Size()
		item.Files = 1
	}
	return item, true
}

// ListSessionClients 返回注册了会话目录的客户端概览（按名称排序）。
func ListSessionClients() []SessionClient {
	out := make([]SessionClient, 0, len(sessionStores))
	for id, st := range sessionStores {
		_, rootAbs, _ := sessionStoreOf(id)
		sc := SessionClient{ID: id, Name: sessionClientName(id), Root: rootAbs}
		if st.Resume.Bin != "" {
			sc.ResumeSupported = true
		} else {
			sc.ResumeNote = st.Resume.Unsupported
		}
		if fi, err := os.Stat(rootAbs); err == nil && fi.IsDir() {
			sc.RootExists = true
			items, lastMod, _ := walkSessionItems(st, rootAbs)
			sc.Count = len(items)
			for _, it := range items {
				sc.TotalSize += it.Size
			}
			if !lastMod.IsZero() {
				sc.LastMod = lastMod.Format("2006-01-02 15:04:05")
			}
		}
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ListSessionItems 返回指定客户端的会话条目（修改时间新→旧）。
func ListSessionItems(clientID string) ([]SessionItem, error) {
	st, rootAbs, ok := sessionStoreOf(clientID)
	if !ok {
		return nil, errors.New("该客户端未登记会话目录: " + clientID)
	}
	if fi, err := os.Stat(rootAbs); err != nil || !fi.IsDir() {
		return []SessionItem{}, nil // 根目录不存在视为无会话，不报错
	}
	items, _, err := walkSessionItems(st, rootAbs)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Mtime > items[j].Mtime })
	return items, nil
}

// SessionResumePlan 描述在终端中恢复一条会话所需的信息。
type SessionResumePlan struct {
	Dir    string   // 终端工作目录（尽量还原该会话的原始项目目录）
	Tokens []string // 终端内要执行的命令 argv（首个为 CLI 可执行名）
}

// BuildSessionResume 校验 path 为该客户端的会话条目（与删除同口径：位于会话
// 根目录内、层级与注册表相符），并按注册表 Resume 模板组装「在终端中恢复该
// 会话」的命令。返回错误的情况：未登记客户端、路径越界/层级不符/条目不存在、
// 该客户端不支持按会话恢复（如 Copilot 仅支持 --continue）。
func BuildSessionResume(clientID, path string) (*SessionResumePlan, error) {
	st, rootAbs, ok := sessionStoreOf(clientID)
	if !ok {
		return nil, errors.New("该客户端未登记会话目录: " + clientID)
	}
	rootAbs = filepath.Clean(rootAbs)
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil, errors.New("路径不在会话目录内: " + path)
	}
	if strings.Count(rel, string(filepath.Separator))+1 != st.Depth {
		return nil, errors.New("路径层级与该客户端的会话结构不符: " + path)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, errors.New("会话不存在或不可读: " + path)
	}
	if st.Resume.Bin == "" {
		reason := st.Resume.Unsupported
		if reason == "" {
			reason = "该客户端不支持按会话恢复"
		}
		return nil, errors.New(reason)
	}
	plan := &SessionResumePlan{Dir: resumeLaunchDir(st, abs, rel, fi.IsDir())}
	needID := false
	for _, a := range st.Resume.Args {
		needID = needID || strings.Contains(a, "{id}")
	}
	var id string
	if needID {
		id, err = resumeSessionID(st, abs, fi.Name())
		if err != nil {
			return nil, err
		}
	}
	for _, a := range st.Resume.Args {
		a = strings.ReplaceAll(a, "{file}", abs)
		if needID {
			a = strings.ReplaceAll(a, "{id}", id)
		}
		plan.Tokens = append(plan.Tokens, a)
	}
	return plan, nil
}

// resumeSessionID 按注册表 IDFrom 规则从会话条目提取 CLI 恢复所用的会话 ID。
func resumeSessionID(st SessionStore, abs, name string) (string, error) {
	switch st.Resume.IDFrom {
	case "stem":
		return strings.TrimSuffix(name, filepath.Ext(name)), nil
	case "name":
		return name, nil
	case "trimSession":
		id := strings.TrimPrefix(name, "session_")
		if id == name {
			return "", errors.New("会话目录名不符合 kimi 结构: " + name)
		}
		return id, nil
	case "jsonlSessionId":
		if v, ok := readSessionJSONField(abs, "session_id"); ok && v != "" {
			return v, nil
		}
		return strings.TrimSuffix(name, filepath.Ext(name)), nil // 兜底用文件名
	default:
		return "", errors.New("会话 ID 提取规则未定义: " + st.Resume.IDFrom)
	}
}

// resumeLaunchDir 决定恢复命令的启动目录：尽量还原该会话的原始项目目录
// （jsonl 里的 cwd 元数据 / grok 的 URL 编码分组目录），否则用用户主目录。
func resumeLaunchDir(st SessionStore, abs, rel string, _ bool) string {
	switch st.Resume.CwdFrom {
	case "jsonlCwd":
		if v, ok := readSessionJSONField(abs, "cwd"); ok && v != "" && isDirPath(v) {
			return v
		}
	case "urlGroup":
		if g := filepath.Dir(rel); g != "." {
			if dec, err := url.PathUnescape(g); err == nil && isDirPath(dec) {
				return dec
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// isDirPath 判断路径是存在的目录。
func isDirPath(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// readSessionJSONField 读取文件开头（最多 256KB）内第一个指定 JSON 字段的字符串值，
// 兼容 JSON 与 JSON Lines 的字段（如 claude/codebuddy 里的 "cwd"、codex rollout 的 "session_id"）。
func readSessionJSONField(path, key string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, 256<<10)
	n, _ := f.Read(buf)
	if n == 0 {
		return "", false
	}
	s := string(buf[:n])
	needle := `"` + key + `"`
	for i := 0; ; {
		j := strings.Index(s[i:], needle)
		if j < 0 {
			return "", false
		}
		k := i + j + len(needle)
		for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
			k++
		}
		if k >= len(s) || s[k] != ':' { // 键不后随冒号（如拼进别的字符串）→ 找下一个
			i = k
			continue
		}
		k++
		for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
			k++
		}
		if k >= len(s) || s[k] != '"' {
			i = k
			continue
		}
		k++
		var sb strings.Builder
		for k < len(s) {
			c := s[k]
			if c == '\\' && k+1 < len(s) { // JSON 转义：\n \t \" \\ 等一律收下一字符
				sb.WriteByte(s[k+1])
				k += 2
				continue
			}
			if c == '"' {
				return sb.String(), true
			}
			sb.WriteByte(c)
			k++
		}
		return "", false
	}
}

// DeleteSessionItems 删除指定客户端的若干会话条目。每个路径都会校验：
// 必须位于该客户端会话根目录下、且层级与注册表口径一致（防止误删根目录或分组层）。
// 文件型条目用 os.Remove，目录型整棵 os.RemoveAll。返回第一条失败的原因。
func DeleteSessionItems(clientID string, paths []string) error {
	st, rootAbs, ok := sessionStoreOf(clientID)
	if !ok {
		return errors.New("该客户端未登记会话目录: " + clientID)
	}
	rootAbs = filepath.Clean(rootAbs)
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootAbs, abs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return errors.New("路径不在会话目录内，已拒绝删除: " + p)
		}
		if strings.Count(rel, string(filepath.Separator))+1 != st.Depth {
			return errors.New("路径层级与该客户端的会话结构不符，已拒绝删除: " + p)
		}
		fi, err := os.Lstat(abs)
		if err != nil {
			continue // 已不存在视为删除成功（幂等）
		}
		if fi.IsDir() {
			if st.Kind != "dir" {
				return errors.New("该客户端的会话是文件，目录已拒绝删除: " + p)
			}
			if err := os.RemoveAll(abs); err != nil {
				return err
			}
		} else {
			if st.Kind == "dir" {
				return errors.New("该客户端的会话是目录，文件已拒绝删除: " + p)
			}
			if err := os.Remove(abs); err != nil {
				return err
			}
		}
	}
	return nil
}
