//go:build !windows

package main

import "fmt"

// spawnTermWindow 非 Windows 平台暂不支持：本项目桌面壳只针对 Windows
// （托盘、UAC 提权、ConPTY 都是 Win32 专属）。
func spawnTermWindow(cwd, innerCmdline, title string) error {
	return fmt.Errorf("当前平台不支持启动系统终端")
}

// spawnClientTermWindow 同上：内嵌/外部终端启动均只支持 Windows。
func spawnClientTermWindow(cwd string, tokens []string, title string) error {
	return fmt.Errorf("当前平台不支持启动终端")
}
