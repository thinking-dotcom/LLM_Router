package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"llm-router-go/types"
)

// stableProviderID 根据服务商特征派生确定性 ID（与 main.go 同名实现一致），
// 避免缺失 id 时每次 Normalize 都生成随机 id 导致配置/缓存漂移。
func stableProviderID(name, baseURL, ptype string) string {
	h := sha256.Sum256([]byte(name + "|" + baseURL + "|" + ptype))
	return "p-" + hex.EncodeToString(h[:])[:10]
}

func DefaultConfig() types.Config {
	return types.Config{
		Port:            3000,
		Host:            "127.0.0.1",
		AuthToken:       "",
		DefaultProvider: "",
		Providers:       []types.Provider{},
		ReasoningDepth:  "high",
	}
}

// reasoningDepths 合法的全局推理深度档位（config.reasoningDepth）。
var reasoningDepths = map[string]bool{
	"none":   true, // 不注入
	"low":    true,
	"medium": true,
	"high":   true,
	"max":    true,
}

var (
	configPathCache string
	configPathMu    sync.Mutex
)

func ConfigPath() string {
	configPathMu.Lock()
	defer configPathMu.Unlock()
	if configPathCache != "" {
		return configPathCache
	}
	if env := os.Getenv("LLMROUTER_CONFIG"); env != "" {
		configPathCache = env
		return env
	}
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		pkgPath := filepath.Join(dir, "package.json")
		if data, err := os.ReadFile(pkgPath); err == nil {
			var pkg struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(data, &pkg) == nil && pkg.Name == "llm-router" {
				configPathCache = filepath.Join(dir, "config.json")
				return configPathCache
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	configPathCache = "config.json"
	return configPathCache
}

func Normalize(raw any) types.Config {
	cfg := DefaultConfig()
	m, ok := raw.(map[string]any)
	if !ok {
		return cfg
	}
	if v, ok := m["port"].(float64); ok {
		cfg.Port = int(v)
	}
	if v, ok := m["host"].(string); ok && v != "" {
		cfg.Host = v
	}
	if v, ok := m["authToken"].(string); ok {
		cfg.AuthToken = v
	}
	if v, ok := m["defaultProvider"].(string); ok {
		cfg.DefaultProvider = v
	}
	if v, ok := m["tray"].(bool); ok {
		cfg.Tray = v
	}
	if v, ok := m["reasoningDepth"].(string); ok && reasoningDepths[v] {
		cfg.ReasoningDepth = v
	}
	cfg.Providers = normalizeProviders(m["providers"])
	return cfg
}

func normalizeProviders(raw any) []types.Provider {
	arr, ok := raw.([]any)
	if !ok {
		return []types.Provider{}
	}
	out := make([]types.Provider, 0, len(arr))
	for _, item := range arr {
		p, ok := item.(map[string]any)
		if !ok {
			continue
		}
		prov := types.Provider{}
		if name, ok := p["name"].(string); ok {
			prov.Name = name
		}
		if t, ok := p["type"].(string); ok && isValidProviderType(t) {
			prov.Type = types.ProviderType(t)
		} else {
			prov.Type = types.ProviderOpenAI
		}
		if bu, ok := p["baseUrl"].(string); ok {
			// 去除首尾空白：校验侧用 TrimSpace 判定合法，落库也应一致，
			// 否则带前导空格的地址会让出站 URL 拼接（^https?:// 前缀）失效
			prov.BaseURL = strings.TrimSpace(bu)
		}
		if id, ok := p["id"].(string); ok && id != "" {
			prov.ID = id
		} else {
			// 缺 id 时用确定性哈希 id，同一服务商多次 Normalize 结果一致
			prov.ID = stableProviderID(prov.Name, prov.BaseURL, string(prov.Type))
		}
		if key, ok := p["apiKey"].(string); ok {
			prov.APIKey = key
		}
		prov.Models = normalizeModels(p["models"])
		prov.Aliases = normalizeAliases(p["aliases"])
		if enabled, ok := p["enabled"].(bool); ok {
			prov.Enabled = enabled
		} else {
			prov.Enabled = true
		}
		out = append(out, prov)
	}
	return out
}

func normalizeModels(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func normalizeAliases(raw any) map[string]string {
	out := make(map[string]string)
	m, ok := raw.(map[string]any)
	if !ok {
		return out
	}
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func isValidProviderType(t string) bool {
	for _, pt := range types.ProviderTypes {
		if string(pt) == t {
			return true
		}
	}
	return false
}

func LoadConfig() types.Config {
	p := ConfigPath()
	data, err := os.ReadFile(p)
	if err != nil {
		cfg := DefaultConfig()
		saveConfig(cfg, p)
		return cfg
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		// 不要用空配置覆盖损坏文件——先备份原文件（.bak），保留用户手动修复的机会。
		if rerr := os.Rename(p, p+".bak"); rerr != nil {
			log.Printf("[CONFIG] 备份损坏配置文件失败: %v", rerr)
		}
		cfg := DefaultConfig()
		saveConfig(cfg, p)
		return cfg
	}
	return Normalize(raw)
}

func SaveConfig(cfg types.Config) error {
	return saveConfig(cfg, ConfigPath())
}

// saveSeq 进程内自增序号：临时文件名带唯一后缀，即使上层互斥失效（新增绕过
// Server.SaveConfig 的调用方），并发保存也不会交错写同一个固定 .tmp 路径。
var saveSeq atomic.Uint64

func saveConfig(cfg types.Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 原子写：先写临时文件再 rename，避免进程在写中途崩溃/断电留下半截文件
	// （半截文件下次启动会被误判为"损坏"并清空用户配置）。
	// 0600：config.json 含各上游 API Key，Unix 上防止其它本机用户可读（Windows 忽略权限位）。
	tmp := fmt.Sprintf("%s.tmp%d", path, saveSeq.Add(1))
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// rename 失败（极少见）时退化为直接写，至少不丢数据
		if werr := os.WriteFile(path, append(data, '\n'), 0o600); werr != nil {
			return werr
		}
	}
	return nil
}

var urlRegex = regexp.MustCompile(`^https?://`)

func ValidateConfig(raw any) string {
	m, ok := raw.(map[string]any)
	if !ok {
		return "配置必须是对象"
	}
	// 端口可选：未提供时由 Normalize 补默认值 3000；仅当显式给出且越界才报错。
	if p, ok := m["port"].(float64); ok {
		if pp := int(p); pp < 1 || pp > 65535 {
			return "端口必须是 1-65535 的整数"
		}
	}
	// host 必填：既用于绑定监听，也是 Normalize 无法安全推断的字段。
	host, _ := m["host"].(string)
	if host == "" {
		return "监听地址不能为空"
	}
	// 推理深度档位可选：未提供时由 Normalize 补默认值 high；仅当显式给出且非法才报错。
	if rd, ok := m["reasoningDepth"].(string); ok && rd != "" && !reasoningDepths[rd] {
		return "推理深度档位无效（合法值：none / low / medium / high / max）"
	}
	provs, _ := m["providers"].([]any)
	for i, item := range provs {
		p, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := p["name"].(string)
		if name == "" {
			return fmt.Sprintf("第 %d 个服务商缺少名称", i+1)
		}
		ptype, _ := p["type"].(string)
		if !isValidProviderType(ptype) {
			return fmt.Sprintf("服务商「%s」的 API 格式无效", name)
		}
		base, _ := p["baseUrl"].(string)
		if !urlRegex.MatchString(strings.TrimSpace(base)) {
			return fmt.Sprintf("服务商「%s」的 Base URL 必须以 http(s):// 开头", name)
		}
	}
	return ""
}
