//go:build !windows

package core

import (
	"errors"
	"os"
)

// SetUserEnv 非 Windows 平台不支持自动写入用户环境变量。
func SetUserEnv(name, value string) error {
	return errors.New("仅 Windows 支持自动写入用户环境变量")
}

// GetUserEnv 非 Windows 平台读取当前进程环境变量（只可用于展示，无法持久化写入）。
func GetUserEnv(name string) (string, bool, error) {
	v, ok := os.LookupEnv(name)
	return v, ok, nil
}

// DelUserEnv 非 Windows 平台不支持删除用户级环境变量。
func DelUserEnv(name string) error {
	return errors.New("仅 Windows 支持自动删除用户环境变量")
}
