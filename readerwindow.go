package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
)

// ---- 会话阅读窗口 ----
// 会话列表点「阅读」拉起自身精简实例（--read 参数）：一个独立 OS 窗口渲染该
// 会话的对话内容（只读，无 PTY）。机制与终端窗口（termwindow.go）一致：
// 主程序传参走环境变量，前端走同一 index.html，main.js 按模式挂载阅读页。

const (
	readEnvClient = "LLMROUTER_READ_CLIENT"
	readEnvPath   = "LLMROUTER_READ_PATH"
	readEnvTitle  = "LLMROUTER_READ_TITLE"
)

// isReaderWindowMode 当前进程是否为会话阅读窗口实例。
func isReaderWindowMode() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--read" {
			return true
		}
	}
	return false
}

// spawnReaderWindow 从主程序拉起一个会话阅读窗口进程。
func spawnReaderWindow(clientID, path, title string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--read")
	cmd.Env = append(os.Environ(),
		readEnvClient+"="+clientID,
		readEnvPath+"="+path,
		readEnvTitle+"="+title,
	)
	// GUI 子系统程序，直接 Start 不闪控制台；窗口独立于主进程存活
	return cmd.Start()
}

// runReaderWindow 阅读窗口实例的主循环（--read 模式下 main 直接调它并返回）。
func runReaderWindow() {
	title := os.Getenv(readEnvTitle)
	if title == "" {
		title = "会话阅读"
	}

	frontendAssets, err := fs.Sub(embeddedFS, "frontend/dist")
	if err != nil {
		os.Exit(1)
	}

	err = wails.Run(&options.App{
		Title:            title + " · LLM Router 阅读器",
		Width:            1024,
		Height:           576, // 16:9 初始比例；窗口可自由缩放不锁比例
		MinWidth:         480,
		MinHeight:        320,
		WindowStartState: options.Normal,
		AssetServer:      newAssetServer(frontendAssets),
		OnStartup: func(ctx context.Context) {
			// 绑定的 App 方法里读环境变量取会话路径，这里无需额外初始化
			_ = ctx
		},
		Bind: []any{&App{}},
	})
	if err != nil {
		fmt.Println("[READ] wails error:", err)
	}
}
