//go:build windows

package main

import (
	"context"
	"log"
	golruntime "runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// 系统托盘图标：Wails v2.14 的 vendored 前端运行时未暴露托盘 JS API，
// 这里直接用 Win32 Shell_NotifyIcon + 隐藏窗口原生实现。
// 托盘常驻开关打开时创建图标，关闭时销毁；点图标恢复主窗口，右键菜单可退出。

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procLoadImageW          = user32.NewProc("LoadImageW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenuEx    = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procPostMessage         = user32.NewProc("PostMessageW")
	procMonitorFromPoint      = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW       = user32.NewProc("GetMonitorInfoW")
	procShellNotifyIconW      = shell32.NewProc("Shell_NotifyIconW")
	procGetCurrentThreadId    = kernel32.NewProc("GetCurrentThreadId")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetForegroundWindow   = user32.NewProc("GetForegroundWindow")
	procAttachThreadInput     = user32.NewProc("AttachThreadInput")
)

// 消息/常量定义
const (
	wmTrayCallback = 0x0400 + 1 // WM_APP+1
	wmTrayShutdown = 0x0400 + 2 // WM_APP+2：请求托盘线程销毁窗口并退出消息循环
	trayIconID     = 1

	// ERROR_CLASS_ALREADY_EXISTS：二次开启托盘时窗口类已注册，可复用继续建窗
	errorClassAlreadyExists = syscall.Errno(1410)

	nimAdd    = 0x00000000
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmCommand       = 0x0111
	wmDestroy       = 0x0002
	wmQuit          = 0x0012

	tpmRightButton = 0x0002
	tpmLeftAlign   = 0x0000
	tpmTopAlign    = 0x0000
	tpmBottomAlign = 0x0020
	tpmRightAlign  = 0x0008

	menuShow = 1001
	menuQuit = 1002

	imageIcon = 1
	// 应用图标资源 ID（winc app.go 中 AppIconID = 3）
	appIconResID = 3
)

// wndClassEx WNDCLASSEXW
type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

// notifyIconData NOTIFYICONDATAW（Vista+ 结构，含 guid/hBalloonIcon）
type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

// point POINT
type point struct {
	x, y int32
}

// rect RECT
type rect struct {
	left, top, right, bottom int32
}

// monitorInfo MONITORINFO
type monitorInfo struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
}

// tpmParams TPMPARAMS
type tpmParams struct {
	cbSize    uint32
	rcExclude rect
}

// taskbarDir 任务栏所在方向
type taskbarDir int

const (
	taskbarTop taskbarDir = iota
	taskbarBottom
	taskbarLeft
	taskbarRight
)

// msg MSG（64 位对齐与 Win32 一致）
type msg struct {
	hwnd    uintptr
	message uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
}

var (
	// trayCtxVal / trayHwnd 跨 goroutine 读写（托盘线程 ↔ 主线程），用 atomic 保证可见性
	trayCtxVal atomic.Value // 存 context.Context
	trayHwnd   atomic.Uintptr
	trayDoneCh chan struct{} // 托盘消息循环退出后关闭，trayShutdown 等待销毁完成

	// 回调指针只在进程内创建一次：syscall.NewCallback 每次调用都消耗一个
	// 回调槽位（进程上限约 2000），反复开关托盘会泄漏耗尽。
	trayWndProc = syscall.NewCallback(trayWndProcImpl)

	// 预分配菜单项 UTF16 字符串，避免每次右键重复分配
	trayShowTxt, _ = syscall.UTF16PtrFromString("打开面板")
	trayQuitTxt, _ = syscall.UTF16PtrFromString("退出")
)

// trayWndProc 托盘隐藏窗口消息处理
func trayWndProcImpl(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	switch message {
	case wmTrayCallback:
		switch uint32(lparam) {
		case wmLButtonUp, wmLButtonDblClk:
			showMainWindow()
		case wmRButtonUp:
			showTrayMenu(hwnd)
		}
		return 0
	case wmCommand:
		id := uint32(wparam & 0xFFFF)
		if id == menuShow {
			showMainWindow()
		} else if id == menuQuit {
			quitFromTray()
		}
		return 0
	case wmTrayShutdown:
		// 销毁动作必须发生在创建线程（本线程）：移除图标后 DestroyWindow，
		// WM_DESTROY 里 PostQuitMessage 让 runTrayLoop 自然退出。
		removeTrayIcon(hwnd)
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
	return ret
}

// showMainWindow 从托盘恢复主窗口并置前
func showMainWindow() {
	if ctx := trayCtxVal.Load(); ctx != nil {
		runtime.WindowShow(ctx.(context.Context))
		// 恢复完成后再校正一次位置（最小化状态下无法可靠定位），
		// 防止窗口标题栏被顶部任务栏遮挡
		go func() { time.Sleep(80 * time.Millisecond); clampWindowToWorkArea() }()
	}
	activateExistingWindow()
}

// quitFromTray 托盘菜单「退出」：先置强制退出标志再退出（绕过 OnBeforeClose 的隐藏拦截）
func quitFromTray() {
	if appRef != nil {
		appRef.forceQuit.Store(true)
	}
	if ctx := trayCtxVal.Load(); ctx != nil {
		runtime.Quit(ctx.(context.Context))
	}
}

// showTrayMenu 右键弹出托盘菜单
func showTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	procAppendMenuW.Call(menu, 0, menuShow, uintptr(unsafe.Pointer(trayShowTxt)))
	procAppendMenuW.Call(menu, 0x0800, 0, 0) // MF_SEPARATOR
	procAppendMenuW.Call(menu, 0, menuQuit, uintptr(unsafe.Pointer(trayQuitTxt)))

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// 确保窗口在前台：使用 AttachThreadInput 临时附加到前台线程，
	// 绕过 Windows 对非前台窗口调用 SetForegroundWindow 的 200ms 超时限制。
	// 比 Alt 键模拟方案更可靠、响应更快。
	fgWnd, _, _ := procGetForegroundWindow.Call()
	fgThreadId, _, _ := procGetWindowThreadProcessId.Call(fgWnd, 0)
	myThreadId, _, _ := procGetCurrentThreadId.Call()
	attached := false
	if fgWnd != 0 && fgThreadId != myThreadId {
		procAttachThreadInput.Call(myThreadId, fgThreadId, 1) // TRUE = attach
		attached = true
	}
	procSetForegroundWindow.Call(hwnd)
	if attached {
		procAttachThreadInput.Call(myThreadId, fgThreadId, 0) // FALSE = detach
	}

	// 根据任务栏所在方向选择弹出方向：任务栏在右侧就向左弹出，以此类推。
	// 同时用 rcExclude 排除任务栏矩形作兜底，双保险保证不覆盖任务栏。
	var params tpmParams
	params.cbSize = uint32(unsafe.Sizeof(params))
	flags := tpmRightButton | tpmLeftAlign
	if dir, tb := taskbarInfo(pt.x, pt.y); tb != nil {
		params.rcExclude = *tb
		switch dir {
		case taskbarTop: // 任务栏在上 → 菜单向下展开
			flags = tpmRightButton | tpmTopAlign
		case taskbarBottom: // 任务栏在下 → 菜单向上展开
			flags = tpmRightButton | tpmBottomAlign
		case taskbarLeft: // 任务栏在左 → 菜单向右展开
			flags = tpmRightButton | tpmLeftAlign
		case taskbarRight: // 任务栏在右 → 菜单向左展开
			flags = tpmRightButton | tpmRightAlign
		}
	}

	// TrackPopupMenuEx 阻塞直到菜单关闭；命令经 WM_COMMAND 发回 hwnd。
	// 注意签名只有 6 个参数：(menu, flags, x, y, hwnd, lptpm)，无 reserved 参数，
	// 多传一个 0 会导致 hwnd/lptpm 错位、菜单无法弹出。
	procTrackPopupMenuEx.Call(menu, uintptr(flags),
		uintptr(pt.x), uintptr(pt.y), hwnd, uintptr(unsafe.Pointer(&params)))
	// 经典时序：菜单返回后向自身发一条 WM_NULL，让系统正确收回焦点并关闭菜单，
	// 否则部分 Windows 上会出现第一次弹出后焦点错乱、再点无响应。
	procPostMessage.Call(hwnd, 0, 0, 0)
	procDestroyMenu.Call(menu)
}

// taskbarInfo 返回 (x,y) 所在显示器的任务栏方向与其矩形（屏幕减去工作区的窄带）。
// 任务栏矩形供 TrackPopupMenuEx.rcExclude 使用（保证菜单不覆盖任务栏），
// 方向供调用方选择对齐标志（任务栏在右侧 → 菜单向左弹出，以此类推）。
// MonitorFromPoint 的 POINT 是 8 字节结构体，在 amd64 上按寄存器传值：
// 低 32 位为 x，高 32 位为 y，不能拆成两个参数传入。
func taskbarInfo(x, y int32) (taskbarDir, *rect) {
	packed := uintptr(uint32(x)) | (uintptr(uint32(y)) << 32)
	hmon, _, _ := procMonitorFromPoint.Call(packed, 0) // MONITOR_DEFAULTTONEAREST
	if hmon == 0 {
		return taskbarTop, nil
	}
	var mi monitorInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	ret, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	if ret == 0 {
		return taskbarTop, nil
	}
	// 任务栏矩形 = 显示器矩形与工作区矩形的差。任务栏在四个方向之一：
	// 顶部（rcWork.Top > rcMonitor.Top）、底部（rcWork.Bottom < rcMonitor.Bottom）、
	// 左侧、右侧。仅一个方向有差即视为该方向的窄带。
	r := mi.rcMonitor
	w := mi.rcWork
	switch {
	case w.top > r.top: // 顶部任务栏
		return taskbarTop, &rect{r.left, r.top, r.right, w.top}
	case w.bottom < r.bottom: // 底部任务栏
		return taskbarBottom, &rect{r.left, w.bottom, r.right, r.bottom}
	case w.left > r.left: // 左侧任务栏
		return taskbarLeft, &rect{r.left, r.top, w.left, r.bottom}
	case w.right < r.right: // 右侧任务栏
		return taskbarRight, &rect{w.right, r.top, r.right, r.bottom}
	}
	return taskbarTop, nil
}

// trayMu 串行化 trayInit/trayShutdown：Wails 每个绑定调用在独立 goroutine 执行，
// 快速连续开关托盘时两个 goroutine 都能通过 trayHwnd 检查，各自建托盘窗口并
// NIM_ADD 同一个 uID，造成重复图标残留；trayDoneCh 的写读也无同步（数据竞争）。
var trayMu sync.Mutex

// trayInit 创建托盘图标（在独立线程跑隐藏窗口消息循环，避免占用主线程消息泵）
func trayInit(ctx context.Context) bool {
	trayMu.Lock()
	defer trayMu.Unlock()
	if trayHwnd.Load() != 0 {
		return true
	}
	trayCtxVal.Store(ctx)
	ch := make(chan struct{})
	done := make(chan struct{})
	trayDoneCh = done
	go func() {
		defer close(done)
		// 关键：托盘窗口必须固定在一个 OS 线程上。Win32 窗口属于创建它的线程，
		// 消息队列也挂在该线程。若不 LockOSThread，Go 调度器会在 GetMessage
		// 调用间隙把 goroutine 迁到别的线程，新线程拉不到窗口消息，
		// 表现为右键菜单无响应/应用迟钝。
		golruntime.LockOSThread()
		hwnd := createTrayWindow()
		trayHwnd.Store(hwnd)
		close(ch)
		if hwnd == 0 {
			return
		}
		runTrayLoop(hwnd)
		// 消息循环已随 WM_QUIT 退出（窗口已销毁），清理共享句柄
		trayHwnd.Store(0)
	}()
	<-ch
	return trayHwnd.Load() != 0
}

// createTrayWindow 注册窗口类并创建隐藏窗口 + 添加托盘图标
func createTrayWindow() uintptr {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("LLMRouterTrayWnd")

	// 回调指针 trayWndProc 在包级只创建一次，避免每次开关托盘泄漏回调槽位
	wc := &wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   trayWndProc,
		hInstance:     hInst,
		lpszClassName: className,
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(wc))); atom == 0 {
		// 二次开启托盘时类已注册（ERROR_CLASS_ALREADY_EXISTS），复用继续建窗
		if err != errorClassAlreadyExists {
			log.Println("[TRAY] RegisterClassExW failed:", err)
			return 0
		}
	}

	hwnd, _, _ := procCreateWindowExW.Call(
		0, // exStyle
		uintptr(unsafe.Pointer(className)),
		0, // windowName
		0, // style: 不可见
		0, 0, 0, 0,
		0, 0,
		hInst,
		0)
	if hwnd == 0 {
		log.Println("[TRAY] CreateWindowExW failed")
		return 0
	}

	// 图标：优先应用图标资源，失败退回系统默认图标
	hIcon, _, _ := procLoadImageW.Call(hInst, appIconResID, imageIcon, 16, 16, 0)
	if hIcon == 0 {
		hIcon, _, _ = procLoadImageW.Call(0, 32512, imageIcon, 16, 16, 0x00008000) // IDI_APPLICATION | LR_DEFAULTSIZE
	}

	var nid notifyIconData
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = hwnd
	nid.uID = trayIconID
	nid.uFlags = nifMessage | nifIcon | nifTip
	nid.uCallbackMessage = wmTrayCallback
	nid.hIcon = hIcon
	tip, _ := syscall.UTF16FromString("LLM Router")
	copy(nid.szTip[:], tip)

	ret, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		log.Println("[TRAY] Shell_NotifyIconW NIM_ADD failed")
		procDestroyWindow.Call(hwnd)
		return 0
	}
	return hwnd
}

// runTrayLoop 托盘线程消息循环
func runTrayLoop(hwnd uintptr) {
	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 { // 0=WM_QUIT, -1=错误
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// removeTrayIcon 从通知区域移除托盘图标（必须在托盘线程调用）
func removeTrayIcon(hwnd uintptr) {
	var nid notifyIconData
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = hwnd
	nid.uID = trayIconID
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

// trayShutdown 移除托盘图标并销毁隐藏窗口。
// Win32 规定 DestroyWindow 只能由创建窗口的线程调用，这里只向托盘线程
// PostMessage 投递 wmTrayShutdown，由托盘 wndproc 自己完成销毁并 PostQuitMessage，
// 让 runTrayLoop 自然退出、LockOSThread 的 goroutine 正常结束。
func trayShutdown() {
	trayMu.Lock()
	defer trayMu.Unlock()
	hwnd := trayHwnd.Load()
	if hwnd == 0 {
		return
	}
	done := trayDoneCh
	if done == nil {
		return
	}
	procPostMessage.Call(hwnd, wmTrayShutdown, 0, 0)
	// 等待托盘线程完成销毁：保证随后立刻重新开启托盘时不会撞上半销毁状态
	<-done
}
