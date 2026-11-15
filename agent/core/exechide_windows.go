//go:build windows

package core

import (
	"os/exec"
	"syscall"
)

// hideWindow 让 agent 包拉起的控制台子进程（npm / CLI 版本探测）不弹出可见窗口：
// CREATE_NO_WINDOW（0x08000000）直接不创建新控制台，HideWindow 兜底隐藏。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000, HideWindow: true}
}
