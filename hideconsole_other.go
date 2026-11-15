//go:build !windows

package main

import "os/exec"

// hideConsole 非 Windows 平台无控制台窗口问题，空实现占位。
func hideConsole(cmd *exec.Cmd) {}
