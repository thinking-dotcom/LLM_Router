package main

import "strings"

// 系统终端启动的命令行组装（与平台无关，便于单测）。
//
// 结构：cmd /k title <title> & cd /d "<cwd>" & <innerCmdline>
// title / cwd 可缺省；三段用 & 串联，保证切目录成功后再执行真正的命令。

// buildTermCmdLine 返回传给 cmd.exe 的参数（含前导 /k）。inner 为空时返回空串，
// 由调用方当作错误处理（不启动空窗口）。
func buildTermCmdLine(cwd, innerCmdline, title string) string {
	inner := strings.TrimSpace(innerCmdline)
	if inner == "" {
		return ""
	}
	parts := make([]string, 0, 3)
	if t := sanitizeTermTitle(title); t != "" {
		parts = append(parts, "title "+t)
	}
	if cwd = strings.TrimSpace(cwd); cwd != "" {
		parts = append(parts, `cd /d "`+strings.TrimRight(cwd, `\`)+`"`)
	}
	parts = append(parts, inner)
	return "/k " + strings.Join(parts, " & ")
}

// sanitizeTermTitle 清理窗口标题里会破坏 cmd 命令行的字符（& | < > ^ " 与换行）。
func sanitizeTermTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	repl := strings.NewReplacer("&", " ", "|", " ", "<", " ", ">", " ", "^", " ", `"`, " ", "\r", " ", "\n", " ")
	s = repl.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
