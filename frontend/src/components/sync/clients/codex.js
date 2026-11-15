// Codex（~/.codex/config.toml，固定 /oai 接入，TOML 结构化表单）
// 受管键：model、model_provider、model_reasoning_effort、model_temperature、model_top_p、
// approval_policy（untrusted / on-request / never，on-failure 已弃用）、
// [model_providers.<key>] 的 name/base_url/wire_api、可选的重试与超时三键、二选一的鉴权
// （api_key 明文 → [model_providers.<key>.http_headers] Authorization；api_key 留空且 env_key 非空
// → env_key = <环境变量名>）、[permissions] 审批规则段（allow/deny/ask/disable 数组）、
// 以及 [tui] 外观段（theme/animations/show_tooltips/alternate_screen/notification_method/
// notifications/status_line；留空键不写入，[tui.keymap.*] 等子节原样保留）。
// 其余内容（[projects]、[windows] 等用户自留节）原样保留。base_url 自动取路由器地址。
import { ref, watch } from 'vue'
import { toast } from '../../../toast'
import { t } from '../../../i18n'
import api from '../../../api'
import { permRowsFromLists, permRowsToLists, permToggleRule, permRuleOn, tq } from '../shared'
import { cfgContent, cfgEdit, cfgTarget, cfgFormat, derivedBaseUrl, modelOptions } from '../store'

export const codexAgentsContent = ref('')
export const codexAgentsExists = ref(false)
export const codexAgentsFile = ref('~/.codex/AGENTS.md')

export const codexForm = ref({ model: '', reasoning: '', temperature: '', topP: '', apiKey: '', providerKey: '', name: '', wireApi: 'responses', envKey: '', reqRetry: '', streamRetry: '', idleTimeout: '', approvalPolicy: '', permDisable: '', tuiTheme: '', tuiAnim: '', tuiTooltips: '', tuiAltScreen: '', tuiNotifyMethod: '', tuiNotifications: '', tuiStatusLine: '', sandboxMode: '', wwNetwork: '', modelVerbosity: '', reasoningSummary: '', reviewModel: '', contextWindow: '', autoCompact: '', devInstructions: '', webSearch: '', notify: '', fileOpener: '', hideReasoning: '', showRawReasoning: '', noPasteBurst: '', histPersistence: '', histMaxBytes: '', disableResponseStorage: '', includeApplyPatch: '', preferredAuth: '', projectDocMaxBytes: '', projectDocFallback: '' })
export const codexPermRows = ref([]) // [{ text, mode }] 权限规则点选列表（allow/ask/deny 统一点选，disable 单独保留）
// Codex 权限规则快选（与 CommandCode 同款胶囊下拉）：allow/ask/deny 三个文本域（disable 语义不同，保留纯文本框）
export const codexQuickOpen = ref('')
export const codexQuickFocus = ref('')
export function codexToggleRule(field, rule) { codexForm.value[field] = permToggleRule(codexForm.value[field], rule) }
export function codexRuleOn(field, rule) { return permRuleOn(codexForm.value[field], rule) }
// 「所有命令」三态：以裸工具名 Bash 作通配规则落入 [permissions] 的 allow/ask/deny
// （与 CodeBuddy 同款交互；面板不再提供逐条规则编辑，磁盘既有规则行经 codexPermRows 原样保留写回）
export const CX_CMD_RULE = 'Bash'
export function codexCmdMode() {
  const r = (codexPermRows.value || []).find(x => String(x.text || '').trim() === CX_CMD_RULE)
  return r ? r.mode : ''
}
export function codexCmdSet(mode) {
  const rows = (codexPermRows.value || []).slice()
  const i = rows.findIndex(x => String(x.text || '').trim() === CX_CMD_RULE)
  if (i >= 0) {
    if (rows[i].mode === mode) rows.splice(i, 1) // 再点一次当前档取消
    else rows[i] = { ...rows[i], mode }
  } else if (mode) {
    rows.push({ text: CX_CMD_RULE, mode })
  }
  codexPermRows.value = rows
}
// Codex 内置保留 provider ID，不能用作自定义 model_providers 键（会被 Codex 忽略）
export const CODEX_RESERVED_PROVIDERS = ['openai', 'ollama', 'lmstudio']
export const codexModelOpen = ref(false)

// ---- 模型映射：codex 内置客户端模型 → 路由器模型（写启用服务商的 aliases）----
// 值所见即所得：路由器返回/配置里存的是什么模型名称，文本框就显示什么、保存什么，
// 前端不做任何增删 @后缀 的加工（聚合列表对跨服务商重名模型带 @服务商 去重后缀，
// 那是路由器的命名规则，原样尊重）。行内 prov 仅记录别名键的存储位置（路由由
// 归属服务商决定），不进 UI、不影响显示值。
export const CODEX_BUILTIN_MODELS = ['gpt-5-codex', 'gpt-5.2', 'gpt-5.5', 'gpt-5.6-luna', 'gpt-5.6-terra', 'gpt-6-astra']
export const codexMapRows = ref([])   // [{k, v, prov}] k=内置 ID（只读），v=模型名原样（''=不映射），prov=存储位置
export const codexMapLoaded = ref(false)
export const codexMapOpen = ref(-1)   // 当前展开目标下拉的行下标（-1 = 全收起）
let codexProvHints = []               // 载入时缓存的启用服务商 [{id,name}]，供点选候选时识别 @去重后缀（仅归属元数据）
const eqId = (a, b) => String(a || '').toLowerCase() === String(b || '').toLowerCase()

export async function loadCodexMappings() {
  try {
    const cfg = await api.GetConfig()
    const rows = CODEX_BUILTIN_MODELS.map(k => ({ k, v: '', prov: '' }))
    const enabled = (cfg?.providers || []).filter(p => p.enabled !== false)
    codexProvHints = enabled.map(p => ({ id: p.id || '', name: p.name || '' }))
    for (const p of enabled) {
      const pk = p.id || p.name
      for (const [k, v] of Object.entries(p.aliases || {})) {
        const row = rows.find(r => r.k === k)
        if (!row || row.v) continue // 同名别名多服务商时取先出现的
        row.v = String(v)           // 原样显示，不做任何加工
        row.prov = pk
      }
    }
    codexMapRows.value = rows
    codexMapLoaded.value = true
  } catch (e) { codexMapLoaded.value = false }
}
export function codexMapSetVal(i, v) {
  const rows = codexMapRows.value.slice()
  if (rows[i]) rows[i] = { ...rows[i], v: String(v || '').trim() }
  codexMapRows.value = rows
}
// 映射值下拉：打开时展示全量模型列表（loadModelOptions → GetModelsByFormat，路由器侧聚合），
// 输入即时过滤；候选池是裸模型名，故搜索串不带 @服务商 后缀。
export const codexMapSearch = ref('')
export function codexMapFocusRow(i) {
  codexMapOpen.value = i
  codexMapSearch.value = ''
}
// 候选分组：路由器聚合模型列表剔除内置 ID（避免「内置 ID 映射到内置 ID」的循环别名），
// 按搜索词过滤、首字母分组（agy 同规则）
export function codexMapGroups() {
  const s = codexMapSearch.value.toLowerCase().trim()
  const builtin = new Set(CODEX_BUILTIN_MODELS)
  const list = (modelOptions.value || []).slice().sort().filter(m => {
    const id = String(m)
    if (s && !id.toLowerCase().includes(s)) return false
    const at = id.lastIndexOf('@')
    const clean = at > 0 ? id.slice(0, at) : id
    return !builtin.has(id) && !builtin.has(clean)
  })
  const byLetter = {}
  for (const m of list) {
    const L = (String(m).charAt(0) || '#').toUpperCase()
    ;(byLetter[L] = byLetter[L] || []).push(m)
  }
  return Object.keys(byLetter).sort().map(l => ({ letter: l, items: byLetter[l] }))
}
// 选中候选：路由器返回什么名称就原样填入文本框（不做后缀加工）。若候选带 @服务商
// 去重后缀（聚合列表对重名模型的命名），仅把行归属元数据切到该服务商（隐藏字段，
// 决定别名键写到谁名下），显示值保持原样。
export function codexMapPick(i, m) {
  const rows = codexMapRows.value.slice()
  if (!rows[i]) return
  const v = String(m || '').trim()
  const at = v.lastIndexOf('@')
  if (at > 0) {
    const h = v.slice(at + 1).trim()
    if (h && !/^\d/.test(h) && codexProvHints.some(p => eqId(p.id, h) || eqId(p.name, h))) {
      rows[i] = { ...rows[i], v, prov: h }
      codexMapRows.value = rows
      codexMapOpen.value = -1
      return
    }
  }
  rows[i] = { ...rows[i], v }
  codexMapRows.value = rows
  codexMapOpen.value = -1
}
// 保存映射：值所见即所得，原样落盘；归属定位：行内 prov（载自磁盘/点选指定）>
// 模型命中（@段前的模型部分参与命中，默认服务商优先）> 默认服务商 > 唯一启用。
// 留空 = 移除该内置 ID 别名；非内置 ID 的既有别名行原样保留。返回错误消息（'' = 成功）。
export async function applyCodexMappings() {
  if (!codexMapLoaded.value) return ''
  try {
    const cfg = JSON.parse(JSON.stringify(await api.GetConfig() || {}))
    const providers = cfg.providers || []
    const enabled = providers.filter(p => p.enabled !== false)
    const builtin = new Set(CODEX_BUILTIN_MODELS)
    const resolveProv = (r, v) => {
      let prov = r.prov ? enabled.find(p => eqId(p.id, r.prov) || eqId(p.name, r.prov)) : null
      if (!prov) {
        const at = v.lastIndexOf('@')
        const part = at > 0 ? v.slice(0, at).trim() : v // @段是聚合去重命名，命中检测用前段
        const hits = enabled.filter(p => (p.models || []).some(m => eqId(m, part)) ||
          Object.keys(p.aliases || {}).some(a => eqId(a, part)) || Object.values(p.aliases || {}).some(x => eqId(x, part)))
        prov = hits.find(p => eqId(p.id, cfg.defaultProvider) || eqId(p.name, cfg.defaultProvider)) || hits[0] || null
      }
      if (!prov) prov = enabled.find(p => eqId(p.id, cfg.defaultProvider) || eqId(p.name, cfg.defaultProvider)) ||
        (enabled.length === 1 ? enabled[0] : null)
      return prov
    }
    const assigned = {} // pk -> { 内置ID: 原样值 }
    for (const r of codexMapRows.value) {
      const k = String(r.k || '').trim()
      const v = String(r.v || '').trim()
      if (!k || !v) continue // 留空 = 不映射
      const prov = resolveProv(r, v)
      if (!prov) return t('sync.codex.mapProviderReq')
      const pk = prov.id || prov.name
      ;(assigned[pk] = assigned[pk] || {})[k] = v
    }
    for (const p of providers) {
      if (p.enabled === false) continue // 未启用服务商原样不动
      const pk = p.id || p.name
      const kept = {}
      for (const [k, v] of Object.entries(p.aliases || {})) {
        if (!builtin.has(k)) kept[k] = v // 非本客户端内置 ID 的别名行原样保留
      }
      p.aliases = { ...kept, ...(assigned[pk] || {}) } // 内置 ID 全量重写：留空即移除
    }
    const r = await api.SaveConfig(cfg)
    if (r && r.ok) return ''
    return (r && r.error) || '?'
  } catch (e) { return e?.message || '?' }
}
export let codexMuted = false
export function setCodexMuted(v) { codexMuted = v }
export function codexTomlQuote(v) { return '"' + String(v ?? '').replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"' }
// 从 TOML 数组字面量中提取全部引号字符串（支持多行数组），每项一行
function parseTomlArray(v) {
  const s = String(v ?? '')
  const m = s.match(/\[(.*)\]/s)
  if (!m) return ''
  const items = []
  const re = /"((?:[^"\\]|\\.)*)"|'([^']*)'/g
  let mm
  while ((mm = re.exec(m[1]))) items.push(mm[1] != null ? mm[1] : mm[2])
  return items.join('\n')
}
const capKey = k => k.charAt(0).toUpperCase() + k.slice(1)
export function codexInitFrom(text) {
  const f = { model: '', reasoning: '', temperature: '', topP: '', apiKey: '', providerKey: '', name: '', wireApi: 'responses', envKey: '', reqRetry: '', streamRetry: '', idleTimeout: '', approvalPolicy: '', permDisable: '', tuiTheme: '', tuiAnim: '', tuiTooltips: '', tuiAltScreen: '', tuiNotifyMethod: '', tuiNotifications: '', tuiStatusLine: '', sandboxMode: '', wwNetwork: '', modelVerbosity: '', reasoningSummary: '', reviewModel: '', contextWindow: '', autoCompact: '', devInstructions: '', webSearch: '', notify: '', fileOpener: '', hideReasoning: '', showRawReasoning: '', noPasteBurst: '', histPersistence: '', histMaxBytes: '', disableResponseStorage: '', includeApplyPatch: '', preferredAuth: '', projectDocMaxBytes: '', projectDocFallback: '' }
  let inProv = false
  let inHeaders = false
  let inPerm = false
  let inTui = false
  let inHist = false
  let inSww = false
  let curPermKey = ''
  let curPermBuf = ''
  let curTuiKey = ''
  let curTuiBuf = ''
  const PERM_KEYS = ['allow', 'deny', 'ask', 'disable']
  const TUI_ARRAY_KEYS = ['notifications', 'status_line']
  const permAcc = { allow: [], ask: [], deny: [] }
  for (const ln of String(text || '').split(/\r?\n/)) {
    const t = ln.trim()
    if (curTuiKey) { // [tui] 下多行数组字面量缓冲
      curTuiBuf += t
      if (t.includes(']')) {
        const raw = String(parseTomlArray(curTuiBuf) || '')
        if (curTuiKey === 'notifications') f.tuiNotifications = raw
        else if (curTuiKey === 'status_line') f.tuiStatusLine = raw
        curTuiKey = ''
        curTuiBuf = ''
      }
      continue
    }
    if (curPermKey) {
      curPermBuf += t
      if (t.includes(']')) {
        const raw = String(parseTomlArray(curPermBuf) || '')
        if (curPermKey === 'disable') f.permDisable = raw
        else if (permAcc[curPermKey]) permAcc[curPermKey] = raw.split('\n').map(s => s.trim()).filter(Boolean)
        curPermKey = ''
        curPermBuf = ''
      }
      continue
    }
    if (/^\[permissions\]$/.test(t)) { inPerm = true; inProv = false; inHeaders = false; inTui = false; inHist = false; inSww = false; continue }
    if (/^\[tui\]$/.test(t)) { inTui = true; inProv = false; inHeaders = false; inPerm = false; inHist = false; inSww = false; continue } // [tui.keymap.*] 等子节走通用头部逻辑（不接管）
    if (/^\[history\]$/.test(t)) { inHist = true; inProv = false; inHeaders = false; inPerm = false; inTui = false; inSww = false; continue }
    if (/^\[sandbox_workspace_write\]$/.test(t)) { inSww = true; inProv = false; inHeaders = false; inPerm = false; inTui = false; inHist = false; continue }
    if (/^\[model_providers\.[^\]]*\.http_headers\]$/.test(t)) { inHeaders = true; inTui = false; inHist = false; inSww = false; continue }
    const h = t.match(/^\[model_providers\.(.+?)\]$/)
    if (h) { inProv = true; inHeaders = false; inPerm = false; inTui = false; inHist = false; inSww = false; f.providerKey = h[1].replace(/^"|"$/g, ''); continue }
    if (/^\[/.test(t)) { inProv = false; inHeaders = false; inPerm = false; inTui = false; inHist = false; inSww = false; continue }
    const kv = t.match(/^([A-Za-z0-9_.-]+)\s*=\s*(.+)$/)
    if (!kv) continue
    const val = kv[2].trim().replace(/^"(.*)"$/, '$1')
    if (!inProv && kv[1] === 'model') f.model = val
    else if (!inProv && kv[1] === 'model_reasoning_effort') f.reasoning = val
    else if (!inProv && kv[1] === 'model_temperature') f.temperature = val
    else if (!inProv && kv[1] === 'model_top_p') f.topP = val
    else if (!inProv && kv[1] === 'approval_policy') f.approvalPolicy = val
    else if (inHeaders && kv[1] === 'Authorization') f.apiKey = val.replace(/^Bearer\s+/i, '')
    else if (inProv && kv[1] === 'name') f.name = val
    else if (inProv && kv[1] === 'wire_api') f.wireApi = val || 'responses'
    else if (inProv && kv[1] === 'env_key') f.envKey = val
    else if (inProv && kv[1] === 'request_max_retries') f.reqRetry = val
    else if (inProv && kv[1] === 'stream_max_retries') f.streamRetry = val
    else if (inProv && kv[1] === 'stream_idle_timeout_ms') f.idleTimeout = val
    else if (inPerm && PERM_KEYS.includes(kv[1])) {
      if (val.includes('[') && !val.includes(']')) { curPermKey = kv[1]; curPermBuf = val }
      else {
        const raw = String(parseTomlArray(val) || '')
        if (kv[1] === 'disable') f.permDisable = raw
        else if (permAcc[kv[1]]) permAcc[kv[1]] = raw.split('\n').map(s => s.trim()).filter(Boolean)
      }
    }
    else if (inTui && TUI_ARRAY_KEYS.includes(kv[1])) {
      // notifications：bool 或事件类型数组；status_line：项数组或 null（禁用）
      if (val.includes('[') && !val.includes(']')) { curTuiKey = kv[1]; curTuiBuf = val }
      else if (kv[1] === 'status_line' && val.toLowerCase() === 'null') f.tuiStatusLine = 'null'
      else if (kv[1] === 'notifications' && (val === 'true' || val === 'false')) f.tuiNotifications = val
      else {
        const raw = String(parseTomlArray(val) || '')
        if (kv[1] === 'notifications') f.tuiNotifications = raw
        else f.tuiStatusLine = raw
      }
    }
    else if (inTui && kv[1] === 'theme') f.tuiTheme = val
    else if (inTui && kv[1] === 'animations') f.tuiAnim = val
    else if (inTui && kv[1] === 'show_tooltips') f.tuiTooltips = val
    else if (inTui && kv[1] === 'alternate_screen') f.tuiAltScreen = val
    else if (inTui && kv[1] === 'notification_method') f.tuiNotifyMethod = val
    else if (inHist && kv[1] === 'persistence') f.histPersistence = val
    else if (inHist && kv[1] === 'max_bytes') f.histMaxBytes = val
    else if (inSww && kv[1] === 'network_access') f.wwNetwork = val
    else if (!inProv && kv[1] === 'sandbox_mode') f.sandboxMode = val
    else if (!inProv && kv[1] === 'model_verbosity') f.modelVerbosity = val
    else if (!inProv && kv[1] === 'model_reasoning_summary') f.reasoningSummary = val
    else if (!inProv && kv[1] === 'review_model') f.reviewModel = val
    else if (!inProv && kv[1] === 'model_context_window') f.contextWindow = val
    else if (!inProv && kv[1] === 'model_auto_compact_token_limit') f.autoCompact = val
    else if (!inProv && kv[1] === 'developer_instructions') f.devInstructions = val
    else if (!inProv && kv[1] === 'web_search') f.webSearch = val
    else if (!inProv && kv[1] === 'notify') f.notify = String(parseTomlArray(val) || val)
    else if (!inProv && kv[1] === 'file_opener') f.fileOpener = val
    else if (!inProv && kv[1] === 'hide_agent_reasoning') f.hideReasoning = val
    else if (!inProv && kv[1] === 'show_raw_agent_reasoning') f.showRawReasoning = val
    else if (!inProv && kv[1] === 'disable_paste_burst') f.noPasteBurst = val
    else if (!inProv && kv[1] === 'disable_response_storage') f.disableResponseStorage = val
    else if (!inProv && kv[1] === 'include_apply_patch_tool') f.includeApplyPatch = val
    else if (!inProv && kv[1] === 'preferred_auth_method') f.preferredAuth = val
    else if (!inProv && kv[1] === 'project_doc_max_bytes') f.projectDocMaxBytes = val
    else if (!inProv && kv[1] === 'project_doc_fallback_filenames') f.projectDocFallback = String(parseTomlArray(val) || val)
  }
  codexPermRows.value = permRowsFromLists(permAcc)
  codexForm.value = f
  return f
}
export function codexWriteToml(text) {
  const f = codexForm.value
  const key = (f.providerKey || '').trim() || 'llmrouter'
  const lines = String(text || '').split('\n')
  const lead = []   // 头部注释/空行：受管块插在它们之后
  const rest = []   // 其余未受管内容
  let inLead = true
  let inProv = false
  let inPerm = false
  let inTui = false
  let inHist = false
  let inSww = false
  let justCut = false
  for (const ln of lines) {
    const t = ln.trim()
    if (inLead && (t === '' || t.startsWith('#'))) { lead.push(ln); continue }
    inLead = false
    if (/^\[model_providers\.[^\]]+\]$/.test(t)) { inProv = true; justCut = true; continue } // 旧受管节：剔除
    if (inProv) {
      if (!/^\[/.test(t)) continue // 节内受管行：剔除（不清 justCut，便于吃掉随后的孤立空行）
      inProv = false               // 新节开始：该行落到通用逻辑
    }
    if (/^\[permissions\]$/.test(t)) { inPerm = true; justCut = true; continue } // 旧受管权限段：剔除
    if (inPerm) {
      if (!/^\[/.test(t)) continue
      inPerm = false
    }
    if (/^\[tui\]$/.test(t)) { inTui = true; justCut = true; continue } // 旧受管 [tui] 段：剔除（[tui.keymap.*] 等子节不在此列，原样保留）
    if (inTui) {
      if (!/^\[/.test(t)) continue
      inTui = false
    }
    if (/^\[history\]$/.test(t)) { inHist = true; justCut = true; continue } // 旧受管 [history] 段：剔除
    if (inHist) {
      if (!/^\[/.test(t)) continue
      inHist = false
    }
    if (/^\[sandbox_workspace_write\]$/.test(t)) { inSww = true; justCut = true; continue } // 旧受管沙箱子段：剔除
    if (inSww) {
      if (!/^\[/.test(t)) continue
      inSww = false
    }
    if (/^\[/.test(t)) { justCut = false; rest.push(ln); continue }
    if (!/^\[/.test(t) && /^(model|model_provider|model_reasoning_effort|model_temperature|model_top_p|approval_policy|sandbox_mode|model_verbosity|model_reasoning_summary|review_model|model_context_window|model_auto_compact_token_limit|developer_instructions|web_search|notify|file_opener|hide_agent_reasoning|show_raw_agent_reasoning|disable_paste_burst|disable_response_storage|include_apply_patch_tool|preferred_auth_method|project_doc_max_bytes|project_doc_fallback_filenames)\s*=/.test(t)) { justCut = true; continue } // 旧受管键：剔除
    if (t === '' && justCut) { justCut = false; continue } // 吞掉被删内容后的孤立空行，保证往返稳定
    justCut = false
    rest.push(ln)
  }
  const block = [
    'model = ' + codexTomlQuote(f.model),
    'model_provider = ' + codexTomlQuote(key),
    ...((f.reasoning || '').trim() ? ['model_reasoning_effort = ' + codexTomlQuote(f.reasoning.trim())] : []),
    ...((f.approvalPolicy || '').trim() ? ['approval_policy = ' + codexTomlQuote(f.approvalPolicy.trim())] : []),
  ]
  // 可选采样参数：留空或非数字不写入（TOML 浮点/整数，不加引号）
  const numFloat = v => (/^\d+(\.\d+)?$/.test(String(v ?? '').trim()) ? String(v).trim() : '')
  const tm = numFloat(f.temperature), tp = numFloat(f.topP)
  if (tm) block.push('model_temperature = ' + tm)
  if (tp) block.push('model_top_p = ' + tp)
  // 受管可选键：留空不写入（bool 用裸字面量、整数不加引号，其余按 TOML 字符串）
  const bstr = v => (v === 'true' || v === 'false' ? v : '')
  const intStr = v => (/^\d+$/.test(String(v ?? '').trim()) ? String(v).trim() : '')
  if ((f.sandboxMode || '').trim()) block.push('sandbox_mode = ' + codexTomlQuote(f.sandboxMode.trim()))
  if ((f.modelVerbosity || '').trim()) block.push('model_verbosity = ' + codexTomlQuote(f.modelVerbosity.trim()))
  if ((f.reasoningSummary || '').trim()) block.push('model_reasoning_summary = ' + codexTomlQuote(f.reasoningSummary.trim()))
  if ((f.reviewModel || '').trim()) block.push('review_model = ' + codexTomlQuote(f.reviewModel.trim()))
  const ctxWin = intStr(f.contextWindow)
  if (ctxWin) block.push('model_context_window = ' + ctxWin)
  const autoCp = intStr(f.autoCompact)
  if (autoCp) block.push('model_auto_compact_token_limit = ' + autoCp)
  if ((f.devInstructions || '').trim()) block.push('developer_instructions = ' + codexTomlQuote(f.devInstructions.trim()))
  const webSearch = bstr((f.webSearch || '').trim())
  if (webSearch) block.push('web_search = ' + webSearch)
  {
    const items = String(f.notify || '').split(/[,\n]/).map(s => s.trim()).filter(Boolean)
    if (items.length) block.push('notify = [' + items.map(i => codexTomlQuote(i)).join(', ') + ']')
  }
  if ((f.fileOpener || '').trim()) block.push('file_opener = ' + codexTomlQuote(f.fileOpener.trim()))
  const hideR = bstr((f.hideReasoning || '').trim())
  if (hideR) block.push('hide_agent_reasoning = ' + hideR)
  const rawR = bstr((f.showRawReasoning || '').trim())
  if (rawR) block.push('show_raw_agent_reasoning = ' + rawR)
  const noPb = bstr((f.noPasteBurst || '').trim())
  if (noPb) block.push('disable_paste_burst = ' + noPb)
  // 官方顶层键：关闭响应存储（第三方/API Key 场景）、内置补丁工具、首选鉴权方式、AGENTS.md 载入策略
  const drs = bstr((f.disableResponseStorage || '').trim())
  if (drs) block.push('disable_response_storage = ' + drs)
  const iap = bstr((f.includeApplyPatch || '').trim())
  if (iap) block.push('include_apply_patch_tool = ' + iap)
  if ((f.preferredAuth || '').trim()) block.push('preferred_auth_method = ' + codexTomlQuote(f.preferredAuth.trim()))
  const pdm = intStr(f.projectDocMaxBytes)
  if (pdm) block.push('project_doc_max_bytes = ' + pdm)
  {
    const items = String(f.projectDocFallback || '').split(/[,\n]/).map(s => s.trim()).filter(Boolean)
    if (items.length) block.push('project_doc_fallback_filenames = [' + items.map(i => codexTomlQuote(i)).join(', ') + ']')
  }
  block.push(
    '',
    '[model_providers.' + key + ']',
    'name = ' + codexTomlQuote(f.name),
    'base_url = ' + codexTomlQuote(derivedBaseUrl()),
    // wire_api：新版 Codex 已移除 chat 支持，固定写 responses
    'wire_api = "responses"',
  )
  // 可选的重试与超时三键：留空或非纯数字不写入（TOML 整数，不加引号）
  const numOrEmpty = v => (/^\d+$/.test(String(v ?? '').trim()) ? String(v).trim() : '')
  const rr = numOrEmpty(f.reqRetry), sr = numOrEmpty(f.streamRetry), it = numOrEmpty(f.idleTimeout)
  if (rr) block.push('request_max_retries = ' + rr)
  if (sr) block.push('stream_max_retries = ' + sr)
  if (it) block.push('stream_idle_timeout_ms = ' + it)
  const ak = (f.apiKey || '').trim()
  if (ak) {
    // 密钥明文直写配置文件（http_headers 静态头），优先于 env_key
    block.push('', '[model_providers.' + key + '.http_headers]', 'Authorization = ' + codexTomlQuote('Bearer ' + ak))
  } else if ((f.envKey || '').trim()) {
    block.push('env_key = ' + codexTomlQuote(f.envKey.trim()))
  }
  // [permissions] 审批规则段：allow/ask/deny 来自点选列表按类别分组；disable 单独保留（语义不同）
  const permLines = []
  const lists = permRowsToLists(codexPermRows.value)
  for (const k of ['allow', 'ask', 'deny']) {
    const items = lists[k]
    if (items.length) permLines.push(k + ' = [' + items.map(i => codexTomlQuote(i)).join(', ') + ']')
  }
  {
    const items = String(f.permDisable || '').split('\n').map(s => s.trim()).filter(Boolean)
    if (items.length) permLines.push('disable = [' + items.map(i => codexTomlQuote(i)).join(', ') + ']')
  }
  if (permLines.length) block.push('', '[permissions]', ...permLines)
  // [tui] 外观段：留空键不写入；notifications 支持 true/false 或逗号分隔事件类型；
  // status_line 支持逗号分隔项或字面 null（禁用状态栏）
  const tuiLines = []
  if ((f.tuiTheme || '').trim()) tuiLines.push('theme = ' + codexTomlQuote(f.tuiTheme.trim()))
  const tuiBool = v => (v === 'true' || v === 'false' ? v : '')
  const tuiAnim = tuiBool((f.tuiAnim || '').trim())
  if (tuiAnim) tuiLines.push('animations = ' + tuiAnim)
  const tuiTips = tuiBool((f.tuiTooltips || '').trim())
  if (tuiTips) tuiLines.push('show_tooltips = ' + tuiTips)
  if ((f.tuiAltScreen || '').trim()) tuiLines.push('alternate_screen = ' + codexTomlQuote(f.tuiAltScreen.trim()))
  if ((f.tuiNotifyMethod || '').trim()) tuiLines.push('notification_method = ' + codexTomlQuote(f.tuiNotifyMethod.trim()))
  {
    const n = (f.tuiNotifications || '').trim()
    if (n === 'true' || n === 'false') tuiLines.push('notifications = ' + n)
    else if (n) {
      const items = n.split(/[,\n]/).map(s => s.trim()).filter(Boolean)
      if (items.length) tuiLines.push('notifications = [' + items.map(i => codexTomlQuote(i)).join(', ') + ']')
    }
  }
  {
    const sl = (f.tuiStatusLine || '').trim()
    if (sl.toLowerCase() === 'null') tuiLines.push('status_line = null')
    else if (sl) {
      const items = sl.split(/[,\n]/).map(s => s.trim()).filter(Boolean)
      if (items.length) tuiLines.push('status_line = [' + items.map(i => codexTomlQuote(i)).join(', ') + ']')
    }
  }
  if (tuiLines.length) block.push('', '[tui]', ...tuiLines)
  // [history] 会话历史持久化
  {
    const histLines = []
    if ((f.histPersistence || '').trim()) histLines.push('persistence = ' + codexTomlQuote(f.histPersistence.trim()))
    const hmb = intStr(f.histMaxBytes)
    if (hmb) histLines.push('max_bytes = ' + hmb)
    if (histLines.length) block.push('', '[history]', ...histLines)
  }
  // [sandbox_workspace_write] 沙箱写目录的联网开关（配合 sandbox_mode = workspace-write）
  {
    const na = bstr((f.wwNetwork || '').trim())
    if (na) block.push('', '[sandbox_workspace_write]', 'network_access = ' + na)
  }
  const outLines = [...lead, ...block]
  if (rest.length && rest[0].trim() !== '') outLines.push('') // 受管块与后续内容空一行分隔
  outLines.push(...rest)
  let s = outLines.join('\n')
  if (!s.endsWith('\n')) s += '\n'
  return s
}
export function codexTomlPreset() {
  return `# ~/.codex/config.toml
# Codex CLI 参考模版：模型提供商指向本路由器

[model_providers.llmrouter]
name = "LLM Router"
base_url = "` + derivedBaseUrl() + `"
wire_api = "responses"
`
}

// watchers 在 init 中注册（组件 setup 上下文内）
export function initCodex() {
  watch(codexForm, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'codex' || codexMuted) return
    if (cfgFormat.value !== 'toml') return
    try { cfgContent.value = codexWriteToml(cfgContent.value) } catch (e) {}
  }, { deep: true })
}