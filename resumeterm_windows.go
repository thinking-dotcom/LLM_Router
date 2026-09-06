//go:build windows

package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// spawnInNewConsole 以 CREATE_NEW_CONSOLE 直接启动 args[0] 描述的命令并返回，
// 进程句柄即关闭（窗口由其自身控制台承载，父进程无需等待）。
//
// 为什么不走 exec.Command + CREATE_NEW_CONSOLE：Go 的 os/exec 在未显式设置
// Stdin/Stdout/Stderr 时会把这些标准句柄接到 NUL（os.DevNull）。本应用为
// GUI 进程、本身没有控制台，于是 cmd/ps 子进程虽然弹出了新控制台窗口，
// 但 shell 的输入来自 NUL（立即 EOF）、提示符与命令输出全部写入 NUL——
// 窗口里只剩 conhost 的光标闪烁、命令毫无反应。
//
// 本函数用 x/sys/windows 的 CreateProcess 直启且【不设置 STARTF_USESTDHANDLES】，
// 系统会把新进程的标准输入/输出自动绑定到新建的控制台，命令与交互恢复正常。
// dir 非空时作为子进程工作目录（lpCurrentDirectory）。
func spawnInNewConsole(args []string, dir string) error {
	if len(args) == 0 || args[0] == "" {
		return errors.New("空命令行")
	}
	return spawnConsole(windows.ComposeCommandLine(args), dir)
}

// spawnConsole 以 CREATE_NEW_CONSOLE 直启 cmdline；见 spawnInNewConsole 说明。
func spawnConsole(cmdline, dir string) error {
	cl, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		return err
	}
	var dirPtr *uint16
	if dir != "" {
		dirPtr, err = windows.UTF16PtrFromString(dir)
		if err != nil {
			return err
		}
	}
	si := &windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var pi windows.ProcessInformation
	err = windows.CreateProcess(nil, cl, nil, nil, false, windows.CREATE_NEW_CONSOLE, nil, dirPtr, si, &pi)
	if err != nil {
		return err
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	return nil
}
