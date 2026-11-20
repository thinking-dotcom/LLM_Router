//go:build !windows

package core

import "os"

// 非 Windows：用符号链接（os.Symlink 无需提权）。
func createDirLink(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(src, dst)
}

func removeDirLink(path string) error {
	return os.Remove(path)
}
