//go:build !windows

package main

// 内嵌 Git Bash 仅 Windows；非 Windows 平台空实现保证可编译。

func warmupBundledGitBash() {}
