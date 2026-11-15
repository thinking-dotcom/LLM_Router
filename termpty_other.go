//go:build !windows

package main

import "errors"

// 内嵌终端仅支持 Windows（ConPTY / Git Bash 均 Win32 专属）；
// 非 Windows 平台保留类型与桩保证可编译，与 termspawn_other.go 同理。

type termProcess struct{}

func startBashTerm(bashExe, cwd string, tokens []string, cols, rows uint16,
	onOut func([]byte), onExit func(int)) (*termProcess, error) {
	return nil, errors.New("内嵌终端仅支持 Windows")
}
