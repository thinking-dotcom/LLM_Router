//go:build windows

package main

import (
	"fmt"
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

// shellExecute 以给定动词启动可执行文件（ShellExecuteW），供系统终端统一使用。
func shellExecute(exe, verbName, params, cwd string) error {
	verb, err := syscall.UTF16PtrFromString(verbName)
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(exe)
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

// spawnTermWindow 用系统 cmd 打开终端窗口执行命令，并请求管理员权限。
func spawnTermWindow(cwd, innerCmdline, title string) error {
	params := buildTermCmdLine(cwd, innerCmdline, title)
	if params == "" {
		return fmt.Errorf("空命令，无法启动终端")
	}
	return shellExecute("cmd.exe", "runas", params, cwd)
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

// spawnClientTermWindow 启动一个执行 tokens 命令的系统终端（自动检测到的首个）。
// 旧调用（会话恢复等）保持兼容；需要用户选择的路径走 launchInSystemTerminal。
func spawnClientTermWindow(cwd string, tokens []string, title string) error {
	return launchInDefaultTerminal(cwd, tokens, title)
}
