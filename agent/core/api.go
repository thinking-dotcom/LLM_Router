package core

// 本文件是 agent/core 对 agent/clients 的导出层。
//
// 设计约定：内核内部实现一律保持包内小写命名（不为了跨包调用把每个工具函数
// 都改成大写，避免污染内核可读性）；只有「客户端适配器确实需要」的少量原语
// 在这里统一导出。agent/clients 只依赖本文件导出的原语、本包的类型
// （Skill / MCPServer / TargetInfo / TargetAdapter / RegisterTarget）以及路径、
// 存储、会话、安装等既有导出函数，不直接触碰内核内部细节。

import (
	"encoding/json"
	"os"
)

// ---------- 路径 ----------

// ClientPath 返回指定客户端某项用途的真实配置路径（~ 已展开）；未配置返回空串。
// key 取值见 client-paths.json（config / mcp / skills / agents / settings / …）。
func ClientPath(id, key string) string { return clientPath(id, key) }

// JSONModelsDir 返回参考模版根目录 agent/Json_modles/<sub>。
// 优先级：LLMROUTER_JSON_MODLES_DIR 环境变量 > 从当前工作目录逐级向上查找 > 可执行文件同目录。
func JSONModelsDir(sub string) string { return jsonModelsDir(sub) }

// ---------- 文件读写 ----------

// LockConfigFile / UnlockConfigFile 串行化「读-改-写同一个配置文件」的操作。
// 保存配置与 MCP 注入可能被前端并发触发，无锁时后完成方会整体覆盖先完成方。
// 用法与 sync.Mutex 一致：LockConfigFile(); defer UnlockConfigFile()。
func LockConfigFile() { muCfgFile.Lock() }

// UnlockConfigFile 释放配置文件读-改-写锁（见 LockConfigFile）。
func UnlockConfigFile() { muCfgFile.Unlock() }

// GetRealText 读取真实配置文件文本；真实文件不存在而预设模板存在时，
// 返回模板内容作为首次编辑初始值（exists=false，filePath 始终为真实路径）。
func GetRealText(realFile, presetFile string) (content string, exists bool, filePath string, errMsg string) {
	return getRealText(realFile, presetFile)
}

// NormalizeJSONSave 校验 JSON 后生成落盘字节（保留键序/精度/缩进，空内容写空对象）。
func NormalizeJSONSave(content string) ([]byte, string) { return normalizeJSONSave(content) }

// WriteFileAtomic 原子写文件（同目录临时文件 + rename），按调用方指定权限位落盘。
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return writeFileAtomic(path, data, perm)
}

// BackupIfAbsent 若目标文件尚无 <file>.bak 备份则创建一份（保留最初状态供手动回退）。
func BackupIfAbsent(path string) (string, error) { return backupIfAbsent(path) }

// StripBOM 剥除 UTF-8 文件头 BOM（严格 JSON 解析器不接受）。
func StripBOM(b []byte) []byte { return stripBOM(b) }

// ---------- JSON / JSONC 定点编辑 ----------

// UpsertJSONObjectKey 在顶层 JSON 对象中替换 key 的值，其余字节原样保留。
func UpsertJSONObjectKey(doc []byte, key string, fn func(existing json.RawMessage) (json.RawMessage, error)) ([]byte, error) {
	return upsertJSONObjectKey(doc, key, fn)
}

// DeleteJSONObjectKey 删除顶层 JSON 对象中的指定键（含其值），返回 (新文档, 是否删除, 错误)。
func DeleteJSONObjectKey(doc []byte, key string) ([]byte, bool, error) {
	return deleteJSONObjectKey(doc, key)
}

// JSONCToJSON 把 JSONC 文本剥离注释与字符串外尾逗号，返回严格 JSON 字节。
func JSONCToJSON(doc []byte) ([]byte, error) { return jsoncToJSON(doc) }

// JSONStringLiteralLen 返回 doc 中以 end 为结束位置的 JSON 字符串字面量的引号内字节长度。
func JSONStringLiteralLen(doc []byte, end int) (int, error) { return jsonStringLiteralLen(doc, end) }

// ---------- 注入原语 ----------

// InjectSkills 是注入 skills 的通用实现：把每个技能复制到目标 skills 目录，
// 并记录注入清单（撤销时只删本程序注入过的副本）。
func InjectSkills(skillsDir string, skills []Skill) error { return injectSkills(skillsDir, skills) }

// UndoSkills 是撤销 skills 的通用实现：按注入清单删除目标目录下的技能副本。
func UndoSkills(skillsDir string) error { return undoSkills(skillsDir) }

// FoldTomlInlineTables 把 TOML 内联表 {...} 内部的换行折叠为空格（严格 TOML 不允许内联表跨行）。
func FoldTomlInlineTables(content string) string { return foldTomlInlineTables(content) }

// ---------- 注册表（测试自检用） ----------

// TargetOf 按 id 取已注册的目标适配器；未注册返回 ok=false。
// 供 agent/clients 的注册自检测试使用，业务代码请走 ListTargets / SyncTargets。
func TargetOf(id string) (TargetAdapter, bool) {
	a, ok := registry[id]
	return a, ok
}

// UnregisterTarget 从注册表移除一个目标（仅测试清理临时注册时使用）。
func UnregisterTarget(id string) { delete(registry, id) }
