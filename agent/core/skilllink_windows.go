//go:build windows

package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Windows 目录联接（junction）：普通用户即可创建，无需管理员权限或开发者模式。
// 用系统 mklink /J 创建（让 Windows 处理 reparse buffer，比手工构造可靠）。
// 仅能链接目录，散放的单文件技能仍由调用方复制。

// createDirLink 创建从 src 到 dst 的目录联接；dst 已存在时先清理（旧链接或旧副本）。
func createDirLink(src, dst string) error {
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	dst, err = filepath.Abs(dst)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// mklink /J dst src：目标必须不存在。用 /c 且不加外层引号，由 Go 处理参数转义
	cmd := exec.Command("cmd", "/c", "mklink", "/J", dst, src)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建目录联接失败: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// removeDirLink 删除链接本身：符号链接直接 Remove；junction 在 Windows 下
// Lstat 表现为目录，os.Remove 对 junction 只删链接；普通非空目录回退 RemoveAll。
func removeDirLink(path string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	if err := os.Remove(path); err == nil {
		return nil
	}
	return os.RemoveAll(path)
}
