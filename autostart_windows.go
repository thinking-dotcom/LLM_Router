//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

const autoStartRegPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const autoStartValue = "LLM Router"

// getAutoStartEnabled 读取当前用户 Run 键中是否已注册本程序自启。
func getAutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(autoStartValue)
	return err == nil
}

// setAutoStartEnabled 写入/删除当前用户 Run 键中的自启项。
// 自启命令带引号防止路径含空格；失败返回错误由调用方提示。
func setAutoStartEnabled(enabled bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if enabled {
		// 追加 --hidden：配合 main.go 的 startHidden 逻辑，开机自启且托盘常驻开启时
		// 直接隐藏到托盘（托盘关闭时 --hidden 不生效，窗口正常显示）。
		return k.SetStringValue(autoStartValue, `"`+exe+`" --hidden`)
	}
	if err := k.DeleteValue(autoStartValue); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}
