//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestStartBashTermSmoke 端到端冒烟：内嵌 Git Bash 解包 → winpty 会话 →
// bash --login。验证三条链路——启动命令注入（bash 就绪后自动执行）、
// 输出回收、resize / 键入 / stop 生命周期。首次运行会解压内嵌包（十几秒）。
func TestStartBashTermSmoke(t *testing.T) {
	bash, err := findGitBash()
	if err != nil {
		t.Skipf("本机无可用 Git Bash：%v", err)
	}
	if !strings.HasPrefix(strings.ToLower(filepath.Clean(bash)), strings.ToLower(filepath.Clean(bundledBashRoot()))) {
		t.Logf("使用系统 Git Bash（内嵌包不可用）：%s", bash)
	} else {
		t.Logf("使用内嵌 Git Bash：%s", bash)
	}

	const marker1 = "LLMROUTER_PTY_OK_9F3A"
	const marker2 = "LLMROUTER_PTY_OK2_77"

	var mu sync.Mutex
	var out strings.Builder
	appendOut := func(b []byte) {
		mu.Lock()
		out.Write(b)
		mu.Unlock()
	}
	waitFor := func(marker string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			mu.Lock()
			hit := strings.Contains(out.String(), marker)
			mu.Unlock()
			if hit {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}

	exited := make(chan int, 1)
	tp, err := startBashTerm(bash, `C:\`, []string{"echo", marker1}, 100, 30, appendOut,
		func(code int) { exited <- code })
	if err != nil {
		t.Fatalf("startBashTerm: %v", err)
	}

	// 1) 启动命令应在 bash 就绪后自动执行并回显标记
	found := make(chan bool, 1)
	go func() { found <- waitFor(marker1, 30*time.Second) }()
	select {
	case code := <-exited:
		mu.Lock()
		buf := out.String()
		mu.Unlock()
		t.Fatalf("bash 提前退出（code=%d）\n输出尾部: %q", code, tailStr(buf, 400))
	case ok := <-found:
		if !ok {
			mu.Lock()
			buf := out.String()
			mu.Unlock()
			t.Fatalf("30s 内未见标记 %s\n输出尾部: %q", marker1, tailStr(buf, 400))
		}
	}
	t.Logf("启动命令注入/回显 OK")

	// 2) resize 与键入（write 链路）：注入第二条命令
	if err := tp.resize(120, 40); err != nil {
		t.Errorf("resize: %v", err)
	}
	if err := tp.write([]byte("echo " + marker2 + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !waitFor(marker2, 15*time.Second) {
		mu.Lock()
		buf := out.String()
		mu.Unlock()
		t.Fatalf("键入命令 15s 内未见回显 %s\n输出尾部: %q", marker2, tailStr(buf, 400))
	}
	t.Logf("键入/输出链路 OK")

	// 3) stop 生命周期：应触发 onExit 且不悬挂
	tp.stop()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("stop 后 10s 未收到 onExit")
	}
}

// tailStr 取末尾 n 字节，便于失败时看输出。
func tailStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
