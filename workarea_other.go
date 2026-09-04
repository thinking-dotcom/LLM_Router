//go:build !windows

package main

// 非 Windows 平台无需任务栏工作区检测（占位实现）
func startWorkAreaWatchdog()   {}
func centerOnceWhenVisible() {}
