//go:build !windows

package main

import "errors"

// spawnInNewConsole 非 Windows 平台由 openTerminalCommand 的 POSIX 分支
// （Terminal.app / gnome-terminal 等）接管，此处占位保证 main.go 可编译。
func spawnInNewConsole(args []string, dir string) error {
	return errors.New("spawnInNewConsole 仅支持 Windows")
}
