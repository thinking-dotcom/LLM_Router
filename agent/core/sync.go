package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ---------- 通用文件操作 ----------

// muCfgFile 串行化 agent 包内所有「读-改-写同一配置文件」的操作：Wails 绑定
// 方法由前端异步调用，「保存配置」与「注入 MCP」可能并发读写同一文件，无锁时
// 后完成方会整体覆盖先完成方（丢失更新）。
var muCfgFile sync.Mutex

// utf8BOM 是 UTF-8 文件头 BOM（Windows 编辑器常见）；严格 JSON 解析器不接受，
// 各 MCP 合并/定点替换路径统一先剥除再解析。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func stripBOM(b []byte) []byte {
	return bytes.TrimPrefix(b, utf8BOM)
}

// normalizeJSONSave 校验 JSON 后生成落盘字节：按原文返回（仅剥 BOM、统一结尾
// 换行），不经 any 反序列化回写，保留用户的键序、数字精度与缩进；空内容写空对象。
// 顶层必须是 JSON 对象（各客户端主配置均为对象），数组/标量/null 视为非法，
// 避免把客户端解析不了的形状落盘。
func normalizeJSONSave(content string) ([]byte, string) {
	if strings.TrimSpace(content) == "" {
		return []byte("{}\n"), ""
	}
	payload := strings.TrimPrefix(content, "\uFEFF")
	if !json.Valid([]byte(payload)) {
		return nil, "JSON 格式错误，请检查后再保存"
	}
	var probe any
	if err := json.Unmarshal([]byte(payload), &probe); err != nil {
		return nil, "JSON 格式错误，请检查后再保存"
	}
	if _, ok := probe.(map[string]any); !ok {
		return nil, "JSON 顶层必须是对象"
	}
	if !strings.HasSuffix(payload, "\n") {
		payload += "\n"
	}
	return []byte(payload), ""
}

// getRealText 读取真实配置文件文本。真实文件不存在而预设模板（presetFile）
// 存在时，返回模板内容作为首次编辑的初始值（exists=false，filePath 始终为
// 真实路径）——模板只作初始值来源，不是编辑/同步桥梁。
func getRealText(realFile, presetFile string) (content string, exists bool, filePath string, errMsg string) {
	b, err := os.ReadFile(realFile)
	if err != nil {
		if os.IsNotExist(err) {
			if presetFile != "" {
				if pb, perr := os.ReadFile(presetFile); perr == nil && len(pb) > 0 {
					return string(pb), false, realFile, ""
				}
			}
			return "", false, realFile, ""
		}
		return "", false, realFile, err.Error()
	}
	return string(b), true, realFile, ""
}

// writeFileAtomic 原子写文件：先写同目录临时文件再 rename 覆盖，
// 避免崩溃/断电时 os.WriteFile 截断直写留下半截文件。
// Go 的 os.Rename 在 Windows 上可替换已存在目标；临时文件以 . 开头避免被误扫描。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// CreateTemp 固定 0600，rename 前改回调用方期望的权限位
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = "" // 已成功改名，defer 不再清理
	return nil
}

// copyFile 复制单个文件，自动创建目标目录。
// 权限沿用源文件（备份可能含密钥/会话数据，不能被放宽为 0644 世界可读）。
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(dst, data, sourcePerm(src))
}

// sourcePerm 取源文件当前权限位；不可用时回退 0600（宁可收紧不可放宽）。
func sourcePerm(src string) os.FileMode {
	if fi, err := os.Stat(src); err == nil {
		return fi.Mode().Perm()
	}
	return 0o600
}

// copyDirRecursive 递归复制 src 目录内容到 dst 目录（保留结构，覆盖同名）。
func copyDirRecursive(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// backupIfAbsent 若目标文件尚无备份则创建备份，用于保留编辑保存前的原始状态，
// 供手动回退（主配置保存时使用）。
func backupIfAbsent(path string) (backupPath string, _ error) {
	backupPath = path + ".bak"
	if _, err := os.Stat(backupPath); err == nil {
		return backupPath, nil // 已有备份，保留首次原始状态
	} else if !os.IsNotExist(err) {
		return backupPath, err // 权限/IO 错误不能继续，否则会覆盖首次备份
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return backupPath, nil // 目标文件本就不存在，无内容可备份
	}
	if err := copyFile(path, backupPath); err != nil {
		return backupPath, err
	}
	return backupPath, nil
}
