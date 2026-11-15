// Package clients 是各 AI CLI 客户端的配置适配器集合：一个客户端一组文件，
// 每个客户端只在本包内独立维护自己的路径、注入算法与注册，互不牵连。
//
// 依赖方向单向：clients → core（llm-router-go/agent/core）。所有与具体客户端
// 无关的机制（注册表、适配器接口、通用注入原语、文件读写与备份、路径解析、
// 校验、JSON/JSONC/TOML/YAML 定点编辑）都在内核 agent/core，本包统一经
// agent/core 的导出层 api.go 调用；新增客户端 = 新增一个文件 +
// 在 core.RegisterTarget 注册。
//
// 文件与客户端的对应关系（客户端 id → 文件）：
//
//	claude-code  claude_templates.go
//	opencode     opencode_template.go / opencode_agents.go
//	pi           pi_templates.go / pi_settings.go / pi_mcp.go / pi_agents.go
//	grok-build   grok_templates.go / grok_agents.go（MCP 段落手术见 toml_mcp.go）
//	codex        codex_templates.go（同上）
//	commandcode  commandcode_config.go
//	copilot      copilot.go
//	kimi         kimi_templates.go / kimi_agents.go / kimi_mcp.go
//	mimocode     mimocode.go
//	agy          agy_templates.go / agy_mcp.go
//	codebuddy    codebuddy_files.go
//	deepcode     deepcode_files.go
//
// 两个共用文件不属于单个客户端：
//
//	targets.go   各客户端专属适配器形态 + 全部 RegisterTarget 注册（新增客户端的接入点）
//	toml_mcp.go  Grok Build / Codex 共用的 TOML [mcp_servers.*] 段落手术
package clients
