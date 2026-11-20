//go:build !windows

package main

import "fmt"

// SystemTerminal 非 Windows 平台空实现（桌面壳仅支持 Windows）。
type SystemTerminal struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type systemTerminalSpec struct {
	SystemTerminal
	Exe  string
	Kind string
}

func detectSystemTerminals() []systemTerminalSpec { return nil }

func findSystemTerminal(id string) (systemTerminalSpec, bool) {
	return systemTerminalSpec{}, false
}

func launchInSystemTerminal(t systemTerminalSpec, cwd, title string, tokens []string) error {
	return fmt.Errorf("当前平台不支持启动系统终端")
}

func launchInDefaultTerminal(cwd string, tokens []string, title string) error {
	return fmt.Errorf("当前平台不支持启动终端")
}
