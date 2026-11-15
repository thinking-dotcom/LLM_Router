//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ---- 官方 Git Bash 内嵌与探测（内嵌终端的默认宿主）----
// 项目内嵌官方 PortableGit 自解压包（assets/gitbash/），首次使用终端时静默
// 解压到 %LOCALAPPDATA%\LLMRouter\gitbash\<版本>\，之后直接使用——开箱即用，
// 不要求用户安装 Git。内嵌包不可用（未打包/解压失败）时回退探测系统里的
// Git Bash：PATH → 注册表 GitForWindows InstallPath → 常见安装路径；便携版
// Git 常装在非标准盘，不能只硬编码 Program Files。

// 内嵌包信息（升级 Git 时同步改这三处与 go:embed 文件名）。
const (
	bundledGitVersion  = "2.56.0.4"
	bundledGitSFXFile  = "PortableGit-2.56.0.4-64-bit.7z.exe"
	bundledGitShareDir = "LLMRouter\\gitbash"
)

//go:embed assets/gitbash/PortableGit-2.56.0.4-64-bit.7z.exe
var bundledGitBashSFX []byte

var bundledMu sync.Mutex // 解包并发保护（warmup goroutine 与首次启动竞态）

// findGitBash 返回一个可用的 bash.exe 绝对路径；内嵌包优先，找不到返回错误。
func findGitBash() (string, error) {
	// 1) 内嵌官方包（开箱即用的主路径）
	if bash, err := ensureBundledGitBash(); err == nil {
		return bash, nil
	} else {
		_ = err // 解压失败时静默走系统探测，由外部兜底
	}
	// 2) 系统安装：PATH 里的 bash.exe（须验证确实是 Git for Windows 自带，
	//    排除 WSL 的 C:\Windows\System32\bash.exe）
	if p, err := exec.LookPath("bash"); err == nil && isGitBashExe(p) {
		return filepath.Clean(p), nil
	}
	// 3) 注册表：Git for Windows 安装器写 HKLM/HKCU（64/32 位视图）
	for _, key := range []string{
		`HKLM\SOFTWARE\GitForWindows`,
		`HKCU\SOFTWARE\GitForWindows`,
		`HKLM\SOFTWARE\WOW6432Node\GitForWindows`,
	} {
		if p := bashFromRegistry(key); p != "" {
			return p, nil
		}
	}
	// 4) 常见安装路径兜底
	home, _ := os.UserHomeDir()
	roots := []string{
		`C:\Program Files\Git`,
		`C:\Program Files (x86)\Git`,
		filepath.Join(home, `AppData\Local\Programs\Git`),
	}
	for _, root := range roots {
		for _, rel := range []string{`bin\bash.exe`, `usr\bin\bash.exe`} {
			p := filepath.Join(root, rel)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && isGitBashExe(p) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("未找到可用的 Git Bash（内嵌包不可用且系统未安装）")
}

// bundledBashRoot 内嵌包的目标安装根目录（带版本号，升级后自然重新解包）。
func bundledBashRoot() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), filepath.FromSlash(bundledGitShareDir), bundledGitVersion)
}

// ensureBundledGitBash 确保内嵌 Git Bash 已解压并返回其 bash.exe 路径。
// 首次调用会写 SFX 到临时文件并静默解压（7z SFX 的 -o<目录> -y 参数），
// 耗时十几秒；main 里启动的 warmup goroutine 会提前触发，调用方在此等待。
func ensureBundledGitBash() (string, error) {
	root := bundledBashRoot()
	bash := filepath.Join(root, "usr", "bin", "bash.exe")
	if fi, err := os.Stat(bash); err == nil && !fi.IsDir() {
		return bash, nil
	}
	bundledMu.Lock()
	defer bundledMu.Unlock()
	// 双检：等待期间可能已被 warmup 解压完成
	if fi, err := os.Stat(bash); err == nil && !fi.IsDir() {
		return bash, nil
	}
	if len(bundledGitBashSFX) == 0 {
		return "", fmt.Errorf("内嵌 Git Bash 包缺失（构建时未打包 assets/gitbash/%s）", bundledGitSFXFile)
	}
	// 覆盖残留的半成品解压目录（上次中断会留下不完整树）
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return "", err
	}
	// SFX 必须落盘才能执行；放在目标根的同级，解压完即删
	sfx := filepath.Join(filepath.Dir(root), "portablegit-"+bundledGitVersion+".exe")
	if err := os.WriteFile(sfx, bundledGitBashSFX, 0o755); err != nil {
		return "", err
	}
	defer os.Remove(sfx)
	cmd := exec.Command(sfx, "-o"+root, "-y")
	hideConsole(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(root)
		return "", fmt.Errorf("内嵌 Git Bash 解压失败：%w（%s）", err, strings.TrimSpace(string(out)))
	}
	if fi, err := os.Stat(bash); err != nil || fi.IsDir() {
		_ = os.RemoveAll(root)
		return "", fmt.Errorf("内嵌 Git Bash 解压后缺少 bash.exe")
	}
	return bash, nil
}

// warmupBundledGitBash 主程序启动时后台预热解包，用户点「启动」时通常已就绪。
func warmupBundledGitBash() {
	_, _ = ensureBundledGitBash()
}

// isGitBashExe 验证 bash.exe 属于 Git for Windows：其所在目录向上 2 级内
// 能找到 git.exe（官方布局 root\bin、root\usr\bin 或 root\mingw64\bin）。
// WSL 的 System32\bash.exe 上游没有 git.exe，被此检查排除。
func isGitBashExe(bashExe string) bool {
	if fi, err := os.Stat(bashExe); err != nil || fi.IsDir() {
		return false
	}
	dir := filepath.Dir(filepath.Clean(bashExe))
	for i := 0; i < 3 && dir != ""; i++ {
		for _, rel := range []string{`bin\git.exe`, `cmd\git.exe`, `mingw64\bin\git.exe`, `ucrt64\bin\git.exe`} {
			if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
				return true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}

// bashFromRegistry 从 reg query 输出里取 InstallPath，并验证 bin 下有 bash。
func bashFromRegistry(key string) string {
	cmd := exec.Command("reg", "query", key, "/v", "InstallPath")
	hideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "InstallPath") {
			continue
		}
		i := strings.Index(line, "REG_SZ")
		if i < 0 {
			return ""
		}
		root := strings.TrimSpace(line[i+len("REG_SZ"):])
		for _, rel := range []string{`bin\bash.exe`, `usr\bin\bash.exe`} {
			p := filepath.Join(root, rel)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// bashEnv 组装 bash 子进程环境：完整继承父进程（MIMOCODE_BIN_PATH 等客户端
// 变量得以透传），再叠加——MSYSTEM 指定 mingw64 环境（/etc/profile 据此组装
// PATH）；CHERE_INVOKING 让 --login 不切回 HOME；bashRoot 非 nil 时把内嵌包
// 的 git 相关目录置于 PATH 最前（保证用的是随包分发的 git）。
func bashEnv(bashRoot string) []string {
	env := make([]string, 0, len(os.Environ())+5)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "MSYSTEM=") || strings.HasPrefix(kv, "CHERE_INVOKING=") || strings.HasPrefix(kv, "PATH=") {
			continue
		}
		env = append(env, kv)
	}
	path := os.Getenv("PATH")
	if bashRoot != "" {
		pre := []string{
			filepath.Join(bashRoot, "cmd"),
			filepath.Join(bashRoot, "usr", "bin"),
			filepath.Join(bashRoot, "mingw64", "bin"),
			filepath.Join(bashRoot, "ucrt64", "bin"),
		}
		var keep []string
		for _, p := range pre {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				keep = append(keep, p)
			}
		}
		path = strings.Join(append(keep, path), string(os.PathListSeparator))
	}
	return append(env,
		"MSYSTEM=MINGW64",
		"CHERE_INVOKING=1",
		"PATH="+path,
	)
}

// bashQuote 按 POSIX 单引号规则给 token 加引号（' → '\”）。bash 单引号内
// 除引号外一切字面，路径里的 & | 空格 %VAR% 等都安全。
func bashQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildBashCommand 把启动命令 token 组装成注入 bash 的命令行（不带换行）。
// 启动目录不进命令：spawn 直接以目标目录为 cwd，配合 CHERE_INVOKING 登录后
// 即在该目录。空 token 以 ” 占位保持参数位置。
func buildBashCommand(tokens []string) string {
	parts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		parts = append(parts, bashQuote(tok))
	}
	return strings.Join(parts, " ")
}
