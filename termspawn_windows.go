//go:build windows

package main

import (
	"fmt"
	"log"
	"strings"
	"syscall"
	"unsafe"
)

var (
	shell32DLL               = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW        = shell32DLL.NewProc("ShellExecuteW")
	swShowNormal      uint16 = 1
)

// ShellExecuteW 返回值 <= 32 视为失败，常见错误码
const (
	seErrCancelled = 1223 // 用户取消 UAC 提权
	seErrFileNF    = 2
	seErrAccess    = 5
	seErrNoAssoc   = 31
)

// spawnTermWindow 用系统 cmd 打开终端窗口执行命令，并请求管理员权限。
//
// 已不再是默认路径：Git Bash 可用时客户端/会话启动走内嵌终端窗口
// （spawnClientTermWindow → termwindow.go），此处保留为找不到 Git Bash
// 或内嵌启动失败时的兜底。通过 ShellExecuteW 的 "runas" 动词启动 cmd.exe
// 触发 UAC；用户在提权确认框点「否」时返回可读错误，由调用方提示。
func spawnTermWindow(cwd, innerCmdline, title string) error {
	params := buildTermCmdLine(cwd, innerCmdline, title)
	if params == "" {
		return fmt.Errorf("空命令，无法启动终端")
	}

	verb, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString("cmd.exe")
	if err != nil {
		return err
	}
	par, err := syscall.UTF16PtrFromString(params)
	if err != nil {
		return err
	}
	var dirp *uint16
	if cwd = strings.TrimSpace(cwd); cwd != "" {
		if dirp, err = syscall.UTF16PtrFromString(cwd); err != nil {
			return err
		}
	}

	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(par)),
		uintptr(unsafe.Pointer(dirp)),
		uintptr(swShowNormal),
	)
	if ret <= 32 {
		return fmt.Errorf("启动终端失败：%s", shellExecErrText(ret))
	}
	return nil
}

// shellExecErrText 把 ShellExecuteW 的失败码翻成可读文案。
func shellExecErrText(code uintptr) string {
	switch code {
	case seErrCancelled:
		return "用户取消了管理员权限授权（UAC）"
	case seErrAccess:
		return "没有权限启动终端"
	case seErrFileNF, seErrNoAssoc:
		return "找不到 cmd.exe"
	default:
		return fmt.Sprintf("系统调用错误码 %d", code)
	}
}

// spawnClientTermWindow 启动一个执行 tokens 命令的终端（内嵌优先）：
// 找到 Git Bash 就拉起自身 --term 内嵌实例（winpty + bash --login -i），
// 未找到 Git Bash 或内嵌实例启动失败时回退系统 cmd 外部窗口。返回 nil = 已启动。
func spawnClientTermWindow(cwd string, tokens []string, title string) error {
	if len(tokens) == 0 || strings.TrimSpace(tokens[0]) == "" {
		return fmt.Errorf("空命令，无法启动终端")
	}
	if bashExe, err := findGitBash(); err == nil {
		if err := spawnTermInstance(bashExe, cwd, title, tokens); err == nil {
			return nil
		} else {
			log.Printf("[TERM] 内嵌终端实例启动失败，回退外部 cmd：%v", err)
		}
	}
	return spawnTermWindow(cwd, winCmdline(tokens), title)
}
