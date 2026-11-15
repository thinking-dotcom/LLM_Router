//go:build !windows

package core

import "os/exec"

// hideWindow 非 Windows 平台无「隐藏控制台窗口」概念，空实现。
func hideWindow(_ *exec.Cmd) {}
