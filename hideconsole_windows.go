//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideConsole 让 GUI 进程（托盘应用）拉起的控制台子进程不弹出可见窗口：
// CREATE_NO_WINDOW （0x08000000）直接不创建新控制台，HideWindow 兜底隐藏。
// 否则每次保存配置（setx / reg 查询删除）都会闪过一个终端窗口。
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000, HideWindow: true}
}
