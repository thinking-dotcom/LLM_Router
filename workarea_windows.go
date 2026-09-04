//go:build windows

package main

import (
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// 工作区 = 全屏减去任务栏（无论任务栏在顶部/底部/左侧/右侧都成立）。
// 把窗口位置/尺寸夹紧到工作区内，即可保证窗口不被任务栏遮挡。
//
// 注意：窗口最小化时 GetWindowRect 返回 -32000 哨兵坐标，且系统恢复窗口时
// 会还原旧位置，因此最小化状态下跳过校正；恢复显示后再夹紧。
type workRect struct {
	Left, Top, Right, Bottom int32
}

var (
	user32Work            = syscall.NewLazyDLL("user32.dll")
	procFindWindowW       = user32Work.NewProc("FindWindowW")
	procIsIconic          = user32Work.NewProc("IsIconic")
	procIsZoomed          = user32Work.NewProc("IsZoomed")
	procIsWindow          = user32Work.NewProc("IsWindow")
	procGetWindowRect     = user32Work.NewProc("GetWindowRect")
	procSetWindowPos      = user32Work.NewProc("SetWindowPos")
	procMonitorFromWindow = user32Work.NewProc("MonitorFromWindow")
)

const (
	// MONITOR_DEFAULTTONEAREST：窗口不在任何显示器上时取最近的一个
	monitorDefaultToNearest = 2
	swpNoZOrder             = 0x0004
	swpNoActivate           = 0x0010
)

// cachedHwnd 缓存主窗口句柄（FindWindowW 开销小但没必要每秒都查）；
// 看门狗与单次校正并发读写，用 atomic 保证可见性。
var cachedHwnd atomic.Uintptr

func findMainWindowHwnd() uintptr {
	// 缓存句柄可能已失效（窗口销毁/重建），每次先经 IsWindow 校验，无效则重找
	if hwnd := cachedHwnd.Load(); hwnd != 0 {
		if ok, _, _ := procIsWindow.Call(hwnd); ok != 0 {
			return hwnd
		}
		cachedHwnd.Store(0)
	}
	title, _ := syscall.UTF16PtrFromString("LLM Router")
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd != 0 {
		cachedHwnd.Store(hwnd)
	}
	return hwnd
}

// getWorkAreaWA 返回 hwnd 所在显示器的工作区（全屏减任务栏）。
// 注意：SPI_GETWORKAREA 只能取主显示器的工作区，会把副屏上的窗口误判为越界
// 而被看门狗拽回主屏，因此必须按窗口所在显示器取工作区（monitorInfo 定义
// 与 GetMonitorInfoW 过程复用 tray_windows.go 中的既有实现）。
func getWorkAreaWA(hwnd uintptr) (workRect, bool) {
	hmon, _, _ := procMonitorFromWindow.Call(hwnd, uintptr(monitorDefaultToNearest))
	if hmon == 0 {
		return workRect{}, false
	}
	var mi monitorInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	if r1, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); r1 == 0 {
		return workRect{}, false
	}
	return workRect{
		Left:   mi.rcWork.left,
		Top:    mi.rcWork.top,
		Right:  mi.rcWork.right,
		Bottom: mi.rcWork.bottom,
	}, true
}

// pu 把 int32 坐标安全转为 uintptr（保留低 32 位，负数坐标不丢符号位）
func pu(v int32) uintptr { return uintptr(uint32(v)) }

// clampWindowToWorkArea 单次校正：窗口可见且越出工作区时夹紧位置/尺寸
func clampWindowToWorkArea() {
	hwnd := findMainWindowHwnd()
	if hwnd == 0 {
		return
	}
	// 最小化（图标态）时坐标是哨兵值，跳过
	if r1, _, _ := procIsIconic.Call(hwnd); r1 != 0 {
		return
	}
	// 最大化时窗口矩形会越出工作区（系统不可见边框），夹紧会破坏最大化状态，跳过
	if r1, _, _ := procIsZoomed.Call(hwnd); r1 != 0 {
		return
	}
	wa, ok := getWorkAreaWA(hwnd)
	if !ok {
		return
	}
	var wr workRect
	if r2, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr))); r2 == 0 {
		return
	}
	w, h := wr.Right-wr.Left, wr.Bottom-wr.Top
	maxW, maxH := wa.Right-wa.Left, wa.Bottom-wa.Top
	if maxW <= 0 || maxH <= 0 {
		return
	}
	if w > maxW {
		w = maxW
	}
	if h > maxH {
		h = maxH
	}
	x, y := wr.Left, wr.Top
	if x < wa.Left {
		x = wa.Left
	}
	if y < wa.Top {
		y = wa.Top
	}
	if x+w > wa.Right {
		x = wa.Right - w
	}
	if y+h > wa.Bottom {
		y = wa.Bottom - h
	}
	if x != wr.Left || y != wr.Top || w != wr.Right-wr.Left || h != wr.Bottom-wr.Top {
		procSetWindowPos.Call(hwnd, 0, pu(x), pu(y), pu(w), pu(h), uintptr(swpNoZOrder|swpNoActivate))
	}
}

// startWorkAreaWatchdog 后台看门狗：周期性校正窗口位置，
// 覆盖任务栏移动/分辨率变化/从任务栏点击恢复等所有场景。
func startWorkAreaWatchdog() {
	go func() {
		for {
			clampWindowToWorkArea()
			time.Sleep(800 * time.Millisecond)
		}
	}()
}

// centerWindowInWorkArea 把窗口水平垂直居中于工作区（单次）
func centerWindowInWorkArea() bool {
	hwnd := findMainWindowHwnd()
	if hwnd == 0 {
		return false
	}
	if r1, _, _ := procIsIconic.Call(hwnd); r1 != 0 {
		return false
	}
	wa, ok := getWorkAreaWA(hwnd)
	if !ok {
		return false
	}
	var wr workRect
	if r2, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr))); r2 == 0 {
		return false
	}
	w, h := wr.Right-wr.Left, wr.Bottom-wr.Top
	x := wa.Left + ((wa.Right-wa.Left)-w)/2
	y := wa.Top + ((wa.Bottom-wa.Top)-h)/2
	procSetWindowPos.Call(hwnd, 0, pu(x), pu(y), pu(w), pu(h), uintptr(swpNoZOrder|swpNoActivate))
	return true
}

// centerOnceWhenVisible 等待窗口首次变为可见（从最小化恢复）后居中一次。
// 启动时窗口是最小化状态，定位不可靠，因此等真正显示时再居中。
func centerOnceWhenVisible() {
	go func() {
		for i := 0; i < 600; i++ { // 最长等待约 2 分钟
			hwnd := findMainWindowHwnd()
			if hwnd != 0 {
				if r1, _, _ := procIsIconic.Call(hwnd); r1 == 0 {
					centerWindowInWorkArea()
					return
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()
}
