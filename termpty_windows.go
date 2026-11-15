//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// ---- 内嵌终端会话（winpty + Git Bash，仅 Windows）----
// bash 以登录交互 shell 启动（--login -i）：cwd 由 spawn 直接给定，
// CHERE_INVOKING=1 保证登录后不切回 HOME；启动命令在 bash 就绪前写入 conin
// 管道（agent 有缓冲），就绪后自动执行——执行完留在提示符，窗口仍是可用的
// bash 会话。进程退出后 agent 收尾、管道断开，onExit 上报退出码。

type termProcess struct {
	s        *winptySession
	proc     windows.Handle
	pumpDone chan struct{} // 输出泵结束（管道断开），保证 onExit 晚于最后一块输出
	exited   atomic.Bool   // waiter 置位；stop 据此决定是否还需要 TerminateProcess
	stopOnce sync.Once
}

// envBlock 把 k=v 列表编成 winpty_spawn 需要的 UTF-16 环境块（双 \0 结尾）。
// 不能走 UTF16PtrFromString：整块含内部 \0，syscall 层会拒绝。
func envBlock(env []string) *uint16 {
	u16 := make([]uint16, 0, 2*len(env)+2)
	for _, e := range env {
		u16 = append(u16, utf16.Encode([]rune(e))...)
		u16 = append(u16, 0)
	}
	u16 = append(u16, 0)
	return &u16[0]
}

// startBashTerm 创建 winpty 会话、启动 bash 并注入启动命令。
// onOut 在独立 goroutine 中被调用（整块输出）；onExit 恰好调用一次，
// 且保证晚于全部 onOut。
func startBashTerm(bashExe, cwd string, tokens []string, cols, rows uint16,
	onOut func([]byte), onExit func(int)) (*termProcess, error) {

	if fi, err := os.Stat(bashExe); err != nil || fi.IsDir() {
		return nil, fmt.Errorf("bash 不可用：%s", bashExe)
	}
	s, err := openWinpty(cols, rows)
	if err != nil {
		return nil, err
	}
	// 内嵌包的 bash：把包内 git 目录置于 PATH 最前（系统 PATH 上的其它 git 不干扰）
	var bashRoot string
	if strings.HasPrefix(filepath.Clean(bashExe), filepath.Clean(bundledBashRoot())+string(os.PathSeparator)) {
		bashRoot = bundledBashRoot()
	}
	if err := s.spawn(bashExe, cwd, envBlock(bashEnv(bashRoot))); err != nil {
		s.close()
		return nil, err
	}
	tp := &termProcess{s: s, proc: s.proc, pumpDone: make(chan struct{})}
	go tp.pump(onOut)
	go tp.waiter(onExit)
	// 命令注入放在 spawn 之后：agent 对先于读取的写入有缓冲，bash 就绪即执行
	_ = s.write([]byte(buildBashCommand(tokens) + "\n"))
	return tp, nil
}

// pump 持续把会话输出整块回调给 onOut，直到管道断开（agent 收尾/会话关闭）。
func (t *termProcess) pump(onOut func([]byte)) {
	defer close(t.pumpDone)
	buf := make([]byte, 32*1024)
	for {
		n, err := t.s.read(buf)
		if n > 0 {
			onOut(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// waiter 等进程退出后收尾：关会话 → 输出泵自然结束 → 上报退出码。
// 退出码按 int32 解读（shell 异常路径常以 0xFFFFFFFF 形式出现）。
func (t *termProcess) waiter(onExit func(int)) {
	windows.WaitForSingleObject(t.proc, windows.INFINITE)
	code := uint32(0)
	_ = windows.GetExitCodeProcess(t.proc, &code)
	t.s.close()
	<-t.pumpDone
	t.exited.Store(true)
	windows.CloseHandle(t.proc)
	onExit(int(int32(code)))
}

func (t *termProcess) write(b []byte) error { return t.s.write(b) }

func (t *termProcess) resize(cols, rows uint16) error { return t.s.resize(cols, rows) }

// stop 终止 bash。TerminateProcess 只杀 bash 本身，其子进程（node 等 CLI）
// 失去控制台后大多自行退出，不做进程树遍历（与关闭外部终端窗口等价）；
// WINPTY_SPAWN_FLAG_AUTO_SHUTDOWN 让 agent 随之收尾。
func (t *termProcess) stop() {
	if t.exited.Load() {
		return
	}
	t.stopOnce.Do(func() { _ = windows.TerminateProcess(t.proc, 1) })
}
