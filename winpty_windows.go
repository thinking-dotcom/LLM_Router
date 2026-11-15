//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---- winpty 传输层（内嵌终端的 PTY 实现）----
// 不用 ConPTY 的原因：本机（Win10 19045）实测伪控制台属性被系统无视——
// CreatePseudoConsole 成功返回句柄、属性表构建正确、CreateProcess 成功，
// 但子进程仍落回父控制台（cmd/bash 均复现，WMI 脱离进程树亦然）；而近期
// 系统变化前 ConPTY 曾正常。winpty 走「隐藏控制台 + agent 代理」机制，不经
// condrv 伪控制台路径，实测本机可用，兼容全部 Windows 版本，且随 Git for
// Windows 一起分发（内嵌包自带，零额外体积）。
//
// 链路：winpty_open 启动 agent（隐藏控制台）→ 拿 conin/conout 命名管道 →
// winpty_spawn 在其中启动 bash → 父进程读写管道、winpty_set_size 调整尺寸。

// winptyAPI 已解析的 winpty.dll 过程表（进程内懒加载一次）。
type winptyAPI struct {
	dll *windows.LazyDLL

	configNew       *windows.LazyProc // winpty_config_new(flags, **err) → *cfg
	configSetSize   *windows.LazyProc // winpty_config_set_initial_size(cfg, cols, rows)
	configFree      *windows.LazyProc
	open            *windows.LazyProc // winpty_open(cfg, **err) → *winpty_t
	free            *windows.LazyProc
	coninName       *windows.LazyProc // → LPCWSTR
	conoutName      *windows.LazyProc // → LPCWSTR
	spawnConfigNew  *windows.LazyProc // winpty_spawn_config_new(spawnFlags, app, cmd, cwd, env, **err)
	spawnConfigFree *windows.LazyProc
	spawn           *windows.LazyProc // winpty_spawn(wp, cfg, *procH, *threadH, *err32, **err) → BOOL
	setSize         *windows.LazyProc // winpty_set_size(wp, cols, rows, **err) → BOOL
	errorMsg        *windows.LazyProc
	errorFree       *windows.LazyProc
}

var (
	winptyLoaded *winptyAPI
	winptyErr    error
	winptyMu     sync.Mutex
)

// locateWinptyDLL 在 Git Bash 目录树里找 winpty.dll（winpty-agent.exe 必须与
// 其同目录，winpty.dll 会从自身所在目录拉起 agent）。优先内嵌包，其次系统
// Git Bash（usr/bin 与根 bin 两种布局都向上找）。
func locateWinptyDLL() (string, error) {
	bash, err := findGitBash()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(bash)
	for i := 0; i < 3 && dir != ""; i++ {
		cand := filepath.Join(dir, "winpty.dll")
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("Git Bash 目录树内未找到 winpty.dll")
}

// loadWinpty 懒加载 winpty.dll 并绑定过程表；结果（含失败）进程内缓存。
func loadWinpty() (*winptyAPI, error) {
	winptyMu.Lock()
	defer winptyMu.Unlock()
	if winptyLoaded != nil || winptyErr != nil {
		return winptyLoaded, winptyErr
	}
	dllPath, err := locateWinptyDLL()
	if err != nil {
		winptyErr = err
		return nil, err
	}
	api := &winptyAPI{dll: windows.NewLazyDLL(dllPath)}
	api.configNew = api.dll.NewProc("winpty_config_new")
	api.configSetSize = api.dll.NewProc("winpty_config_set_initial_size")
	api.configFree = api.dll.NewProc("winpty_config_free")
	api.open = api.dll.NewProc("winpty_open")
	api.free = api.dll.NewProc("winpty_free")
	api.coninName = api.dll.NewProc("winpty_conin_name")
	api.conoutName = api.dll.NewProc("winpty_conout_name")
	api.spawnConfigNew = api.dll.NewProc("winpty_spawn_config_new")
	api.spawnConfigFree = api.dll.NewProc("winpty_spawn_config_free")
	api.spawn = api.dll.NewProc("winpty_spawn")
	api.setSize = api.dll.NewProc("winpty_set_size")
	api.errorMsg = api.dll.NewProc("winpty_error_msg")
	api.errorFree = api.dll.NewProc("winpty_error_free")
	if err := api.dll.Load(); err != nil {
		winptyErr = fmt.Errorf("加载 winpty.dll 失败（%s）：%w", dllPath, err)
		return nil, winptyErr
	}
	winptyLoaded = api
	return api, nil
}

// werr 把 winpty 错误对象翻成 Go error 并释放。
func (a *winptyAPI) werr(what string, errPtr *uintptr) error {
	if errPtr == nil || *errPtr == 0 {
		return fmt.Errorf("%s 失败（无详细信息）", what)
	}
	defer a.errorFree.Call(*errPtr)
	msg, _, _ := a.errorMsg.Call(*errPtr)
	text := ""
	if msg != 0 {
		text = windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(msg)), 512))
		if i := strings.IndexRune(text, 0); i >= 0 {
			text = text[:i]
		}
	}
	if text == "" {
		return fmt.Errorf("%s 失败", what)
	}
	return fmt.Errorf("%s：%s", what, text)
}

// winptySession 一个 winpty 会话：agent 句柄 + conin/conout 管道 + bash 进程。
type winptySession struct {
	api    *winptyAPI
	wp     uintptr // winpty_t*
	conin  windows.Handle
	conout windows.Handle
	proc   windows.Handle
}

// openWinpty 创建 agent（cols×rows 初始尺寸）并打开 conin/conout 管道。
func openWinpty(cols, rows uint16) (*winptySession, error) {
	api, err := loadWinpty()
	if err != nil {
		return nil, err
	}
	var errPtr uintptr
	cfg, _, _ := api.configNew.Call(0, uintptr(unsafe.Pointer(&errPtr)))
	if cfg == 0 {
		return nil, api.werr("winpty_config_new", &errPtr)
	}
	defer api.configFree.Call(cfg)
	api.configSetSize.Call(cfg, uintptr(cols), uintptr(rows))
	wp, _, _ := api.open.Call(cfg, uintptr(unsafe.Pointer(&errPtr)))
	if wp == 0 {
		return nil, api.werr("winpty_open", &errPtr)
	}
	s := &winptySession{api: api, wp: wp}
	coninP, _, _ := api.coninName.Call(wp)
	conoutP, _, _ := api.conoutName.Call(wp)
	if coninP == 0 || conoutP == 0 {
		s.close()
		return nil, fmt.Errorf("获取 winpty 管道名失败")
	}
	if s.conin, err = windows.CreateFile(
		windows.StringToUTF16Ptr(utf16CStr(coninP)),
		windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0); err != nil {
		s.close()
		return nil, fmt.Errorf("打开 conin 管道失败：%w", err)
	}
	if s.conout, err = windows.CreateFile(
		windows.StringToUTF16Ptr(utf16CStr(conoutP)),
		windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0); err != nil {
		s.close()
		return nil, fmt.Errorf("打开 conout 管道失败：%w", err)
	}
	return s, nil
}

// utf16CStr 读取 winpty 返回的零结尾 LPCWSTR。
func utf16CStr(p uintptr) string {
	return windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), 256))
}

// spawn 在 agent 的隐藏控制台里启动 bash --login -i。
// envBlock 非 nil 时以该环境块启动（同 CreateProcess 格式），nil 则继承。
func (s *winptySession) spawn(bashExe, cwd string, envBlock *uint16) error {
	var errPtr uintptr
	appP := windows.StringToUTF16Ptr(bashExe)
	cmdP := windows.StringToUTF16Ptr(`"` + bashExe + `" --login -i`)
	var cwdP, envP uintptr
	if cwd != "" {
		cwdP = uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(cwd)))
	}
	if envBlock != nil {
		envP = uintptr(unsafe.Pointer(envBlock))
	}
	cfg, _, _ := s.api.spawnConfigNew.Call(
		1, // WINPTY_SPAWN_FLAG_AUTO_SHUTDOWN：子进程退出后 agent 自动收尾
		uintptr(unsafe.Pointer(appP)), uintptr(unsafe.Pointer(cmdP)),
		cwdP, envP, uintptr(unsafe.Pointer(&errPtr)),
	)
	if cfg == 0 {
		return s.api.werr("winpty_spawn_config_new", &errPtr)
	}
	defer s.api.spawnConfigFree.Call(cfg)
	var procH, threadH uintptr
	var cpErr uint32
	r1, _, _ := s.api.spawn.Call(s.wp, cfg,
		uintptr(unsafe.Pointer(&procH)), uintptr(unsafe.Pointer(&threadH)),
		uintptr(unsafe.Pointer(&cpErr)), uintptr(unsafe.Pointer(&errPtr)))
	if r1 == 0 {
		return s.api.werr(fmt.Sprintf("winpty_spawn（createProcessError=%d）", cpErr), &errPtr)
	}
	windows.CloseHandle(windows.Handle(threadH))
	s.proc = windows.Handle(procH)
	return nil
}

func (s *winptySession) write(b []byte) error {
	n := uint32(0)
	for len(b) > 0 {
		if err := windows.WriteFile(s.conin, b, &n, nil); err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

func (s *winptySession) read(b []byte) (int, error) {
	n := uint32(0)
	if err := windows.ReadFile(s.conout, b, &n, nil); err != nil {
		return int(n), err
	}
	return int(n), nil
}

func (s *winptySession) resize(cols, rows uint16) error {
	var errPtr uintptr
	r1, _, _ := s.api.setSize.Call(s.wp, uintptr(cols), uintptr(rows), uintptr(unsafe.Pointer(&errPtr)))
	if r1 == 0 {
		return s.api.werr("winpty_set_size", &errPtr)
	}
	return nil
}

// close 释放 agent 与管道（不终止已 spawn 的子进程）。
func (s *winptySession) close() {
	if s.wp != 0 {
		s.api.free.Call(s.wp)
		s.wp = 0
	}
	if s.conin != 0 {
		windows.CloseHandle(s.conin)
		s.conin = 0
	}
	if s.conout != 0 {
		windows.CloseHandle(s.conout)
		s.conout = 0
	}
}
