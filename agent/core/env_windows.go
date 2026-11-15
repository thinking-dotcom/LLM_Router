//go:build windows

package core

import (
	"unsafe"

	"golang.org/x/sys/windows"
	winreg "golang.org/x/sys/windows/registry"
)

// GetUserEnv 读取当前用户级环境变量（HKCU\Environment）。
// 返回 (值, 是否存在, 错误)；变量不存在时 exists=false 且 error 为 nil。
func GetUserEnv(name string) (string, bool, error) {
	if name == "" {
		return "", false, nil
	}
	k, err := winreg.OpenKey(winreg.CURRENT_USER, `Environment`, winreg.QUERY_VALUE)
	if err != nil {
		// OpenKey 返回原始 Errno 而非 winreg.ErrNotExist；键不存在视为“变量不存在”
		if err == windows.ERROR_FILE_NOT_FOUND || err == windows.ERROR_PATH_NOT_FOUND {
			return "", false, nil
		}
		return "", false, err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		if err == winreg.ErrNotExist {
			return "", false, nil
		}
		return "", false, err
	}
	return v, true, nil
}

// DelUserEnv 删除当前用户级环境变量（HKCU\Environment），并广播 WM_SETTINGCHANGE。
// 变量不存在时视为成功（幂等）。
func DelUserEnv(name string) error {
	if name == "" {
		return nil
	}
	k, err := winreg.OpenKey(winreg.CURRENT_USER, `Environment`, winreg.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil {
		if err == winreg.ErrNotExist {
			return nil
		}
		return err
	}
	broadcastEnvChange()
	return nil
}

// SetUserEnv 写入当前用户级环境变量（注册表 HKCU\Environment，无需管理员权限），
// 并广播 WM_SETTINGCHANGE 通知系统（资源管理器等据此刷新；已启动的进程不受影响，
// 需重开终端才能拿到新值）。
func SetUserEnv(name, value string) error {
	if name == "" {
		return nil
	}
	k, err := winreg.OpenKey(winreg.CURRENT_USER, `Environment`, winreg.SET_VALUE|winreg.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	// 已存在的 REG_EXPAND_SZ 变量须沿用展开字符串写入，否则 %VAR% 展开语义丢失；
	// 其余（含不存在的新变量）维持 REG_SZ。
	// 注意：必须在 SET_VALUE|QUERY_VALUE 权限下打开，RegQueryValueEx 需要查询权限；
	// GetValue(name, nil) 为"查询所需大小"模式，同样需要该权限。
	if valtype, _, qerr := k.GetValue(name, nil); qerr == nil && valtype == winreg.EXPAND_SZ {
		if err := k.SetExpandStringValue(name, value); err != nil {
			return err
		}
		broadcastEnvChange()
		return nil
	}
	if err := k.SetStringValue(name, value); err != nil {
		return err
	}
	broadcastEnvChange()
	return nil
}

// broadcastEnvChange 广播 WM_SETTINGCHANGE("Environment")。
// 注意 SendMessageTimeoutW 共 7 个参数，末位 lpdwResult 必须显式传入，
// 否则函数会把结果写入垃圾地址导致 ACCESS_VIOLATION 进程崩溃。
func broadcastEnvChange() {
	const (
		HWND_BROADCAST   = 0xFFFF
		WM_SETTINGCHANGE = 0x001A
		SMTO_ABORTIFHUNG = 0x0002
	)
	namePtr, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	var result uintptr // lpdwResult：必须有效；函数仅在有硬件会话时写回
	_, _, _ = proc.Call(
		HWND_BROADCAST,
		WM_SETTINGCHANGE,
		0,
		uintptr(unsafe.Pointer(namePtr)),
		SMTO_ABORTIFHUNG,
		2000,
		uintptr(unsafe.Pointer(&result)),
	)
}
