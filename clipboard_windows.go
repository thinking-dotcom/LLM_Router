//go:build windows

package main

import (
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 剪贴板直读（Win32 API）：原经 powershell Get-Clipboard，本机实测单次 ~4.8s
//（进程冷启 + PS 初始化），是「启动客户端」拉终端前最大的同步耗时。改直接调
// user32/kernel32，微秒级返回，行为对齐原实现：优先 CF_UNICODETEXT 文本，为空
// 时取 CF_HDROP 文件路径列表（取首条即可，消费方 clipboardLaunchDir 只用首行）。

var (
	cliUser32            = windows.NewLazySystemDLL("user32.dll")
	procOpenClipboard    = cliUser32.NewProc("OpenClipboard")
	procCloseClipboard   = cliUser32.NewProc("CloseClipboard")
	procGetClipboardData = cliUser32.NewProc("GetClipboardData")
	cliKernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalLock       = cliKernel32.NewProc("GlobalLock")
	procGlobalUnlock     = cliKernel32.NewProc("GlobalUnlock")
	procGlobalSize       = cliKernel32.NewProc("GlobalSize")
)

const (
	cfUnicodeText = 13
	cfHDrop       = 15
)

// dropFiles 对应 Win32 DROPFILES 结构（CF_HDROP 全局内存的头部）。
type dropFiles struct {
	Files uint32 // 文件名列表相对块首的偏移
	X, Y  int32
	Wide  int32 // 非 0 表示宽字符
}

// clipboardTextWin 读剪贴板文本或文件路径首条。ok=false 表示剪贴板打不开
// （被其它进程长期占用等，罕见），调用方退回 powershell 兜底；剪贴板为空属
// 正常情况，返回 ("", true)。
func clipboardTextWin() (text string, ok bool) {
	opened := false
	for i := 0; i < 10; i++ {
		if r, _, _ := procOpenClipboard.Call(uintptr(0)); r != 0 {
			opened = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !opened {
		return "", false
	}
	defer procCloseClipboard.Call()

	if h, _, _ := procGetClipboardData.Call(cfUnicodeText); h != 0 {
		if s := globalMemString(h); strings.TrimSpace(s) != "" {
			return s, true
		}
	}
	if h, _, _ := procGetClipboardData.Call(cfHDrop); h != 0 {
		if s := globalMemFirstPath(h); s != "" {
			return s, true
		}
	}
	return "", true
}

// globalMemString 锁定全局内存读 NUL 结尾的宽字符文本。
func globalMemString(h uintptr) string {
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)
	n, _, _ := procGlobalSize.Call(h)
	if n == 0 {
		return ""
	}
	w := unsafe.Slice((*uint16)(unsafe.Pointer(p)), n/2)
	return windows.UTF16ToString(w)
}

// globalMemFirstPath 解析 CF_HDROP 块，返回首个文件完整路径（无文件返回空）。
func globalMemFirstPath(h uintptr) string {
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)
	n, _, _ := procGlobalSize.Call(h)
	if n < unsafe.Sizeof(dropFiles{}) {
		return ""
	}
	hdr := (*dropFiles)(unsafe.Pointer(p))
	if hdr.Wide == 0 || uintptr(hdr.Files) >= n {
		return ""
	}
	w := unsafe.Slice((*uint16)(unsafe.Pointer(uintptr(p)+uintptr(hdr.Files))), (n-uintptr(hdr.Files))/2)
	for i, c := range w {
		if c == 0 {
			return windows.UTF16ToString(w[:i])
		}
	}
	return ""
}
