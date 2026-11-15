package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	// CLI 非空 = 该客户端会话存在本地库（无逐会话文件），只能经自身 CLI
	// 子命令枚举/删除；此时 Root/Kind/Pattern/Depth 仅用于「打开所在目录」展示。
	CLI *CLIStore
}

// CLIStore 描述「会话存于本地库、靠 CLI 子命令管理」的客户端（当前只有 MiMo Code）。
// ListArgs 需输出 JSON 数组（元素含 id/title/updated/directory 字段）。
// DeleteArgs 为单条删除模板，{id} 替换为会话 ID。
//
// 注意：这类 CLI 的枚举命令是「按当前工作目录所属项目」分桶的（同一目录下
// 无会话时输出为空而非 []），所以要在多个目录分别执行再合并——ListDirs 为
// 固定目录，ListDirsFrom 为按客户端数据目录动态生成的补充目录。
type CLIStore struct {
	Bin          string
	ListArgs     []string
	DeleteArgs   []string
	ListDirsFrom string // trustedWorkspaces = 读 <Root>/trusted-workspaces.json 的 trustedPaths
	// ListFallbackArgs 主枚举命令失败时的兜底（OpenCode 用 `session list` 兜 SQL 查询）
	ListFallbackArgs []string
	// HasStats 列表里带 msgs / tokensIn / tokensOut 字段（可填消息数与 Token 列）
	HasStats bool
}

// 枚举多目录时的并发上限与目录数量上限：单次调用约 6~8s（Bun 冷启动），
// 并发既能压总时长又不至于同时拉起太多运行时。
const (
	cliListMaxDirs = 4
	cliListWorkers = 4 // 与目录上限一致：一次并行跑完，冷启动总耗时≈单次
)

// cliSession 是 CLI 侧会话列表的 JSON 元素（各客户端字段名以 registered 形态为准，
// 缺失字段容错为零值）。
type cliSession struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Updated   int64  `json:"updated"`
	Created   int64  `json:"created"`
	Directory string `json:"directory"`
	Msgs      int    `json:"msgs"`
	TokensIn  int64  `json:"tokensIn"`
	TokensOut int64  `json:"tokensOut"`
}

// opencodeSessionSQL 直接查 OpenCode 本地库的会话清单（列别名与 cliSession 的
// JSON 字段对齐；tokens 输入侧含缓存读 / 缓存写，与其它客户端的统计口径一致）。
// 排除 time_archived 非空的归档会话；子代理会话（parent_id 非空）一并列出。
const opencodeSessionSQL = "select s.id as id, s.title as title, s.time_updated as updated, s.time_created as created, " +
	"s.directory as directory, (select count(*) from message m where m.session_id = s.id) as msgs, " +
	"(s.tokens_input + s.tokens_cache_read + s.tokens_cache_write) as tokensIn, s.tokens_output as tokensOut " +
	"from session s where s.time_archived is null order by s.time_updated desc"

// sessionStores 已接入客户端的会话目录注册表。结构随客户端版本演进可能
// 变化；目录不存在时计数为 0，不影响其它客户端。未知布局的客户端不注册。
// Resume 为各客户端「终端按会话恢复」的命令模板（各 CLI 恢复参数经本机 --help 核实）：
//
//	claude-code / codebuddy：文件名为会话 ID，claude --resume <id>（CD 还原原始项目目录，
//	 因官方文档明确旧版本按 ID 查找范围限于当前项目目录及其 worktrees）
//	commandcode：支持直接按会话文件恢复 commandcode --session <file>
//	codex / opencode / kimi：按各自会话 ID 恢复；codex 读 rollout 内 session_id（旧版）
//	grok-build：会话目录名即 UUID，grok --resume <uuid>，分组目录是 URL 编码的真实项目路径
//	copilot：仅 --continue（恢复最近会话），无法按指定会话恢复；pi 未安装 → 均不登记恢复能力
var sessionStores = map[string]SessionStore{
	// Claude Code：~/.claude/projects/<项目编码>/<会话id>.jsonl
	"claude-code": {Root: "~/.claude/projects", Kind: "file", Pattern: "*.jsonl", Depth: 2,
		Resume: ResumeSpec{Bin: "claude", Args: []string{"claude", "--resume", "{id}"}, IDFrom: "stem", CwdFrom: "jsonlCwd"}},
	// OpenCode：新版会话入本地 SQLite 库（~/.local/share/opencode/opencode.db），
	// 旧版 ~/.local/share/opencode/storage/session/<会话id>/ 已不再写入。
	// 枚举走官方 `opencode db` 直接查 session 表（`session list` 只列当前项目分桶，
	// 跨项目会话看不到），失败则兜底 `session list`；删除走 `session delete`。
	"opencode": {Root: "~/.local/share/opencode", CLI: &CLIStore{
		Bin:              "opencode",
		ListArgs:         []string{"db", opencodeSessionSQL, "--format", "json"},
		ListFallbackArgs: []string{"session", "list", "--format", "json"},
		DeleteArgs:       []string{"session", "delete", "{id}"},
		HasStats:         true,
	},
		Resume: ResumeSpec{Bin: "opencode", Args: []string{"opencode", "--session", "{id}"}, IDFrom: "name"}},
	// pi：~/.pi/agent/sessions/<工作目录编码>/<时间戳_会话id>.jsonl（本机未安装 pi，无恢复能力登记）
	"pi": {Root: "~/.pi/agent/sessions", Kind: "file", Pattern: "*.jsonl", Depth: 2,
		Resume: ResumeSpec{Unsupported: "pi 未安装，且未登记其会话恢复命令"}},
	// Grok Build：~/.grok/sessions/<项目URL编码>/<会话uuid>/
	"grok-build": {Root: "~/.grok/sessions", Kind: "dir", Depth: 2,
		Resume: ResumeSpec{Bin: "grok", Args: []string{"grok", "--resume", "{id}"}, IDFrom: "name", CwdFrom: "urlGroup"}},
	// Codex：~/.codex/sessions/年/月/日/rollout-*.jsonl（旧版 rollout；新版会话入本地库）
	"codex": {Root: "~/.codex/sessions", Kind: "file", Pattern: "*.jsonl", Depth: 4,
		Resume: ResumeSpec{Bin: "codex", Args: []string{"codex", "resume", "{id}"}, IDFrom: "jsonlSessionId", CwdFrom: "jsonlCwd"}},
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
	// Deep Code CLI：~/.deepcode/projects/<项目编码>/<会话id>.jsonl（Claude 风格，
	// 同级还有 sessions-index.json，用 *.jsonl 通配自然排除）
	"deepcode": {Root: "~/.deepcode/projects", Kind: "file", Pattern: "*.jsonl", Depth: 2,
		Resume: ResumeSpec{Bin: "deepcode", Args: []string{"deepcode", "-r", "{id}"}, IDFrom: "stem", CwdFrom: "jsonlCwd"}},
	// Antigravity CLI：~/.gemini/antigravity-cli/conversations/<会话id>.db
	// 每个会话一个 SQLite 库文件（无 JSONL 转写），文件名即会话 ID；
	// 删库即删会话，恢复用 agy --conversation=<id>。
	"agy": {Root: "~/.gemini/antigravity-cli/conversations", Kind: "file", Pattern: "*.db", Depth: 1,
		Resume: ResumeSpec{Bin: "agy", Args: []string{"agy", "--conversation={id}"}, IDFrom: "stem"}},
	// MiMo Code：会话存于本地 SQLite 库（~/.local/share/mimocode/mimocode.db），
	// 没有逐会话文件，只能经 `mimo session list/delete` 子命令管理。
	// Root 仅用于「打开所在目录」，枚举与删除走 CLI。
	"mimocode": {Root: "~/.local/share/mimocode", CLI: &CLIStore{
		Bin:          "mimo",
		ListArgs:     []string{"session", "list", "--format", "json"},
		DeleteArgs:   []string{"session", "delete", "{id}"},
		ListDirsFrom: "trustedWorkspaces",
	},
		// 恢复目录取会话库里的 directory 字段（见 buildCLISessionResume）
		Resume: ResumeSpec{Bin: "mimo", Args: []string{"mimo", "-s", "{id}"}, IDFrom: "name"}},
}

// SessionItem 描述一个会话条目（文件或目录）。
type SessionItem struct {
	Path  string `json:"path"`  // 绝对路径
	Name  string `json:"name"`  // 条目名（文件/目录名）
	ID    string `json:"id"`    // 会话 ID（按各客户端注册的恢复 ID 口径提取，无 ID 语义时兜底去扩展名的条目名）
	Title string `json:"title"` // 会话标题（会话文件里第一条用户消息，截断到 60 字符；取不到为空，展示层回退 ID）
	Cmd   string `json:"cmd"`   // 在终端恢复该会话的命令行（展示用；客户端不支持按会话恢复时为空）
	Group string `json:"group"` // 相对 Root 的父目录（项目/分组名，用于展示）
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`  // 字节数（目录为递归合计）
	Files int    `json:"files"` // 包含文件数（文件条目为 1）
	Mtime string `json:"mtime"` // 最后修改时间
	// 会话内容统计（需通读会话文件，按「路径 + 修改时间」缓存；目录型或未知格式时 Stats=false）
	Msgs      int   `json:"msgs"`      // 消息条数（user / assistant 记录数）
	TokensIn  int64 `json:"tokensIn"`  // 输入 token 累计（含缓存读 / 缓存写）
	TokensOut int64 `json:"tokensOut"` // 输出 token 累计
	Stats     bool  `json:"stats"`     // 上述三项是否可用
	// Cli=true 表示该会话来自「库型存储 + CLI 子命令」的客户端（如 MiMo Code）：
	// 没有可直接阅读/定位的磁盘文件，Path 存的是会话 ID（删除/恢复仍按 ID 走 CLI）。
	Cli bool `json:"cli"`
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
	// 会话存储是否已接入文件级管理（可浏览/删除）。false 的客户端（会话为
	// 库型存储或无本地落盘）在选择条置灰，Note 给出原因。
	Managed bool   `json:"managed"`
	Note    string `json:"note"`
	// 终端「按会话恢复」能力（false = 该客户端不支持，ResumeNote 给原因）
	ResumeSupported bool   `json:"resumeSupported"`
	ResumeNote      string `json:"resumeNote"`
}

// sessionClientName 从目标注册表解析客户端显示名，未注册时回退为 ID。
func sessionClientName(id string) string {
	if n := clientNameOf(id); n != "" {
		return n
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

// sessionUnmanagedNote 未接入会话管理的注册客户端置灰原因（当前已全部接入，
// 保留该表以便新增客户端暂时无落盘时给出具体说明）。
var sessionUnmanagedNote = map[string]string{}

// ---- 库型存储客户端（CLI 子命令通道）----
// MiMo Code 把会话存进本地 SQLite 库，没有逐会话文件；官方只提供
// `mimo session list/delete`。这两个子命令实测单次 6~10s（Bun 运行时冷启动 +
// 插件初始化），所以列表结果带 TTL 缓存：导航栏计数只读缓存不阻塞，
// 用户点开该客户端时才同步刷新。

const cliSessionCacheTTL = 60 * time.Second
const cliSessionTimeout = 30 * time.Second

type cliSessionCacheEntry struct {
	at    time.Time
	items []cliSession
	err   error
}

var (
	cliSessionMu     sync.Mutex
	cliSessionCache  = map[string]cliSessionCacheEntry{}
	cliSessionFlight = map[string]chan struct{}{}
)

// cliSessionList 取某客户端会话列表（TTL 内直接命中；并发调用合并为一次执行）。
func cliSessionList(id string, st SessionStore) ([]cliSession, error) {
	cs := st.CLI
	if cs == nil {
		return nil, errors.New("该客户端未登记 CLI 会话通道: " + id)
	}
	cliSessionMu.Lock()
	if c, ok := cliSessionCache[id]; ok && time.Since(c.at) < cliSessionCacheTTL {
		cliSessionMu.Unlock()
		return c.items, c.err
	}
	if ch, ok := cliSessionFlight[id]; ok { // 已有同客户端的查询在跑，等它
		cliSessionMu.Unlock()
		<-ch
		cliSessionMu.Lock()
		c := cliSessionCache[id]
		cliSessionMu.Unlock()
		return c.items, c.err
	}
	ch := make(chan struct{})
	cliSessionFlight[id] = ch
	cliSessionMu.Unlock()

	items, err := runCLISessionList(st, cs)

	cliSessionMu.Lock()
	cliSessionCache[id] = cliSessionCacheEntry{at: time.Now(), items: items, err: err}
	delete(cliSessionFlight, id)
	cliSessionMu.Unlock()
	close(ch)
	return items, err
}

// cliSessionListCached 只读缓存（不阻塞调用方）：导航栏计数用；过期时后台预热。
func cliSessionListCached(id string, st SessionStore) ([]cliSession, bool) {
	cs := st.CLI
	if cs == nil {
		return nil, false
	}
	cliSessionMu.Lock()
	c, ok := cliSessionCache[id]
	fresh := ok && time.Since(c.at) < cliSessionCacheTTL
	cliSessionMu.Unlock()
	if fresh {
		return c.items, true
	}
	if lookBin(cs.Bin) != "" {
		go func() { _, _ = cliSessionList(id, st) }() // 后台预热，下次打开即命中
	}
	if ok {
		return c.items, true // 过期数据先用着，避免导航栏计数抖动
	}
	return nil, false
}

// runCLISessionList 在若干工作目录分别执行枚举子命令，合并去重后返回。
func runCLISessionList(st SessionStore, cs *CLIStore) ([]cliSession, error) {
	if lookBin(cs.Bin) == "" {
		return nil, errors.New("未检测到 " + cs.Bin + " CLI，请先安装")
	}
	dirs := cliListDirs(st, cs)
	type res struct {
		list []cliSession
		err  error
	}
	out := make([]res, len(dirs))
	sem := make(chan struct{}, cliListWorkers)
	var wg sync.WaitGroup
	for i, dir := range dirs {
		wg.Add(1)
		go func(i int, dir string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			raw, err := runCLIListArgs(dir, cs.Bin, cs.ListArgs)
			if err != nil && len(cs.ListFallbackArgs) > 0 {
				raw, err = runCLIListArgs(dir, cs.Bin, cs.ListFallbackArgs) // 主命令失败 → 兜底命令
			}
			if err != nil {
				out[i] = res{err: err}
				return
			}
			list, perr := parseCLISessionList(raw)
			out[i] = res{list: list, err: perr}
		}(i, dir)
	}
	wg.Wait()

	merged := map[string]cliSession{}
	var firstErr error
	ok := 0
	for _, r := range out {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		ok++
		for _, s := range r.list {
			if s.ID == "" {
				continue
			}
			if old, has := merged[s.ID]; !has || s.Updated > old.Updated {
				merged[s.ID] = s
			}
		}
	}
	if ok == 0 && firstErr != nil { // 全部目录都失败才算失败
		return nil, firstErr
	}
	list := make([]cliSession, 0, len(merged))
	for _, s := range merged {
		list = append(list, s)
	}
	return list, nil
}

// runCLIListArgs 在指定目录执行一条枚举命令（带超时）。
func runCLIListArgs(dir, bin string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliSessionTimeout)
	defer cancel()
	return runCLIInDir(ctx, dir, bin, args...)
}

// parseCLISessionList 从命令输出里截取 JSON 数组（容忍前置日志行）；空输出视为无会话。
func parseCLISessionList(raw []byte) ([]cliSession, error) {
	s := string(raw)
	if strings.TrimSpace(s) == "" {
		return nil, nil // 该目录下没有会话时 CLI 输出为空
	}
	// 逐個 '[' 尝试解码：CLI 常把日志（如 "[INFO] ..."）混进 stdout，
	// 取「首个 [ 到末个 ]」的切片法会被日志里的方括号带偏，直接解析失败。
	// 这里让 json.Decoder 从每个候选起点试一次，成功即用。
	for i := 0; i < len(s); i++ {
		if s[i] != '[' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(s[i:]))
		var list []cliSession
		if err := dec.Decode(&list); err == nil {
			return list, nil
		}
	}
	return nil, errors.New("会话列表输出不是 JSON 数组")
}

// cliListDirs 返回枚举时依次使用的工作目录：用户主目录 + 客户端登记的补充目录
// （去重、只保留存在的目录、上限 cliListMaxDirs）。
func cliListDirs(st SessionStore, cs *CLIStore) []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(p string) {
		if len(dirs) >= cliListMaxDirs {
			return
		}
		if p = strings.TrimSpace(p); p == "" || seen[p] || !isDirPath(p) {
			return
		}
		seen[p] = true
		dirs = append(dirs, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(home)
	}
	if cs.ListDirsFrom == "trustedWorkspaces" {
		for _, p := range readMimoTrustedWorkspaces(expandHome(st.Root)) {
			add(p)
		}
	}
	if len(dirs) == 0 {
		dirs = append(dirs, "")
	}
	return dirs
}

// readMimoTrustedWorkspaces 读 MiMo Code 的信任工作区清单（会话按项目目录分桶，
// 这些目录是唯一可枚举到该客户端的项目级会话来源）。
func readMimoTrustedWorkspaces(root string) []string {
	b, err := os.ReadFile(filepath.Join(root, "trusted-workspaces.json"))
	if err != nil {
		return nil
	}
	var v struct {
		TrustedPaths []string `json:"trustedPaths"`
	}
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	return v.TrustedPaths
}

// runCLIInDir 在指定工作目录执行 CLI 命令（Windows 经 cmd /c 承载 .cmd 垫片）。
func runCLIInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", append([]string{"/c", name}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, name, args...)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	hideWindow(cmd)
	return cmd.CombinedOutput()
}

// cliSessionIDPattern 会话 ID 白名单：首字符为字母/数字/下划线，其余仅字母数字、
// 下划线、连字符——排除空串与以 - 开头的形态，防止拼接进子命令参数时被当成选项
// 或注入额外参数。
var cliSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)

// deleteCLISessions 逐条调用 CLI 删除子命令；ID 先经白名单校验，全部成功后清缓存。
func deleteCLISessions(id string, cs *CLIStore, ids []string) error {
	for _, sid := range ids {
		if !cliSessionIDPattern.MatchString(sid) {
			return errors.New("会话 ID 非法，已拒绝删除: " + sid)
		}
		args := make([]string, 0, len(cs.DeleteArgs))
		for _, a := range cs.DeleteArgs {
			args = append(args, strings.ReplaceAll(a, "{id}", sid))
		}
		ctx, cancel := context.WithTimeout(context.Background(), cliSessionTimeout)
		out, err := runHidden(ctx, cs.Bin, args...)
		cancel()
		if err != nil {
			msg := stripANSI(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return errors.New("删除会话失败（" + sid + "）：" + msg)
		}
	}
	cliSessionMu.Lock()
	delete(cliSessionCache, id) // 删除后重新枚举
	cliSessionMu.Unlock()
	return nil
}

// ansiSeqRe 匹配 CSI / OSC 转义序列（CLI 报错常带彩色 ANSI，展示前要剥掉）。
var ansiSeqRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

// stripANSI 去 ANSI 转义、压平空白，返回一行可直接展示的文本。
func stripANSI(s string) string {
	s = ansiSeqRe.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

// cliSessionItems 把 CLI 枚举结果转成会话条目：Mtime 取 updated 毫秒时间戳，
// 标题直接采用会话库里的标题，消息数 / token 用量不可得（Stats=false），
// Path 存会话 ID（删除与恢复都按 ID 走）。
func cliSessionItems(st SessionStore, list []cliSession) []SessionItem {
	items := make([]SessionItem, 0, len(list))
	for _, s := range list {
		if s.ID == "" {
			continue
		}
		it := SessionItem{
			Path:  s.ID,
			Name:  s.ID,
			ID:    s.ID,
			Title: strings.TrimSpace(s.Title),
			Group: s.Directory,
			Cli:   true,
		}
		if s.Updated > 0 {
			it.Mtime = time.UnixMilli(s.Updated).Local().Format("2006-01-02 15:04:05")
		}
		it.Cmd = sessionResumeCmd(st, it)
		// 列表自带消息数与 Token 用量（OpenCode）：兜底命令（session list）不带这些
		// 字段，全为 0 时按「不可用」处理，避免前端显示误导性的 0
		if st.CLI != nil && st.CLI.HasStats && (s.Msgs > 0 || s.TokensIn > 0 || s.TokensOut > 0) {
			it.Msgs, it.TokensIn, it.TokensOut, it.Stats = s.Msgs, s.TokensIn, s.TokensOut, true
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Mtime > items[j].Mtime })
	return items
}

// ListSessionClients 返回客户端选择条概览：已接入文件级会话管理的客户端
// （sessionStores 登记，带会话数/总体积/最近修改）排在前；其余注册客户端
// （会话为库型存储或无本地落盘）置灰列于后，Note 给出原因。两段各自按名称排序。
func ListSessionClients() []SessionClient {
	var managed []SessionClient
	for id := range sessionStores {
		st, rootAbs, _ := sessionStoreOf(id)
		sc := SessionClient{ID: id, Name: sessionClientName(id), Root: rootAbs, Managed: true}
		if st.Resume.Bin != "" {
			sc.ResumeSupported = true
		} else {
			sc.ResumeNote = st.Resume.Unsupported
		}
		// 库型存储（CLI 通道）：目录只是展示用，计数走缓存，不阻塞概览
		if st.CLI != nil {
			if lookBin(st.CLI.Bin) != "" {
				sc.RootExists = true
				if list, ok := cliSessionListCached(id, st); ok {
					sc.Count = len(list)
					var latest int64
					for _, s := range list {
						if s.Updated > latest {
							latest = s.Updated
						}
					}
					if latest > 0 {
						sc.LastMod = time.UnixMilli(latest).Local().Format("2006-01-02 15:04:05")
					}
				}
			}
			managed = append(managed, sc)
			continue
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
		managed = append(managed, sc)
	}
	sort.Slice(managed, func(i, j int) bool { return managed[i].Name < managed[j].Name })

	var unmanaged []SessionClient
	for _, ti := range ListTargets() {
		if _, ok := sessionStores[ti.ID]; ok {
			continue
		}
		note := sessionUnmanagedNote[ti.ID]
		if note == "" {
			note = "该客户端的会话存储暂未接入会话管理"
		}
		unmanaged = append(unmanaged, SessionClient{ID: ti.ID, Name: ti.Name, Note: note})
	}
	sort.Slice(unmanaged, func(i, j int) bool { return unmanaged[i].Name < unmanaged[j].Name })
	return append(managed, unmanaged...)
}

// sessionDisplayID 提取展示用会话 ID：口径与终端恢复所用的 ID 一致
// （stem/name/trimSession/jsonlSessionId）。客户端未登记 ID 提取规则
// （恢复按会话文件载入，如 commandcode；或未登记恢复，如 pi/copilot）
// 或提取失败时兜底：文件去扩展名、目录用条目名——展示场景永不报错。
func sessionDisplayID(st SessionStore, it SessionItem) string {
	id := ""
	switch st.Resume.IDFrom {
	case "stem":
		id = strings.TrimSuffix(it.Name, filepath.Ext(it.Name))
	case "name":
		id = it.Name
	case "trimSession":
		id = strings.TrimPrefix(it.Name, "session_")
	case "jsonlSessionId":
		if v, ok := readSessionJSONField(it.Path, "session_id"); ok && v != "" {
			id = v
		}
	}
	if id == "" {
		if it.IsDir {
			id = it.Name
		} else {
			id = strings.TrimSuffix(it.Name, filepath.Ext(it.Name))
		}
	}
	return id
}

// sessionResumeCmd 组装该会话的展示用恢复命令行：按注册表 Resume 模板替换
// {id} / {file} 占位，token 含空白时加引号。客户端不支持按会话恢复（Bin 空）
// 时返回空串。
func sessionResumeCmd(st SessionStore, it SessionItem) string {
	if st.Resume.Bin == "" {
		return ""
	}
	args := make([]string, 0, len(st.Resume.Args))
	for _, a := range st.Resume.Args {
		a = strings.ReplaceAll(a, "{file}", it.Path)
		a = strings.ReplaceAll(a, "{id}", it.ID)
		args = append(args, a)
	}
	return joinCmdline(args)
}

// joinCmdline 把 argv 拼成可读/可复制的命令行：含空白（空格/制表/引号）的
// token 用双引号包裹，内部双引号反斜杠转义——与 posix/win 的常规展示一致。
func joinCmdline(tokens []string) string {
	quoted := make([]string, 0, len(tokens))
	for _, tk := range tokens {
		if strings.ContainsAny(tk, " \t\"") {
			quoted = append(quoted, `"`+strings.ReplaceAll(tk, `"`, `\"`)+`"`)
		} else {
			quoted = append(quoted, tk)
		}
	}
	return strings.Join(quoted, " ")
}

// ListSessionItems 返回指定客户端的会话条目（修改时间新→旧）。
func ListSessionItems(clientID string) ([]SessionItem, error) {
	st, rootAbs, ok := sessionStoreOf(clientID)
	if !ok {
		return nil, errors.New("该客户端未登记会话目录: " + clientID)
	}
	if st.CLI != nil { // 库型存储：只能经 CLI 子命令枚举
		list, err := cliSessionList(clientID, st)
		if err != nil {
			return nil, err
		}
		return cliSessionItems(st, list), nil
	}
	if fi, err := os.Stat(rootAbs); err != nil || !fi.IsDir() {
		return []SessionItem{}, nil // 根目录不存在视为无会话，不报错
	}
	items, _, err := walkSessionItems(st, rootAbs)
	if err != nil {
		return nil, err
	}
	// 消息条数 / token 用量需通读会话文件：并发解析（最多 6 并发），
	// 结果按「路径 + 修改时间」缓存；目录型条目没有单一会话文件，保持 Stats=false。
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i := range items {
		items[i].ID = sessionDisplayID(st, items[i])
		items[i].Cmd = sessionResumeCmd(st, items[i])
		// 目录型会话（grok-build / kimi / opencode 等）没有单一会话文件：
		// 标题与统计从目录内元数据取（取不到则前端回退显示会话 ID）
		if items[i].IsDir {
			t, m, tin, tout, sok := dirSessionMeta(clientID, items[i].Path)
			items[i].Title, items[i].Msgs, items[i].TokensIn, items[i].TokensOut, items[i].Stats = t, m, tin, tout, sok
			continue
		}
		// 会话标题：读会话文件第一条用户消息
		items[i].Title = sessionTitle(items[i].Path)
		wg.Add(1)
		go func(it *SessionItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			msgs, in, out, ok := sessionUsageStatsCached(it.Path)
			it.Msgs, it.TokensIn, it.TokensOut, it.Stats = msgs, in, out, ok
		}(&items[i])
	}
	wg.Wait()
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
	if st.CLI != nil { // 库型存储：path 即会话 ID，启动目录取会话库里的 directory
		return buildCLISessionResume(clientID, st, path)
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

// buildCLISessionResume 为库型存储客户端组装恢复命令：sid 为会话 ID（非文件路径），
// 启动目录取会话库里的 directory（不存在则回退用户主目录）。
func buildCLISessionResume(clientID string, st SessionStore, sid string) (*SessionResumePlan, error) {
	if st.Resume.Bin == "" {
		reason := st.Resume.Unsupported
		if reason == "" {
			reason = "该客户端不支持按会话恢复"
		}
		return nil, errors.New(reason)
	}
	if !cliSessionIDPattern.MatchString(sid) {
		return nil, errors.New("会话 ID 非法: " + sid)
	}
	dir := ""
	if st.CLI != nil {
		if list, err := cliSessionList(clientID, st); err == nil {
			for _, s := range list {
				if s.ID == sid {
					dir = s.Directory
					break
				}
			}
		}
	}
	if !isDirPath(dir) {
		dir, _ = os.UserHomeDir()
	}
	plan := &SessionResumePlan{Dir: dir}
	for _, a := range st.Resume.Args {
		plan.Tokens = append(plan.Tokens, strings.ReplaceAll(a, "{id}", sid))
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

// ===== 会话标题：从会话文件里提取「第一条用户消息」=====
// 各客户端会话文件的 JSON 形状不同，这里按已知形状依次尝试；取不到就返回空串
//（目录型会话、加密/二进制、格式不认识 → 展示层回退显示会话 ID）。

const sessionTitleMaxRunes = 60 // 标题最长字符数（超出截断加省略号）

// sessionTitle 扫描会话文件开头若干行，返回第一条用户消息文本。
// ---- 会话内容统计（消息条数 / token 用量）----

// sessionStatsMaxBytes 是参与统计的单文件上限：超大会话文件直接跳过（Stats=false），
// 避免列表加载时长时间占用磁盘 IO。
const sessionStatsMaxBytes = 64 << 20

// sessionStatsEntry 是缓存项：mod 为文件修改时间（UnixNano），不一致即重新解析。
type sessionStatsEntry struct {
	mod  int64
	msgs int
	in   int64
	out  int64
	ok   bool
}

var sessionStatsCache sync.Map // map[string]sessionStatsEntry

// sessionUsageStatsCached 按「路径 + 修改时间」取统计结果；文件未变时不重复通读。
func sessionUsageStatsCached(path string) (int, int64, int64, bool) {
	var mod int64
	if fi, err := os.Stat(path); err == nil {
		mod = fi.ModTime().UnixNano()
	}
	if v, hit := sessionStatsCache.Load(path); hit {
		if e, isEntry := v.(sessionStatsEntry); isEntry && e.mod == mod {
			return e.msgs, e.in, e.out, e.ok
		}
	}
	msgs, in, out, ok := sessionUsageStats(path)
	sessionStatsCache.Store(path, sessionStatsEntry{mod: mod, msgs: msgs, in: in, out: out, ok: ok})
	return msgs, in, out, ok
}

// sessionUsageStats 通读一个会话文件，统计消息条数与 token 用量（输入含缓存读 / 缓存写）。
// 已知形状：
//
//	Claude 系（claude-code / codebuddy / commandcode 等 jsonl）
//	  消息：{"type":"user"|"assistant"}；用量：{"message":{"usage":{input_tokens,
//	  cache_creation_input_tokens, cache_read_input_tokens, output_tokens}}}，逐条累加。
//	Codex rollout
//	  消息：{"payload":{"type":"user_message"|"agent_message"}}；用量：
//	  {"payload":{"info":{"total_token_usage":{...}}}} 为累计值，取最后一次。
//
// 其它形状（grok 等目录型或自有结构）识别不到时返回 ok=false，
// 前端据此显示占位符，而不是误导性的 0。
func sessionUsageStats(path string) (msgs int, in, out int64, ok bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Size() > sessionStatsMaxBytes {
		return 0, 0, 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20) // 单行上限 4MB：带 usage 的 assistant 行可能很长
	var sawUsage bool
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var rec map[string]any
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		if sessionRecordIsMessage(rec) {
			msgs++
		}
		// Claude 系：逐条 usage 累加（input 侧含缓存读 / 缓存写）
		if u, found := sessionStatsMap(rec, "message", "usage"); found {
			in += sessionStatsInt(u["input_tokens"]) +
				sessionStatsInt(u["cache_creation_input_tokens"]) +
				sessionStatsInt(u["cache_read_input_tokens"])
			out += sessionStatsInt(u["output_tokens"])
			sawUsage = true
		}
		// Codex：total_token_usage 是累计值，直接覆盖
		if t, found := sessionStatsMap(rec, "payload", "info", "total_token_usage"); found {
			in, out = sessionStatsInt(t["input_tokens"]), sessionStatsInt(t["output_tokens"])
			sawUsage = true
		}
	}
	return msgs, in, out, msgs > 0 || sawUsage
}

// sessionRecordIsMessage 判断一条会话记录是否代表一次对话消息（覆盖各客户端已知形状）：
//
//	Claude 系      {"type":"user"|"assistant"}
//	通用           {"role":"user"|"assistant"} / {"message":{"role":…}}
//	Codex rollout  旧 {"payload":{"type":"user_message"|"agent_message"}}
//	               新 {"payload":{"type":"message","role":"user"|"assistant"}}
//	               （payload.role = developer 属开发者指令，不计入对话消息）
func sessionRecordIsMessage(rec map[string]any) bool {
	if s, _ := rec["type"].(string); s == "user" || s == "assistant" {
		return true
	}
	if s, _ := rec["role"].(string); s == "user" || s == "assistant" {
		return true
	}
	if m, found := rec["message"].(map[string]any); found {
		if s, _ := m["role"].(string); s == "user" || s == "assistant" {
			return true
		}
	}
	if p, found := rec["payload"].(map[string]any); found {
		if s, _ := p["type"].(string); s == "user_message" || s == "agent_message" {
			return true
		}
		// Codex 新版：payload.type = message，角色落在 payload.role
		if s, _ := p["type"].(string); s == "message" {
			if r, _ := p["role"].(string); r == "user" || r == "assistant" {
				return true
			}
		}
	}
	return false
}

// sessionStatsMap 按 key 路径逐层取子对象（任一层缺失或类型不符返回 false）。
func sessionStatsMap(rec map[string]any, keys ...string) (map[string]any, bool) {
	cur := rec
	for i, k := range keys {
		v, found := cur[k]
		if !found {
			return nil, false
		}
		m, isMap := v.(map[string]any)
		if !isMap {
			return nil, false
		}
		if i == len(keys)-1 {
			return m, true
		}
		cur = m
	}
	return nil, false
}

// sessionStatsInt 兼容 float64 / json.Number 等数值形状取整。
func sessionStatsInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// ---- 目录型会话（grok-build / kimi / opencode 等）：没有单一会话文件，标题与统计从目录内取 ----

// dirSessionMeta 取目录型会话的标题与统计：
//  1. 优先按已知客户端结构解析：grok-build 的 summary.json（generated_title /
//     num_chat_messages）与 signals.json（userMessageCount + assistantMessageCount）；
//  2. 通用兜底：目录内第一个 jsonl（≤8MB）按 jsonl 会话扫描（kimi / opencode 等目录内常见）。
//
// 都取不到时返回 ok=false，前端回退显示会话 ID。
func dirSessionMeta(clientID, dir string) (title string, msgs int, in, out int64, ok bool) {
	if clientID == "grok-build" {
		title, msgs, ok = grokSessionMeta(dir)
		if title != "" || msgs > 0 {
			return title, msgs, 0, 0, ok
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, 0, 0, false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jsonl") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if fi, ferr := e.Info(); ferr == nil && fi.Size() > 8<<20 {
			continue
		}
		t := sessionTitle(p)
		m, tin, tout, mok := sessionUsageStats(p)
		if t == "" && m == 0 {
			continue
		}
		return t, m, tin, tout, mok
	}
	return "", 0, 0, 0, false
}

// grokSessionMeta 解析 grok-build 会话目录：
// 标题取 summary.json 的 generated_title（为空时回退 session_summary 截断），
// 消息条数取 signals.json 的「用户 + 助手」计数（回退 summary.json 的 num_chat_messages）。
// grok 不落累计 token 用量，故 token 侧留 0（前端显示占位）。
func grokSessionMeta(dir string) (title string, msgs int, ok bool) {
	var sum struct {
		GeneratedTitle  string `json:"generated_title"`
		SessionSummary  string `json:"session_summary"`
		NumChatMessages int    `json:"num_chat_messages"`
	}
	if b, err := os.ReadFile(filepath.Join(dir, "summary.json")); err == nil {
		if json.Unmarshal(b, &sum) == nil {
			title = strings.TrimSpace(sum.GeneratedTitle)
			if title == "" {
				title = truncateTitle(strings.TrimSpace(sum.SessionSummary))
			}
			msgs = sum.NumChatMessages
			ok = title != "" || msgs > 0
		}
	}
	var sig struct {
		User      int `json:"userMessageCount"`
		Assistant int `json:"assistantMessageCount"`
	}
	if b, err := os.ReadFile(filepath.Join(dir, "signals.json")); err == nil {
		if json.Unmarshal(b, &sig) == nil && sig.User+sig.Assistant > 0 {
			msgs = sig.User + sig.Assistant
			ok = true
		}
	}
	return title, msgs, ok
}

// truncateTitle 按展示口径截断标题（与 sessionTitle 的截断保持一致）。
func truncateTitle(s string) string {
	if rs := []rune(s); len(rs) > sessionTitleMaxRunes {
		return string(rs[:sessionTitleMaxRunes]) + "…"
	}
	return s
}

func sessionTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20) // 单行上限 1MB：会话行可能很长
	for i := 0; i < 200 && sc.Scan(); i++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var rec any
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		if s := userTextFromRecord(rec); s != "" {
			if rs := []rune(s); len(rs) > sessionTitleMaxRunes {
				return string(rs[:sessionTitleMaxRunes]) + "…"
			}
			return s
		}
	}
	return ""
}

// userTextFromRecord 从一条会话记录里取用户消息文本，按各客户端已知的 JSON 形状尝试：
//
//	Claude 风格   {"type":"user","message":{"content":"…" | [{"type":"text","text":"…"}]}}
//	Codex rollout {"type":"event_msg","payload":{"type":"user_message","message":"…"}}
//	通用          {"role":"user","content":…} / {"message":{"role":"user","content":…}}
func userTextFromRecord(rec any) string {
	m, ok := rec.(map[string]any)
	if !ok {
		return ""
	}
	if t, _ := m["type"].(string); t == "user" {
		if msg, ok := m["message"].(map[string]any); ok {
			if s := textOfContent(msg["content"]); s != "" {
				return s
			}
		}
		if s := textOfContent(m["content"]); s != "" {
			return s
		}
	}
	if p, ok := m["payload"].(map[string]any); ok {
		if pt, _ := p["type"].(string); pt == "user_message" || pt == "user" {
			if s, _ := p["message"].(string); s != "" {
				if cleaned := cleanUserText(s); cleaned != "" {
					return cleaned
				}
			}
			if s := textOfContent(p["content"]); s != "" {
				return s
			}
		}
		if r, _ := p["role"].(string); r == "user" {
			if s := textOfContent(p["content"]); s != "" {
				return s
			}
		}
	}
	if r, _ := m["role"].(string); r == "user" {
		if s := textOfContent(m["content"]); s != "" {
			return s
		}
	}
	if msg, ok := m["message"].(map[string]any); ok {
		if r, _ := msg["role"].(string); r == "user" {
			if s := textOfContent(msg["content"]); s != "" {
				return s
			}
		}
	}
	return ""
}

// textOfContent 处理消息 content：
//
//	字符串
//	[{type:"text"|"input_text", text:"…"}]（Claude / Codex）
//	[{text:"…"}]（不带 type 的形状，直接按 text 取）
func textOfContent(v any) string {
	switch c := v.(type) {
	case string:
		return cleanUserText(c)
	case []any:
		for _, it := range c {
			mm, ok := it.(map[string]any)
			if !ok {
				continue
			}
			// 带 type 的元素只认文本类型；不带 type 的直接按 text 取
			if t, hasType := mm["type"].(string); hasType && t != "text" && t != "input_text" {
				continue
			}
			if s, _ := mm["text"].(string); s != "" {
				if cleaned := cleanUserText(s); cleaned != "" {
					return cleaned
				}
			}
		}
	}
	return ""
}

// cleanUserText 归一化候选标题：跳过系统注入/命令包装的记录（以 "<" 开头、
// Caveat/System 前缀等），压平空白；过滤后为空则视为不是标题，继续找下一条。
func cleanUserText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(s, "<") || strings.HasPrefix(low, "caveat:") || strings.HasPrefix(low, "system:") {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
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
		// 截到配对的双引号（跳过转义字符），再交给 strconv.Unquote 还原：
		// 手写「反斜杠取下一字节」会把 \u5f20 解成 u5f20，非 ASCII 路径（中文
		// 用户名、Unicode 目录）读出来就是坏的，会话恢复会静默回退到主目录。
		start := k
		for k < len(s) {
			if s[k] == '\\' {
				k += 2
				continue
			}
			if s[k] == '"' {
				break
			}
			k++
		}
		if k >= len(s) {
			return "", false
		}
		raw := s[start:k]
		if v, uerr := strconv.Unquote(`"` + raw + `"`); uerr == nil {
			return v, true
		}
		return raw, true // 非法转义序列时退回原文，不因为一个坏字符丢掉整条路径
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
	if st.CLI != nil { // 库型存储：交给客户端的删除子命令
		return deleteCLISessions(clientID, st.CLI, paths)
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
