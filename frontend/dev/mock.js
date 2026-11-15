/*
 * 纯浏览器 UI 预览桩：仅通过 vite dev 的 /dev/preview.html 打开时生效
 * （classic script 先于 deferred 模块执行，种好 window.go 后 api.js 再取绑定）。
 * 只覆盖让「客户端卡片网格 + 配置抽屉」完整渲染所需的最小绑定；
 * 其余方法由 Proxy 兜底返回 {ok:false}，各调用点按失败路径静默处理。
 * 不参与 wails 构建（dist 仅由 index.html 单入口产出）。
 */
(function () {
  // dev 预览诊断：收集页面错误（agent-browser 无控制台读取，用 window.__errs 兜底）
  window.__errs = []
  window.addEventListener('error', function (e) { window.__errs.push('E:' + e.message + ' @ ' + e.filename + ':' + e.lineno + ':' + e.colno) })
  window.addEventListener('unhandledrejection', function (e) { var r = e.reason; window.__errs.push('P:' + ((r && (r.stack || r.message)) || String(r))) })
  if (window.go && window.go.main && window.go.main.App) return
  var BASE = 'http://127.0.0.1:3000'
  var MODELS = ['llm-gpt5', 'llm-claude-sonnet', 'llm-gemini-pro', 'llm-deepseek-v4', 'llm-kimi-k3', 'llm-grok-5']
  function T(id, name, extra) {
    return Object.assign({
      id: id, name: name, apiFormat: '/oai', configFormat: 'json',
      skillsSupported: false, mcpSupported: false,
      installed: true, version: '1.2.0', canInstall: true, canUninstall: true,
    }, extra || {})
  }
  var targets = [
    T('claude-code', 'Claude Code', { bin: 'claude', pkg: '@anthropic-ai/claude-code', apiFormat: '/aai', mcpSupported: true }),
    T('codex', 'Codex CLI', { bin: 'codex', pkg: '@openai/codex', configFormat: 'toml', mcpSupported: true }),
    T('grok-build', 'Grok Build', { bin: 'grok', configFormat: 'toml' }),
    T('opencode', 'OpenCode', { bin: 'opencode', pkg: 'opencode-ai', mcpSupported: true }),
    T('pi', 'Pi CLI', { bin: 'pi', pkg: '@mariozechner/pi', mcpSupported: true }),
    T('kimi', 'Kimi CLI', { bin: 'kimi', configFormat: 'toml', mcpSupported: true }),
    T('commandcode', 'Command Code', { bin: 'commandcode', mcpSupported: true }),
    T('codebuddy', 'CodeBuddy CLI', { bin: 'codebuddy', pkg: '@tencent/codebuddy', mcpSupported: true }),
    T('agy', 'Antigravity CLI', { bin: 'agy', mcpSupported: true }),
    T('mimocode', 'Mimo Code', { bin: 'mimo' }),
    T('copilot', 'Copilot CLI', { bin: 'copilot', pkg: '@github/copilot' }),
    T('deepcode', 'Deep Code', { bin: 'deepcode' }),
  ]
  var json = function (v) { return JSON.stringify(v, null, 2) }
  var cfgs = {
    'claude-code': json({
      env: { ANTHROPIC_BASE_URL: BASE + '/router', ANTHROPIC_AUTH_TOKEN: 'sk-demo-token' },
      permissions: { defaultMode: 'acceptEdits' },
      includeCoAuthoredBy: 'true', cleanupPeriodDays: 30,
    }),
    'codex': 'model = "llm/llm-gpt5"\nmodel_provider = "llmrouter"\n',
    'grok-build': '',
    'kimi': '',
    'opencode': json({
      provider: { llm: { options: { baseURL: BASE + '/router', apiKey: '' } } },
      model: 'llm/llm-gpt5', small_model: 'llm/llm-kimi-k3',
    }),
    'pi': json({
      providers: { LLM: { api: 'openai-completions', baseUrl: BASE, apiKey: 'sk-demo',
        models: [{ id: 'llm-gpt5', name: 'GPT 5', contextWindow: 128000, reasoning: true, supportsTools: true, supportsVision: false }] } },
      defaultProvider: 'LLM', defaultModel: 'llm-gpt5', theme: 'dark',
    }),
    'commandcode': json({ providers: { llmrouter: { models: {} } }, defaultModel: 'llm/llm-gpt5' }),
    'codebuddy': json({ model: 'llm/llm-gpt5', reasoningEffort: 'medium' }),
    'agy': json({ modelProvider: 'gemini' }),
    'mimocode': json({ providerKey: 'llm', apiKey: 'sk-demo' }),
    'copilot': json({}),
    'deepcode': json({}),
  }
  var src = function (file, content) { return { ok: true, exists: !!content, file: file, content: content } }
  // ---- 全页面预览数据（GUI 显示检查用）----
  var now = Date.now()
  var MIN = 60e3, H = 3600e3, DAY = 864e5
  var pad = function (n) { return (n < 10 ? '0' : '') + n }
  var stamp = function (msAgo) { var d = new Date(now - msAgo); return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds()) }
  var providers = [
    { id: 'p-deepseek', name: 'DeepSeek 官方', type: 'openai', baseUrl: 'https://api.deepseek.com', apiKey: 'sk-demo-ds', models: ['deepseek-v4', 'deepseek-v4-flash-vision-exp'], aliases: { 'gpt-5-codex': 'deepseek-v4-flash-vision-exp' }, enabled: true },
    { id: 'p-openai', name: 'OpenAI 中转', type: 'openai', baseUrl: 'https://one.example.com/v1', apiKey: 'sk-demo-oa', models: ['gpt-5.5', 'gpt-5.6-terra', 'o4-mini'], aliases: {}, enabled: true },
    { id: 'p-anthropic', name: 'Claude 中转（停用）', type: 'anthropic', baseUrl: 'https://ant.example.com', apiKey: 'sk-demo-an', models: ['claude-sonnet-4-6'], aliases: {}, enabled: false },
  ]
  var config = { port: 3000, host: '127.0.0.1', authToken: 'sk-demo-token', defaultProvider: 'p-deepseek', fallbackModels: ['deepseek-v4'], providers: providers, reasoningDepth: 'high', tray: true }
  var logs = [
    { time: stamp(2 * MIN), path: '/router/v1/messages', model: 'llm-gpt5', provider: 'DeepSeek 官方', reasoning: 'reasoning_effort=high*', reasoningOut: 'thinkingConfig.thinkingBudget=24576', status: 200, ms: 8421, usage: { prompt_tokens: 18342, completion_tokens: 2210 } },
    { time: stamp(9 * MIN), path: '/v1/chat/completions', model: 'llm-claude-sonnet', provider: 'OpenAI 中转', reasoning: 'thinkingConfig.thinkingBudget=-1', reasoningOut: 'reasoning_effort=high', status: 200, ms: 3120, usage: { prompt_tokens: 5210, completion_tokens: 880 } },
    { time: stamp(40 * MIN), path: '/router/v1/messages', model: 'llm-gemini-pro', provider: 'DeepSeek 官方', reasoning: '', reasoningOut: '', status: 200, ms: 1150, usage: { prompt_tokens: 990, completion_tokens: 122 } },
    { time: stamp(3 * H), path: '/v1/chat/completions', model: 'llm-deepseek-v4', provider: 'DeepSeek 官方', reasoning: 'reasoning_effort=medium', reasoningOut: '', status: 429, ms: 302 },
    { time: stamp(5 * H), path: '/router/v1/messages', model: 'llm-kimi-k3', provider: 'OpenAI 中转', reasoning: 'high', reasoningOut: 'output_config.effort=high', status: 500, ms: 65535, usage: { prompt_tokens: 77, completion_tokens: 0 } },
    { time: stamp(26 * H), path: '/router/v1/messages', model: 'llm-grok-5', provider: 'OpenAI 中转', reasoning: '', reasoningOut: '', status: 200, ms: 22300, usage: { prompt_tokens: 999999, completion_tokens: 123456 } },
  ]
  var days = {}
  for (var i = 13; i >= 0; i--) { var d = new Date(now - i * DAY); var k = d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()); days[k] = { count: 20 + (i * 7) % 60, prompt: 120000 + i * 3300, completion: 30000 + i * 900 } }
  var mday = function (scale) { var m = {}; for (var key in days) m[key] = { count: Math.round(days[key].count * scale), prompt: Math.round(days[key].prompt * scale), completion: Math.round(days[key].completion * scale) }; return m }
  var stats = {
    days: days,
    perModel: { 'llm-gpt5': { days: mday(0.4) }, 'llm-claude-sonnet': { days: mday(0.25) }, 'llm-gemini-pro': { days: mday(0.2) }, 'llm-deepseek-v4': { days: mday(0.1) }, 'llm-kimi-k3': { days: mday(0.05) } },
    perProvider: { 'DeepSeek 官方': { count: 128, prompt: 456000, completion: 98000, avg_ms: 2340 }, 'OpenAI 中转': { count: 64, prompt: 210000, completion: 44000, avg_ms: 1870 } },
  }
  var sessionClients = [
    { id: 'claude-code', name: 'Claude Code', root: 'C:/Users/Administrator/.claude/projects', rootExists: true, count: 6, totalSize: 48211003, lastMod: stamp(30 * MIN), managed: true, note: '', resumeSupported: true, resumeNote: '' },
    { id: 'codex', name: 'Codex CLI', root: 'C:/Users/Administrator/.codex/sessions', rootExists: true, count: 4, totalSize: 12345678, lastMod: stamp(26 * H), managed: true, note: '', resumeSupported: true, resumeNote: '' },
    { id: 'grok-build', name: 'Grok Build', root: 'C:/Users/Administrator/.grok/sessions', rootExists: true, count: 3, totalSize: 998001, lastMod: stamp(50 * H), managed: true, note: '', resumeSupported: true, resumeNote: '' },
    { id: 'copilot', name: 'Copilot CLI', root: 'C:/Users/Administrator/.copilot/chats', rootExists: true, count: 2, totalSize: 444444, lastMod: stamp(74 * H), managed: true, note: '', resumeSupported: false, resumeNote: 'Copilot CLI 仅支持 --continue 恢复最近一次会话，无法按指定会话恢复' },
    { id: 'pi', name: 'Pi CLI', root: 'C:/Users/Administrator/.pi/agent/sessions', rootExists: false, count: 0, totalSize: 0, lastMod: '', managed: true, note: '', resumeSupported: false, resumeNote: 'pi 未安装，且未登记其会话恢复命令' },
  ]
  var sessionItems = {
    'claude-code': [
      { path: 'C:/Users/Administrator/.claude/projects/C--Users-Admin-Desktop-LLMrouter/0a11a6f5-9a2c-4f8e-b1a3-77c2e1d4a9b7.jsonl', name: '0a11a6f5-9a2c-4f8e-b1a3-77c2e1d4a9b7.jsonl', id: '0a11a6f5-9a2c-4f8e-b1a3-77c2e1d4a9b7', title: '修复 Sessions 页面命令速查浮层在 backdrop-filter 面板下的定位偏移，Teleport 到 body 后重新锚定光标正下方', cmd: 'claude --resume 0a11a6f5-9a2c-4f8e-b1a3-77c2e1d4a9b7', group: 'C--Users-Admin-Desktop-LLMrouter', isDir: false, size: 4821100, files: 1, mtime: stamp(30 * MIN), msgs: 86, tokensIn: 1523400, tokensOut: 89400, stats: true, cli: false },
      { path: 'C:/Users/Administrator/.claude/projects/C--Users-Admin-Desktop-LLMrouter/b2f9c3d1-1111-2222-3333-444455556666.jsonl', name: 'b2f9c3d1-1111-2222-3333-444455556666.jsonl', id: 'b2f9c3d1-1111-2222-3333-444455556666', title: '', cmd: 'claude --resume b2f9c3d1-1111-2222-3333-444455556666', group: 'C--Users-Admin-Desktop-LLMrouter', isDir: false, size: 20480, files: 1, mtime: stamp(5 * H), msgs: 0, tokensIn: 0, tokensOut: 0, stats: false, cli: false },
      { path: 'C:/Users/Administrator/.claude/projects/D--work-web-portal/7c1e8a2b-aabb-ccdd-eeff-000111222333.jsonl', name: '7c1e8a2b-aabb-ccdd-eeff-000111222333.jsonl', id: '7c1e8a2b-aabb-ccdd-eeff-000111222333', title: '为门户项目接入新的鉴权中间件并在 README 中补充本地启动与联调说明', cmd: 'claude --resume 7c1e8a2b-aabb-ccdd-eeff-000111222333', group: 'D--work-web-portal', isDir: false, size: 913664, files: 1, mtime: stamp(30 * H), msgs: 41, tokensIn: 320400, tokensOut: 20100, stats: true, cli: false },
    ],
    'codex': [
      { path: 'C:/Users/Administrator/.codex/sessions/2026/10/27/rollout-2026-10-27T09-30-11-0a11a6f5-9a2c-4f8e-b1a3-77c2e1d4a9b7.jsonl', name: 'rollout-2026-10-27T09-30-11-0a11a6f5-9a2c-4f8e-b1a3-77c2e1d4a9b7.jsonl', id: '0a11a6f5-9a2c-4f8e-b1a3-77c2e1d4a9b7', title: '重构 agent/clients 目录并迁移全部客户端模板', cmd: 'codex resume 0a11a6f5-9a2c-4f8e-b1a3-77c2e1d4a9b7', group: '2026/10/27', isDir: false, size: 3041856, files: 1, mtime: stamp(26 * H), msgs: 132, tokensIn: 4211200, tokensOut: 260500, stats: true, cli: false },
      { path: 'C:/Users/Administrator/.codex/sessions/2026/10/26/rollout-2026-10-26T18-02-45-f1e2d3c4-5555-6666-7777-888899990000.jsonl', name: 'rollout-2026-10-26T18-02-45-f1e2d3c4-5555-6666-7777-888899990000.jsonl', id: 'f1e2d3c4-5555-6666-7777-888899990000', title: '', cmd: 'codex resume f1e2d3c4-5555-6666-7777-888899990000', group: '2026/10/26', isDir: false, size: 102400, files: 1, mtime: stamp(27 * H), msgs: 12, tokensIn: 54300, tokensOut: 8800, stats: true, cli: false },
    ],
    'grok-build': [
      { path: 'C:/Users/Administrator/.grok/sessions/wksp-alpha/9f8e7d6c-1212-3434-5656-787899aabbcc', name: '9f8e7d6c-1212-3434-5656-787899aabbcc', id: '9f8e7d6c-1212-3434-5656-787899aabbcc', title: '排查 Windows 下 ConPTY 附加失效问题', cmd: 'grok --resume 9f8e7d6c-1212-3434-5656-787899aabbcc', group: 'wksp-alpha', isDir: true, size: 998001, files: 12, mtime: stamp(50 * H), msgs: 58, tokensIn: 890100, tokensOut: 43200, stats: true, cli: false },
    ],
    'copilot': [
      { path: 'C:/Users/Administrator/.copilot/chats/chat-2026-10-24-1', name: 'chat-2026-10-24-1', id: 'chat-2026-10-24-1', title: '升级依赖并修复构建告警', cmd: '', group: '', isDir: true, size: 222222, files: 5, mtime: stamp(74 * H), msgs: 23, tokensIn: 121000, tokensOut: 15400, stats: true, cli: false },
    ],
    'pi': [],
  }
  var skillsList = [
    { name: 'code-review', path: 'C:/Users/Administrator/.llmrouter/skills/code-review/SKILL.md', desc: '逐文件审查代码改动，输出分级意见与修复建议' },
    { name: 'commit-writer', path: 'C:/Users/Administrator/.llmrouter/skills/commit-writer/SKILL.md', desc: 'Conventional Commits 风格提交信息生成' },
    { name: 'a-very-long-skill-name-for-overflow-check-abcdefghijklmnopqrstuvwxyz-0123456789', path: 'C:/Users/Administrator/.llmrouter/skills/overflow/SKILL.md', desc: '超长名称溢出检查用例：' + '这是一段用于检查换行与截断表现的很长的技能描述。'.repeat(6) },
  ]
  // Copilot BYOK 内存态（暴露到 window 供预览断言）
  var copilotEnv = {}
  var copilotSettings = {}
  window.__copilotEnv = copilotEnv
  window.__copilotSettings = copilotSettings
  var specific = {
    IsReaderWindow: function () { return Promise.resolve(false) },
    IsTermWindow: function () { return Promise.resolve(false) },
    GetSessionClients: function () { return Promise.resolve(sessionClients) },
    GetSessionItems: function (id) { return Promise.resolve({ ok: true, items: (sessionItems[id] || []).slice() }) },
    DeleteSessionItems: function (id, paths) {
      var del = paths || []
      sessionItems[id] = (sessionItems[id] || []).filter(function (it) { return del.indexOf(it.path) < 0 && del.indexOf(it.id) < 0 })
      var sc = sessionClients.filter(function (c) { return c.id === id })[0]
      if (sc) { sc.count = sessionItems[id].length; sc.totalSize = sessionItems[id].reduce(function (s, it) { return s + it.size }, 0) }
      return Promise.resolve({ ok: true })
    },
    OpenSessionTerminal: function () { return Promise.resolve({ ok: true }) },
    OpenSessionReader: function () { return Promise.resolve({ ok: true }) },
    SaveTargetConfig: function (id, text) { window.__lastSave = { id: id, text: text }; return Promise.resolve({ ok: true }) },
    GetRouterBase: function () { return Promise.resolve(BASE) },
    ListTargets: function () { return Promise.resolve(targets) },
    GetTargetConfig: function (id) { return Promise.resolve({ ok: true, exists: true, file: '~/' + id + '/config', content: cfgs[id] != null ? cfgs[id] : '{}' }) },
    GetConfig: function () { return Promise.resolve(JSON.parse(JSON.stringify(config))) },
    GetStatus: function () { return Promise.resolve({ running: true, error: '', port: 3000, host: '127.0.0.1', localBase: BASE, providers: providers.length }) },
    SaveConfig: function () { return Promise.resolve({ ok: true, restartNeeded: false }) },
    RestartServer: function () { return Promise.resolve({ ok: true }) },
    GetLogs: function () { return Promise.resolve(logs.slice()) },
    GetStats: function () { return Promise.resolve(JSON.parse(JSON.stringify(stats))) },
    GetActiveModels: function () { return Promise.resolve({ models: ['llm-gpt5'] }) },
    GetAppInfo: function () { return Promise.resolve({ name: 'LLM Router', version: '3.0.0-dev', updateAvailable: false, latestVersion: '', downloadURL: '' }) },
    GetAutoStartEnabled: function () { return Promise.resolve(true) },
    SetAutoStartEnabled: function () { return Promise.resolve({ ok: true }) },
    GetTrayEnabled: function () { return Promise.resolve(true) },
    SetTrayEnabled: function () { return Promise.resolve({ ok: true }) },
    GetSkillsList: function () { return Promise.resolve(skillsList.slice()) },
    GetSkillsDir: function () { return Promise.resolve('C:/Users/Administrator/.llmrouter/skills') },
    GetAgentSkills: function () {
      return Promise.resolve([
        { name: 'Claude Code', path: 'C:/Users/Administrator/.claude/skills', skills: [{ name: 'code-review', path: 'C:/Users/Administrator/.claude/skills/code-review/SKILL.md', desc: '逐文件审查代码改动' }] },
        { name: 'Codex CLI', path: 'C:/Users/Administrator/.codex/skills', skills: [] },
        { name: 'Grok Build', path: 'C:/Users/Administrator/.grok/skills', skills: [{ name: 'commit-writer', path: 'C:/Users/Administrator/.grok/skills/commit-writer/SKILL.md', desc: '' }] },
      ])
    },
    GetSkillContent: function (name) { return Promise.resolve('# ' + (name || 'skill') + '\n\n这是技能正文（预览桩）。') },
    SyncAllSkills: function () { return Promise.resolve({ ok: true }) },
    GetMcps: function () {
      return Promise.resolve({
        baseUrl: BASE + '/mcp',
        servers: [
          { name: 'filesystem', desc: '本地文件读写（stdio 示例）', type: 'stdio', command: 'npx', args: ['-y', '@modelcontextprotocol/server-filesystem', 'C:/work'], url: '' },
          { name: 'web-search', desc: '网络搜索（http 示例）', type: 'http', command: '', args: [], url: 'https://mcp.example.com/search' },
        ],
      })
    },
    GetMcpInjectedTargets: function () { return Promise.resolve({ filesystem: ['claude-code', 'codex'], 'web-search': [] }) },
    GetMcpDir: function () { return Promise.resolve('C:/Users/Administrator/.llmrouter/mcp') },
    GetModelsByFormat: function () { return Promise.resolve({ models: MODELS }) },
    GetProviderModels: function () { return Promise.resolve({ models: MODELS }) },
    GetClientRealPaths: function () { return Promise.resolve({ ok: true, paths: {} }) },
    CheckClientUpdates: function () { return Promise.resolve({ ok: true, updates: {} }) },
    OpenFileLocation: function () { return Promise.resolve({ ok: true }) },
    GetGrokAgents: function () { return Promise.resolve(src('~/.grok/AGENTS.md', '')) },
    GetKimiAgents: function () { return Promise.resolve(src('~/.kimi-code/AGENTS.md', '')) },
    GetPiSettings: function () { return Promise.resolve(src('~/.pi/agent/settings.json', json({ theme: 'dark' }))) },
    GetPiAgents: function () { return Promise.resolve(src('~/.pi/agent/AGENTS.md', '')) },
    GetPiMcp: function () { return Promise.resolve(src('~/.pi/agent/mcp.json', '{}')) },
    GetOpencodeAgents: function () { return Promise.resolve(src('~/.config/opencode/AGENTS.md', '')) },
    GetCommandcodeSettings: function () { return Promise.resolve(src('~/.commandcode/settings.json', '{}')) },
    GetCommandcodeProviders: function () { return Promise.resolve(src('~/.commandcode/providers.json', '{}')) },
    GetCommandcodeNoLogin: function () { return Promise.resolve(false) },
    GetAgyMcp: function () { return Promise.resolve(src('~/.gemini/config/mcp_config.json', '{}')) },
    GetCodebuddyModels: function () { return Promise.resolve(src('~/.codebuddy/models.json', json({ models: [{ id: 'llm-gpt5', name: 'GPT 5', vendor: 'OpenAI', supportsToolCall: true, supportsImages: true, supportsReasoning: true }] }))) },
    GetCodebuddyMcp: function () { return Promise.resolve(src('~/.codebuddy/mcp.json', '{}')) },
    GetCodexAgents: function () { return Promise.resolve(src('~/.codex/AGENTS.md', '')) },
    GetClaudeMarkdown: function () { return Promise.resolve(src('~/.claude/CLAUDE.md', '# CLAUDE')) },
    GetCopilotSettings: function () { return Promise.resolve({ ok: true, exists: true, file: '~/.copilot/settings.json', settings: JSON.parse(JSON.stringify(copilotSettings)) }) },
    SaveCopilotSettings: function (patch) {
      for (var k in patch) {
        var v = patch[k]
        if (v === '' || v === null || (Array.isArray(v) && !v.length)) delete copilotSettings[k]
        else copilotSettings[k] = v
      }
      return Promise.resolve({ ok: true })
    },
    GetCopilotModel: function () { return Promise.resolve({ ok: true, exists: 'model' in copilotSettings, model: copilotSettings.model || '', file: '~/.copilot/settings.json' }) },
    GetUserEnv: function (k) { return Promise.resolve({ ok: true, exists: Object.prototype.hasOwnProperty.call(copilotEnv, k), value: copilotEnv[k] || '' }) },
    SetUserEnv: function (k, v) { copilotEnv[k] = v; window.__envWrites = (window.__envWrites || 0) + 1; return Promise.resolve({ ok: true }) },
    DelUserEnv: function (k) { delete copilotEnv[k]; return Promise.resolve({ ok: true }) },
    GetCopilotPermissions: function () { return Promise.resolve({ ok: true, exists: false, content: '{}' }) },
    SaveCopilotPermissions: function () { return Promise.resolve({ ok: true }) },
    GetCopilotPermLocCandidates: function () { return Promise.resolve({ ok: true, dirs: [] }) },
    FetchModels: function () { return Promise.resolve({ ok: true, models: MODELS }) },
  }
  var fallback = function () { return Promise.resolve({ ok: false }) }
  window.go = { main: { App: new Proxy(specific, {
    get: function (t, k) { return Object.prototype.hasOwnProperty.call(t, k) ? t[k] : fallback },
  }) } }
})()
