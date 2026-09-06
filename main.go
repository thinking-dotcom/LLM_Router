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

	"llm-router-go/agent"
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
// 不再弹浏览器登录窗；模型请求走哪由 config.json 的 provider（BYOK 服务商）决定，与此值无关。
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
// agent 包的 writeFileAtomic 未导出，此处自带同型实现。
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
	// 清除改用 agent.DelUserEnv 而非 reg delete：删除幂等（值不存在视为成功，
	// 与 GetCommandcodeNoLogin 的「从未开启也能清除」预期一致），且删除后会
	// 广播 WM_SETTINGCHANGE，已运行的 Explorer / 新开终端能立即感知变更
	if err := agent.DelUserEnv(commandcodeNoLoginEnvVar); err != nil {
		return "清除环境变量失败：" + err.Error()
	}
	return ""
}

// GetSkillsList 返回 data/skills 下的技能列表。
func (a *App) GetSkillsList() []agent.Skill {
	return agent.ListSkills()
}

// GetSkillsDir 返回技能存放目录（执行文件同目录的 data/skills），供前端展示。
func (a *App) GetSkillsDir() string {
	return agent.SkillsDir()
}

// OpenSkillsDir 在系统文件管理器中打开技能目录（目录不存在时先创建）
func (a *App) OpenSkillsDir() map[string]any {
	dir := agent.SkillsDir()
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

// GetClientRealPaths 返回各客户端真实配置路径清单（~ 已展开为绝对路径），
// 来源 agent/Json_modles/client-paths.json（缺失时为内置默认），供前端定位真实文件。
func (a *App) GetClientRealPaths() map[string]any {
	return map[string]any{"ok": true, "paths": agent.ClientRealPaths()}
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

// GetAgentSkills 返回各 Agent（Claude Code、Codex、Gemini、
// Grok Build、OpenCode、Hermes、pi）在系统用户目录技能文件夹里
// 的技能，按 Agent 分组返回。

func (a *App) GetAgentSkills() []agent.AgentSkills {
	return agent.ListAgentSkills()
}

// GetSkillContent 返回指定技能的 SKILL.md 内容。
func (a *App) GetSkillContent(name string) string {
	return agent.GetSkillContent(name)
}

// SaveSkill 新建或更新技能。
func (a *App) SaveSkill(name, content string) map[string]any {
	if errMsg := agent.SaveSkill(name, content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// DeleteSkill 删除技能。
func (a *App) DeleteSkill(name string) map[string]any {
	if errMsg := agent.DeleteSkill(name); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// ---- 会话管理（各 AI CLI 客户端落盘的历史会话记录）----

// GetSessionClients 返回登记了会话目录的客户端概览（数量/体积/最近活跃）。
func (a *App) GetSessionClients() []agent.SessionClient {
	return agent.ListSessionClients()
}

// GetSessionItems 返回指定客户端的会话条目列表（修改时间新→旧）。
func (a *App) GetSessionItems(clientID string) map[string]any {
	items, err := agent.ListSessionItems(clientID)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true, "items": items}
}

// DeleteSessionItems 批量删除指定客户端的会话条目（逐路径校验位于该客户端
// 会话根目录内且层级相符后才删除；目录型会话整棵移除）。删除不可恢复。
func (a *App) DeleteSessionItems(clientID string, paths []string) map[string]any {
	if err := agent.DeleteSessionItems(clientID, paths); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// OpenSessionTerminal 在系统终端中「按该客户端的恢复命令拉起会话」：
// 例如 Claude Code 会话 → claude --resume <session-id>（在尽量还原的原始项目目录启动）。
// 该客户端不支持按会话恢复（如 Copilot 仅支持 --continue）时返回原因。
func (a *App) OpenSessionTerminal(clientID, path string) map[string]any {
	plan, err := agent.BuildSessionResume(clientID, path)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	if err := openTerminalCommand(plan.Dir, plan.Tokens); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// openTerminalCommand 新开一个终端窗口执行 tokens 命令（目录定位到 dir）。
func openTerminalCommand(dir string, tokens []string) error {
	if len(tokens) == 0 {
		return errors.New("恢复命令为空")
	}
	if _, err := exec.LookPath(tokens[0]); err != nil {
		return errors.New("未找到可执行命令: " + tokens[0] + "，请确认已安装该 CLI")
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	switch runtime.GOOS {
	case "windows":
		// 各 AI CLI 多为 node .cmd shim，命令统一经 cmd /K 执行（出错留窗可看）。
		inner := winCmdline(tokens)
		// 优先 Windows Terminal：wt -d <dir> cmd /K <inner>
		if _, err := exec.LookPath("wt.exe"); err == nil {
			wt := exec.Command("wt.exe", "-d", dir, "cmd.exe", "/K", inner)
			if err := wt.Start(); err == nil {
				go func() { _ = wt.Wait() }()
				return nil
			}
		}
		// 回退经典控制台：cmd 以 dir 为工作目录启动，强制新控制台窗口避免复用父进程
		cmd := exec.Command("cmd.exe", "/K", inner)
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	case "darwin":
		cmdStr := posixCmdline(dir, tokens)
		esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(cmdStr)
		cmd := exec.Command("osascript", "-e", `tell application "Terminal" to do script "`+esc+`"`)
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	default:
		cmdStr := posixCmdline(dir, tokens)
		for _, cand := range [][]string{
			{"gnome-terminal", "--working-directory=" + dir, "--", "bash", "-lc", cmdStr},
			{"konsole", "--workdir", dir, "-e", "bash", "-lc", cmdStr},
			{"xfce4-terminal", "--working-directory=" + dir, "-x", "bash", "-lc", cmdStr},
		} {
			p, lerr := exec.LookPath(cand[0])
			if lerr != nil {
				continue
			}
			cmd := exec.Command(p, cand[1:]...)
			if err := cmd.Start(); err == nil {
				go func() { _ = cmd.Wait() }()
				return nil
			}
		}
		return errors.New("未找到可用的终端程序")
	}
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
			parts = append(parts, `"`+strings.ReplaceAll(tok, `"`, `""`)+`"`)
		} else {
			parts = append(parts, tok)
		}
	}
	return strings.Join(parts, " ")
}

// posixCmdline 生成 `cd <dir> && exec <argv...>` 的 POSIX shell 命令行（单引号安全引用）。
func posixCmdline(dir string, tokens []string) string {
	parts := make([]string, 0, len(tokens)+1)
	parts = append(parts, "cd "+shQuote(dir))
	parts = append(parts, "exec")
	for _, tok := range tokens {
		parts = append(parts, shQuote(tok))
	}
	return strings.Join(parts, " ")
}

// shQuote 用单引号包住字符串并转义内部单引号（POSIX shell 安全）。
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// GetMcps 返回 MCP 配置（路由器地址 + 服务项列表）。
func (a *App) GetMcps() map[string]any {
	base, servers := agent.GetMcps()
	return map[string]any{"baseUrl": base, "servers": servers}
}

// SaveMcpFile 把“内容”文本原样写入对应的 MCP 配置 JSON 文件。
func (a *App) SaveMcpFile(name string, content string) map[string]any {
	if strings.TrimSpace(name) == "" {
		return map[string]any{"ok": false, "error": "名称为空"}
	}
	if err := agent.WriteMcpFile(agent.McpFileName(name), content); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// SaveMcps 保存 MCP 配置。
func (a *App) SaveMcps(raw map[string]any) map[string]any {
	base, _ := raw["baseUrl"].(string)
	servers := []agent.MCPServer{}
	if list, ok := raw["servers"].([]any); ok {
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				s := agent.MCPServer{
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
					if agent.McpManagedKey(k) {
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
	if errMsg := agent.SaveMcps(base, servers); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// ListTargets 返回可注入/撤销的目标客户端列表。
func (a *App) ListTargets() []agent.TargetInfo {
	return agent.ListTargets()
}

// GetTargetConfig 读取目标客户端的主配置文件内容。
func (a *App) GetTargetConfig(targetID string) map[string]any {
	content, exists, filePath, errMsg := agent.GetTargetConfig(targetID)
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveTargetConfig 保存目标客户端的主配置文件内容。
func (a *App) SaveTargetConfig(targetID, content string) map[string]any {
	if errMsg := agent.SaveTargetConfig(targetID, content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetPiSettings 读取 pi 的全局设置（~/.pi/agent/settings.json）。
func (a *App) GetPiSettings() map[string]any {
	content, exists, filePath, errMsg := agent.GetPiSettings()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// GetGeminiDotEnv 读取 ~/.gemini/.env 内容。
func (a *App) GetGeminiDotEnv() map[string]any {
	content, exists, filePath, errMsg := agent.GetGeminiDotEnv()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveGeminiDotEnv 保存 ~/.gemini/.env（纯文本原样写入）。
func (a *App) SaveGeminiDotEnv(content string) map[string]any {
	if errMsg := agent.SaveGeminiDotEnv(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// SavePiSettings 保存 pi 的全局设置（~/.pi/agent/settings.json）。
func (a *App) SavePiSettings(content string) map[string]any {
	if errMsg := agent.SavePiSettings(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetPiMcp 读取 pi 的 MCP 服务配置（~/.pi/agent/mcp.json）。
func (a *App) GetPiMcp() map[string]any {
	content, exists, filePath, errMsg := agent.GetPiMcp()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SavePiMcp 保存 pi 的 MCP 服务配置（~/.pi/agent/mcp.json）。
func (a *App) SavePiMcp(content string) map[string]any {
	if errMsg := agent.SavePiMcp(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetAgyMcp 读取 agy 的 MCP 服务配置（~/.gemini/config/mcp_config.json）。
func (a *App) GetAgyMcp() map[string]any {
	content, exists, filePath, errMsg := agent.GetAgyMcp()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveAgyMcp 保存 agy 的 MCP 服务配置（~/.gemini/config/mcp_config.json）。
func (a *App) SaveAgyMcp(content string) map[string]any {
	if errMsg := agent.SaveAgyMcp(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCodebuddyModels 读取 codebuddy 的端点模型清单（~/.codebuddy/models.json）。
func (a *App) GetCodebuddyModels() map[string]any {
	content, exists, filePath, errMsg := agent.GetCodebuddyModels()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveCodebuddyModels 保存 codebuddy 的端点模型清单（~/.codebuddy/models.json）。
func (a *App) SaveCodebuddyModels(content string) map[string]any {
	if errMsg := agent.SaveCodebuddyModels(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCodebuddyMcp 读取 codebuddy 的 MCP 服务配置（~/.codebuddy/mcp.json）。
func (a *App) GetCodebuddyMcp() map[string]any {
	content, exists, filePath, errMsg := agent.GetCodebuddyMcp()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveCodebuddyMcp 保存 codebuddy 的 MCP 服务配置（~/.codebuddy/mcp.json）。
func (a *App) SaveCodebuddyMcp(content string) map[string]any {
	if errMsg := agent.SaveCodebuddyMcp(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetPiAgents 读取 pi 的 AGENTS.md（~/.pi/agent/AGENTS.md）。
func (a *App) GetPiAgents() map[string]any {
	content, exists, filePath, errMsg := agent.GetPiAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// GetClaudeMarkdown 读取 ~/.claude/CLAUDE.md（Claude Code 全局记忆/指令文件）。
func (a *App) GetClaudeMarkdown() map[string]any {
	content, exists, filePath, errMsg := agent.GetClaudeMarkdown()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveClaudeMarkdown 保存 ~/.claude/CLAUDE.md（纯文本原样写入）。
func (a *App) SaveClaudeMarkdown(content string) map[string]any {
	if errMsg := agent.SaveClaudeMarkdown(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// SavePiAgents 保存 pi 的 AGENTS.md（~/.pi/agent/AGENTS.md，纯文本原样写入）。
func (a *App) SavePiAgents(content string) map[string]any {
	if errMsg := agent.SavePiAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetGrokAgents 读取 grok 的 AGENTS.md（~/.grok/AGENTS.md）。
func (a *App) GetGrokAgents() map[string]any {
	content, exists, filePath, errMsg := agent.GetGrokAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveGrokAgents 保存 grok 的 AGENTS.md（~/.grok/AGENTS.md，纯文本原样写入）。
func (a *App) SaveGrokAgents(content string) map[string]any {
	if errMsg := agent.SaveGrokAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetKimiAgents 读取 kimi 的 AGENTS.md（~/.kimi-code/AGENTS.md）。
func (a *App) GetKimiAgents() map[string]any {
	content, exists, filePath, errMsg := agent.GetKimiAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveKimiAgents 保存 kimi 的 AGENTS.md（~/.kimi-code/AGENTS.md，纯文本原样写入）。
func (a *App) SaveKimiAgents(content string) map[string]any {
	if errMsg := agent.SaveKimiAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetOpencodeAgents 读取 opencode 的 AGENTS.md（模版 agent/Json_modles/opencode/AGENTS.md）。
func (a *App) GetOpencodeAgents() map[string]any {
	content, exists, filePath, errMsg := agent.GetOpencodeAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveOpencodeAgents 保存 opencode 的 AGENTS.md（写回模版并覆写 ~/.config/opencode/AGENTS.md）。
func (a *App) SaveOpencodeAgents(content string) map[string]any {
	if errMsg := agent.SaveOpencodeAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetKiloAgents 读取 kilo 的 AGENTS.md（模版 agent/Json_modles/kilo/AGENTS.md）。
func (a *App) GetKiloAgents() map[string]any {
	content, exists, filePath, errMsg := agent.GetKiloAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveKiloAgents 保存 kilo 的 AGENTS.md（覆写 ~/.config/kilo/AGENTS.md）。
func (a *App) SaveKiloAgents(content string) map[string]any {
	if errMsg := agent.SaveKiloAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetGeminiAgents 读取 gemini 的 GEMINI.md（~/.gemini/GEMINI.md）。
func (a *App) GetGeminiAgents() map[string]any {
	content, exists, filePath, errMsg := agent.GetGeminiAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveGeminiAgents 保存 gemini 的 GEMINI.md（~/.gemini/GEMINI.md，纯文本原样写入）。
func (a *App) SaveGeminiAgents(content string) map[string]any {
	if errMsg := agent.SaveGeminiAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCodexAgents 读取 codex 的 AGENTS.md（模版 agent/Json_modles/codex/AGENTS.md）。
func (a *App) GetCodexAgents() map[string]any {
	content, exists, filePath, errMsg := agent.GetCodexAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveCodexAgents 保存 codex 的 AGENTS.md（写回模版并覆写 ~/.codex/AGENTS.md）。
func (a *App) SaveCodexAgents(content string) map[string]any {
	if errMsg := agent.SaveCodexAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetClineModels 读取 cline 的 models.json（模版 agent/Json_modles/cline/models.json）。
func (a *App) GetClineModels() map[string]any {
	content, exists, filePath, errMsg := agent.GetClineModels()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveClineModels 保存 cline 的 models.json（写回模版并覆写 ~/.cline/data/settings/models.json）。
func (a *App) SaveClineModels(content string) map[string]any {
	if errMsg := agent.SaveClineModels(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetClineProviders 读取 cline 的 providers.json（模版 agent/Json_modles/cline/providers.json）。
func (a *App) GetClineProviders() map[string]any {
	content, exists, filePath, errMsg := agent.GetClineProviders()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveClineProviders 保存 cline 的 providers.json（写回模版并覆写 ~/.cline/data/settings/providers.json）。
func (a *App) SaveClineProviders(content string) map[string]any {
	if errMsg := agent.SaveClineProviders(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetClineAgents 读取 cline 的 AGENTS.md（模版 agent/Json_modles/cline/AGENTS.md）。
func (a *App) GetClineAgents() map[string]any {
	content, exists, filePath, errMsg := agent.GetClineAgents()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveClineAgents 保存 cline 的 AGENTS.md（写回模版并覆写 ~/.agents/AGENTS.md）。
func (a *App) SaveClineAgents(content string) map[string]any {
	if errMsg := agent.SaveClineAgents(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetClineGlobalState 读取 Cline 的 globalState.json（~/.cline/data/globalState.json，真实文件直读）。
func (a *App) GetClineGlobalState() map[string]any {
	content, exists, filePath, errMsg := agent.GetClineGlobalState()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveClineGlobalState 保存 Cline 的 globalState.json（JSON 校验后原子覆写真实文件，不写参考模版）。
func (a *App) SaveClineGlobalState(content string) map[string]any {
	if errMsg := agent.SaveClineGlobalState(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCommandcodeSettings 读取 commandcode 的 settings.json（模版 agent/Json_modles/commandcode/settings.json）。
func (a *App) GetCommandcodeSettings() map[string]any {
	content, exists, filePath, errMsg := agent.GetCommandcodeSettings()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveCommandcodeSettings 保存 commandcode 的 settings.json（写回模版并覆写 ~/.commandcode/settings.json）。
func (a *App) SaveCommandcodeSettings(content string) map[string]any {
	if errMsg := agent.SaveCommandcodeSettings(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCommandcodeProviders 读取 commandcode 的 providers.json（模版 agent/Json_modles/commandcode/providers.json）。
func (a *App) GetCommandcodeProviders() map[string]any {
	content, exists, filePath, errMsg := agent.GetCommandcodeProviders()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "content": content, "exists": exists, "file": filePath}
}

// SaveCommandcodeProviders 保存 commandcode 的 providers.json（写回模版并覆写 ~/.commandcode/providers.json）。
func (a *App) SaveCommandcodeProviders(content string) map[string]any {
	if errMsg := agent.SaveCommandcodeProviders(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// SyncTargets 手动触发对指定目标客户端的注入（inject）或撤销（undo）。
// ops 形如 [{targetId, action}]。
func (a *App) SyncTargets(ops []agent.TargetOp, routerBase string) []agent.TargetResult {
	return agent.SyncTargets(ops, routerBase)
}

// SyncMcpMapping 按「客户端 ↔ MCP 服务」映射，把选中的 MCP 服务注入选中的客户端。
func (a *App) SyncMcpMapping(targetIDs []string, serverNames []string) []agent.TargetResult {
	return agent.SyncMcpMapping(targetIDs, serverNames, a.server.LocalBase())
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
	if err := agent.SetUserEnv(name, value); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// GetUserEnv 读取当前用户级环境变量（仅 Windows 持久；其它平台读进程环境变量，只可用于展示）。
// 返回 ok / exists / value，供 Copilot CLI 等环境变量型客户端面板回显当前值。
func (a *App) GetUserEnv(name string) map[string]any {
	v, exists, err := agent.GetUserEnv(name)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true, "exists": exists, "value": v}
}

// DelUserEnv 删除当前用户级环境变量（广播生效通知；仅 Windows；不存在视为幂等成功）。
// Copilot CLI 等环境变量型客户端的「清除」按钮使用。
func (a *App) DelUserEnv(name string) map[string]any {
	if err := agent.DelUserEnv(name); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// GetCopilotModel 读取 ~/.copilot/settings.json 的 model 键（Copilot CLI 主配置，
// 等价持久化的 COPILOT_MODEL；JSONC 宽容读取）。model 键缺失时 exists=false。
func (a *App) GetCopilotModel() map[string]any {
	model, exists, file, errMsg := agent.GetCopilotModel()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "exists": exists, "model": model, "file": file}
}

// SaveCopilotModel 写入 / 移除 ~/.copilot/settings.json 的 model 键（空值 = 移除；
// 严格 JSON 定点替换，文件含注释等非严格 JSON 时拒绝覆写）。
func (a *App) SaveCopilotModel(model string) map[string]any {
	if errMsg := agent.SaveCopilotModel(model); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCopilotSettings 读取 ~/.copilot/settings.json 中「接入配置」面板管理的全部键
// （model / effortLevel / allowedUrls / deniedUrls / permissions.disableBypassPermissionsMode；
// JSONC 宽容读取，缺省键返回空值）。
func (a *App) GetCopilotSettings() map[string]any {
	s, exists, file, errMsg := agent.GetCopilotSettings()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "exists": exists, "settings": s, "file": file}
}

// SaveCopilotSettings 批量写入 ~/.copilot/settings.json 的接入配置键
// （严格 JSON 定点替换；空串 / 空数组 / 缺失跳过时移除对应键；非法档位拒绝）。
func (a *App) SaveCopilotSettings(settings map[string]any) map[string]any {
	if errMsg := agent.SaveCopilotSettings(settings); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

// GetCopilotPermissions 读取 ~/.copilot/permissions-config.json（按项目位置保存的
// 工具审批：tool_approvals + allowed_directories）。
func (a *App) GetCopilotPermissions() map[string]any {
	content, exists, file, errMsg := agent.GetCopilotPermissions()
	if errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true, "exists": exists, "content": content, "file": file}
}

// SaveCopilotPermissions 校验并保存 ~/.copilot/permissions-config.json（严格 JSON，
// 顶层必须是对象；首次保存前备份）。
func (a *App) SaveCopilotPermissions(content string) map[string]any {
	if errMsg := agent.SaveCopilotPermissions(content); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg}
	}
	return map[string]any{"ok": true}
}

func (a *App) SaveConfig(raw map[string]any) map[string]any {
	// 与 HTTP /api/config POST 一致:保存前先校验配置。
	// 桌面端此前直接 Normalize+落盘,端口越界/缺 host/服务商信息不合法等
	// 脏数据会写进 config.json,下次启动才能暴露问题。
	if errMsg := config.ValidateConfig(raw); errMsg != "" {
		return map[string]any{"ok": false, "error": errMsg, "restartNeeded": false}
	}
	cfg := config.Normalize(raw)
	result := a.server.SaveConfig(cfg)
	return map[string]any{"ok": result.Ok, "error": result.Error, "restartNeeded": result.RestartNeeded}
}

// SaveProviderAliases 定点更新指定服务商的 aliases 映射（整组替换），其余配置
// 原样保留；按 ID 优先、名称次之匹配服务商。供 agy 编辑窗口的模型映射面板使用，
// 避免把整个配置往返传回导致并发覆盖。
func (a *App) SaveProviderAliases(providerKey string, aliases map[string]string) map[string]any {
	cfg := a.server.GetConfig()
	key := strings.TrimSpace(providerKey)
	found := -1
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == key || cfg.Providers[i].Name == key {
			found = i
			break
		}
	}
	if found < 0 {
		return map[string]any{"ok": false, "error": "服务商不存在: " + providerKey}
	}
	if len(aliases) == 0 {
		cfg.Providers[found].Aliases = map[string]string{}
	} else {
		cfg.Providers[found].Aliases = aliases
	}
	result := a.server.SaveConfig(cfg)
	return map[string]any{"ok": result.Ok, "error": result.Error, "restartNeeded": result.RestartNeeded}
}

// GetProviderAliases 读取指定服务商的 aliases 映射（ID 优先、名称次之匹配）。
func (a *App) GetProviderAliases(providerKey string) map[string]any {
	cfg := a.server.GetConfig()
	key := strings.TrimSpace(providerKey)
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == key || cfg.Providers[i].Name == key {
			return map[string]any{"ok": true, "aliases": cfg.Providers[i].Aliases}
		}
	}
	return map[string]any{"ok": false, "error": "服务商不存在: " + providerKey}
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

// GetProviderModels 返回服务商的模型列表（配置内置模型，否则运行时拉取缓存），
// 不发网络请求，供前端悬停卡片时即时显示模型数量。
func (a *App) GetProviderModels(provider map[string]any) map[string]any {
	p := providerFromMap(provider)
	models := router.GetProviderModels(p)
	if models == nil {
		models = []string{}
	}
	return map[string]any{"ok": true, "models": models, "count": len(models)}
}

// GetModelsByFormat 返回通过指定接入方式从路由器拿到的权威模型列表（等同于
// 目标客户端以该 base 访问 /v1/models 时所见的 ID 集合：跨服务商重名加
// "@服务商"后缀）。供可视化编辑弹窗的模型下拉使用，避免直接读取已启用服务商静态配置。
// 同时返回 entries（每项含所属服务商），供 agy 模型映射面板在保存时精确回写
// 目标模型到其所属服务商（覆盖运行时拉取而非仅静态 p.models 的服务商）。
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
		// 未保存的服务商若没有 id，每次调用都会生成新的随机 id，导致 FetchModels 缓存
		// 挂在不稳定的 id 上无法复用。这里用 name+baseUrl+type 派生一个稳定的 id，
		// 保证同一服务商多次调用一致。
		if p.ID == "" {
			p.ID = stableProviderID(p)
		}
		return p
	}
	return types.Provider{}
}

// stableProviderID 根据服务商特征派生确定性 ID，避免未保存服务商每次请求都换 ID。
func stableProviderID(p types.Provider) string {
	h := sha256.Sum256([]byte(p.Name + "|" + p.BaseURL + "|" + string(p.Type)))
	return "p-" + hex.EncodeToString(h[:])[:10]
}

func main() {
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
		AssetServer:      &assetserver.Options{Assets: frontendAssets},
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
