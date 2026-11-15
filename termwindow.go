package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---- 内嵌终端窗口（--term 实例）----
// 客户端「启动」/ 会话「终端恢复」的默认宿主：主程序按需拉起自身精简实例
// （--term 参数），窗口内 xterm.js 渲染，后端 winpty 挂 Git Bash 执行目标
// CLI（winpty 链路见 winpty_windows.go / termpty_windows.go，Git Bash 见
// gitbash_windows.go）；未找到 Git Bash 时由调用方回退系统 cmd 窗口
// （见 termspawn_windows.go）。机制与 --read 阅读窗口一致：传参走环境变量，
// 前端走同一 index.html，main.js 按模式分流。

const (
	termEnvBash  = "LLMROUTER_TERM_BASH"  // bash.exe 绝对路径
	termEnvCwd   = "LLMROUTER_TERM_CWD"   // 启动目录（Windows 形式）
	termEnvArgs  = "LLMROUTER_TERM_ARGS"  // 启动命令 token 的 JSON 数组
	termEnvTitle = "LLMROUTER_TERM_TITLE" // 窗口标题
)

// Go↔JS 事件（TermWindow.vue 订阅）：输出为 base64；每个事件都带会话号，
// 重启换号后旧会话的尾部事件被前端丢弃。
const (
	termEventOut  = "term:out"
	termEventExit = "term:exit"
)

// isTermWindowMode 当前进程是否为内嵌终端窗口实例。
func isTermWindowMode() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--term" {
			return true
		}
	}
	return false
}

// IsTermWindow 供前端判断当前 WebView 是否为内嵌终端窗口实例（--term 模式）。
func (a *App) IsTermWindow() bool {
	return isTermWindowMode()
}

// spawnTermInstance 从主程序拉起一个内嵌终端窗口进程。
func spawnTermInstance(bashExe, cwd, title string, tokens []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	argsJSON, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--term")
	cmd.Env = append(os.Environ(),
		termEnvBash+"="+bashExe,
		termEnvCwd+"="+cwd,
		termEnvArgs+"="+string(argsJSON),
		termEnvTitle+"="+title,
	)
	// GUI 子系统程序，直接 Start 不闪控制台；窗口独立于主进程存活
	return cmd.Start()
}

// ---- 终端会话（每窗口实例至多一个；TermStart 可重启换新会话）----

var (
	termMu     sync.Mutex
	termCur    *termSession // nil = 无会话（未启动 / 已退出 / 已终止）
	termSidGen atomic.Int64
)

type termSession struct {
	sid int64
	p   *termProcess
}

// TermStart 启动 bash 会话；cols/rows 由前端按窗口尺寸折算（同 xterm 视口）。
// 已有会话时先终止旧的（「重新启动」场景）。启动目录、命令 token 来自环境变量。
func (a *App) TermStart(cols, rows int) map[string]any {
	if !isTermWindowMode() {
		return map[string]any{"ok": false, "error": "仅内嵌终端窗口实例可用"}
	}
	if a.ctx == nil {
		return map[string]any{"ok": false, "error": "窗口尚未就绪"}
	}
	bashExe := os.Getenv(termEnvBash)
	if bashExe == "" {
		return map[string]any{"ok": false, "error": "缺少 Git Bash 路径（LLMROUTER_TERM_BASH）"}
	}
	var tokens []string
	if err := json.Unmarshal([]byte(os.Getenv(termEnvArgs)), &tokens); err != nil || len(tokens) == 0 {
		return map[string]any{"ok": false, "error": "缺少启动命令（LLMROUTER_TERM_ARGS）"}
	}
	if cols < 1 || cols > 500 {
		cols = 120
	}
	if rows < 1 || rows > 300 {
		rows = 32
	}

	termMu.Lock()
	defer termMu.Unlock()
	if termCur != nil {
		termCur.p.stop()
		termCur = nil
	}
	sid := termSidGen.Add(1)
	ctx := a.ctx
	p, err := startBashTerm(bashExe, os.Getenv(termEnvCwd), tokens,
		uint16(cols), uint16(rows),
		func(b []byte) {
			wailsruntime.EventsEmit(ctx, termEventOut, sid, base64.StdEncoding.EncodeToString(b))
		},
		func(code int) {
			termMu.Lock()
			if termCur != nil && termCur.sid == sid {
				termCur = nil
			}
			termMu.Unlock()
			wailsruntime.EventsEmit(ctx, termEventExit, sid, code)
		})
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	termCur = &termSession{sid: sid, p: p}
	// sid 必须回传前端：后续 term:out / term:exit 事件都带此会话号，
	// 前端按它过滤（重启换号后丢弃旧会话的尾部事件）
	return map[string]any{"ok": true, "title": os.Getenv(termEnvTitle), "sid": sid}
}

// TermWrite 写入用户键入（base64 编码，避免事件通道转义问题）。
func (a *App) TermWrite(data string) map[string]any {
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return map[string]any{"ok": false, "error": "输入数据格式错误"}
	}
	termMu.Lock()
	defer termMu.Unlock()
	if termCur == nil {
		return map[string]any{"ok": false, "error": "终端会话未启动"}
	}
	if err := termCur.p.write(b); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// TermResize 同步伪控制台尺寸（前端 fit 后调用）。
func (a *App) TermResize(cols, rows int) map[string]any {
	if cols < 1 || rows < 1 || cols > 500 || rows > 300 {
		return map[string]any{"ok": false}
	}
	termMu.Lock()
	defer termMu.Unlock()
	if termCur == nil {
		return map[string]any{"ok": false}
	}
	if err := termCur.p.resize(uint16(cols), uint16(rows)); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// TermStop 终止当前 bash 会话（连同其子进程尝试一并结束）。
func (a *App) TermStop() map[string]any {
	termMu.Lock()
	defer termMu.Unlock()
	if termCur != nil {
		termCur.p.stop()
		termCur = nil
	}
	return map[string]any{"ok": true}
}

// runTermWindow 终端窗口实例的主循环（--term 模式下 main 直接调它并返回）。
func runTermWindow() {
	title := os.Getenv(termEnvTitle)
	if title != "" {
		title += " · 终端"
	} else {
		title = "终端 · LLM Router"
	}

	frontendAssets, err := fs.Sub(embeddedFS, "frontend/dist")
	if err != nil {
		os.Exit(1)
	}

	app := &App{}
	err = wails.Run(&options.App{
		Title:            title,
		Width:            1000,
		Height:           640,
		MinWidth:         480,
		MinHeight:        320,
		WindowStartState: options.Normal,
		// 深色底，避免 WebView 白闪与终端配色割裂
		BackgroundColour: &options.RGBA{R: 13, G: 17, B: 23, A: 255},
		AssetServer:      newAssetServer(frontendAssets),
		OnStartup: func(ctx context.Context) {
			app.ctx = ctx
		},
		Bind: []any{app},
	})
	if err != nil {
		log.Println("[TERM] wails error:", err)
	}
}
