// Grok Build（~/.grok/config.toml，固定 /oai 接入）：TOML 原文编辑 + 结构化表单
// 受管内容：[models].default/web_search 与 [model."X"] 块（name/description/model/base_url/
// api_key|env_key/api_backend/reasoning_effort/context_window/temperature/top_p/
// max_completion_tokens/extra_headers/supports_backend_search）、[ui] theme/simple_mode/
// vim_mode/scroll_speed/compact_mode/show_thinking_blocks/max_thoughts_width/
// default_selected_permission/remember_tool_approvals/group_tool_verbs/collapsed_edit_blocks/
// page_flip_on_send/screen_mode/permission_mode、[cli] auto_update、[session]
// auto_compact_threshold_percent、[toolset] file_toolset、[toolset.hashline] scheme/hash_len/
// chunk_size、[features] telemetry/feedback/lsp_tools/codebase_indexing/two_pass_compaction/
// remote_fetch、[compat.*] mcps、[permission] 审批规则；其余段（[mcp_servers.*]/[env]/
// [marketplace] 等）原样保留；AGENTS.md 源码直编。
import { ref, watch, nextTick } from 'vue'
import { toast } from '../../toast'
import { t } from '../../i18n'
import api from '../../api'
import { tq, unquoteToml, permRowsFromLists, permRowsToLists } from './shared'
import { cfgContent, cfgEdit, cfgTarget, cfgFormat, derivedBaseUrl, modelOptions } from './store'

// grokEmptyForm：grokForm 的空值结构（单一来源；初始 ref 与 controller 重置共用）
export function grokEmptyForm() {
  return {
    default: '', webSearch: '', modelKey: '', accessMode: '/oai', contextWindow: '', apiKey: '', model: '',
    name: '', description: '', apiBackend: '', reasoningEffort: '', temperature: '', topP: '', maxTokens: '',
    extraHeaders: '', supportsBackendSearch: '', permissionMode: '',
    uiTheme: '', uiSimpleMode: '', uiCompact: '', uiVim: '', uiThinking: '', uiScrollSpeed: '', uiMaxThoughtsWidth: '',
    uiDefaultPerm: '', uiRememberApprovals: '', uiGroupVerbs: '', uiCollapsedEdit: '', uiPageFlip: '', uiScreenMode: '',
    autoUpdate: '', autoCompactThreshold: '', tsFileToolset: '', hlScheme: '', hlHashLen: '', hlChunkSize: '',
    featTelemetry: '', featFeedback: '', featLsp: '', featIndexing: '', featTwoPass: '', featRemoteFetch: '',
    compatClaudeMcps: '', compatCursorMcps: '',
  }
}
export const grokForm = ref(grokEmptyForm())
export const grokPermRows = ref([]) // [{ text, mode }] 权限规则点选列表（[permission] rules）
export const grokUiExtras = ref([]) // [ui] 段内非受管键行（memory 等）原样保留
export const grokCliExtras = ref([]) // [cli] 段内非受管键行原样保留
export const grokSessionExtras = ref([]) // [session] 段内非受管键行原样保留
export const grokToolsetExtras = ref([]) // [toolset] 段内非受管键行原样保留
export const grokHashlineExtras = ref([]) // [toolset.hashline] 段内非受管键行原样保留
export const grokFeaturesExtras = ref([]) // [features] 段内非受管键行原样保留
export const grokCompatClaudeExtras = ref([]) // [compat.claude] 段内非受管键行原样保留
export const grokCompatCursorExtras = ref([]) // [compat.cursor] 段内非受管键行原样保留
export const grokQuickOpen = ref('')
export const grokQuickFocus = ref('')
// Grok Build 权限快选：官方工具过滤名（docs.x.ai/build/features/permissions）
// 仅 Bash/Edit/Read/Grep/MCPTool/WebFetch/WebSearch；通配为空格星号（Bash(git *)），
// 区别于 Claude 族的 Bash(git:*) 语法
export const GROK_PERM_RULES = [
  { v: 'Read', tip: () => t('sync.ruleRead') },
  { v: 'Edit', tip: () => t('sync.ruleEdit') },
  { v: 'Grep', tip: () => t('sync.ruleGrep') },
  { v: 'Bash(git *)', tip: () => 'Bash(git *) — Git' },
  { v: 'Bash(npm *)', tip: () => 'Bash(npm *) — npm' },
  { v: 'Bash(pnpm *)', tip: () => 'Bash(pnpm *) — pnpm' },
  { v: 'Bash(go *)', tip: () => 'Bash(go *) — go' },
  { v: 'Bash(docker *)', tip: () => 'Bash(docker *) — docker' },
  { v: 'WebFetch', tip: () => t('sync.ruleFetch') },
  { v: 'WebSearch', tip: () => t('sync.ruleSearch') },
  { v: 'MCPTool', tip: () => t('sync.ruleMcp') },
]
// 工具名 → 规范显示名（与上方案例一致，避免 mcptool/webfetch/websearch 等被误判为自定义规则而重复展示）
const GROK_TOOL_LABEL = {
  read: 'Read', edit: 'Edit', grep: 'Grep', bash: 'Bash',
  webfetch: 'WebFetch', websearch: 'WebSearch', mcptool: 'MCPTool',
}
// 把常用规则追加/移出 grok 权限文本框（按行去重切换）
export function grokAddPermRule(field, rule) {
  if (!rule) return
  const cur = String(grokForm.value[field] || '').trim()
  const arr = cur ? cur.split(/\r?\n/).map(s => s.trim()).filter(Boolean) : []
  const i = arr.indexOf(rule)
  if (i >= 0) arr.splice(i, 1)
  else arr.push(rule)
  grokForm.value[field] = arr.join('\n')
}
export function grokRuleOn(field, rule) {
  return String(grokForm.value[field] || '').split(/\r?\n/).some(s => s.trim() === rule)
}
// grokMuted：模板载入/源码回读期间静默，避免初始化赋值触发联动重写源码（与 codexMuted 同机制）
export let grokMuted = false
export function setGrokMuted(v) { grokMuted = v }
export const grokBlocks = ref({}) // 解析出的全部 [model."X"] 块：{ key: {model,name,description,baseUrl,contextWindow,apiKey,envKey,apiBackend,reasoningEffort,temperature,topP,maxTokens,supportsBackendSearch} }
export const grokOrder = ref([]) // 模型块出现顺序
export const grokUnmanaged = ref([]) // 非受管内容块（[mcp_servers.*]/[env] 等）：保存时原样保留，不丢弃
export const grokAgentsContent = ref('') // ~/.grok/AGENTS.md 源码
export const grokAgentsExists = ref(false)
export const grokAgentsFile = ref('~/.grok/AGENTS.md')

// Grok Build config.toml 预设模板（文件不存在时作为初始内容；字段对齐真实 Grok Build 架构）
export function grokTomlPreset() {
  return `# ~/.grok/config.toml

[models]
default = "grok-4.5"
web_search = "grok-4.5"

[model."grok-4.5"]
name = "Grok 4.5"
model = "grok-4.5"
description = "Grok 4.5 from xAI"
base_url = "http://127.0.0.1:3000"
api_key = ""                                # 路由令牌；留空则用 env_key 指向的环境变量
api_backend = "responses"
reasoning_effort = "high"
temperature = 0.7
top_p = 0.95
max_completion_tokens = 8192
context_window = 1000000
extra_headers = {}
supports_backend_search = true

[cli]
auto_update = true

[ui]
simple_mode = false
vim_mode = false
scroll_speed = 50
compact_mode = false
theme = "auto"
show_thinking_blocks = true
max_thoughts_width = 120
default_selected_permission = ""
remember_tool_approvals = false
group_tool_verbs = true
collapsed_edit_blocks = false
page_flip_on_send = true
screen_mode = "fullscreen"

[session]
auto_compact_threshold_percent = 85

[toolset]
file_toolset = "standard"

[toolset.hashline]
scheme = "chunk"
hash_len = 3
chunk_size = 8

[features]
telemetry = false
feedback = true
lsp_tools = false
codebase_indexing = true
two_pass_compaction = false
remote_fetch = true

[compat.claude]
mcps = false

[compat.cursor]
mcps = false
`
}
export function grokReadToml(text) {
  const blocks = {}
  const order = []
  const unmanaged = [] // 非受管段（[mcp_servers.*]/[env]/[marketplace] 等）：整段原样保留
  const uiExtras = []  // [ui] 段内非受管键行（memory 等）原样保留
  const cliExtras = [] // [cli] 段内非受管键行原样保留
  const sessionExtras = [] // [session] 段内非受管键行原样保留
  const toolsetExtras = [] // [toolset] 段内非受管键行原样保留
  const hashlineExtras = [] // [toolset.hashline] 段内非受管键行原样保留
  const featuresExtras = [] // [features] 段内非受管键行原样保留
  const compatClaudeExtras = [] // [compat.claude] 段内非受管键行原样保留
  const compatCursorExtras = [] // [compat.cursor] 段内非受管键行原样保留
  let sec = ''
  let curUn = null
  let permBuf = ''     // [permission] rules 多行数组缓冲
  let permissionMode = ''
  let webSearch = ''
  let permAllow = []
  let permAsk = []
  let permDeny = []
  const grokCapTool = s => (GROK_TOOL_LABEL[s && s.toLowerCase()] || (s ? s.charAt(0).toUpperCase() + s.slice(1) : ''))
  for (const raw of (text || '').split('\n')) {
    const line = raw.trim()
    const h = line.match(/^\[([^\]]+)\]$/)
    if (h) {
      const inner = h[1].trim()
      permBuf = '' // 新段开始：未闭合的 rules 数组缓冲作废
      if (inner === 'models') { sec = 'models'; curUn = null; continue }
      if (inner === 'ui') { sec = 'ui'; curUn = null; continue }
      if (inner === 'cli') { sec = 'cli'; curUn = null; continue }
      if (inner === 'session') { sec = 'session'; curUn = null; continue }
      if (inner === 'toolset') { sec = 'toolset'; curUn = null; continue }
      if (inner === 'toolset.hashline') { sec = 'toolset.hashline'; curUn = null; continue }
      if (inner === 'features') { sec = 'features'; curUn = null; continue }
      if (inner === 'compat.claude') { sec = 'compat.claude'; curUn = null; continue }
      if (inner === 'compat.cursor') { sec = 'compat.cursor'; curUn = null; continue }
      if (inner === 'permission') { sec = 'permission'; curUn = null; continue }
      const mk = inner.startsWith('model."') && inner.endsWith('"')
        ? inner.slice('model."'.length, -1)
        : (inner.startsWith('model.') ? inner.slice(6).trim() : '')
      if (mk) {
        sec = 'model:' + mk
        curUn = null
        if (!(mk in blocks)) { blocks[mk] = {}; order.push(mk) }
        continue
      }
      // 非受管段：从段头起整段原样保留（含段内注释/空行/键值）
      sec = ''
      curUn = { name: '[' + inner + ']', lines: [raw] }
      unmanaged.push(curUn)
      continue
    }
    if (curUn) { curUn.lines.push(raw); continue }
    // [permission] rules = [ 多行数组：累积到出现 ] 为止
    if (sec === 'permission' && permBuf) {
      permBuf += '\n' + line
      if (line.includes(']')) {
        for (const item of (permBuf.match(/\{[^}]*\}/g) || [])) {
          const mm = item.match(/action\s*=\s*"(\w+)"\s*,\s*tool\s*=\s*"(\w+)"(?:\s*,\s*pattern\s*=\s*"([^"]*)")?/)
          if (!mm) continue
          const label = grokCapTool(mm[2]) + (mm[3] !== undefined ? '(' + mm[3] + ')' : '')
          if (mm[1] === 'allow') permAllow.push(label)
          else if (mm[1] === 'ask') permAsk.push(label)
          else if (mm[1] === 'deny') permDeny.push(label)
        }
        permBuf = ''
      }
      continue
    }
    const kv = line.match(/^([A-Za-z0-9_.-]+)\s*=\s*(.+)$/)
    if (!kv) continue
    const key = kv[1], val = kv[2].trim()
    if (sec === 'models' && key === 'default') grokForm.value.default = unquoteToml(val)
    else if (sec === 'models' && key === 'web_search') webSearch = unquoteToml(val)
    else if (sec === 'ui' && key === 'theme') grokForm.value.uiTheme = unquoteToml(val)
    else if (sec === 'ui' && key === 'simple_mode') grokForm.value.uiSimpleMode = unquoteToml(val)
    else if (sec === 'ui' && key === 'vim_mode') grokForm.value.uiVim = unquoteToml(val)
    else if (sec === 'ui' && key === 'scroll_speed') grokForm.value.uiScrollSpeed = unquoteToml(val)
    else if (sec === 'ui' && key === 'compact_mode') grokForm.value.uiCompact = unquoteToml(val)
    else if (sec === 'ui' && key === 'show_thinking_blocks') grokForm.value.uiThinking = unquoteToml(val)
    else if (sec === 'ui' && key === 'max_thoughts_width') grokForm.value.uiMaxThoughtsWidth = unquoteToml(val)
    else if (sec === 'ui' && key === 'default_selected_permission') grokForm.value.uiDefaultPerm = unquoteToml(val)
    else if (sec === 'ui' && key === 'remember_tool_approvals') grokForm.value.uiRememberApprovals = unquoteToml(val)
    else if (sec === 'ui' && key === 'group_tool_verbs') grokForm.value.uiGroupVerbs = unquoteToml(val)
    else if (sec === 'ui' && key === 'collapsed_edit_blocks') grokForm.value.uiCollapsedEdit = unquoteToml(val)
    else if (sec === 'ui' && key === 'page_flip_on_send') grokForm.value.uiPageFlip = unquoteToml(val)
    else if (sec === 'ui' && key === 'screen_mode') grokForm.value.uiScreenMode = unquoteToml(val)
    else if (sec === 'ui' && key === 'permission_mode') permissionMode = unquoteToml(val)
    else if (sec === 'ui' && key === 'approval_mode') { if (!permissionMode) permissionMode = unquoteToml(val) }
    else if (sec === 'ui' && key === 'yolo') { if (val === 'true' && !permissionMode) permissionMode = 'always-approve' }
    else if (sec === 'ui') uiExtras.push(raw)
    else if (sec === 'cli' && key === 'auto_update') grokForm.value.autoUpdate = unquoteToml(val)
    else if (sec === 'cli') cliExtras.push(raw)
    else if (sec === 'session' && key === 'auto_compact_threshold_percent') grokForm.value.autoCompactThreshold = unquoteToml(val)
    else if (sec === 'session') sessionExtras.push(raw)
    else if (sec === 'toolset' && key === 'file_toolset') grokForm.value.tsFileToolset = unquoteToml(val)
    else if (sec === 'toolset') toolsetExtras.push(raw)
    else if (sec === 'toolset.hashline' && key === 'scheme') grokForm.value.hlScheme = unquoteToml(val)
    else if (sec === 'toolset.hashline' && key === 'hash_len') grokForm.value.hlHashLen = unquoteToml(val)
    else if (sec === 'toolset.hashline' && key === 'chunk_size') grokForm.value.hlChunkSize = unquoteToml(val)
    else if (sec === 'toolset.hashline') hashlineExtras.push(raw)
    else if (sec === 'features' && key === 'telemetry') grokForm.value.featTelemetry = unquoteToml(val)
    else if (sec === 'features' && key === 'feedback') grokForm.value.featFeedback = unquoteToml(val)
    else if (sec === 'features' && key === 'lsp_tools') grokForm.value.featLsp = unquoteToml(val)
    else if (sec === 'features' && key === 'codebase_indexing') grokForm.value.featIndexing = unquoteToml(val)
    else if (sec === 'features' && key === 'two_pass_compaction') grokForm.value.featTwoPass = unquoteToml(val)
    else if (sec === 'features' && key === 'remote_fetch') grokForm.value.featRemoteFetch = unquoteToml(val)
    else if (sec === 'features') featuresExtras.push(raw)
    else if (sec === 'compat.claude' && key === 'mcps') grokForm.value.compatClaudeMcps = unquoteToml(val)
    else if (sec === 'compat.claude') compatClaudeExtras.push(raw)
    else if (sec === 'compat.cursor' && key === 'mcps') grokForm.value.compatCursorMcps = unquoteToml(val)
    else if (sec === 'compat.cursor') compatCursorExtras.push(raw)
    else if (sec === 'permission' && key === 'rules') {
      if (val.includes('[') && !val.includes(']')) { permBuf = val }
      else {
        for (const item of (val.match(/\{[^}]*\}/g) || [])) {
          const mm = item.match(/action\s*=\s*"(\w+)"\s*,\s*tool\s*=\s*"(\w+)"(?:\s*,\s*pattern\s*=\s*"([^"]*)")?/)
          if (!mm) continue
          const label = grokCapTool(mm[2]) + (mm[3] !== undefined ? '(' + mm[3] + ')' : '')
          if (mm[1] === 'allow') permAllow.push(label)
          else if (mm[1] === 'ask') permAsk.push(label)
          else if (mm[1] === 'deny') permDeny.push(label)
        }
      }
    }
    else if (sec.startsWith('model:')) {
      const b = blocks[sec.slice(6)]
      if (key === 'model') b.model = unquoteToml(val)
      else if (key === 'name') b.name = unquoteToml(val)
      else if (key === 'description') b.description = unquoteToml(val)
      else if (key === 'base_url') b.baseUrl = unquoteToml(val)
      else if (key === 'api_key') b.apiKey = unquoteToml(val)
      else if (key === 'env_key') b.envKey = unquoteToml(val)
      else if (key === 'api_backend') b.apiBackend = unquoteToml(val)
      else if (key === 'reasoning_effort') b.reasoningEffort = unquoteToml(val)
      else if (key === 'context_window') b.contextWindow = val
      else if (key === 'temperature') b.temperature = val
      else if (key === 'top_p') b.topP = val
      else if (key === 'max_completion_tokens') b.maxTokens = val
      else if (key === 'extra_headers') b.extraHeaders = val
      else if (key === 'supports_backend_search') b.supportsBackendSearch = val
      else unmanaged.push({ name: sec.slice(6) + '.' + key, lines: [raw] })
    }
  }
  grokUiExtras.value = uiExtras
  grokCliExtras.value = cliExtras
  grokSessionExtras.value = sessionExtras
  grokToolsetExtras.value = toolsetExtras
  grokHashlineExtras.value = hashlineExtras
  grokFeaturesExtras.value = featuresExtras
  grokCompatClaudeExtras.value = compatClaudeExtras
  grokCompatCursorExtras.value = compatCursorExtras
  grokForm.value.webSearch = webSearch
  grokForm.value.permissionMode = ['ask', 'auto', 'always-approve'].includes(permissionMode) ? permissionMode : ''
  grokPermRows.value = permRowsFromLists({ allow: permAllow, ask: permAsk, deny: permDeny })
  grokUnmanaged.value = unmanaged
  grokBlocks.value = blocks
  grokOrder.value = order
  // 默认选中 default 指向的块；否则选第一个块
  const mk = (grokForm.value.default && blocks[grokForm.value.default]) ? grokForm.value.default : (order[0] || '')
  grokForm.value.modelKey = mk
  grokForm.value.accessMode = '/oai'
  selectGrokModel(mk)
  return grokForm.value
}
export function tgApiFormat() {
  return grokForm.value.accessMode || '/oai'
}
// 切换当前编辑的模型块：把该块字段回填到表单
export function selectGrokModel(key) {
  if (!key) return
  grokForm.value.modelKey = key
  const b = grokBlocks.value[key] || {}
  grokForm.value.model = b.model || ''
  grokForm.value.name = b.name || ''
  grokForm.value.description = b.description || ''
  grokForm.value.contextWindow = b.contextWindow || ''
  grokForm.value.apiKey = b.apiKey || ''
  grokForm.value.apiBackend = b.apiBackend || ''
  grokForm.value.reasoningEffort = b.reasoningEffort || ''
  grokForm.value.temperature = b.temperature || ''
  grokForm.value.topP = b.topP || ''
  grokForm.value.maxTokens = b.maxTokens || ''
  grokForm.value.extraHeaders = b.extraHeaders || ''
  grokForm.value.supportsBackendSearch = b.supportsBackendSearch || ''
  grokForm.value.accessMode = '/oai'
}
// 新增模型块：写入 blocks/order 并切到该块，等保存时由 grokBuildToml 生成对应段落
export function addGrokModel(key) {
  const mk = String(key || '').trim()
  if (!mk) return
  if (mk in grokBlocks.value) { selectGrokModel(mk); return }
  grokBlocks.value[mk] = {}
  grokOrder.value.push(mk)
  selectGrokModel(mk)
  if (!grokForm.value.default) grokForm.value.default = mk
}
// 整体重生成 config.toml：仅输出受管的 [models] 段、[model."key"] 块与
// [ui]/[cli]/[session]/[toolset]/[toolset.hashline]/[features]/[compat.*] 段，
// 字段与真实 Grok Build 架构一一对应（name/description/model/base_url/api_key|env_key/
// api_backend/reasoning_effort/context_window/temperature/top_p/max_completion_tokens/
// extra_headers/supports_backend_search 等）
export function grokBuildToml() {
  const f = grokForm.value
  // 当前编辑块的表单值先回写到块数据
  const mk = f.modelKey || f.default
  if (mk && grokBlocks.value[mk]) {
    const cur = grokBlocks.value[mk]
    cur.model = f.model || ''
    cur.name = f.name || ''
    cur.description = String(f.description ?? '').trim()
    cur.baseUrl = derivedBaseUrl(f.accessMode)
    cur.apiKey = f.apiKey || ''
    cur.apiBackend = String(f.apiBackend ?? '').trim()
    cur.reasoningEffort = String(f.reasoningEffort || '').trim()
    cur.contextWindow = String(f.contextWindow ?? '').trim()
    cur.temperature = String(f.temperature ?? '').trim()
    cur.topP = String(f.topP ?? '').trim()
    cur.maxTokens = String(f.maxTokens ?? '').trim()
    cur.extraHeaders = String(f.extraHeaders ?? '').trim()
    cur.supportsBackendSearch = String(f.supportsBackendSearch ?? '').trim()
  }
  const order = grokOrder.value.length ? grokOrder.value : (mk ? [mk] : [])
  const mlines = ['default = "' + (f.default || order[0] || '') + '"']
  const ws = String(f.webSearch || '').trim()
  if (ws) mlines.push('web_search = ' + tq(ws))
  const out = ['# ~/.grok/config.toml', '', '[models]', ...mlines]
  for (const key of order) {
    const b = grokBlocks.value[key] || {}
    out.push('', '[model."' + key + '"]')
    if (String(b.name || '').trim()) out.push('name = ' + tq(b.name))
    if (String(b.description || '').trim()) out.push('description = ' + tq(b.description))
    out.push('model = ' + tq(b.model || ''))
    out.push('base_url = ' + tq(b.baseUrl || derivedBaseUrl(f.accessMode)))
    // env_key 与 api_key 二选一：用户显式填写了 api_key 时以 api_key 为准
    //（此前 env_key 恒优先，手填的 api_key 被静默丢弃）；未填写则沿用 env_key
    if (String(f.apiKey || '').trim()) {
      delete b.envKey
      out.push('api_key = ' + tq(b.apiKey || ''))
    } else if (String(b.envKey || '').trim()) {
      out.push('env_key = ' + tq(b.envKey))
    } else {
      out.push('api_key = ' + tq(b.apiKey || ''))
    }
    if (String(b.apiBackend || '').trim()) out.push('api_backend = ' + tq(b.apiBackend))
    if (String(b.reasoningEffort || '').trim()) out.push('reasoning_effort = ' + tq(b.reasoningEffort))
    if (String(b.temperature || '').trim()) out.push('temperature = ' + String(b.temperature).trim())
    if (String(b.topP || '').trim()) out.push('top_p = ' + String(b.topP).trim())
    if (String(b.maxTokens || '').trim()) out.push('max_completion_tokens = ' + String(b.maxTokens).trim())
    const cw = String(b.contextWindow ?? '').trim()
    if (cw) out.push('context_window = ' + cw)
    if (String(b.supportsBackendSearch || '').trim()) out.push('supports_backend_search = ' + String(b.supportsBackendSearch).trim())
    if (String(b.extraHeaders || '').trim()) out.push('extra_headers = ' + String(b.extraHeaders).trim())
  }
  // 非受管段原样保留在末尾（如 MCP 注入写入的 [mcp_servers.*] 与 Grok 自身维护的 [env]/[marketplace] 等）
  for (const blk of grokUnmanaged.value) {
    if (blk && Array.isArray(blk.lines) && blk.lines.length) out.push('', ...blk.lines)
  }
  // 权限设置：[ui] permission_mode（官方仅支持用户级 config.toml）+ [permission] allow/ask/deny 规则。
  // [ui] 内非受管键（memory 等）原样保留；无任何权限内容且无 [ui] 附加键时不输出空段
  const uiLines = grokUiExtras.value.slice()
  for (const [k, v, q] of [
    ['theme', f.uiTheme, true],
    ['simple_mode', f.uiSimpleMode, false],
    ['vim_mode', f.uiVim, false],
    ['scroll_speed', f.uiScrollSpeed, false],
    ['compact_mode', f.uiCompact, false],
    ['show_thinking_blocks', f.uiThinking, false],
    ['max_thoughts_width', f.uiMaxThoughtsWidth, false],
    ['default_selected_permission', f.uiDefaultPerm, true],
    ['remember_tool_approvals', f.uiRememberApprovals, false],
    ['group_tool_verbs', f.uiGroupVerbs, false],
    ['collapsed_edit_blocks', f.uiCollapsedEdit, false],
    ['page_flip_on_send', f.uiPageFlip, false],
    ['screen_mode', f.uiScreenMode, true],
  ]) {
    const s = String(v ?? '').trim()
    if (s) uiLines.push(k + ' = ' + (q ? tq(s) : s))
  }
  const pm = String(f.permissionMode || '').trim()
  if (['ask', 'auto', 'always-approve'].includes(pm)) uiLines.push('permission_mode = ' + tq(pm))
  if (uiLines.length) out.push('', '[ui]', ...uiLines)
  // [cli] / [session]：模版内常用字段受管，其余键原样保留；整段为空时不输出
  const cliLines = grokCliExtras.value.slice()
  const au = String(f.autoUpdate ?? '').trim()
  if (au) cliLines.push('auto_update = ' + au)
  if (cliLines.length) out.push('', '[cli]', ...cliLines)
  const sessionLines = grokSessionExtras.value.slice()
  const act = String(f.autoCompactThreshold ?? '').trim()
  if (act) sessionLines.push('auto_compact_threshold_percent = ' + act)
  if (sessionLines.length) out.push('', '[session]', ...sessionLines)
  // [toolset] / [toolset.hashline]：文件工具集与哈希行配置
  const toolsetLines = grokToolsetExtras.value.slice()
  const fts = String(f.tsFileToolset ?? '').trim()
  if (fts) toolsetLines.push('file_toolset = ' + tq(fts))
  if (toolsetLines.length) out.push('', '[toolset]', ...toolsetLines)
  const hashlineLines = grokHashlineExtras.value.slice()
  for (const [k, v, q] of [['scheme', f.hlScheme, true], ['hash_len', f.hlHashLen, false], ['chunk_size', f.hlChunkSize, false]]) {
    const s = String(v ?? '').trim()
    if (s) hashlineLines.push(k + ' = ' + (q ? tq(s) : s))
  }
  if (hashlineLines.length) out.push('', '[toolset.hashline]', ...hashlineLines)
  // [features]：模版内常用字段受管，其余键原样保留；整段为空时不输出
  const featuresLines = grokFeaturesExtras.value.slice()
  for (const [k, v] of [['telemetry', f.featTelemetry], ['feedback', f.featFeedback], ['lsp_tools', f.featLsp], ['codebase_indexing', f.featIndexing], ['two_pass_compaction', f.featTwoPass], ['remote_fetch', f.featRemoteFetch]]) {
    const s = String(v ?? '').trim()
    if (s) featuresLines.push(k + ' = ' + s)
  }
  if (featuresLines.length) out.push('', '[features]', ...featuresLines)
  // [compat.*]：Claude / Cursor 兼容开关
  const compatClaudeLines = grokCompatClaudeExtras.value.slice()
  const cm1 = String(f.compatClaudeMcps ?? '').trim()
  if (cm1) compatClaudeLines.push('mcps = ' + cm1)
  if (compatClaudeLines.length) out.push('', '[compat.claude]', ...compatClaudeLines)
  const compatCursorLines = grokCompatCursorExtras.value.slice()
  const cm2 = String(f.compatCursorMcps ?? '').trim()
  if (cm2) compatCursorLines.push('mcps = ' + cm2)
  if (compatCursorLines.length) out.push('', '[compat.cursor]', ...compatCursorLines)
  // [permission] rules：来自点选列表（每条规则一行，格式 Tool 或 Tool(pattern)，如 Bash(git *) / Read）
  const ruleLines = []
  for (const row of grokPermRows.value) {
    const act = (row.mode === 'ask' || row.mode === 'deny') ? row.mode : 'allow'
    const line = String(row.text || '').trim()
    if (!line) continue
    const mm = line.match(/^([A-Za-z]+)(?:\((.*)\))?$/)
    if (!mm) continue
    // 生成单行内联表：{ action = "allow", tool = "bash" },
    let r = '{ action = "' + act + '", tool = "' + mm[1].toLowerCase() + '"'
    if (mm[2] !== undefined && String(mm[2]).trim() !== '') r += ', pattern = ' + tq(mm[2].trim())
    ruleLines.push('  ' + r + ' },' )
  }
  if (ruleLines.length) {
    out.push('', '[permission]', 'rules = [', ...ruleLines, ']')
  }
  return out.join('\n')
}

// ---- grok：模型列表按 base_url 实时拉取（600ms 防抖） ----
export const grokBaseModels = ref([])
export const grokFetchLoading = ref(false)
let grokFetchTimer = null
let grokFetchSeq = 0
export function grokFmtToType(fmt) {
  if (fmt === '/aai') return 'anthropic'
  if (fmt === '/gai') return 'gemini'
  return 'openai'
}
export async function fetchGrokBaseModels() {
  const url = derivedBaseUrl(grokForm.value.accessMode)
  if (!url) { grokBaseModels.value = []; grokFetchLoading.value = false; return }
  const seq = ++grokFetchSeq
  grokFetchLoading.value = true
  try {
    const provider = {
      name: 'grok-build',
      type: grokFmtToType(grokForm.value.accessMode),
      baseUrl: url,
      apiKey: String(grokForm.value.apiKey || '').trim(),
    }
    const r = await api.FetchModels(provider)
    if (seq !== grokFetchSeq) return
    if (r && r.ok && Array.isArray(r.models) && r.models.length) grokBaseModels.value = r.models.slice()
  } catch (e) {} finally {
    if (seq === grokFetchSeq) grokFetchLoading.value = false
  }
}
// ---- Grok 模型下拉：拉取列表 + 路由器聚合，首字母分组 ----
export const grokModelOpen = ref(false)
export const grokModelSearch = ref('')
export function grokModelGroups() {
  const s = String(grokModelSearch.value || '').toLowerCase().trim()
  const pool = cfgTarget.value?.id === 'grok-build'
    ? [...new Set([...grokBaseModels.value, ...modelOptions.value])]
    : modelOptions.value
  let list = pool.slice()
  const cur = grokForm.value.model
  if (cur && !list.includes(cur)) list.push(cur) // 已填但不在候选中的模型保留
  list.sort()
  list = list.filter(m => !s || String(m).toLowerCase().includes(s))
  const byLetter = {}
  for (const m of list) {
    const L = (String(m).charAt(0) || '#').toUpperCase()
    ;(byLetter[L] = byLetter[L] || []).push(m)
  }
  return Object.keys(byLetter).sort().map(l => ({ letter: l, items: byLetter[l] }))
}
export function pickGrokModel(m) {
  grokForm.value.model = m
  grokModelOpen.value = false
  grokModelSearch.value = ''
}
// [models] default / web_search 下拉候选：现有块 + 接入方式返回的模型列表（base_url 拉取 + 路由器聚合）
export function grokDefaultCandidates() {
  const pool = [...new Set([...grokOrder.value, ...grokBaseModels.value, ...modelOptions.value, ...(grokForm.value.webSearch ? [grokForm.value.webSearch] : [])])]
  const cur = grokForm.value.default
  if (cur && !pool.includes(cur)) pool.push(cur)
  return pool.sort()
}
// 选中默认模型：若该模型尚无块则自动新建（default 始终指向真实存在的块），并回填上游模型 id
export function pickGrokDefault(m) {
  grokForm.value.default = m
  if (!m) return
  if (!grokOrder.value.includes(m)) addGrokModel(m)
  if (!grokForm.value.model) grokForm.value.model = m
}
// 选中 web_search 模型：无对应块时静默补建（不切换当前编辑块），使 web_search 始终指向存在的块
export function pickGrokWebSearch(m) {
  grokForm.value.webSearch = m
  if (!m) return
  if (m in grokBlocks.value) return
  grokBlocks.value[m] = { model: m }
  grokOrder.value.push(m)
  if (!grokForm.value.default) grokForm.value.default = m
}
export function grokModelKeyNav(e) {
  if (!/^[a-zA-Z]$/.test(e.key)) return
  const drop = e.currentTarget.closest('.pi-sel')?.querySelector('.pi-sel-drop')
  if (!drop) return
  const target = drop.querySelector('[data-letter="' + e.key.toUpperCase() + '"]')
  if (target) { target.scrollIntoView({ block: 'start' }); e.preventDefault() }
}

// watchers 在 init 中注册（组件 setup 上下文内，随组件生命周期清理）
export function initGrok() {
  // 表单 → 模版 TOML 实时联动：填写面板时，config.toml 源码文本框实时更新
  watch(grokForm, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'grok-build' || grokMuted) return
    if (cfgFormat.value !== 'toml') return
    try { cfgContent.value = grokBuildToml() } catch (e) {}
  }, { deep: true })
  // 权限规则（三态点选）与各段 extras 变更同样实时回写源码
  watch([grokPermRows, grokUiExtras, grokCliExtras, grokSessionExtras, grokToolsetExtras, grokHashlineExtras, grokFeaturesExtras, grokCompatClaudeExtras, grokCompatCursorExtras], () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'grok-build' || grokMuted) return
    if (cfgFormat.value !== 'toml') return
    try { cfgContent.value = grokBuildToml() } catch (e) {}
  }, { deep: true })
  // 模型列表按 base_url 实时拉取（600ms 防抖）
  watch(
    () => [grokForm.value.accessMode, grokForm.value.apiKey],
    () => {
      if (!cfgEdit.value || cfgTarget.value?.id !== 'grok-build') return
      clearTimeout(grokFetchTimer)
      grokFetchTimer = setTimeout(fetchGrokBaseModels, 600)
    }
  )
}