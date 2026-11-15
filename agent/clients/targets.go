package clients

// 本文件是「客户端适配器形态 + 注册」的归集处：
//
//   - 通用形态（FileMcpTarget / SkillsMcpTarget）与注册表在内核 agent/core；
//     只注入 skills 或 mcp 的客户端直接实例化内核形态即可。
//   - 需要专属注入算法的客户端（TOML 段落手术、Kimi mcp.json、agy 的
//     serverUrl 形态等）在本文件定义自己的形态类型。
//   - 每个客户端的注册集中在下方 init()，新增客户端 = 新增一个形态类型
//     （或在客户端文件里定义）+ 在此追加一段 RegisterTarget。
//
// 内核类型与 skill / mcp 数据模型见 llm-router-go/agent/core。

import (
	"llm-router-go/agent/core"
)

// ---------- 客户端专属适配器形态 ----------

// tomlTarget 表示编辑 TOML 主配置 + 注入 MCP 的目标（Grok Build / Codex）。
// 两者的 MCP 配置都在主配置 config.toml 的 [mcp_servers.*] 段，注入采用
// 文本级段落合并（injectTomlMcpServers），保留文件其余内容与注释。
type tomlTarget struct {
	id         string
	name       string
	apiFormat  string
	skillsDir  string // 用户级技能目录（~/.codex/skills 等）；空 = 不支持 skills 注入
	mcpFile    string // 真实 config.toml
	configFile string // 真实主配置路径（同 mcpFile）
	presetFile string // 首次初始值模板（仅真实文件不存在时用，非同步桥梁）
}

func (g *tomlTarget) Info() core.TargetInfo {
	return core.TargetInfo{ID: g.id, Name: g.name, APIFormat: g.apiFormat, ConfigFormat: "toml",
		SkillsSupported: g.skillsDir != "", SkillsDir: g.skillsDir,
		McpSupported: true, McpFile: g.mcpFile, ConfigFile: g.configFile, PresetFile: g.presetFile}
}
func (g *tomlTarget) InjectSkills(_ string, skills []core.Skill) error {
	if g.skillsDir == "" {
		return nil
	}
	return core.InjectSkills(g.skillsDir, skills)
}
func (g *tomlTarget) UndoSkills() error {
	if g.skillsDir == "" {
		return nil
	}
	return core.UndoSkills(g.skillsDir)
}
func (g *tomlTarget) InjectMcp(_ string, servers []core.MCPServer) error {
	return injectTomlMcpServers(g.mcpFile, servers)
}

// kimiTarget 表示 skills + mcp + 编辑配置的目标（Kimi Code）。
// skills 注入到 ~/.kimi-code/skills；MCP 声明在独立文件 ~/.kimi-code/mcp.json
// 顶层 mcpServers 键（Claude 风格：stdio 用 command/args，HTTP 用 url，
// SSE 用 transport + url），注入采用字节级定点合并（mergeKimiMcp）保留其余键；
// 主配置直接读写真实 config.toml（无模板中转，格式校验后原文直存），
// 模板仅作首次初始值。
type kimiTarget struct {
	id         string
	name       string
	apiFormat  string
	skillsDir  string
	mcpFile    string
	configFile string
	presetFile string
}

func (k *kimiTarget) Info() core.TargetInfo {
	return core.TargetInfo{ID: k.id, Name: k.name, APIFormat: k.apiFormat, ConfigFormat: "toml",
		SkillsSupported: true, McpSupported: true, SkillsDir: k.skillsDir, McpFile: k.mcpFile,
		ConfigFile: k.configFile, PresetFile: k.presetFile}
}
func (k *kimiTarget) InjectSkills(_ string, skills []core.Skill) error {
	return core.InjectSkills(k.skillsDir, skills)
}
func (k *kimiTarget) UndoSkills() error { return core.UndoSkills(k.skillsDir) }
func (k *kimiTarget) InjectMcp(_ string, servers []core.MCPServer) error {
	return mergeKimiMcp(k.mcpFile, servers)
}

// agyTarget 表示 Antigravity CLI：skills + mcp + 编辑配置。
// skills 注入全局目录 ~/.gemini/config/skills；MCP 声明在全局
// ~/.gemini/config/mcp_config.json 顶层 mcpServers 键（stdio 用 command/args，
// 远程用 serverUrl），注入采用字节级定点合并（mergeAgyMcp）保留其余键；
// 主配置直接读写真实 ~/.gemini/antigravity-cli/settings.json，模板仅作首次初始值。
type agyTarget struct {
	id         string
	name       string
	apiFormat  string
	skillsDir  string
	mcpFile    string
	configFile string
	presetFile string
}

func (a *agyTarget) Info() core.TargetInfo {
	return core.TargetInfo{ID: a.id, Name: a.name, APIFormat: a.apiFormat, ConfigFormat: "json",
		SkillsSupported: true, McpSupported: true, SkillsDir: a.skillsDir, McpFile: a.mcpFile,
		ConfigFile: a.configFile, PresetFile: a.presetFile}
}
func (a *agyTarget) InjectSkills(_ string, skills []core.Skill) error {
	return core.InjectSkills(a.skillsDir, skills)
}
func (a *agyTarget) UndoSkills() error { return core.UndoSkills(a.skillsDir) }
func (a *agyTarget) InjectMcp(_ string, servers []core.MCPServer) error {
	return mergeAgyMcp(a.mcpFile, servers)
}

// ---------- 已接入客户端注册 ----------

func init() {
	// Claude Code：skills + mcp。主配置直接读写真实文件 ~/.claude/settings.json（无模板中转），
	// 模板仅作首次初始值（真实文件不存在时返回模板内容作为编辑初始值）。
	core.RegisterTarget(&core.SkillsMcpTarget{
		ID: "claude-code", Name: "Claude Code", APIFormat: "/oai",
		SkillsDir:  core.ClientPath("claude-code", "skills"),
		McpFile:    claudeSettingsRealFile(),
		ConfigFile: claudeSettingsRealFile(),
		PresetFile: claudeCodeTemplateFile(),
	})

	// OpenCode：skills + mcp。主配置直接读写真实文件 ~/.config/opencode/opencode.json
	//（无模板中转），模板仅作首次初始值。
	core.RegisterTarget(&core.SkillsMcpTarget{
		ID: "opencode", Name: "OpenCode", APIFormat: "/oai",
		SkillsDir:  core.ClientPath("opencode", "skills"),
		McpFile:    opencodeRealFile(),
		ConfigFile: opencodeRealFile(),
		PresetFile: opencodeTemplateFile(),
	})

	// pi：skills + mcp + 编辑配置。skills 注入用户级目录 ~/.pi/agent/skills（pi
	// 新版默认路径，旧 ~/.pi/skills 已废弃）；MCP 注入走真实路径 ~/.pi/agent/mcp.json；
	// 主配置直接读写真实 models.json（无模板中转），模板仅作首次初始值。
	core.RegisterTarget(&core.SkillsMcpTarget{
		ID: "pi", Name: "Pi", APIFormat: "/oai",
		SkillsDir:  core.ClientPath("pi", "skills"),
		McpFile:    piMcpRealFile(),
		ConfigFile: piModelRealFile(),
		PresetFile: piModelTemplateFile(),
	})

	// Grok Build：skills + mcp + 编辑配置。skills 注入用户级目录 ~/.grok/skills；
	// MCP 配置与主配置同文件（~/.grok/config.toml 的 [mcp_servers.*] 段）；
	// 主配置直接读写真实 config.toml（无模板中转），模板仅作首次初始值。
	core.RegisterTarget(&tomlTarget{
		id: "grok-build", name: "Grok Build", apiFormat: "/oai",
		skillsDir:  core.ClientPath("grok-build", "skills"),
		mcpFile:    grokRealFile(),
		configFile: grokRealFile(),
		presetFile: grokTemplateFile(),
	})

	// Codex：skills + mcp + 编辑配置。skills 注入用户级目录 ~/.codex/skills；
	// MCP 配置与主配置同文件（~/.codex/config.toml 的 [mcp_servers.*] 段，
	// stdio 用 command/args、远程用 url）；主配置直接读写真实 config.toml
	//（无模板中转），模板仅作首次初始值。
	core.RegisterTarget(&tomlTarget{
		id: "codex", name: "Codex", apiFormat: "/oai",
		skillsDir:  core.ClientPath("codex", "skills"),
		mcpFile:    codexRealFile(),
		configFile: codexRealFile(),
		presetFile: codexTemplateFile(),
	})

	// Command Code：skills + mcp + 编辑配置。skills 注入用户级目录
	// ~/.commandcode/skills；MCP 注入走 user 作用域 ~/.commandcode/mcp.json
	//（Claude 风格 mcpServers 对象）；主配置直接读写真实 config.json（官方约定可改键：
	// provider / model / theme / compactMode / reasoningEffort 等，无模板中转），
	// 模板仅作首次初始值；settings.json（权限、推理深度）与 providers.json（BYOK
	// 模型端点）由前端经 Get/SaveCommandcodeSettings / Get/SaveCommandcodeProviders
	// 并入同一编辑页。
	core.RegisterTarget(&core.SkillsMcpTarget{
		ID: "commandcode", Name: "Command Code", APIFormat: "/oai", Format: "json",
		SkillsDir:  core.ClientPath("commandcode", "skills"),
		McpFile:    commandcodeMcpRealFile(),
		ConfigFile: commandcodeRealFile(),
		PresetFile: commandcodeTemplateFile(),
	})

	// Kimi Code：skills + mcp + 编辑配置。skills 注入用户级目录
	// ~/.kimi-code/skills；MCP 注入走独立声明文件 ~/.kimi-code/mcp.json（顶层
	// mcpServers 键，Claude 风格）；主配置直接读写真实 config.toml（无模板
	// 中转，TOML 格式校验后原文直存），模板仅作首次初始值；AGENTS.md
	//（~/.kimi-code/AGENTS.md，全局 Kimi 专属 Agent 指令）由前端经
	// Get/SaveKimiAgents 并入同一编辑页。
	core.RegisterTarget(&kimiTarget{
		id: "kimi", name: "Kimi Code", apiFormat: "/oai",
		skillsDir:  kimiSkillsDir(),
		mcpFile:    kimiMcpRealFile(),
		configFile: kimiRealFile(),
		presetFile: kimiTemplateFile(),
	})

	// Copilot CLI（GitHub Copilot）：mcp + 编辑配置 + 「配置选项」BYOK 面板。
	// MCP 注入走 ~/.copilot/mcp-config.json（Claude 风格 mcpServers 对象，与通用
	// MCP 合并注入兼容）；主配置编辑窗口指向 ~/.copilot/settings.json（model 与
	// 通用偏好，JSONC）。接入走官方 BYOK：端点三个环境变量（COPILOT_PROVIDER_
	// BASE_URL / COPILOT_PROVIDER_TYPE / COPILOT_PROVIDER_API_KEY）与模型环境变量
	// COPILOT_MODEL 由前端面板经 SetUserEnv / DelUserEnv 写入/清除（BYOK 校验只认
	// COPILOT_MODEL / COPILOT_PROVIDER_MODEL_ID / --model）；该面板另把 model 及
	// effortLevel / URL 白黑名单 / permissions.* / sandbox.* 等写入 settings.json
	// 供持久化与回显，不在技能/MCP 注入管线内。
	core.RegisterTarget(&core.FileMcpTarget{
		ID: "copilot", Name: "Copilot", APIFormat: "/oai",
		McpFile:    copilotMcpRealFile(),
		ConfigFile: copilotSettingsRealFile(),
	})

	// MimoCode（小米 MiMo Code）：skills + mcp + 编辑配置。skills 注入配置目录
	// ~/.config/mimocode/skills；MCP 声明在主配置 mimocode.jsonc 顶层 mcp 键
	//（local/remote + enabled，自定义合并 mergeMimoMcp）；主配置直接读写真实
	// mimocode.jsonc（JSONC 宽容，保存前校验），模板仅作首次初始值。
	core.RegisterTarget(&mimoTarget{
		id: "mimocode", name: "MimoCode", apiFormat: "/oai",
		skillsDir:  mimoSkillsDir(),
		mcpFile:    mimoMcpRealFile(),
		configFile: mimoRealFile(),
		presetFile: mimoTemplateFile(),
	})

	// Antigravity CLI（agy）：skills + mcp + 编辑配置。skills 注入全局目录
	// ~/.gemini/config/skills；MCP 声明在全局 mcp_config.json（~/.gemini/config/
	// mcp_config.json 顶层 mcpServers 键，Claude 风格 command/args + 远程 serverUrl，
	// 自定义合并 mergeAgyMcp）；主配置直接读写真实
	// ~/.gemini/antigravity-cli/settings.json（无模板中转，保存前 JSON 校验），
	// 模板仅作首次初始值。
	core.RegisterTarget(&agyTarget{
		id: "agy", name: "Antigravity", apiFormat: "/oai",
		skillsDir:  agySkillsDir(),
		mcpFile:    agyMcpRealFile(),
		configFile: agySettingsRealFile(),
		presetFile: agySettingsTemplateFile(),
	})

	// CodeBuddy CLI：mcp + 编辑配置。主配置直接读写真实 ~/.codebuddy/settings.json
	//（权限 / 模型 / 沙箱等官方设置，无模板中转），模板仅作首次初始值；
	// models.json（端点模型清单，url/apiKey 指向本路由器）与 mcp.json 由前端
	// 经 Get/SaveCodebuddyModels / Get/SaveCodebuddyMcp 并入同一编辑页。
	core.RegisterTarget(&core.FileMcpTarget{
		ID: "codebuddy", Name: "CodeBuddy", APIFormat: "/oai",
		McpFile:    codebuddyMcpRealFile(),
		ConfigFile: codebuddySettingsRealFile(),
		PresetFile: codebuddySettingsTemplateFile(),
	})

	// Deep Code CLI（@vegamo/deepcode-cli）：skills + mcp + 编辑配置。
	// skills 注入用户级 ~/.deepcode/skills；MCP 与主配置同文件（settings.json 顶层
	// mcpServers，CLI 只认 stdio 的 command/args），注入用 mergeDeepcodeMcp 定点合并、
	// 保留其余键；主配置直接读写真实 ~/.deepcode/settings.json（env.BASE_URL/API_KEY、
	// 顶层 model/thinkingEnabled/reasoningEffort），模板仅作首次初始值。
	core.RegisterTarget(&deepcodeTarget{
		id: "deepcode", name: "Deep Code", apiFormat: "/oai",
		skillsDir:  deepcodeSkillsDir(),
		mcpFile:    deepcodeMcpRealFile(),
		configFile: deepcodeRealFile(),
		presetFile: deepcodePresetFile(),
	})
}
