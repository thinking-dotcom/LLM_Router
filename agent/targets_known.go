package agent

// ---------- 通用适配器构造 ----------

// editTarget 仅提供「编辑配置」的客户端：主配置文件路径已知、但 skills/mcp 注入细节
// 尚未确认（或无需注入）。注入相关方法为空操作，编辑走 ConfigFile。
type editTarget struct {
	id         string
	name       string
	apiFormat  string
	configFile string
	format     string // json / toml
}

func (e *editTarget) Info() TargetInfo {
	return TargetInfo{ID: e.id, Name: e.name, APIFormat: e.apiFormat, ConfigFormat: e.format, ConfigFile: e.configFile}
}
func (e *editTarget) InjectSkills(string, []Skill) error  { return nil }
func (e *editTarget) UndoSkills() error                   { return nil }
func (e *editTarget) InjectMcp(string, []MCPServer) error { return nil }

// dirTarget 表示一个只注入 skills 的目标。
type dirTarget struct {
	id          string
	name        string
	apiFormat   string
	skillsDir   string
	configFile  string
	skillsFiles []string // 保留字段：当前未被注入逻辑使用，注册时一律留空
}

func (d *dirTarget) Info() TargetInfo {
	return TargetInfo{ID: d.id, Name: d.name, APIFormat: d.apiFormat, SkillsSupported: true, SkillsDir: d.skillsDir, ConfigFile: d.configFile}
}
func (d *dirTarget) InjectSkills(_ string, skills []Skill) error {
	return injectSkills(d.skillsDir, skills)
}
func (d *dirTarget) UndoSkills() error                       { return undoSkills(d.skillsDir) }
func (d *dirTarget) InjectMcp(_ string, _ []MCPServer) error { return nil }

// fileMcpTarget 表示只注入 mcp 的目标。
type fileMcpTarget struct {
	id         string
	name       string
	apiFormat  string
	mcpFile    string
	configFile string // 真实主配置路径（空则回退 mcpFile）
	presetFile string // 首次初始值模板（仅真实文件不存在时用，非同步桥梁）
	format     string // 主配置格式（json 等），空表示默认
}

func (f *fileMcpTarget) Info() TargetInfo {
	cf := f.configFile
	if cf == "" {
		cf = f.mcpFile
	}
	return TargetInfo{ID: f.id, Name: f.name, APIFormat: f.apiFormat, ConfigFormat: f.format,
		McpSupported: true, McpFile: f.mcpFile, ConfigFile: cf, PresetFile: f.presetFile}
}
func (f *fileMcpTarget) InjectSkills(_ string, _ []Skill) error { return nil }
func (f *fileMcpTarget) UndoSkills() error                      { return nil }
func (f *fileMcpTarget) InjectMcp(_ string, servers []MCPServer) error {
	return injectMcp(f.mcpFile, "", servers)
}

// skillsMcpTarget 表示 skills 与 mcp 都注入的目标。
type skillsMcpTarget struct {
	id         string
	name       string
	apiFormat  string
	skillsDir  string
	mcpFile    string
	configFile string // 真实主配置路径（空则回退 mcpFile）
	presetFile string // 首次初始值模板（仅真实文件不存在时用，非同步桥梁）
}

func (s *skillsMcpTarget) Info() TargetInfo {
	cf := s.configFile
	if cf == "" {
		cf = s.mcpFile
	}
	return TargetInfo{ID: s.id, Name: s.name, APIFormat: s.apiFormat,
		SkillsSupported: true, McpSupported: true, SkillsDir: s.skillsDir, McpFile: s.mcpFile, ConfigFile: cf, PresetFile: s.presetFile}
}
func (s *skillsMcpTarget) InjectSkills(_ string, skills []Skill) error {
	return injectSkills(s.skillsDir, skills)
}
func (s *skillsMcpTarget) UndoSkills() error { return undoSkills(s.skillsDir) }
func (s *skillsMcpTarget) InjectMcp(_ string, servers []MCPServer) error {
	return injectMcp(s.mcpFile, "", servers)
}

// tomlTarget 表示编辑 TOML 主配置 + 注入 MCP 的目标（Grok Build / Codex）。
// 两者的 MCP 配置都在主配置 config.toml 的 [mcp_servers.*] 段，注入采用
// 文本级段落合并（injectTomlMcpServers），保留文件其余内容与注释。
type tomlTarget struct {
	id         string
	name       string
	apiFormat  string
	mcpFile    string // 真实 config.toml
	configFile string // 真实主配置路径（同 mcpFile）
	presetFile string // 首次初始值模板（仅真实文件不存在时用，非同步桥梁）
}

func (g *tomlTarget) Info() TargetInfo {
	return TargetInfo{ID: g.id, Name: g.name, APIFormat: g.apiFormat, ConfigFormat: "toml",
		McpSupported: true, McpFile: g.mcpFile, ConfigFile: g.configFile, PresetFile: g.presetFile}
}
func (g *tomlTarget) InjectSkills(string, []Skill) error { return nil }
func (g *tomlTarget) UndoSkills() error                  { return nil }
func (g *tomlTarget) InjectMcp(_ string, servers []MCPServer) error {
	return injectTomlMcpServers(g.mcpFile, servers)
}

// hermesTarget 表示编辑 YAML 主配置 + 注入 MCP 的目标（Hermes Agent）。
// MCP 配置在主配置 config.yaml 顶层的 mcp_servers 键下，注入采用
// 文本级段落合并（injectYamlMcpServers），保留文件其余内容与注释。
type hermesTarget struct {
	id         string
	name       string
	apiFormat  string
	mcpFile    string // 真实 config.yaml
	configFile string // 真实主配置路径（同 mcpFile）
	presetFile string // 首次初始值模板（仅真实文件不存在时用，非同步桥梁）
}

func (h *hermesTarget) Info() TargetInfo {
	return TargetInfo{ID: h.id, Name: h.name, APIFormat: h.apiFormat, ConfigFormat: "yaml",
		McpSupported: true, McpFile: h.mcpFile, ConfigFile: h.configFile, PresetFile: h.presetFile}
}
func (h *hermesTarget) InjectSkills(string, []Skill) error { return nil }
func (h *hermesTarget) UndoSkills() error                  { return nil }
func (h *hermesTarget) InjectMcp(_ string, servers []MCPServer) error {
	return injectYamlMcpServers(h.mcpFile, servers)
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

func (k *kimiTarget) Info() TargetInfo {
	return TargetInfo{ID: k.id, Name: k.name, APIFormat: k.apiFormat, ConfigFormat: "toml",
		SkillsSupported: true, McpSupported: true, SkillsDir: k.skillsDir, McpFile: k.mcpFile,
		ConfigFile: k.configFile, PresetFile: k.presetFile}
}
func (k *kimiTarget) InjectSkills(_ string, skills []Skill) error {
	return injectSkills(k.skillsDir, skills)
}
func (k *kimiTarget) UndoSkills() error { return undoSkills(k.skillsDir) }
func (k *kimiTarget) InjectMcp(_ string, servers []MCPServer) error {
	return mergeKimiMcp(k.mcpFile, servers)
}

// geminiTarget 表示编辑配置 + 注入 MCP 的目标（Gemini CLI）。
// MCP 配置在主配置 settings.json 顶层的 mcpServers 键（Claude 风格；
// stdio 用 command/args，SSE 用 url，HTTP 用 httpUrl），整体 JSON 重排写回。
type geminiTarget struct {
	id         string
	name       string
	apiFormat  string
	mcpFile    string // 真实 settings.json
	configFile string // 真实主配置路径（同 mcpFile）
	presetFile string // 首次初始值模板（仅真实文件不存在时用，非同步桥梁）
}

func (g *geminiTarget) Info() TargetInfo {
	return TargetInfo{ID: g.id, Name: g.name, APIFormat: g.apiFormat, ConfigFormat: "json",
		McpSupported: true, McpFile: g.mcpFile, ConfigFile: g.configFile, PresetFile: g.presetFile}
}
func (g *geminiTarget) InjectSkills(string, []Skill) error { return nil }
func (g *geminiTarget) UndoSkills() error                  { return nil }
func (g *geminiTarget) InjectMcp(_ string, servers []MCPServer) error {
	return mergeGeminiMcp(g.mcpFile, servers)
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

func (a *agyTarget) Info() TargetInfo {
	return TargetInfo{ID: a.id, Name: a.name, APIFormat: a.apiFormat, ConfigFormat: "json",
		SkillsSupported: true, McpSupported: true, SkillsDir: a.skillsDir, McpFile: a.mcpFile,
		ConfigFile: a.configFile, PresetFile: a.presetFile}
}
func (a *agyTarget) InjectSkills(_ string, skills []Skill) error {
	return injectSkills(a.skillsDir, skills)
}
func (a *agyTarget) UndoSkills() error { return undoSkills(a.skillsDir) }
func (a *agyTarget) InjectMcp(_ string, servers []MCPServer) error {
	return mergeAgyMcp(a.mcpFile, servers)
}

// ---------- 已接入客户端注册 ----------

func init() {
	// Claude Code：skills + mcp。主配置直接读写真实文件 ~/.claude/settings.json（无模板中转），
	// 模板仅作首次初始值（真实文件不存在时返回模板内容作为编辑初始值）。
	RegisterTarget(&skillsMcpTarget{
		id: "claude-code", name: "Claude Code", apiFormat: "/oai",
		skillsDir:  clientPath("claude-code", "skills"),
		mcpFile:    claudeSettingsRealFile(),
		configFile: claudeSettingsRealFile(),
		presetFile: claudeCodeTemplateFile(),
	})

	// Claude Desktop：仅 mcp（~/.claude.json）。该文件是含会话状态的活文件，
	// 不做模板化部署：不设 configFile，编辑窗口直接读写真实文件本身（读取回退 mcpFile）。
	RegisterTarget(&fileMcpTarget{
		id: "claude-desktop", name: "Claude Desktop", apiFormat: "/oai",
		mcpFile: clientPath("claude-desktop", "config"),
	})

	// OpenCode：skills + mcp。主配置直接读写真实文件 ~/.config/opencode/opencode.json
	//（无模板中转），模板仅作首次初始值。
	RegisterTarget(&skillsMcpTarget{
		id: "opencode", name: "OpenCode", apiFormat: "/oai",
		skillsDir:  clientPath("opencode", "skills"),
		mcpFile:    opencodeRealFile(),
		configFile: opencodeRealFile(),
		presetFile: opencodeTemplateFile(),
	})

	// Gemini CLI：mcp + 编辑配置。MCP 注入走真实路径 ~/.gemini/settings.json
	// 的顶层 mcpServers 键；主配置直接读写真实 settings.json（无模板中转），
	// 模板仅作首次初始值。
	RegisterTarget(&geminiTarget{
		id: "gemini", name: "Gemini CLI", apiFormat: "/gai",
		mcpFile:    geminiSettingsRealFile(),
		configFile: geminiSettingsRealFile(),
		presetFile: geminiSettingsTemplateFile(),
	})

	// pi：mcp + 编辑配置。MCP 注入走真实路径 ~/.pi/agent/mcp.json；主配置直接读写
	// 真实 models.json（无模板中转），模板仅作首次初始值。
	RegisterTarget(&fileMcpTarget{
		id: "pi", name: "pi", apiFormat: "/oai",
		mcpFile:    piMcpRealFile(),
		configFile: piModelRealFile(),
		presetFile: piModelTemplateFile(),
	})

	// Grok Build：mcp + 编辑配置。MCP 配置与主配置同文件（~/.grok/config.toml 的
	// [mcp_servers.*] 段）；主配置直接读写真实 config.toml（无模板中转），模板仅作首次初始值。
	RegisterTarget(&tomlTarget{
		id: "grok-build", name: "Grok Build", apiFormat: "/oai",
		mcpFile:    grokRealFile(),
		configFile: grokRealFile(),
		presetFile: grokTemplateFile(),
	})

	// Codex：mcp + 编辑配置。MCP 配置与主配置同文件（~/.codex/config.toml 的
	// [mcp_servers.*] 段，stdio 用 command/args、远程用 url）；主配置直接读写
	// 真实 config.toml（无模板中转），模板仅作首次初始值。
	RegisterTarget(&tomlTarget{
		id: "codex", name: "Codex", apiFormat: "/oai",
		mcpFile:    codexRealFile(),
		configFile: codexRealFile(),
		presetFile: codexTemplateFile(),
	})

	// Cline：仅 mcp（cline_mcp_settings.json，默认独立版桌面端路径
	// ~/.cline/data/settings/，可用 CLINE_MCP_SETTINGS 环境变量覆盖为 VSCode
	// 扩展版的 globalStorage 路径）。主配置直接读写真实文件（无模板中转），模板仅作首次初始值。
	RegisterTarget(&fileMcpTarget{
		id: "cline", name: "Cline", apiFormat: "/oai",
		mcpFile:    clineMcpRealFile(),
		configFile: clineMcpRealFile(),
		presetFile: clineTemplateFile(),
	})

	// Hermes：mcp + 编辑配置。MCP 配置与主配置同文件（~/.hermes/config.yaml 顶层
	// mcp_servers 键）；主配置直接读写真实 config.yaml（无模板中转，YAML 校验通过后
	// 原文直存），模板仅作首次初始值。
	RegisterTarget(&hermesTarget{
		id: "hermes", name: "Hermes", apiFormat: "/oai",
		mcpFile:    hermesRealFile(),
		configFile: hermesRealFile(),
		presetFile: hermesTemplateFile(),
	})

	// Command Code：mcp + 编辑配置。MCP 注入走 user 作用域 ~/.commandcode/mcp.json
	//（Claude 风格 mcpServers 对象）；主配置直接读写真实 config.json（官方约定可改键：
	// provider / model / theme / compactMode / reasoningEffort 等，无模板中转），
	// 模板仅作首次初始值；settings.json（权限、推理深度）与 providers.json（BYOK
	// 模型端点）由前端经 Get/SaveCommandcodeSettings / Get/SaveCommandcodeProviders
	// 并入同一编辑页。
	RegisterTarget(&fileMcpTarget{
		id: "commandcode", name: "Command Code", apiFormat: "/oai", format: "json",
		mcpFile:    commandcodeMcpRealFile(),
		configFile: commandcodeRealFile(),
		presetFile: commandcodeTemplateFile(),
	})

	// Kimi Code：skills + mcp + 编辑配置。skills 注入用户级目录
	// ~/.kimi-code/skills；MCP 注入走独立声明文件 ~/.kimi-code/mcp.json（顶层
	// mcpServers 键，Claude 风格）；主配置直接读写真实 config.toml（无模板
	// 中转，TOML 格式校验后原文直存），模板仅作首次初始值；AGENTS.md
	//（~/.kimi-code/AGENTS.md，全局 Kimi 专属 Agent 指令）由前端经
	// Get/SaveKimiAgents 并入同一编辑页。
	RegisterTarget(&kimiTarget{
		id: "kimi", name: "Kimi Code", apiFormat: "/oai",
		skillsDir:  kimiSkillsDir(),
		mcpFile:    kimiMcpRealFile(),
		configFile: kimiRealFile(),
		presetFile: kimiTemplateFile(),
	})

	// Copilot CLI（GitHub Copilot）：mcp + 编辑配置 + 环境变量 BYOK 面板。
	// MCP 注入走 ~/.copilot/mcp-config.json（Claude 风格 mcpServers 对象，与通用
	// MCP 合并注入兼容）；主配置编辑窗口指向 ~/.copilot/settings.json（model 等
	// 用户设置，JSONC）。接入走官方 BYOK：端点三个环境变量（COPILOT_PROVIDER_
	// BASE_URL / COPILOT_PROVIDER_TYPE / COPILOT_PROVIDER_API_KEY）由前端面板经
	// SetUserEnv / DelUserEnv 写入/清除；模型经 Get/SaveCopilotModel 读写
	// settings.json 的 model 键（等价持久化 COPILOT_MODEL），不在注入管线内。
	RegisterTarget(&fileMcpTarget{
		id: "copilot", name: "Copilot CLI", apiFormat: "/oai",
		mcpFile:    copilotMcpRealFile(),
		configFile: copilotSettingsRealFile(),
	})

	// Kilo：skills + mcp + 编辑配置。skills 注入配置目录 ~/.config/kilo/skills；
	// MCP 声明在主配置 kilo.jsonc 顶层 mcp 键（Kilo 格式：local/remote +
	// enabled，自定义合并 mergeKiloMcp）；主配置直接读写真实 kilo.jsonc
	//（JSONC 宽容，保存前校验），模板仅作首次初始值。
	RegisterTarget(&kiloTarget{
		id: "kilo", name: "Kilo", apiFormat: "/oai",
		skillsDir:  kiloSkillsDir(),
		mcpFile:    kiloMcpRealFile(),
		configFile: kiloRealFile(),
		presetFile: kiloTemplateFile(),
	})

	// Antigravity CLI（agy）：skills + mcp + 编辑配置。skills 注入全局目录
	// ~/.gemini/config/skills；MCP 声明在全局 mcp_config.json（~/.gemini/config/
	// mcp_config.json 顶层 mcpServers 键，Claude 风格 command/args + 远程 serverUrl，
	// 自定义合并 mergeAgyMcp）；主配置直接读写真实
	// ~/.gemini/antigravity-cli/settings.json（无模板中转，保存前 JSON 校验），
	// 模板仅作首次初始值。
	RegisterTarget(&agyTarget{
		id: "agy", name: "Antigravity CLI", apiFormat: "/oai",
		skillsDir:  agySkillsDir(),
		mcpFile:    agyMcpRealFile(),
		configFile: agySettingsRealFile(),
		presetFile: agySettingsTemplateFile(),
	})
}
