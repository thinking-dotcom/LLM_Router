//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SystemTerminal 供前端展示并选择的系统终端。
type SystemTerminal struct {
	ID   string `json:"id"`   // 稳定标识：cmd / powershell / pwsh / wt / gitbash
	Name string `json:"name"` // 展示名
}

// systemTerminalSpec 内部使用的终端定义（含可执行文件路径）。
type systemTerminalSpec struct {
	SystemTerminal
	Exe  string
	Kind string // cmd | powershell | wt | bash
}

// fileExists 判断文件存在且不是目录。
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// lookTerminalExe 先按 PATH 查，再按给定绝对路径查。
func lookTerminalExe(name, abs string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if abs != "" && fileExists(abs) {
		return abs
	}
	return ""
}

// detectSystemTerminals 从 PATH 与常见系统环境变量（ComSpec/SystemRoot/
// ProgramFiles/LOCALAPPDATA）自动检测可用终端，顺序即展示优先级。
func detectSystemTerminals() []systemTerminalSpec {
	local := os.Getenv("LOCALAPPDATA")
	programFiles := os.Getenv("ProgramFiles")
	sysRoot := os.Getenv("SystemRoot")
	candidates := []systemTerminalSpec{
		{SystemTerminal: SystemTerminal{"wt", "Windows Terminal"}, Exe: lookTerminalExe("wt.exe", filepath.Join(local, `Microsoft\WindowsApps\wt.exe`)), Kind: "wt"},
		{SystemTerminal: SystemTerminal{"pwsh", "PowerShell 7"}, Exe: lookTerminalExe("pwsh.exe", filepath.Join(programFiles, `PowerShell\7\pwsh.exe`)), Kind: "powershell"},
		{SystemTerminal: SystemTerminal{"powershell", "Windows PowerShell"}, Exe: lookTerminalExe("powershell.exe", filepath.Join(sysRoot, `System32\WindowsPowerShell\v1.0\powershell.exe`)), Kind: "powershell"},
		{SystemTerminal: SystemTerminal{"cmd", "命令提示符"}, Exe: lookTerminalExe("cmd.exe", os.Getenv("ComSpec")), Kind: "cmd"},
	}
	if p, err := exec.LookPath("bash.exe"); err == nil && isGitBashExe(p) {
		candidates = append(candidates, systemTerminalSpec{SystemTerminal: SystemTerminal{"gitbash", "Git Bash"}, Exe: p, Kind: "bash"})
	}
	out := make([]systemTerminalSpec, 0, len(candidates))
	for _, t := range candidates {
		if t.Exe != "" {
			out = append(out, t)
		}
	}
	return out
}

// findSystemTerminal 按 ID 找可用终端。
func findSystemTerminal(id string) (systemTerminalSpec, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, t := range detectSystemTerminals() {
		if t.ID == id {
			return t, true
		}
	}
	return systemTerminalSpec{}, false
}

// psQuote PowerShell 单引号字符串转义。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// psLaunchCommand 生成在指定目录执行 argv 的 PowerShell 命令。
func psLaunchCommand(cwd string, tokens []string) string {
	args := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		args = append(args, psQuote(tok))
	}
	parts := []string{"Set-Location -LiteralPath " + psQuote(cwd)}
	if len(args) > 0 {
		parts = append(parts, "& "+strings.Join(args, " "))
	}
	return strings.Join(parts, "; ")
}

// buildSystemTermParams 按终端类型组装启动参数。
func buildSystemTermParams(t systemTerminalSpec, cwd, title string, tokens []string) string {
	switch t.Kind {
	case "cmd":
		return buildTermCmdLine(cwd, winCmdline(tokens), title)
	case "powershell":
		return "-NoExit -Command " + winCmdline([]string{psLaunchCommand(cwd, tokens)})
	case "wt":
		// wt -d "<cwd>" --title "<title>" powershell -NoExit -Command "<cmd>"
		inner := []string{"powershell", "-NoExit", "-Command", psLaunchCommand(cwd, tokens)}
		args := append([]string{"-d", cwd, "--title", sanitizeTermTitle(title)}, inner...)
		return winCmdline(args)
	case "bash":
		// Git Bash 用 POSIX 路径；末尾 exec bash 保证命令结束后窗口保留
		posixCwd := strings.ReplaceAll(cwd, `\`, "/")
		args := make([]string, 0, len(tokens))
		for _, tok := range tokens {
			args = append(args, bashQuote(tok))
		}
		script := "cd " + bashQuote(posixCwd) + " && " + strings.Join(args, " ") + " ; exec bash"
		return "-c " + winCmdline([]string{script})
	}
	return ""
}

// launchInSystemTerminal 用指定系统终端执行命令（ShellExecute runas，UAC 同旧路径）。
func launchInSystemTerminal(t systemTerminalSpec, cwd, title string, tokens []string) error {
	if len(tokens) == 0 || strings.TrimSpace(tokens[0]) == "" {
		return fmt.Errorf("空命令，无法启动终端")
	}
	params := buildSystemTermParams(t, cwd, title, tokens)
	if params == "" {
		return fmt.Errorf("终端 %s 不支持启动命令", t.Name)
	}
	return shellExecute(t.Exe, "runas", params, cwd)
}

// launchInDefaultTerminal 默认用列表中的第一个终端（会话恢复等旧调用保持可用）。
func launchInDefaultTerminal(cwd string, tokens []string, title string) error {
	all := detectSystemTerminals()
	if len(all) == 0 {
		return fmt.Errorf("未检测到可用的系统终端")
	}
	return launchInSystemTerminal(all[0], cwd, title, tokens)
}
