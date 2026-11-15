package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"llm-router-go/agent/clients"
	agentcore "llm-router-go/agent/core"
	"llm-router-go/config"
	"llm-router-go/core"
	"llm-router-go/router"
	"llm-router-go/types"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var embeddedFS embed.FS

type App struct {
	server    *core.Server
	ctx       context.Context
	forceQuit atomic.Bool // 托盘菜单「退出」置位（托盘线程写、OnBeforeClose 读），绕过隐藏拦截
}

// appRef 供托盘回调访问 App 实例（tray_windows.go 使用）
var appRef *App

// appVersion 是桌面程序当前版本号。有新版本时由前端在「关于」中显示下载入口。
// 上游发布最新版本与目的地址后，可在此或通过更新检查逻辑维护。
const appVersion = "1.0.0"

func (a *App) GetStatus() map[string]any {
	return map[string]any{
		"running":   a.server.Running(),
		"error":     a.server.StartError(),
		"port":      a.server.Port(),
		"host":      a.server.Host(),
		"localBase": a.server.LocalBase(),
		"providers": len(a.server.GetConfig().Providers),
	}
}

// GetAppInfo 返回桌面程序的「关于」信息：程序名、当前版本号，以及（若有）
// 更新可用时的最新版本号与下载地址。目的地址暂为空，供后续接入更新检查。
func (a *App) GetAppInfo() map[string]any {
	return map[string]any{
		"name":            "LLM Router",
		"version":         appVersion,
		"updateAvailable": false, // 是否检测到更新（暂未接入更新检查）
		"latestVersion":   "",    // 最新版本号（空 = 无更新）
		"downloadURL":     "",    // 下载地址（暂时为空）
	}
}

// ---- 下方为多目标 Agent 配置（skills / mcp 手动注入与撤销）----

// GetRouterBase 返回本路由器的接入地址（host:port），供「自动填路由器地址」使用。
func (a *App) GetRouterBase() string {
	return a.server.LocalBase()
}

// commandcodeNoLoginEnvVar 是 Command Code 的启动鉴权环境变量名：值非空即视为已登录，
// 不再弹浏览器登录窗；模型请求走哪由 config.json 的 provider（BYOK 供应商）决定，与此值无关。
// auth.json 的 apiKey 字段是同效兜底：env 变量只对「重开终端」生效，auth.json 当前终端立刻生效。
// 两处均为占位值，请求仍直连本路由器（providers.json 声明 apiKey:false），不会发往云端。
const commandcodeNoLoginEnvVar = "COMMAND_CODE_API_KEY"
const commandcodeNoLoginKey = "llmrouter-local"

func commandcodeAuthFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".commandcode", "auth.json")
}

// readCommandcodeAuthFile 返回 ~/.commandcode/auth.json 中的 apiKey（无文件/无字段返回空串）
func readCommandcodeAuthFile() string {
	b, err := os.ReadFile(commandcodeAuthFilePath())
	if err != nil {
		return ""
	}
	var v struct {
		APIKey string `json:"apiKey"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	return strings.TrimSpace(v.APIKey)
}

// writeAtomicFile 原子写文件（tmp+rename），避免进程写中途崩溃留下半截配置。
// agentcore（agent/core）的 writeFileAtomic 未导出，此处自带同型实现。
func writeAtomicFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(data)
	if writeErr == nil {
		writeErr = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if writeErr == nil {
		if renameErr := os.Rename(tmpName, path); renameErr == nil {
			tmpName = "" // 已改名成功，不再清理
		} else {
			// rename 失败必须如实上报：此前吞掉该错误会谎报成功，
			// 实际目标文件未更新（如 Windows 下目标被其它进程锁定时）
			writeErr = renameErr
		}
	}
	if tmpName != "" {
		_ = os.Remove(tmpName)
	}
	return writeErr
}

// writeCommandcodeAuthFile 写入/清除 auth.json 的 apiKey 字段（其余字段原样保留），
// 清除后若文件只剩空壳则删除整个文件
func writeCommandcodeAuthFile(enable bool) error {
	p := commandcodeAuthFilePath()
	if p == "" {
		return errors.New("无法定位用户主目录")
	}
	v := map[string]any{}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &v)
	}
	if enable {
		v["apiKey"] = commandcodeNoLoginKey
	} else {
		delete(v, "apiKey")
		if len(v) == 0 {
			os.Remove(p)
			return nil
		}
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(p, b)
}

// GetCommandcodeNoLogin 查询免登录凭据是否已写入（auth.json 的 apiKey 或环境变量）。
// Windows 环境变量读用户注册表（不受本进程启动时间影响），其余平台看当前进程环境。
func (a *App) GetCommandcodeNoLogin() bool {
	if readCommandcodeAuthFile() != "" {
		return true
	}
	if runtime.GOOS != "windows" {
		return strings.TrimSpace(os.Getenv(commandcodeNoLoginEnvVar)) != ""
	}
	cmd := exec.Command("reg", "query", `HKCU\Environment`, "/v", commandcodeNoLoginEnvVar)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(string(out), commandcodeNoLoginEnvVar)
}

// SetCommandcodeNoLogin 写入/清除免登录凭据：用户级环境变量（setx，重开终端生效）
// + ~/.commandcode/auth.json 的 apiKey（当前终端立即生效）。
// 返回空串 = 成功，否则为错误信息。
func (a *App) SetCommandcodeNoLogin(enable bool) string {
	if err := writeCommandcodeAuthFile(enable); err != nil {
		return "auth.json 写入失败：" + err.Error()
	}
	if runtime.GOOS != "windows" {
		if enable {
			return "当前平台已写入 auth.json 兜底；env 变量请手动在 shell 配置中导出 " + commandcodeNoLoginEnvVar
		}
		return ""
	}
	var cmd *exec.Cmd
	if enable {
		cmd = exec.Command("setx", commandcodeNoLoginEnvVar, commandcodeNoLoginKey)
		hideConsole(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			return strings.TrimSpace(string(out))
		}
		return ""
	}
	// 清除改用 agentcore.DelUserEnv 而非 reg delete：删除幂等（值不存在视为成功，
	// 与 GetCommandcodeNoLogin 的「从未开启也能清除」预期一致），且删除后会
	// 广播 WM_SETTINGCHANGE，已运行的 Explorer / 新开终端能立即感知变更
	if err := agentcore.DelUserEnv(commandcodeNoLoginEnvVar); err != nil {
		return "清除环境变量失败：" + err.Error()
	}
	return ""
}

// GetSkillsList 返回 data/skills 下的技能列表。
func (a *App) GetSkillsList() []agentcore.Skill {
	return agentcore.ListSkills()
}

// GetSkillsDir 返回技能存放目录（执行文件同目录的 data/skills），供前端展示。
func (a *App) GetSkillsDir() string {
	return agentcore.SkillsDir()
}

// SyncAllSkills 一键注入（inject=true）/ 一键踢出（inject=false）：把 data/skills
// 的全部技能整体复制进各支持客户端的技能目录，或从客户端技能目录移除本程序
// 注入的技能副本（按注入清单，客户端自有的其它文件不受影响）。
func (a *App) SyncAllSkills(inject bool) map[string]any {
	return map[string]any{"ok": true, "results": agentcore.BulkSyncSkills(inject)}
}

// OpenSkillsDir 在系统文件管理器中打开技能目录（目录不存在时先创建）
// openDirInFileManager 目录不存在则先创建，再用系统文件管理器打开。
func openDirInFileManager(dir string) map[string]any {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	go func() { _ = cmd.Wait() }() // explorer 总是返回非零码，后台回收避免僵尸进程
	return map[string]any{"ok": true}
}

func (a *App) OpenSkillsDir() map[string]any {
	return openDirInFileManager(agentcore.SkillsDir())
}

// GetMcpDir 返回 MCP 配置文件存放目录（执行文件同目录的 data/mcp），供前端展示。
func (a *App) GetMcpDir() string {
	return agentcore.McpDir()
}

// OpenMcpDir 在系统文件管理器中打开 MCP 配置目录（目录不存在时先创建）
func (a *App) OpenMcpDir() map[string]any {
	return openDirInFileManager(agentcore.McpDir())
}

// EjectAllMcp 一键踢出：清空所有客户端 MCP 配置文件里的全部服务（整体替换为空，
// 文件其它内容保留）。返回 ok / count / error（error 为空 = 全部成功）。
func (a *App) EjectAllMcp() map[string]any {
	n, errMsg := clients.EjectAllMcp()
	res := map[string]any{"ok": true, "count": n}
	if errMsg != "" {
		res["error"] = errMsg
	}
	return res
}

// RemoveMcpFromClient 从指定客户端的 MCP 配置文件中剔除单个服务（「踢出」：
// 整体替换法，其余服务与文件其它内容原样保留）。
func (a *App) RemoveMcpFromClient(clientID, serverName string) map[string]any {
	if errMsg := clients.RemoveMcpFromClient(clientID, serverName); errMsg != nil {
		return map[string]any{"ok": false, "error": errMsg.Error()}
	}
	return map[string]any{"ok": true}
}

// OpenClientMcpLocation 在系统文件管理器中打开指定客户端 MCP 配置文件所在位置并选中。
func (a *App) OpenClientMcpLocation(clientID string) map[string]any {
	p, err := agentcore.ClientMcpPath(clientID)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return a.OpenFileLocation(p)
}

// GetClientRealPaths 返回各客户端真实配置路径清单（~ 已展开为绝对路径），
// 来源 agent/Json_modles/client-paths.json（缺失时为内置默认），供前端定位真实文件。
func (a *App) GetClientRealPaths() map[string]any {
	return map[string]any{"ok": true, "paths": agentcore.ClientRealPaths()}
}

// OpenFileLocation 在系统文件管理器中打开真实配置文件所在目录并选中该文件。
// path 支持以 ~ 开头的主目录缩写；文件不存在时返回错误（不自动创建）。
func (a *App) OpenFileLocation(path string) map[string]any {
	p := strings.TrimSpace(path)
	if p == "" {
		return map[string]any{"ok": false, "error": "路径为空"}
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		p = filepath.Join(home, p[1:])
	}
	if _, err := os.Stat(p); err != nil {
		return map[string]any{"ok": false, "error": "文件不存在，保存后创建"}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,"+p)
	case "darwin":
		cmd = exec.Command("open", "-R", p)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(p))
	}
	if err := cmd.Start(); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	go func() { _ = cmd.Wait() }()
	return map[string]any{"ok": true}
}

// GetAgentSkills 返回各 Agent（Claude Code、Codex、
// Grok Build、OpenCode、pi）在系统用户目录技能文件夹里
// 的技能，按 Agent 分组返回。

func (a *App) GetAgentSkills() []agentcore.AgentSkills {
	return agentcore.ListAgentSkills()
}

// GetSkillContent 返回指定技能的 SKILL.md 内容。
func (a *App) GetSkillContent(name string) string {
	return agentcore.GetSkillContent(name)
}

// SaveSkill 新建或更新技能。
func (a *App) SaveSkill(name, content string) map[string]any {
	if errMsg := agentcore.SaveSkill(name, content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// DeleteSkill 删除技能。
func (a *App) DeleteSkill(name string) map[string]any {
	if errMsg := agentcore.DeleteSkill(name); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// ---- 会话管理（各 AI CLI 客户端落盘的历史会话记录）----

// GetSessionClients 返回登记了会话目录的客户端概览（数量/体积/最近活跃）。
func (a *App) GetSessionClients() []agentcore.SessionClient {
	return agentcore.ListSessionClients()
}

// GetSessionItems 返回指定客户端的会话条目列表（修改时间新→旧）。
func (a *App) GetSessionItems(clientID string) map[string]any {
	items, err := agentcore.ListSessionItems(clientID)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true, "items": items}
}

// DeleteSessionItems 批量删除指定客户端的会话条目（逐路径校验位于该客户端
// 会话根目录内且层级相符后才删除；目录型会话整棵移除）。删除不可恢复。
func (a *App) DeleteSessionItems(clientID string, paths []string) map[string]any {
	if err := agentcore.DeleteSessionItems(clientID, paths); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// OpenSessionTerminal 拉起终端「按该客户端的恢复命令恢复会话」：
// 例如 Claude Code 会话 → claude --resume <session-id>（在尽量还原的原始项目
// 目录启动）。默认内嵌终端窗口（Git Bash + winpty，--term 实例），无 Git Bash
// 时回退系统 cmd 外部窗口（管理员）。该客户端不支持按会话恢复（如 Copilot
// 仅支持 --continue）时返回原因。shell 参数保留以兼容旧调用（外部终端时代选宿主）。
func (a *App) OpenSessionTerminal(clientID, path, shell string) map[string]any {
	plan, err := agentcore.BuildSessionResume(clientID, path)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	if err := spawnClientTermWindow(plan.Dir, plan.Tokens, clientID); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// IsReaderWindow 供前端判断当前 WebView 是否为会话阅读窗口实例（--read 模式）。
func (a *App) IsReaderWindow() bool {
	return isReaderWindowMode()
}

// OpenSessionReader 打开会话阅读窗口：拉起自身 --read 精简实例渲染该会话内容。
func (a *App) OpenSessionReader(clientID, path, title string) map[string]any {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(path) == "" {
		return map[string]any{"ok": false, "error": "缺少会话参数"}
	}
	if err := spawnReaderWindow(clientID, path, title); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// LoadReaderContent 阅读窗口前端取会话内容：会话参数经环境变量传入（--read 实例）。
func (a *App) LoadReaderContent() map[string]any {
	clientID := os.Getenv(readEnvClient)
	path := os.Getenv(readEnvPath)
	msgs, ok := agentcore.SessionContent(clientID, path)
	if !ok {
		return map[string]any{"ok": false, "error": "该会话没有可读的对话内容"}
	}
	return map[string]any{
		"ok":       true,
		"title":    os.Getenv(readEnvTitle),
		"messages": msgs,
	}
}

// clipboardText 读取系统剪贴板文本（尽力而为，失败/无文本返回空）。
// Windows 经 PowerShell Get-Clipboard；无新依赖，命令本身毫秒级返回。
func clipboardText() string {
	switch runtime.GOOS {
	case "windows":
		// 直读 Win32 剪贴板 API（微秒级）；powershell Get-Clipboard 本机实测 ~4.8s，
		// 仅在剪贴板打不开（罕见）时兜底
		if s, ok := clipboardTextWin(); ok {
			return strings.TrimSpace(s)
		}
		cmd := exec.Command("powershell.exe", "-NoProfile", "-Command",
			"$t = Get-Clipboard; if (-not $t) { $t = (Get-Clipboard -Format FileDropList) | ForEach-Object { $_.FullName } }; $t")
		hideConsole(cmd) // GUI 进程拉控制台子进程必弹窗，同 reg/setx 处理
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	case "darwin":
		out, err := exec.Command("pbpaste").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	default:
		if out, err := exec.Command("wl-paste", "--no-newline").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
		out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
}

// clipboardLaunchDir 从剪贴板取终端启动目录：第一行若为存在的目录则直接采用，
// 是文件则取其所在目录（资源管理器里复制路径/复制文件均可），否则回退用户主目录。
func clipboardLaunchDir() string {
	txt := clipboardText()
	if i := strings.IndexAny(txt, "\r\n"); i >= 0 {
		txt = txt[:i]
	}
	txt = strings.TrimSpace(txt)
	if txt != "" && len(txt) <= 500 {
		if fi, err := os.Stat(txt); err == nil {
			if fi.IsDir() {
				return txt
			}
			return filepath.Dir(txt)
		}
	}
	dir, _ := os.UserHomeDir()
	return dir
}

// LaunchClientTerminal 启动指定客户端 CLI（客户端卡片「启动」图标）：默认拉起
// 内嵌终端窗口（自身 --term 精简实例，xterm.js + winpty + 内嵌 Git Bash），可自由
// 移动、独立最小化/最大化；无 Git Bash 时回退系统 cmd 外部窗口。工作目录优先
// 取剪贴板里的路径（目录/文件均支持，见 clipboardLaunchDir）。
func (a *App) LaunchClientTerminal(clientID string) map[string]any {
	bin, ok := agentcore.ClientBinOf(clientID)
	if !ok {
		return map[string]any{"ok": false, "error": "该客户端未登记 CLI 可执行名"}
	}
	if _, err := exec.LookPath(bin); err != nil {
		return map[string]any{"ok": false, "error": "未检测到 " + bin + " CLI，请先安装"}
	}
	dir := clipboardLaunchDir()
	if err := spawnClientTermWindow(dir, []string{bin}, clientID); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// winCmdline 把 argv 拼成 cmd.exe 可执行的命令行（含空格的参数加引号，引号翻倍转义）。
func winCmdline(tokens []string) string {
	parts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if tok == "" {
			parts = append(parts, `""`)
			continue
		}
		if strings.ContainsAny(tok, " \t\"&|<>^%") {
			// 注意：引号不阻止 cmd.exe 的 %VAR% 展开（交互式 cmd 无可靠转义），
			// 目前会话 ID/路径不含 %，若未来出现含 % 的参数需另想承载方式
			parts = append(parts, `"`+strings.ReplaceAll(tok, `"`, `""`)+`"`)
		} else {
			parts = append(parts, tok)
		}
	}
	return strings.Join(parts, " ")
}

// GetMcps 返回 MCP 配置（路由器地址 + 服务项列表）。
func (a *App) GetMcps() map[string]any {
	base, servers := agentcore.GetMcps()
	return map[string]any{"baseUrl": base, "servers": servers}
}

// SaveMcpFile 把"内容"文本原样写入对应的 MCP 配置 JSON 文件。
func (a *App) SaveMcpFile(name string, content string) map[string]any {
	if strings.TrimSpace(name) == "" {
		return map[string]any{"ok": false, "error": "名称为空"}
	}
	if err := agentcore.WriteMcpFile(agentcore.McpFileName(name), content); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// SaveMcps 保存 MCP 配置。
func (a *App) SaveMcps(raw map[string]any) map[string]any {
	base, _ := raw["baseUrl"].(string)
	servers := []agentcore.MCPServer{}
	if list, ok := raw["servers"].([]any); ok {
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				s := agentcore.MCPServer{
					Name:    str(m, "name"),
					Desc:    str(m, "desc"),
					Type:    str(m, "type"),
					Command: str(m, "command"),
					URL:     str(m, "url"),
				}
				if arr, ok := m["args"].([]any); ok {
					for _, arg := range arr {
						if strArg, ok := arg.(string); ok {
							s.Args = append(s.Args, strArg)
						}
					}
				}
				// 未受管的自定义键（env/headers/transport 等）原样收进 Extra，
				// 避免整列表持久化（改名/删除）时抹掉用户手写字段
				extra := map[string]json.RawMessage{}
				for k, v := range m {
					if agentcore.McpManagedKey(k) {
						continue
					}
					if b, err := json.Marshal(v); err == nil {
						extra[k] = b
					}
				}
				if len(extra) > 0 {
					s.Extra = extra
				}
				servers = append(servers, s)
			}
		}
	}
	if errMsg := agentcore.SaveMcps(base, servers); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// ListTargets 返回可注入/撤销的目标客户端列表。
func (a *App) ListTargets() []agentcore.TargetInfo {
	return agentcore.ListTargets()
}

// GetClientVersion 现场探测某客户端 CLI 的版本号（执行 `<bin> --version`）。
// 列表接口只返回 npm 包版本（读文件，不起进程）；读不到时由前端按需调本接口补。
func (a *App) GetClientVersion(targetID string) map[string]any {
	ver, err := agentcore.ProbeClientVersion(targetID)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true, "version": ver}
}

// InstallClientCLI 安装/更新某客户端 CLI（npm 全局）：update=false 走 `<pkg>`（首次安装），
// true 走 `<pkg>@latest`（更新）。返回 {ok, version, output, error}；output 为 npm 输出尾部。
func (a *App) InstallClientCLI(targetID string, update bool) map[string]any {
	out, ver, err := agentcore.InstallClientCLI(targetID, update)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "version": ver, "output": out}
	}
	return map[string]any{"ok": true, "version": ver, "output": out}
}

// CheckClientUpdates 探测各客户端 npm 包在 registry 上的最新版本（并行、每包 8s 超时）。
// 返回 {ok, updates: {id: {installed, latest, outdated, error}}}；网络不可用时单包带 error，
// 前端据此让「更新」保持可用（不打断使用）。
func (a *App) CheckClientUpdates() map[string]any {
	return map[string]any{"ok": true, "updates": agentcore.CheckClientUpdates()}
}

// UninstallClientCLI 卸载某客户端 CLI（npm 全局卸载）。purge=true 时一并清理本机配置残留：
// 撤销注入的技能 + 删除已登记的配置文件 + 回收空目录；会话记录保留。
// 返回 {ok, removed, output, error}，removed 为实际删除的路径。
func (a *App) UninstallClientCLI(targetID string, purge bool) map[string]any {
	out, removed, err := agentcore.UninstallClientCLI(targetID, purge)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "removed": removed, "output": out}
	}
	return map[string]any{"ok": true, "removed": removed, "output": out}
}

// GetTargetConfig 读取目标客户端的主配置文件内容。
func (a *App) GetTargetConfig(targetID string) map[string]any {
	content, exists, filePath, errMsg := agentcore.GetTargetConfig(targetID)
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveTargetConfig 保存目标客户端的主配置文件内容。
func (a *App) SaveTargetConfig(targetID, content string) map[string]any {
	if errMsg := agentcore.SaveTargetConfig(targetID, content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetPiSettings 读取 pi 的全局设置（~/.pi/agent/settings.json）。
func (a *App) GetPiSettings() map[string]any {
	content, exists, filePath, errMsg := clients.GetPiSettings()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SavePiSettings 保存 pi 的全局设置（~/.pi/agent/settings.json）。
func (a *App) SavePiSettings(content string) map[string]any {
	if errMsg := clients.SavePiSettings(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetPiMcp 读取 pi 的 MCP 服务配置（~/.pi/agent/mcp.json）。
func (a *App) GetPiMcp() map[string]any {
	content, exists, filePath, errMsg := clients.GetPiMcp()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SavePiMcp 保存 pi 的 MCP 服务配置（~/.pi/agent/mcp.json）。
func (a *App) SavePiMcp(content string) map[string]any {
	if errMsg := clients.SavePiMcp(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetAgyMcp 读取 agy 的 MCP 服务配置（~/.gemini/config/mcp_config.json）。
func (a *App) GetAgyMcp() map[string]any {
	content, exists, filePath, errMsg := clients.GetAgyMcp()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveAgyMcp 保存 agy 的 MCP 服务配置（~/.gemini/config/mcp_config.json）。
func (a *App) SaveAgyMcp(content string) map[string]any {
	if errMsg := clients.SaveAgyMcp(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCodebuddyModels 读取 codebuddy 的端点模型清单（~/.codebuddy/models.json）。
func (a *App) GetCodebuddyModels() map[string]any {
	content, exists, filePath, errMsg := clients.GetCodebuddyModels()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveCodebuddyModels 保存 codebuddy 的端点模型清单（~/.codebuddy/models.json）。
func (a *App) SaveCodebuddyModels(content string) map[string]any {
	if errMsg := clients.SaveCodebuddyModels(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCodebuddyMcp 读取 codebuddy 的 MCP 服务配置（~/.codebuddy/mcp.json）。
func (a *App) GetCodebuddyMcp() map[string]any {
	content, exists, filePath, errMsg := clients.GetCodebuddyMcp()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveCodebuddyMcp 保存 codebuddy 的 MCP 服务配置（~/.codebuddy/mcp.json）。
func (a *App) SaveCodebuddyMcp(content string) map[string]any {
	if errMsg := clients.SaveCodebuddyMcp(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetPiAgents 读取 pi 的 AGENTS.md（~/.pi/agent/AGENTS.md）。
func (a *App) GetPiAgents() map[string]any {
	content, exists, filePath, errMsg := clients.GetPiAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// GetClaudeMarkdown 读取 ~/.claude/CLAUDE.md（Claude Code 全局记忆/指令文件）。
func (a *App) GetClaudeMarkdown() map[string]any {
	content, exists, filePath, errMsg := clients.GetClaudeMarkdown()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveClaudeMarkdown 保存 ~/.claude/CLAUDE.md（纯文本原样写入）。
func (a *App) SaveClaudeMarkdown(content string) map[string]any {
	if errMsg := clients.SaveClaudeMarkdown(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// SavePiAgents 保存 pi 的 AGENTS.md（~/.pi/agent/AGENTS.md，纯文本原样写入）。
func (a *App) SavePiAgents(content string) map[string]any {
	if errMsg := clients.SavePiAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetGrokAgents 读取 grok 的 AGENTS.md（~/.grok/AGENTS.md）。
func (a *App) GetGrokAgents() map[string]any {
	content, exists, filePath, errMsg := clients.GetGrokAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveGrokAgents 保存 grok 的 AGENTS.md（~/.grok/AGENTS.md，纯文本原样写入）。
func (a *App) SaveGrokAgents(content string) map[string]any {
	if errMsg := clients.SaveGrokAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetKimiAgents 读取 kimi 的 AGENTS.md（~/.kimi-code/AGENTS.md）。
func (a *App) GetKimiAgents() map[string]any {
	content, exists, filePath, errMsg := clients.GetKimiAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveKimiAgents 保存 kimi 的 AGENTS.md（~/.kimi-code/AGENTS.md，纯文本原样写入）。
func (a *App) SaveKimiAgents(content string) map[string]any {
	if errMsg := clients.SaveKimiAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetOpencodeAgents 读取 opencode 的 AGENTS.md（模版 agent/Json_modles/opencode/AGENTS.md）。
func (a *App) GetOpencodeAgents() map[string]any {
	content, exists, filePath, errMsg := clients.GetOpencodeAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveOpencodeAgents 保存 opencode 的 AGENTS.md（写回模版并覆写 ~/.config/opencode/AGENTS.md）。
func (a *App) SaveOpencodeAgents(content string) map[string]any {
	if errMsg := clients.SaveOpencodeAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCodexAgents 读取 codex 的 AGENTS.md（模版 agent/Json_modles/codex/AGENTS.md）。
func (a *App) GetCodexAgents() map[string]any {
	content, exists, filePath, errMsg := clients.GetCodexAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveCodexAgents 保存 codex 的 AGENTS.md（写回模版并覆写 ~/.codex/AGENTS.md）。
func (a *App) SaveCodexAgents(content string) map[string]any {
	if errMsg := clients.SaveCodexAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCommandcodeSettings 读取 commandcode 的 settings.json（模版 agent/Json_modles/commandcode/settings.json）。
func (a *App) GetCommandcodeSettings() map[string]any {
	content, exists, filePath, errMsg := clients.GetCommandcodeSettings()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveCommandcodeSettings 保存 commandcode 的 settings.json（写回模版并覆写 ~/.commandcode/settings.json）。
func (a *App) SaveCommandcodeSettings(content string) map[string]any {
	if errMsg := clients.SaveCommandcodeSettings(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCommandcodeProviders 读取 commandcode 的 providers.json（模版 agent/Json_modles/commandcode/providers.json）。
func (a *App) GetCommandcodeProviders() map[string]any {
	content, exists, filePath, errMsg := clients.GetCommandcodeProviders()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveCommandcodeProviders 保存 commandcode 的 providers.json（写回模版并覆写 ~/.commandcode/providers.json）。
func (a *App) SaveCommandcodeProviders(content string) map[string]any {
	if errMsg := clients.SaveCommandcodeProviders(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// SyncTargets 手动触发对指定目标客户端的注入（inject）或撤销（undo）。
// ops 形如 [{targetId, action}]。
func (a *App) SyncTargets(ops []agentcore.TargetOp, routerBase string) []agentcore.TargetResult {
	return agentcore.SyncTargets(ops, routerBase)
}

// SyncMcpMapping 按「客户端 ↔ MCP 服务」映射，把选中的 MCP 服务注入选中的客户端。
func (a *App) SyncMcpMapping(targetIDs []string, serverNames []string) []agentcore.TargetResult {
	return agentcore.SyncMcpMapping(targetIDs, serverNames, a.server.LocalBase())
}

// GetMcpInjectedTargets 扫描各客户端的 MCP 配置文件，返回 {服务名: 已注入客户端id列表}，
// 供 MCP 列表在服务名旁标注客户端小图标。
func (a *App) GetMcpInjectedTargets() map[string][]string {
	return agentcore.McpInjectedTargets()
}

// str 从 map 中读取字符串字段。
func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func (a *App) GetConfig() map[string]any {
	return toMap(a.server.GetConfig())
}

// SetUserEnv 写入当前用户级环境变量（HKCU\Environment，广播生效通知；仅 Windows）。
// Codex env_key 自动映射使用：把路由 Token 写进 env_key 指向的变量。
func (a *App) SetUserEnv(name, value string) map[string]any {
	if err := agentcore.SetUserEnv(name, value); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// GetUserEnv 读取当前用户级环境变量（仅 Windows 持久；其它平台读进程环境变量，只可用于展示）。
// 返回 ok / exists / value，供 Copilot CLI 等环境变量型客户端面板回显当前值。
func (a *App) GetUserEnv(name string) map[string]any {
	v, exists, err := agentcore.GetUserEnv(name)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true, "exists": exists, "value": v}
}

// DelUserEnv 删除当前用户级环境变量（广播生效通知；仅 Windows；不存在视为幂等成功）。
// Copilot CLI 等环境变量型客户端的「清除」按钮使用。
func (a *App) DelUserEnv(name string) map[string]any {
	if err := agentcore.DelUserEnv(name); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// GetCopilotModel 读取 ~/.copilot/settings.json 的 model 键（Copilot CLI 主配置的
// 模型持久化/回显值；BYOK 校验只认 COPILOT_MODEL 环境变量，本键不参与；JSONC
// 宽容读取）。model 键缺失时 exists=false。
func (a *App) GetCopilotModel() map[string]any {
	model, exists, file, errMsg := clients.GetCopilotModel()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "exists": exists, "model": model, "file": file}
}

// SaveCopilotModel 写入 / 移除 ~/.copilot/settings.json 的 model 键（空值 = 移除；
// 严格 JSON 定点替换，文件含注释等非严格 JSON 时拒绝覆写）。
func (a *App) SaveCopilotModel(model string) map[string]any {
	if errMsg := clients.SaveCopilotModel(model); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCopilotSettings 读取 ~/.copilot/settings.json 中「配置选项」页签管理的全部键
// （model / effortLevel / allowedUrls / deniedUrls / permissions.disableBypassPermissionsMode /
// 沙箱 sandbox.* / IDE ide.* / 通用偏好 theme、banner、keepAlive 等；JSONC 宽容读取，
// 缺省键返回空值）。
func (a *App) GetCopilotSettings() map[string]any {
	s, exists, file, errMsg := clients.GetCopilotSettings()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "exists": exists, "settings": s, "file": file}
}

// SaveCopilotSettings 批量写入 ~/.copilot/settings.json 的面板键（严格 JSON 定点替换；
// 空串 / 空数组移除对应键、嵌套父对象变空自动清理；非法档位拒绝；map 中缺失的键保持不动）。
func (a *App) SaveCopilotSettings(settings map[string]any) map[string]any {
	if errMsg := clients.SaveCopilotSettings(settings); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCopilotPermissions 读取 ~/.copilot/permissions-config.json（按项目位置保存的
// 工具审批：tool_approvals + allowed_directories）。
func (a *App) GetCopilotPermissions() map[string]any {
	content, exists, file, errMsg := clients.GetCopilotPermissions()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "exists": exists, "content": content, "file": file}
}

// SaveCopilotPermissions 校验并保存 ~/.copilot/permissions-config.json（严格 JSON，
// 顶层必须是对象；首次保存前备份）。
func (a *App) SaveCopilotPermissions(content string) map[string]any {
	if errMsg := clients.SaveCopilotPermissions(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCopilotPermLocCandidates 返回权限面板「作用位置」候选目录
// （~/.copilot/config.json trustedFolders ∪ permissions-config.json locations
// 已有键，去重排序；file 为候选来源 config.json 路径）。
func (a *App) GetCopilotPermLocCandidates() map[string]any {
	folders, file, errMsg := clients.GetCopilotPermLocCandidates()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "folders": folders, "file": file}
}

func (a *App) SaveConfig(raw map[string]any) map[string]any {
	// 与 HTTP /api/config POST 一致:保存前先校验配置。
	// 桌面端此前直接 Normalize+落盘,端口越界/缺 host/供应商信息不合法等
	// 脏数据会写进 config.json,下次启动才能暴露问题。
	if errMsg := config.ValidateConfig(raw); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg, "restartNeeded": false}
	}
	cfg := config.Normalize(raw)
	result := a.server.SaveConfig(cfg)
	return map[string]any{"ok": result.Ok, "error": result.Error, "restartNeeded": result.RestartNeeded}
}

// SaveProviderAliases 定点更新指定供应商的 aliases 映射（整组替换），其余配置
// 原样保留；按 ID 优先、名称次之匹配供应商。供 agy 编辑窗口的模型映射面板使用，
// 避免把整个配置往返传回导致并发覆盖。
func (a *App) SaveProviderAliases(providerKey string, aliases map[string]string) map[string]any {
	cfg := a.server.GetConfig()
	// GetConfig 是按值返回，但 Providers 是切片、和内部配置共享底层数组：
	// 直接改 cfg.Providers[i] 会写进正在被请求处理读取的那份配置（数据竞争）。
	// 先复制切片再改，落盘失败时内存也不会和磁盘不一致。
	provs := make([]types.Provider, len(cfg.Providers))
	copy(provs, cfg.Providers)
	cfg.Providers = provs
	key := strings.TrimSpace(providerKey)
	found := -1
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == key || cfg.Providers[i].Name == key {
			found = i
			break
		}
	}
	if found < 0 {
		return map[string]any{"ok": false, "error": "供应商不存在: " + providerKey}
	}
	if len(aliases) == 0 {
		cfg.Providers[found].Aliases = map[string]string{}
	} else {
		cfg.Providers[found].Aliases = aliases
	}
	result := a.server.SaveConfig(cfg)
	return map[string]any{"ok": result.Ok, "error": result.Error, "restartNeeded": result.RestartNeeded}
}

// GetProviderAliases 读取指定供应商的 aliases 映射（ID 优先、名称次之匹配）。
func (a *App) GetProviderAliases(providerKey string) map[string]any {
	cfg := a.server.GetConfig()
	key := strings.TrimSpace(providerKey)
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == key || cfg.Providers[i].Name == key {
			return map[string]any{"ok": true, "aliases": cfg.Providers[i].Aliases}
		}
	}
	return map[string]any{"ok": false, "error": "供应商不存在: " + providerKey}
}

func (a *App) FetchModels(provider map[string]any) map[string]any {
	p := providerFromMap(provider)
	result := a.server.FetchModelsList(p)
	return toMap(result)
}

func (a *App) FetchModelsList(provider map[string]any) map[string]any {
	p := providerFromMap(provider)
	result := a.server.FetchModelsList(p)
	return toMap(result)
}

// GetProviderModels 返回供应商「上游返回的模型列表」（运行时拉取缓存，不含配置里的
// 静态 models，也不含别名），不发网络请求，供卡片统计模型个数；该供应商从未拉取过时
// 返回空列表，由前端再主动 FetchModels 一次。
func (a *App) GetProviderModels(provider map[string]any) map[string]any {
	p := providerFromMap(provider)
	models := router.GetFetchedModels(p.ID)
	if models == nil {
		models = []string{}
	}
	return map[string]any{"ok": true, "models": models, "count": len(models)}
}

// GetModelsByFormat 返回通过指定接入方式从路由器拿到的权威模型列表（等同于
// 目标客户端以该 base 访问 /v1/models 时所见的 ID 集合：跨供应商重名加
// "@供应商"后缀）。供可视化编辑弹窗的模型下拉使用，避免直接读取已启用供应商静态配置。
// 同时返回 entries（每项含所属供应商），供 agy 模型映射面板在保存时精确回写
// 目标模型到其所属供应商（覆盖运行时拉取而非仅静态 p.models 的供应商）。
func (a *App) GetModelsByFormat(format string) map[string]any {
	cfg := a.server.GetConfig()
	entries := router.AggregateModels(&cfg)
	ids := make([]string, 0, len(entries))
	type entry struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
	}
	detailed := make([]entry, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
		detailed = append(detailed, entry{ID: e.ID, Provider: e.Provider})
	}
	return map[string]any{"ok": true, "models": ids, "entries": detailed, "count": len(ids)}
}

func (a *App) GetStats() map[string]any { return toMap(core.ComputeStats(a.server.GetAllLogs())) }

// GetLogs 返回最近请求日志（最新在前，最多 200 条），供概览页「最近请求」列表使用。
func (a *App) GetLogs() []types.LogEntry { return a.server.GetLogs() }

// ClearLogs 清空全部请求日志并立即落盘。
func (a *App) ClearLogs() { a.server.ClearLogs() }

// GetActiveModels 返回最近 withinSeconds 秒内有请求的模型名（用于概览页光波动画）
func (a *App) GetActiveModels(withinSeconds int) map[string]any {
	return map[string]any{"models": a.server.ActiveModels(withinSeconds)}
}

func (a *App) StartServer() map[string]any {
	if err := a.server.Start(); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

func (a *App) StopServer() map[string]any {
	a.server.Stop()
	return map[string]any{"ok": true}
}

func (a *App) RestartServer() map[string]any {
	if err := a.server.Restart(); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// GetAutoStartEnabled 返回开机自启是否已开启（读注册表当前状态）
func (a *App) GetAutoStartEnabled() bool {
	return getAutoStartEnabled()
}

// SetAutoStartEnabled 开启/关闭开机自启（写当前用户 Run 键）
func (a *App) SetAutoStartEnabled(enabled bool) map[string]any {
	if err := setAutoStartEnabled(enabled); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// GetTrayEnabled 返回托盘常驻是否开启（读配置）
func (a *App) GetTrayEnabled() bool {
	return a.server.GetConfig().Tray
}

// SetTrayEnabled 开启/关闭托盘常驻：创建/销毁托盘图标，并把开关持久化到配置。
// 开启托盘常驻后，关闭窗口会隐藏到托盘（服务保持运行），不会退出应用。
func (a *App) SetTrayEnabled(enabled bool) map[string]any {
	cfg := a.server.GetConfig()
	cfg.Tray = enabled
	if res := a.server.SaveConfig(cfg); !res.Ok {
		return map[string]any{"ok": false, "error": res.Error}
	}
	if enabled {
		if !trayInit(a.ctx) {
			return map[string]any{"ok": true, "warning": "tray init failed"}
		}
	} else {
		trayShutdown()
	}
	return map[string]any{"ok": true}
}

func (a *App) OpenURL(url string) map[string]any {
	// 仅允许 http(s) 链接，并拒绝含 shell 元字符的地址，防止 cmd /c start 注入任意命令。
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return map[string]any{"ok": false, "error": "非法的链接: 仅支持 http(s)"}
	}
	if strings.ContainsAny(url, "&|<>\"'`$;^%()") {
		return map[string]any{"ok": false, "error": "非法的链接: 包含特殊字符"}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// rundll32 FileProtocolHandler 在部分 Windows 环境静默失败，改用 cmd /c start
		cmd = exec.Command("cmd", "/c", "start", "", url)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("[APP] open URL failed: %v", err)
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	json.Unmarshal(b, &m)
	return m
}

// isPortInUseError 判断启动失败是否为端口被占用（已有实例在运行）
func isPortInUseError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "only one usage of each socket address")
}

// showMessageBox 弹出系统提示框（MB_ICONINFORMATION）
func showMessageBox(title, message string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	msgBox := user32.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(message)
	msgBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x40) // MB_ICONINFORMATION
}

// activateExistingWindow 将已运行的 LLM Router 窗口带到前台
func activateExistingWindow() {
	user32 := syscall.NewLazyDLL("user32.dll")
	findWindow := user32.NewProc("FindWindowW")
	setForeground := user32.NewProc("SetForegroundWindow")
	showWindow := user32.NewProc("ShowWindow")
	title, _ := syscall.UTF16PtrFromString("LLM Router")
	hwnd, _, _ := findWindow.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd != 0 {
		showWindow.Call(hwnd, 9) // SW_RESTORE
		setForeground.Call(hwnd)
	}
}

func providerFromMap(m map[string]any) types.Provider {
	cfg := config.Normalize(map[string]any{"providers": []any{m}})
	if len(cfg.Providers) > 0 {
		p := cfg.Providers[0]
		// 未保存的供应商若没有 id，每次调用都会生成新的随机 id，导致 FetchModels 缓存
		// 挂在不稳定的 id 上无法复用。这里用 name+baseUrl+type 派生一个稳定的 id，
		// 保证同一供应商多次调用一致。
		if p.ID == "" {
			p.ID = stableProviderID(p)
		}
		return p
	}
	return types.Provider{}
}

// stableProviderID 根据供应商特征派生确定性 ID，避免未保存供应商每次请求都换 ID。
func stableProviderID(p types.Provider) string {
	h := sha256.Sum256([]byte(p.Name + "|" + p.BaseURL + "|" + string(p.Type)))
	return "p-" + hex.EncodeToString(h[:])[:10]
}

// newAssetServer 构造带缓存策略的前端资源服务。HTML（/ 与 *.html）强制免缓存：
// WebView2 对无缓存头的 index.html 会启发式缓存，应用更新后沿用旧 main.js 会与
// 新后端绑定错位（症状：--term 终端窗口挂载成主界面、新窗口功能整体失灵）。
// 带内容哈希的 /assets/ 内容不可变，保持浏览器正常缓存。
func newAssetServer(assets fs.FS) *assetserver.Options {
	return &assetserver.Options{
		Assets: assets,
		Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p := r.URL.Path
				if p == "/" || p == "/index.html" || strings.HasSuffix(p, ".html") {
					w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				}
				next.ServeHTTP(w, r)
			})
		},
	}
}

func main() {
	// 后台预热内嵌 Git Bash（首次解压十几秒，提前做用户无感）
	go warmupBundledGitBash()

	// 内嵌终端窗口实例（--term）：独立窗口 xterm.js 渲染，winpty 挂 Git Bash
	if isTermWindowMode() {
		runTermWindow()
		return
	}

	// 会话阅读窗口实例（--read）：独立窗口渲染会话对话内容（只读），同上分支
	if isReaderWindowMode() {
		runReaderWindow()
		return
	}

	app := &App{server: core.NewServer()}
	appRef = app

	// 开机自启时 Windows 可能以 --hidden 参数启动：若托盘常驻已开启，直接启动后隐藏到托盘
	startHidden := false
	for _, arg := range os.Args[1:] {
		if arg == "--hidden" {
			startHidden = true
			break
		}
	}

	if err := app.server.Start(); err != nil {
		if isPortInUseError(err) {
			// 单例保护：端口被占用说明已有实例在运行，提示并激活已有窗口后退出
			log.Printf("[APP] 端口已被占用，已有实例在运行: %v", err)
			showMessageBox("LLM Router 已在运行",
				"端口 "+strconv.Itoa(app.server.Port())+" 已被占用，\n检测到已有一个 LLM Router 实例在运行。\n请使用已打开的窗口。")
			activateExistingWindow()
			os.Exit(0)
		}
		log.Printf("Warning: HTTP server startup failed: %v", err)
	}

	frontendAssets, err := fs.Sub(embeddedFS, "frontend/dist")
	if err != nil {
		log.Fatalf("Failed to resolve frontend: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		app.server.Stop()
		app.server.FlushLogs() // 显式强制落盘，确保防抖中的最新日志不随 os.Exit 丢失
		trayShutdown()
		os.Exit(0)
	}()

	err = wails.Run(&options.App{
		Title:  "LLM Router",
		Width:  1200,
		Height: 633,
		// 启动默认最小化到任务栏（托盘常驻时也可从托盘恢复窗口）。
		// 最小窗口尺寸：保证七块仪表盘（左概览 + 右导航页签）内容不挤压变形
		MinWidth:         960,
		MinHeight:        633,
		WindowStartState: options.Minimised,
		AssetServer:      newAssetServer(frontendAssets),
		OnStartup: func(ctx context.Context) {
			app.ctx = ctx
			log.Println("[APP] started")
			// 托盘常驻已开启时创建托盘图标
			if app.server.GetConfig().Tray {
				trayInit(ctx)
			}
			// 开机自启（--hidden）且托盘常驻开启：窗口直接隐藏，仅留托盘图标
			if startHidden && app.server.GetConfig().Tray {
				wailsruntime.WindowHide(ctx)
			}
			go func() {
				time.Sleep(500 * time.Millisecond)
				// 首次显示时窗口水平垂直居中于工作区（避开任务栏），
				// 之后由看门狗周期校正，防止被任务栏遮挡
				centerOnceWhenVisible()
				startWorkAreaWatchdog()
			}()
		},
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			// 托盘菜单「退出」：forceQuit 置位则允许真正退出
			if app.forceQuit.Load() {
				return false
			}
			// 托盘常驻开启时，关闭窗口→隐藏到托盘而不是退出
			if app.server.GetConfig().Tray {
				wailsruntime.WindowHide(ctx)
				return true
			}
			return false
		},
		OnShutdown: func(ctx context.Context) {
			app.server.Stop()
			app.server.FlushLogs() // 与信号退出路径一致：防抖中的最新日志不随退出丢失
			trayShutdown()
		},
		Bind: []any{app},
	})
	if err != nil {
		log.Fatalf("Wails app error: %v", err)
	}
}
