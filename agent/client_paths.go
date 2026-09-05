package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 客户端真实配置路径集中管理：默认取内置 clientPathsDefaults()；若参考模板根目录
//（agent/Json_modles/，定位逻辑同 jsonModelsDir）下存在 client-paths.json，则按
// 键级合并覆盖。路径支持以 ~ 开头的主目录缩写，clientPath 取值时展开为绝对路径。

var (
	clientPathsOnce sync.Once
	clientPathsVal  map[string]map[string]string
)

// clientPathsDefaults 内置默认路径（与 Json_modles/client-paths.json 保持一致）。
func clientPathsDefaults() map[string]map[string]string {
	return map[string]map[string]string{
		"claude-code": {"config": "~/.claude/settings.json", "mcp": "~/.claude/settings.json", "skills": "~/.claude/skills", "md": "~/.claude/CLAUDE.md"},
		"opencode":    {"config": "~/.config/opencode/opencode.json", "agents": "~/.config/opencode/AGENTS.md", "skills": "~/.config/opencode/skills"},
		"gemini":      {"config": "~/.gemini/settings.json", "mcp": "~/.gemini/settings.json", "env": "~/.gemini/.env", "agents": "~/.gemini/GEMINI.md", "skills": "~/.gemini/skills"},
		"pi":          {"config": "~/.pi/agent/models.json", "settings": "~/.pi/agent/settings.json", "mcp": "~/.pi/agent/mcp.json", "agents": "~/.pi/agent/AGENTS.md", "skills": "~/.pi/skills"},
		"grok-build":  {"config": "~/.grok/config.toml", "mcp": "~/.grok/config.toml", "agents": "~/.grok/AGENTS.md", "skills": "~/.grok/skills"},
		"codex":       {"config": "~/.codex/config.toml", "mcp": "~/.codex/config.toml", "agents": "~/.codex/AGENTS.md", "skills": "~/.codex/skills"},
		"cline": {
			"config":      "~/.cline/data/settings/cline_mcp_settings.json",
			"models":      "~/.cline/data/settings/models.json",
			"providers":   "~/.cline/data/settings/providers.json",
			"agents":      "~/.agents/AGENTS.md",
			"globalstate": "~/.cline/data/globalState.json",
		},
		"hermes":  {"config": "~/.hermes/config.yaml", "mcp": "~/.hermes/config.yaml", "skills": "~/.hermes/skills"},
		"copilot": {"config": "~/.copilot/settings.json", "mcp": "~/.copilot/mcp-config.json", "permissions": "~/.copilot/permissions-config.json"},
		"commandcode": {
			"config":    "~/.commandcode/config.json",
			"settings":  "~/.commandcode/settings.json",
			"providers": "~/.commandcode/providers.json",
			"mcp":       "~/.commandcode/mcp.json",
			"skills":    "~/.commandcode/skills",
		},
		"kimi": {
			"config": "~/.kimi-code/config.toml",
			"mcp":    "~/.kimi-code/mcp.json",
			"agents": "~/.kimi-code/AGENTS.md",
			"skills": "~/.kimi-code/skills",
		},
		"kilo": {
			"config": "~/.config/kilo/kilo.jsonc",
			"mcp":    "~/.config/kilo/kilo.jsonc",
			"agents": "~/.config/kilo/AGENTS.md",
			"skills": "~/.config/kilo/skills",
		},
		"agy": {
			"config": "~/.gemini/antigravity-cli/settings.json",
			"mcp":    "~/.gemini/config/mcp_config.json",
			"skills": "~/.gemini/config/skills",
		},
	}
}

func loadClientPaths() {
	clientPathsOnce.Do(func() {
		clientPathsVal = clientPathsDefaults()
		b, err := os.ReadFile(filepath.Join(jsonModelsDir(""), "client-paths.json"))
		if err != nil || len(b) == 0 {
			return // 清单缺失时用内置默认
		}
		var ext map[string]json.RawMessage
		if json.Unmarshal(b, &ext) != nil {
			return // 清单损坏时用内置默认
		}
		for id, raw := range ext {
			// 非对象条目（如顶层 _comment 说明文本）跳过，不因单个异常键废弃整个清单
			if strings.HasPrefix(id, "_") {
				continue
			}
			var kv map[string]string
			if json.Unmarshal(raw, &kv) != nil {
				continue
			}
			if len(kv) == 0 {
				delete(clientPathsVal, id) // 空对象表示移除该客户端条目
				continue
			}
			m := clientPathsVal[id]
			if m == nil {
				m = map[string]string{}
			}
			for k, v := range kv {
				if strings.TrimSpace(v) == "" {
					delete(m, k) // 空值表示移除该项
					continue
				}
				m[k] = v
			}
			clientPathsVal[id] = m
		}
	})
}

// expandHome 把 ~/ 或 ~\ 开头的主目录缩写展开为绝对路径。
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home := HomeDir()
	if home == "" {
		return p
	}
	return filepath.Join(home, p[1:])
}

// clientPath 返回指定客户端某项用途的真实配置路径（~ 已展开）；未配置返回空串。
func clientPath(id, key string) string {
	loadClientPaths()
	p := clientPathsVal[id][key]
	if p == "" {
		return ""
	}
	return expandHome(p)
}

// ClientRealPaths 返回全部客户端真实路径清单的深拷贝（~ 已展开），供绑定层输出给前端。
func ClientRealPaths() map[string]map[string]string {
	loadClientPaths()
	out := make(map[string]map[string]string, len(clientPathsVal))
	for id, kv := range clientPathsVal {
		m := make(map[string]string, len(kv))
		for k, v := range kv {
			m[k] = expandHome(v)
		}
		out[id] = m
	}
	return out
}
