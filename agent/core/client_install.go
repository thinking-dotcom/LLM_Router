package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 客户端 CLI 的安装/版本信息：供「客户端」页卡片下半区的「安装 / 更新 / 版本号」使用。
//
// 设计要点：
//   - Bin：CLI 可执行名（PATH 中查找）。用于两类场景：① npm 全局包读不到版本时
//     现场执行 `<bin> --version` 探测；② 判断「本机是否已安装」。
//   - Pkg：npm 全局包名。非空 = 支持一键安装/更新（npm install -g <pkg>[@latest]）；
//     空 = 官方脚本/单二进制安装，不做自动安装，只探测版本（如 agy）。
//   - 版本优先读 npm 全局包自己的 package.json（离线、毫秒级、与 npm 口径一致），
//     读不到才现场执行 CLI 探测（异步、按需，不进列表接口的同步路径）。

// ClientInstall 描述一个客户端 CLI 的安装信息。
//
// 两类安装通道：
//   - Method="npm"：Pkg 为 npm 全局包名，安装/更新走 `npm install -g <pkg>[@latest]`，
//     卸载走 `npm uninstall -g <pkg>`（唯一支持自动卸载的通道）。
//   - Method="cmd"：非 npm 客户端（单二进制 / 官方脚本），按 InstallArgv / UpdateArgv
//     给出的 argv 直接执行（不走 shell 拼接，避免引号转义问题）。
//   - Method=""：未登记自动安装方式（前端按钮置灰并给出原因）。
type ClientInstall struct {
	Bin         string   // 可执行名（PATH 查找）
	Pkg         string   // npm 全局包名（Method="npm" 时必填）
	Method      string   // "npm" | "cmd"（空 = 不支持自动安装）
	InstallArgv []string // Method="cmd"：安装命令 argv
	InstallWin  []string // Method="cmd"：Windows 专用安装 argv（有则优先）
	UpdateArgv  []string // Method="cmd"：更新命令 argv（如 `agy update`）

	// 自动卸载要删除的二进制路径（Method="cmd" 且没有 npm 包时靠它清理本体）：
	// 支持 ~ 与 %LOCALAPPDATA% 之类环境变量，展开后按文件/目录删除。
	UninstallPaths    []string
	UninstallPathsWin []string
}

// uninstallPaths 返回当前平台要删除的二进制路径（Windows 优先取 UninstallPathsWin），
// 已展开 ~ 与环境变量；返回空表示没有可自动删除的本体。
func (c ClientInstall) uninstallPaths() []string {
	raw := c.UninstallPaths
	if runtime.GOOS == "windows" && len(c.UninstallPathsWin) > 0 {
		raw = c.UninstallPathsWin
	}
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if p = expandEnvPath(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// winEnvRe 匹配 Windows 风格的 %VAR%（os.ExpandEnv 只认 $VAR / ${VAR}）。
var winEnvRe = regexp.MustCompile(`%[A-Za-z_][A-Za-z0-9_]*%`)

// expandEnvPath 展开路径里的 ~ 与环境变量（同时支持 %VAR% 与 $VAR 两种写法）。
func expandEnvPath(p string) string {
	p = expandHome(p)
	if runtime.GOOS == "windows" {
		p = winEnvRe.ReplaceAllStringFunc(p, func(m string) string {
			if v := os.Getenv(m[1 : len(m)-1]); v != "" {
				return v
			}
			return m
		})
	}
	return os.ExpandEnv(p)
}

// installArgv 返回当前平台的安装 argv（Windows 优先取 InstallWin）。
func (c ClientInstall) installArgv() []string {
	if runtime.GOOS == "windows" && len(c.InstallWin) > 0 {
		return c.InstallWin
	}
	return c.InstallArgv
}

// installDisplay 返回可展示给用户的安装命令（前端 tooltip 用，透明说明将执行什么）。
func (c ClientInstall) installDisplay() string {
	if c.Method == "npm" {
		return "npm install -g " + c.Pkg
	}
	return strings.Join(c.installArgv(), " ")
}

// updateDisplay 返回可展示给用户的更新命令（空 = 未登记自动更新）。
func (c ClientInstall) updateDisplay() string {
	if c.Method == "npm" {
		return "npm install -g " + c.Pkg + "@latest"
	}
	return strings.Join(c.UpdateArgv, " ")
}

// clientInstalls 是各客户端 CLI 的「可执行名 + 安装通道」登记表：
// Method="npm" 走 `npm install -g`，Method="cmd" 走登记的官方命令 argv。
// npm 包名与 bin 名经本机 npm 全局安装目录逐个核对（npm ls -g --depth=0 + 各包 package.json 的 bin 字段）。
var clientInstalls = map[string]ClientInstall{
	"claude-code": {Bin: "claude", Pkg: "@anthropic-ai/claude-code", Method: "npm"},
	"codebuddy":   {Bin: "codebuddy", Pkg: "@tencent-ai/codebuddy-code", Method: "npm"},
	"codex":       {Bin: "codex", Pkg: "@openai/codex", Method: "npm"},
	"commandcode": {Bin: "commandcode", Pkg: "command-code", Method: "npm"},
	"copilot":     {Bin: "copilot", Pkg: "@github/copilot", Method: "npm"},
	"deepcode":    {Bin: "deepcode", Pkg: "@vegamo/deepcode-cli", Method: "npm"},
	"grok-build":  {Bin: "grok", Pkg: "@xai-official/grok", Method: "npm"},
	"kimi":        {Bin: "kimi", Pkg: "@moonshot-ai/kimi-code", Method: "npm"},
	// MiMo Code：包 @mimo-ai/cli，可执行名是 mimo（不是 mimocode）
	"mimocode": {Bin: "mimo", Pkg: "@mimo-ai/cli", Method: "npm"},
	"opencode": {Bin: "opencode", Pkg: "opencode-ai", Method: "npm"},
	"pi":       {Bin: "pi", Pkg: "@earendil-works/pi-coding-agent", Method: "npm"},
	// Antigravity CLI（agy）：Google 官方 Go 单二进制，**没有 npm 包**（registry 上的
	// `agy` / `antigravity` 都是同名占位包，与官方无关）。安装走官方脚本（用户指定
	// Windows 用 PowerShell 那条），更新走 CLI 自带的 `agy update`。
	"agy": {
		Bin:    "agy",
		Method: "cmd",
		InstallWin: []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command",
			"irm https://antigravity.google/cli/install.ps1 | iex"},
		InstallArgv: []string{"sh", "-c", "curl -fsSL https://antigravity.google/cli/install.sh | bash"},
		UpdateArgv:  []string{"agy", "update"},
		// 卸载：官方安装器把二进制放在 %LOCALAPPDATA%\agy（Windows）/ ~/.local/bin/agy（其它）
		UninstallPathsWin: []string{`%LOCALAPPDATA%\agy`},
		UninstallPaths:    []string{"~/.local/bin/agy"},
	},
}

// ClientInstallInfoOf 返回某客户端 CLI 的安装登记信息（未登记时 ok=false）。
func ClientInstallInfoOf(id string) (ClientInstall, bool) {
	ci, ok := clientInstalls[id]
	return ci, ok
}

// ClientBinOf 返回指定客户端登记的 CLI 可执行名（未登记时 ok=false）。
func ClientBinOf(id string) (string, bool) {
	ci, ok := clientInstalls[id]
	if !ok || ci.Bin == "" {
		return "", false
	}
	return ci.Bin, true
}

// chatClientOrder 「对话」终端候选客户端的固定展示顺序（常用在前）。
var chatClientOrder = []string{
	"claude-code", "codex", "kimi", "opencode", "grok-build",
	"commandcode", "codebuddy", "copilot", "pi", "mimocode", "deepcode", "agy",
}

// ChatClients 返回「对话」终端可选客户端清单：注册客户端 ∩ 登记了 CLI 可执行名者，
// 附 bin 名；id→显示名取目标注册表，避免两处名字漂移。
func ChatClients() []map[string]any {
	out := make([]map[string]any, 0, len(chatClientOrder))
	for _, id := range chatClientOrder {
		ci, ok := clientInstalls[id]
		if !ok || ci.Bin == "" {
			continue
		}
		name := id
		if a, found := TargetOf(id); found {
			name = a.Info().Name
		}
		out = append(out, map[string]any{"id": id, "name": name, "bin": ci.Bin})
	}
	return out
}

// decorateInstallInfo 把 CLI 安装信息填进 TargetInfo（列表接口用，全部为本机文件/PATH 探测，不起进程）。
func decorateInstallInfo(ti *TargetInfo) {
	ci, ok := clientInstalls[ti.ID]
	if !ok {
		return
	}
	ti.Bin, ti.Pkg = ci.Bin, ci.Pkg
	ti.CanInstall = ci.Method != "" // npm 或登记了命令的客户端都支持一键安装
	// 自动卸载：npm 通道走 npm uninstall；命令通道需登记了二进制路径（如 agy）
	ti.CanUninstall = ci.Pkg != "" || len(ci.uninstallPaths()) > 0
	ti.InstallCmd = ci.installDisplay()
	ti.UpdateCmd = ci.updateDisplay()
	if ps := ci.uninstallPaths(); len(ps) > 0 {
		ti.UninstallPaths = strings.Join(ps, "、")
	}
	if v := npmPkgVersion(ci.Pkg); v != "" {
		ti.Installed, ti.Version = true, v
		return
	}
	ti.Installed = lookBin(ci.Bin) != ""
}

// ---------- npm 全局包信息 ----------

// npmRootOnce/npmRootVal 缓存 `npm root -g` 结果（进程内只查一次）。
var npmRootOnce sync.Once
var npmRootVal string

// npmRootGlobal 返回 npm 全局 node_modules 目录；npm 不在 PATH 时返回空串。
func npmRootGlobal() string {
	npmRootOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		out, err := runHidden(ctx, "npm", "root", "-g")
		if err != nil {
			return
		}
		if s := strings.TrimSpace(string(out)); s != "" {
			npmRootVal = s
		}
	})
	return npmRootVal
}

// npmPkgVersion 读取 npm 全局安装的包版本（未安装返回空串）。
func npmPkgVersion(pkg string) string {
	if pkg == "" {
		return ""
	}
	root := npmRootGlobal()
	if root == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pkg), "package.json"))
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return strings.TrimSpace(m.Version)
}

// lookBin 在 PATH 中查找可执行文件；找不到返回空串。
func lookBin(bin string) string {
	if bin == "" {
		return ""
	}
	if p, err := exec.LookPath(bin); err == nil {
		return p
	}
	return ""
}

// ---------- 版本探测 ----------

// verRe 从 CLI 的 --version 输出里抓版本号（允许 1.2.3-beta.1 之类后缀）。
var verRe = regexp.MustCompile(`\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.]+)?`)

// ProbeClientVersion 现场执行 `<bin> --version` 解析版本号。
// 仅用于 npm 包版本读不到的场景（非 npm 安装、或 npm 不可用），按需调用、不阻塞列表接口。
func ProbeClientVersion(id string) (string, error) {
	ci, ok := clientInstalls[id]
	if !ok {
		return "", errors.New("未登记该客户端的安装信息：" + id)
	}
	if ci.Bin == "" {
		return "", errors.New("未登记该客户端的可执行名")
	}
	if lookBin(ci.Bin) == "" {
		return "", fmt.Errorf("未在 PATH 中找到 %s，未安装或未加入 PATH", ci.Bin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := runHidden(ctx, ci.Bin, "--version")
	txt := strings.TrimSpace(string(out))
	if m := verRe.FindString(txt); m != "" {
		return m, nil
	}
	if err != nil {
		// 超时/退出码非 0 时给出去尾的错误摘要，便于用户判断
		if msg := firstLine(txt); msg != "" {
			return "", fmt.Errorf("%s --version 执行失败：%s", ci.Bin, msg)
		}
		return "", fmt.Errorf("%s --version 执行失败：%v", ci.Bin, err)
	}
	if msg := firstLine(txt); msg != "" {
		return "", fmt.Errorf("无法从输出解析版本号：%s", msg)
	}
	return "", errors.New("无法从输出解析版本号")
}

// firstLine 取文本首行（截断到 120 字符），用于错误提示。
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// ---------- 安装 / 更新 / 卸载 ----------

// npm 类操作（安装/更新/卸载）同一客户端不并行跑两条。
var opMu sync.Mutex
var opRunning = map[string]bool{}

func beginOp(id string) error {
	opMu.Lock()
	defer opMu.Unlock()
	if opRunning[id] {
		return errors.New("该客户端正在安装/卸载中，请稍候")
	}
	opRunning[id] = true
	return nil
}
func endOp(id string) {
	opMu.Lock()
	delete(opRunning, id)
	opMu.Unlock()
}

// installerOf 取客户端的安装登记信息；未登记安装方式时给出可直接展示给用户的原因。
func installerOf(id string) (ClientInstall, error) {
	ci, ok := clientInstalls[id]
	if !ok {
		return ci, errors.New("未登记该客户端的安装信息：" + id)
	}
	if ci.Method == "" || (ci.Method == "npm" && ci.Pkg == "") {
		return ci, errors.New("该客户端未登记自动安装方式（非 npm 且无官方命令登记），请按官方文档安装")
	}
	return ci, nil
}

// npmPkgOf 取客户端的 npm 包名（仅卸载用）；非 npm 安装时给出原因。
func npmPkgOf(id string) (string, error) {
	ci, ok := clientInstalls[id]
	if !ok {
		return "", errors.New("未登记该客户端的安装信息：" + id)
	}
	if ci.Pkg == "" {
		return "", errors.New("该客户端不是通过 npm 安装的，未登记自动卸载方式，请按官方文档卸载")
	}
	return ci.Pkg, nil
}

// InstallClientCLI 用 npm 全局安装/更新某客户端 CLI：update=true → `<pkg>@latest`，
// false → `<pkg>`（未安装时的首次安装）。返回 (输出尾部, 安装后的版本, 错误)。
func InstallClientCLI(id string, update bool) (string, string, error) {
	ci, err := installerOf(id)
	if err != nil {
		return "", "", err
	}
	if ci.Method == "cmd" {
		return installByCmd(ci, id, update)
	}
	pkg := ci.Pkg
	if err := beginOp(id); err != nil {
		return "", "", err
	}
	defer endOp(id)

	spec := pkg
	if update {
		spec += "@latest"
	}
	// npm 全局安装可能数十秒到数分钟：给足超时，避免中途被截断成"失败"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := runHidden(ctx, "npm", "install", "-g", spec)
	if err != nil && strings.Contains(string(out), "EBADPLATFORM") {
		// latest 标签解析到的版本不支持本平台（如 @xai-official/grok 的 latest
		// 长期停在 darwin/arm64 的 0.1.4，而 1.0.x 已全平台）：从发布序列由新到旧
		// 找第一个支持当前平台的版本重试
		if v, verr := newestPlatformVersion(pkg); verr == nil && v != "" {
			retry := pkg + "@" + v
			out2, err2 := runHidden(ctx, "npm", "install", "-g", retry)
			if err2 == nil {
				txt2 := tailText(strings.TrimSpace(string(out2)), 1200)
				return txt2, npmPkgVersion(pkg), nil
			}
			out, err = out2, err2
		}
	}
	txt := tailText(strings.TrimSpace(string(out)), 1200)
	ver := npmPkgVersion(pkg) // 装完重读包版本（npmRoot 已缓存，只读文件）
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return txt, ver, errors.New("npm install 超时（10 分钟），已中止")
		}
		msg := firstLine(txt)
		if msg == "" {
			msg = err.Error()
		}
		return txt, ver, fmt.Errorf("npm install 失败：%s", msg)
	}
	return txt, ver, nil
}

// npmOS / npmCPU 把 Go 平台名映射为 npm 的 os/cpu 字段取值（npm 用 win32/ia32 等）。
func npmOS() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	default:
		return runtime.GOOS
	}
}

func npmCPU() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	default:
		return runtime.GOARCH
	}
}

// platformAllows 判断 npm 包声明的 os/cpu 白名单是否放行当前平台（缺省字段 = 不限）。
func platformAllows(osList, cpuList []string) bool {
	if len(osList) > 0 && !contains(osList, npmOS()) {
		return false
	}
	if len(cpuList) > 0 && !contains(cpuList, npmCPU()) {
		return false
	}
	return true
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// newestPlatformVersion 返回包发布序列里由新到旧第一个支持当前平台的版本号
// （最多回看 8 个；查询失败返回错误，找不到返回空串）。
func newestPlatformVersion(pkg string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := runHidden(ctx, "npm", "view", pkg, "versions", "--json")
	if err != nil {
		return "", fmt.Errorf("npm view versions 失败: %w", err)
	}
	var versions []string
	if err := json.Unmarshal(out, &versions); err != nil {
		return "", fmt.Errorf("npm view versions 输出解析失败: %w", err)
	}
	for i := len(versions) - 1; i >= 0 && i >= len(versions)-8; i-- {
		vctx, vcancel := context.WithTimeout(context.Background(), 30*time.Second)
		vo, verr := runHidden(vctx, "npm", "view", pkg+"@"+versions[i], "os", "cpu", "--json")
		vcancel()
		if verr != nil {
			continue
		}
		var fields struct {
			OS  []string `json:"os"`
			CPU []string `json:"cpu"`
		}
		if err := json.Unmarshal(vo, &fields); err != nil {
			continue
		}
		if platformAllows(fields.OS, fields.CPU) {
			return versions[i], nil
		}
	}
	return "", nil
}

// installByCmd 非 npm 客户端（单二进制 / 官方脚本）：按登记的 argv 执行安装或更新。
// 命令以 argv 形式执行（不经 cmd /c 拼接），避免引号与管道被二次转义。
func installByCmd(ci ClientInstall, id string, update bool) (string, string, error) {
	argv := ci.installArgv()
	if update {
		if len(ci.UpdateArgv) == 0 {
			return "", "", errors.New("该客户端未登记自动更新命令，请按官方文档更新")
		}
		argv = ci.UpdateArgv
	}
	if len(argv) == 0 {
		return "", "", errors.New("该客户端未登记安装命令，请按官方文档安装")
	}
	if err := beginOp(id); err != nil {
		return "", "", err
	}
	defer endOp(id)

	// 下载/安装单二进制可能很慢（agy 约 160MB）：与 npm 同档超时
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := runArgv(ctx, argv)
	txt := tailText(strings.TrimSpace(string(out)), 1200)
	// 版本回读：npm 包读不到就现场跑 `<bin> --version`
	ver := npmPkgVersion(ci.Pkg)
	if ver == "" {
		if v, e := ProbeClientVersion(id); e == nil {
			ver = v
		}
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return txt, ver, errors.New("命令执行超时（10 分钟），已中止")
		}
		msg := firstLine(txt)
		if msg == "" {
			msg = err.Error()
		}
		return txt, ver, fmt.Errorf("%s 失败：%s", argv[0], msg)
	}
	return txt, ver, nil
}

// UninstallClientCLI 卸载某客户端 CLI：npm 通道走 `npm uninstall -g <pkg>`，
// 命令通道删除登记的二进制路径。
// purge=true 时进一步「彻底清理」：撤销本程序注入的 skills 后，删除该客户端的
// 全部本机足迹（配置/凭据/会话/缓存，整目录删除，见 clientResidue），再在标准
// 配置根按客户端名做同名兜底清扫，最后删除 client-paths.json 登记的配置路径。
// 保护：登记给其它客户端的路径一律不碰（共用根如 ~/.gemini 不受影响）。
// 返回 (输出尾部, 已删除的路径列表, 错误)。
func UninstallClientCLI(id string, purge bool) (string, []string, error) {
	ci, ok := clientInstalls[id]
	if !ok {
		return "", nil, errors.New("未登记该客户端的安装信息：" + id)
	}
	paths := ci.uninstallPaths()
	if ci.Pkg == "" && len(paths) == 0 {
		return "", nil, errors.New("该客户端未登记自动卸载方式（非 npm 安装且不知其二进制位置），请按官方文档卸载")
	}
	if err := beginOp(id); err != nil {
		return "", nil, err
	}
	defer endOp(id)

	var txt string
	var removed []string
	if ci.Pkg != "" {
		// npm 通道：npm uninstall -g <pkg>
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		out, err := runHidden(ctx, "npm", "uninstall", "-g", ci.Pkg)
		cancel()
		txt = tailText(strings.TrimSpace(string(out)), 1200)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return txt, nil, errors.New("npm uninstall 超时（10 分钟），已中止")
			}
			msg := firstLine(txt)
			if msg == "" {
				msg = err.Error()
			}
			return txt, nil, fmt.Errorf("npm uninstall 失败：%s", msg)
		}
	} else {
		// 命令通道（单二进制）：直接删除登记的二进制路径
		protected := appProtectedPaths()
		for _, p := range paths {
			if !underSafeRoot(p) {
				continue // 兜底：只动主目录 / LOCALAPPDATA 下的路径
			}
			if isAppProtectedPath(p, protected) {
				continue // 绝不删到本程序的数据目录 / 自身所在目录
			}
			if err := os.RemoveAll(p); err != nil {
				continue
			}
			removed = append(removed, p)
			pruneEmptyDirs(filepath.Dir(p), HomeDir(), 2)
		}
	}
	if !purge {
		return txt, removed, nil
	}
	return txt, append(removed, purgeClientConfig(id)...), nil
}

// underSafeRoot 判断路径是否落在「可安全删除」的范围内（用户主目录 / Windows 的
// LOCALAPPDATA、APPDATA、USERPROFILE）。卸载删除本体前的兜底，避免误删系统目录。
func underSafeRoot(p string) bool {
	if p == "" {
		return false
	}
	roots := []string{}
	if home := HomeDir(); home != "" {
		roots = append(roots, home)
	}
	if runtime.GOOS == "windows" {
		for _, k := range []string{"LOCALAPPDATA", "APPDATA", "USERPROFILE"} {
			if v := os.Getenv(k); v != "" {
				roots = append(roots, v)
			}
		}
	}
	for _, r := range roots {
		if r == "" {
			continue
		}
		if p == r || strings.HasPrefix(p, r+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// purgeClientConfig 彻底清理该客户端在本机的全部残留，返回实际删除的路径。
// 三层来源：① clientResidue 登记的足迹（配置/凭据/会话/缓存，整目录删除）；
// ② 标准配置根下的同名兜底清扫（ResidueNames，捞登记外的漏网目录）；
// ③ client-paths.json 登记的配置路径（用户可覆盖到足迹清单之外）。
// 全部删除动作限定在用户主目录 / Windows APPDATA 根之内；登记给其它客户端的
// 路径及其祖先一律跳过，避免 ~/.gemini 之类共用根被误伤。
func purgeClientConfig(id string) []string {
	// 先撤销本程序注入的 skills（只删注入清单里的，避免误删共用目录里其它来源的内容）
	if a, ok := registry[id]; ok {
		_ = a.UndoSkills()
	}
	home := HomeDir()
	// 其它客户端登记的路径：删除禁区（p 等于或包含其中之一即跳过）
	var blocked []string
	for oid, kv := range ClientRealPaths() {
		if oid == id {
			continue
		}
		for _, q := range kv {
			if q != "" {
				blocked = append(blocked, q)
			}
		}
	}
	// 本程序自身的数据目录与可执行文件目录：绝不随客户端卸载一起删
	protected := appProtectedPaths()
	seen := map[string]bool{}
	removed := []string{}
	removeOne := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		if home == "" || p == home || !underSafeRoot(p) {
			return
		}
		if isAppProtectedPath(p, protected) {
			return
		}
		if residueBlocked(p, blocked) {
			return
		}
		if _, err := os.Lstat(p); err != nil {
			return // 不存在（或已随父目录删除）不计入
		}
		if err := os.RemoveAll(p); err != nil {
			return
		}
		removed = append(removed, p)
		pruneEmptyDirs(filepath.Dir(p), home, 3)
	}
	for _, p := range residuePaths(id) {
		removeOne(p)
	}
	for _, p := range sweepResidueByName(id) {
		removeOne(p)
	}
	for _, p := range ClientRealPaths()[id] {
		removeOne(p)
	}
	return removed
}

// appProtectedPaths 返回本程序自身的「禁止连坐删除」路径：数据目录（data/skills、
// data/mcp 都在其下）与可执行文件所在目录。卸载客户端只该清理客户端自己的文件，
// 任何顺带删到这些路径、或删掉它们的祖先目录的操作都必须拒绝。
// 主目录 / 盘根这类过宽的目录不算（否则会挡住所有正常清理）。
func appProtectedPaths() []string {
	out := make([]string, 0, 3)
	if d := DataDir(); d != "" {
		out = append(out, filepath.Clean(d))
	}
	home := filepath.Clean(HomeDir())
	if exe, err := os.Executable(); err == nil {
		if p, aerr := filepath.Abs(exe); aerr == nil {
			p = filepath.Clean(p)
			dir := filepath.Dir(p)
			if dir != home && dir != p && filepath.Dir(dir) != dir {
				out = append(out, dir)
			}
		}
	}
	return out
}

// isAppProtectedPath 判断 p 是否等于保护路径、位于其内部、或是其祖先目录。
func isAppProtectedPath(p string, protected []string) bool {
	cp := filepath.Clean(p)
	sep := string(os.PathSeparator)
	for _, q := range protected {
		if q == "" {
			continue
		}
		cq := filepath.Clean(q)
		if cp == cq {
			return true
		}
		if strings.HasPrefix(cq, cp+sep) { // p 是保护路径的祖先：删它等于连坐
			return true
		}
		if strings.HasPrefix(cp, cq+sep) { // p 落在保护路径内部
			return true
		}
	}
	return false
}

// residueBlocked 判断 p 是否会碰到禁区路径（p 是禁区之一或其祖先）。
func residueBlocked(p string, blocked []string) bool {
	sep := string(os.PathSeparator)
	for _, q := range blocked {
		if q == p || strings.HasPrefix(q, p+sep) {
			return true
		}
	}
	return false
}

// clientResidueInfo 描述一个客户端在本机的「足迹」。
type clientResidueInfo struct {
	// Paths 整删的目录/文件：配置、登录凭据、历史会话、缓存（npm uninstall 只删
	// 包本身，这些会全部留下）。支持 ~ 与 %VAR% 展开，删除前仍过 underSafeRoot 校验。
	Paths []string
	// Names 同名兜底清扫关键词：在标准配置根顶层做精确名匹配（允许前导点与
	// .json 后缀），捞登记之外的漏网残留；必须足够特异，禁止宽泛词（如 cli）。
	Names []string
}

// clientResidue 各客户端的本机足迹登记表（与 clientInstalls 同键）。
// 路径为主目录相对写法，跨平台通用；Windows 专属位置用 %LOCALAPPDATA% 等表达。
var clientResidue = map[string]clientResidueInfo{
	"claude-code": {
		Paths: []string{"~/.claude", "~/.claude.json", "~/.claude.json.backup"},
		Names: []string{"claude", "claude-code"},
	},
	"codex": {Paths: []string{"~/.codex"}, Names: []string{"codex"}},
	"kimi":  {Paths: []string{"~/.kimi-code", "~/.kimi"}, Names: []string{"kimi", "kimi-code"}},
	"opencode": {
		Paths: []string{"~/.config/opencode", "~/.local/share/opencode", "~/.cache/opencode", "~/.opencode"},
		Names: []string{"opencode"},
	},
	"pi":          {Paths: []string{"~/.pi"}, Names: []string{"pi", "pi-coding-agent"}},
	"grok-build":  {Paths: []string{"~/.grok"}, Names: []string{"grok", "grok-build"}},
	"commandcode": {Paths: []string{"~/.commandcode", "~/.command-code"}, Names: []string{"commandcode", "command-code"}},
	"codebuddy":   {Paths: []string{"~/.codebuddy"}, Names: []string{"codebuddy"}},
	"copilot":     {Paths: []string{"~/.copilot"}, Names: []string{"copilot"}},
	"mimocode": {
		Paths: []string{"~/.config/mimocode", "~/.mimocode", "~/.mimo"},
		Names: []string{"mimocode", "mimo"},
	},
	"deepcode": {Paths: []string{"~/.deepcode"}, Names: []string{"deepcode"}},
	// agy 本体（%LOCALAPPDATA%\agy）在非 purge 路径已删；这里补其配置足迹。
	// ~/.gemini 本体是共用根，绝不整删，只动其中 agy 自己的子目录。
	"agy": {
		Paths: []string{"~/.gemini/antigravity-cli", "~/.gemini/config", "~/.antigravity", `%LOCALAPPDATA%\agy`, "~/.local/bin/agy"},
		Names: []string{"agy", "antigravity", "antigravity-cli"},
	},
}

// clientResidueOf 取某客户端的足迹登记（未登记返回零值，不报错——还有同名清扫兜底）。
func clientResidueOf(id string) clientResidueInfo {
	return clientResidue[id]
}

// residuePaths 返回某客户端足迹清单展开后的路径（~ 与 %VAR% 已展开，去重）。
func residuePaths(id string) []string {
	raw := clientResidue[id].Paths
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, p := range raw {
		if p = expandEnvPath(p); p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// residueSweepRoots 同名兜底清扫的标准配置根（各 CLI 生态事实上的存放位置）。
var residueSweepRoots = []string{"~", "~/.config", "~/.cache", "~/.local/share", "~/.local/state"}

// sweepResidueByName 在标准配置根顶层按客户端名精确匹配，返回疑似残留路径。
// 只匹配直接子项（目录或文件），不递归——递归扫全盘既慢又容易误伤同名内容。
func sweepResidueByName(id string) []string {
	names := clientResidue[id].Names
	if len(names) == 0 {
		return nil
	}
	var out []string
	for _, root := range residueSweepRoots {
		dir := expandEnvPath(root)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			base := strings.ToLower(e.Name())
			base = strings.TrimPrefix(base, ".")
			if residueNameMatch(base, names) {
				out = append(out, filepath.Join(dir, e.Name()))
			}
		}
	}
	return out
}

// residueNameMatch 精确名匹配：等于关键词本身，或带 .json 后缀（~/.claude.json 类）。
func residueNameMatch(base string, names []string) bool {
	for _, n := range names {
		if base == n || base == n+".json" {
			return true
		}
	}
	return false
}

// pruneEmptyDirs 自下而上回收空目录：最多 maxLevel 层，不越过 stopAt（用户主目录）。
func pruneEmptyDirs(dir, stopAt string, maxLevel int) {
	if stopAt == "" {
		return
	}
	for i := 0; i < maxLevel; i++ {
		if dir == stopAt || !strings.HasPrefix(dir, stopAt) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// tailText 只保留输出尾部（安装日志很长，前端只展示末尾以便看结论/报错）。
func tailText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// 尽量从一个完整行开始截，避免出现半行
	cut := s[len(s)-max:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i+1 < len(cut) {
		cut = cut[i+1:]
	}
	return "…" + cut
}

// runHidden 执行外部命令并合并 stdout/stderr。
// Windows 上统一经 cmd.exe 执行：npm / 多数 CLI 是 .cmd 批处理垫片，直接
// CreateProcess 不可靠；同时用 hideWindow 关掉控制台窗口（GUI 进程不闪黑框）。
// runArgv 直接执行 argv[0]（不经 cmd.exe 包装）：用于已明确给出可执行文件的场景
// （如 powershell.exe / agy），避免 cmd 对引号与管道做二次转义。
func runArgv(ctx context.Context, argv []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	hideWindow(cmd)
	return cmd.CombinedOutput()
}

func runHidden(ctx context.Context, name string, args ...string) ([]byte, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", append([]string{"/c", name}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, name, args...)
	}
	hideWindow(cmd)
	return cmd.CombinedOutput()
}

// ---------- 最新版本探测（npm registry） ----------
// 「客户端已是最新版 → 更新按钮置灰」需要知道 registry 上的最新版本。
// 探测走 npm 配置的镜像源（npm config get registry，缓存一次），
// 每个包一个并行 HTTP 请求（<registry>/<pkg>/latest），整批不阻塞 UI。

var registryOnce sync.Once
var registryURL string

// npmRegistry 返回 npm 配置的 registry 地址（失败回退官方源）。
func npmRegistry() string {
	registryOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		out, err := runHidden(ctx, "npm", "config", "get", "registry")
		if err == nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				registryURL = strings.TrimRight(s, "/")
			}
		}
	})
	if registryURL == "" {
		registryURL = "https://registry.npmjs.org"
	}
	return registryURL
}

var npmHTTP = &http.Client{Timeout: 8 * time.Second}

// npmLatestVersion 查询某包在 registry 上的最新版本。
func npmLatestVersion(pkg string) (string, error) {
	u := npmRegistry() + "/" + url.PathEscape(pkg) + "/latest"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := npmHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry 返回 %d", resp.StatusCode)
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return "", err
	}
	if strings.TrimSpace(m.Version) == "" {
		return "", errors.New("registry 未返回版本号")
	}
	return strings.TrimSpace(m.Version), nil
}

// ClientUpdate 是单个客户端的「是否有新版本」探测结果。
type ClientUpdate struct {
	Installed string `json:"installed"`        // 本机版本（空 = 未通过 npm 安装）
	Latest    string `json:"latest,omitempty"` // registry 最新版本
	Outdated  bool   `json:"outdated"`         // 本机版本落后
	Error     string `json:"error,omitempty"`  // 探测失败原因（网络不通等），此时前端保持「更新」可用
}

// CheckClientUpdates 并行探测全部已登记 npm 包的最新版本，并与本机已装版本比较。
// 未安装的包不参与（无所谓更新）；探测失败不报错中断，单包记录 Error。
func CheckClientUpdates() map[string]ClientUpdate {
	out := make(map[string]ClientUpdate, len(clientInstalls))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for id, ci := range clientInstalls {
		if ci.Pkg == "" {
			continue
		}
		wg.Add(1)
		go func(id, pkg string) {
			defer wg.Done()
			res := ClientUpdate{}
			ver := npmPkgVersion(pkg)
			if ver == "" {
				mu.Lock()
				out[id] = res // 本机没装这个 npm 包：没有「更新」语义
				mu.Unlock()
				return
			}
			res.Installed = ver
			if latest, err := npmLatestVersion(pkg); err != nil {
				res.Error = err.Error()
			} else {
				res.Latest = latest
				res.Outdated = verCompare(ver, latest) < 0
			}
			mu.Lock()
			out[id] = res
			mu.Unlock()
		}(id, ci.Pkg)
	}
	wg.Wait()
	return out
}

// ---------- 语义化版本比较 ----------

type verParts struct {
	nums [3]int
	pre  []string // 预发布标识（无 = 正式发布）
}

var verParseRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.\-]+))?(?:\+[0-9A-Za-z.\-]+)?$`)

func parseVer(s string) (verParts, bool) {
	m := verParseRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return verParts{}, false
	}
	var v verParts
	v.nums[0], _ = strconv.Atoi(m[1])
	v.nums[1], _ = strconv.Atoi(m[2])
	v.nums[2], _ = strconv.Atoi(m[3])
	if m[4] != "" {
		v.pre = strings.Split(m[4], ".")
	}
	return v, true
}

// verCompare 比较两个版本：a<b → -1、相等 → 0、a>b → 1。
// 任一版本解析失败时退化为字符串比较（保证结果可用）。
func verCompare(a, b string) int {
	pa, oka := parseVer(a)
	pb, okb := parseVer(b)
	if !oka || !okb {
		return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	for i := 0; i < 3; i++ {
		if pa.nums[i] != pb.nums[i] {
			if pa.nums[i] < pb.nums[i] {
				return -1
			}
			return 1
		}
	}
	// 主/次/补丁相同：无预发布 > 有预发布
	if len(pa.pre) == 0 && len(pb.pre) == 0 {
		return 0
	}
	if len(pa.pre) == 0 {
		return 1
	}
	if len(pb.pre) == 0 {
		return -1
	}
	for i := 0; i < len(pa.pre) && i < len(pb.pre); i++ {
		x, y := pa.pre[i], pb.pre[i]
		xn, xe := strconv.Atoi(x)
		yn, ye := strconv.Atoi(y)
		switch {
		case xe == nil && ye != nil:
			return -1 // 数字标识 < 字母标识
		case xe != nil && ye == nil:
			return 1
		case xe == nil && ye == nil && xn != yn:
			if xn < yn {
				return -1
			}
			return 1
		case x != y:
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(pa.pre) < len(pb.pre):
		return -1
	case len(pa.pre) > len(pb.pre):
		return 1
	}
	return 0
}
