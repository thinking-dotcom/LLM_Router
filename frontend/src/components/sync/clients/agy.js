// Antigravity CLI（agy，~/.gemini/antigravity-cli/settings.json，固定 /oai 接入）：
// settings.json 表单 + JSON 源码。字段与 agent/Json_modles/agy/settings.json 参考模版对应：
//   modelProvider / baseUrl / apiKey / model / models[] / systemInstruction
//   parameters{ temperature, maxOutputTokens, topP, topK }
//   permissions{ toolPermission, allow, deny }
//   enableTerminalSandbox / allowNonWorkspaceAccess / useG1Credits / enableTelemetry
//   colorScheme / altScreenMode / runningLightSpeed / verbosity / editor
//   subagents{ enabled, maxSteps, autoExecuteTools }
//
// 接入本路由器靠两个用户级环境变量（agy 官方契约，settings.json 的 baseUrl/apiKey 字段
// 不被 CLI 读取）：
//   GOOGLE_GEMINI_BASE_URL → 本路由器接入地址 /router（agy 自己会追加 /v1beta/models/...）
//   GEMINI_API_KEY          → 路由令牌（modelProvider 为 gemini 时必需，否则 CLI 无法启动）
// settings.json 的 modelProvider 保持 "gemini" 即切换到 API-key 认证模式。
import { ref, watch } from 'vue'
import { cfgObj, cfgEdit, cfgTarget, jsonEcho, syncToJson, routerBase } from '../store'
import { toast } from '../../../toast'
import { t } from '../../../i18n'
import api from '../../../api'

export const agyMcpContent = ref('') // ~/.gemini/config/mcp_config.json 源码
export const agyMcpExists = ref(false)
export const agyMcpFile = ref('~/.gemini/config/mcp_config.json')

// ---- agy 模型映射面板：把 agy 发出的内置模型 ID 定点映射到上游真实模型 ----
// 不选服务商：aliases 文本每行 客户端名 = 上游名；保存时按「目标模型」自动解析
// 目标模型所属的已启用服务商（模型名 / 已有别名匹配），无匹配则退回默认服务商。
export const agyAliasText = ref('')
export const agyAliasLoaded = ref(false) // 面板已成功从磁盘载入 aliases：false 时「保存」跳过映射回写，防止误清
export const agyAliasProviderList = ref([]) // [{id,name}]（已启用服务商，载入面板时解析用，不展示）
export const agyAliasMap = ref({}) // { 内置ID: 路由器模型 }：每个内置 ID 一行映射文本框
// agyAliasText → 各内置 ID 的映射值（非内置 ID 的历史行忽略；值含 @服务商 后缀时原样回显，保存可无损回写）
export function agySyncTextToMap() {
  const map = {}
  for (const m of agyBuiltinModels.value) map[m] = ''
  for (const line of String(agyAliasText.value || '').split('\n')) {
    const sep = ['=', '→', '->'].find(s => line.includes(s))
    if (!sep) continue
    const idx = line.indexOf(sep)
    const k = line.slice(0, idx).trim()
    if (k in map) map[k] = line.slice(idx + sep.length).trim()
  }
  agyAliasMap.value = map
}
// 别名行文本的映射源 key（首个分隔符之前）。非 agy 内置 ID 的行（其它客户端的
// 别名、通配别名 gemini-* 等）面板不展示，但 aliases 是路由器全局配置、被所有
// 客户端共用——行编辑必须原样保留它们，否则一次编辑+保存会把整台路由器的
// 其它别名清空。
function agyLineKey(line) {
  const sep = ['=', '→', '->'].find(s => line.includes(s))
  if (!sep) return ''
  return line.slice(0, line.indexOf(sep)).trim()
}
// 载入时从全部别名行中筛出的「非 agy 内置 ID」行（已带 @服务商 后缀，保存可无损回写）。
export const agyOtherLines = ref([])
// 文本框编辑 → agyAliasText（内置 ID 行随编辑更新；非内置行原样保留；
// 留空 = 不映射，不写入文本）
export function agySetAlias(k, v) {
  agyAliasMap.value = { ...agyAliasMap.value, [k]: String(v || '') }
  const rows = agyBuiltinModels.value
    .filter(m => String(agyAliasMap.value[m] || '').trim())
    .map(m => m + ' = ' + String(agyAliasMap.value[m]).trim())
  agyAliasText.value = [...agyOtherLines.value.filter(l => String(l).trim()), ...rows].join('\n')
}
// 映射目标文本框下拉：聚焦展开接入方式返回的模型列表，按输入过滤，首字母分组 + 字母键定位
export const agyToOpenFor = ref('')   // 当前展开下拉的内置 ID（同一时刻仅一个）
export const agyToFilter = ref('')    // 过滤词（聚焦时清空显示全量，随输入更新）
export function agyToGroups() {
  const s = String(agyToFilter.value || '').toLowerCase().trim()
  // 内置 ID 保存映射后会作为别名混进接入方式的模型列表：映射目标必须剔除它们，
  // 否则下拉会出现「内置 ID 映射到内置 ID」的循环选项
  const builtin = new Set(agyBuiltinModels.value)
  const list = agyRouterModels.value.slice().sort().filter(m => {
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
export function pickAgyTo(k, m) {
  agySetAlias(k, m)
  agyToOpenFor.value = ''
}
// 字母键定位当前字母分组（与 grok/opencode 等 picker 一致）
export function agyToKeyNav(e) {
  const key = e.key
  if (!/^[a-zA-Z0-9]$/.test(key)) return
  const drop = e.currentTarget.closest('.pi-sel')?.querySelector('.pi-sel-drop')
  if (!drop) return
  const L = key.toUpperCase()
  for (const el of drop.querySelectorAll('[data-letter]')) {
    if (el.dataset.letter.toUpperCase() === L) { el.scrollIntoView({ block: 'start' }); e.preventDefault(); return }
  }
}
// 模型映射面板的两个候选清单：
export const agyRouterModels = ref([])   // 路由器接入方式返回的模型列表（agy 走 /oai）
// agy 内置模型 ID（agy CLI 客户端当前提供这五个模型；推理档位在客户端内
// 另作选择，映射针对实际下发的模型 ID）。按版本降序排列，同一版本内轻档在前。
export const AGY_BUILTIN_MODELS = [
  'gemini-3.8-flash',
  'gemini-3.7-flash',
  'gemini-3.6-flash',
  'gemini-3.1-flash-lite-preview',
  'gemini-3.1-pro',
]
// 「执行所有命令」胶囊对应的官方通配规则（permissions.allow/deny 数组元素，command(...) 格式）
export const AGY_ALL_CMD_RULE = 'command(*)'
export const agyBuiltinModels = ref(AGY_BUILTIN_MODELS.slice())
export async function loadAgyAliasPanel() {
  agyAliasText.value = ''
  agyAliasLoaded.value = false
  agyAliasProviderList.value = []
  try {
    const r = await api.GetConfig()
    if (!(r && Array.isArray(r.providers))) return
    const list = r.providers.filter(p => p.enabled !== false)
    agyAliasProviderList.value = list.map(p => ({ id: p.id || '', name: p.name || '' }))
    agyAliasLoaded.value = await agyFetchAliasesMerged()
  } catch (e) {}
  try { await loadAgyModelLists() } catch (e) {}
}
// 汇总所有已启用服务商的 aliases 到文本框（每行 源 = 目标@服务商）。
// 带 @服务商 后缀：跨服务商同名/异构映射也能无损读回，保存时按后缀精确回写，
// 不会因「目标模型不匹配任何服务商」而把别名迁移到默认服务商。
// 已存值自带「@本服务商」尾缀（历史脏数据 / 上游同名模型）时不重复追加，
// 避免出现 @go@go 双重后缀，保存时也能一次剥净自愈回干净上游模型名。
export async function agyFetchAliasesMerged() {
  const lines = []
  let anyOk = false
  const eqTag = (a, b) => String(a || '').toLowerCase() === String(b || '').toLowerCase()
  try {
    const cfg = await api.GetConfig()
    for (const p of (cfg?.providers || [])) {
      if (p.enabled === false) continue
      const tag = p.name || p.id
      for (const [k, v] of Object.entries(p.aliases || {})) {
        const value = String(v)
        const at = value.lastIndexOf('@')
        const tail = at > 0 ? value.slice(at + 1) : ''
        if (at > 0 && tail && (eqTag(tail, p.id) || eqTag(tail, p.name))) {
          lines.push(k + ' = ' + value)
        } else {
          lines.push(k + ' = ' + value + '@' + tag)
        }
      }
    }
    agyAliasText.value = lines.join('\n')
    agySyncTextToMap()
    // 非内置 ID 的别名行（其它客户端别名 / 通配别名等）单独保留，行编辑不清除
    const builtin = new Set(agyBuiltinModels.value)
    agyOtherLines.value = lines.filter(l => {
      const k = agyLineKey(l)
      return k !== '' && !builtin.has(k)
    })
    anyOk = true
  } catch (e) {
    agyAliasText.value = ''
    agyOtherLines.value = []
    agySyncTextToMap()
  }
  return anyOk
}
// 接入方式返回的权威「模型 → 服务商」归属（载入下拉时构建；覆盖运行时拉取/聚合的
// 模型与 @服务商 消歧后缀）。保存时据此定位目标服务商，不再只依赖静态配置 p.models，
// 否则「models 留空的运行时拉取服务商」的目标模型会被静默挂到默认服务商上。
let agyTargetOwners = {} // { 干净模型名: [服务商 key, …] }
function agyRegisterOwner(clean, tag) {
  if (!clean || !tag) return
  const list = agyTargetOwners[clean] || (agyTargetOwners[clean] = [])
  if (!list.includes(tag)) list.push(tag)
}
// 从接入方式返回的模型 ID 解析「干净模型名」与 @服务商 归属标记
function agySplitTargetModel(id) {
  const s = String(id || '')
  const at = s.lastIndexOf('@')
  if (at > 0) {
    const hint = s.slice(at + 1).trim()
    if (hint && !/^\d/.test(hint)) return { clean: s.slice(0, at).trim(), tag: hint }
  }
  return { clean: s, tag: '' }
}
// 解析目标模型所属服务商：@服务商后缀 > 接入方式权威归属 > 模型/别名命中 > 默认兜底
function agyResolveProviderFor(target, cfg, hint) {
  const enabled = (cfg?.providers || []).filter(p => p.enabled !== false)
  const eq = (a, b) => String(a || '').toLowerCase() === String(b || '').toLowerCase()
  if (hint) {
    const byHint = enabled.find(p => eq(p.id, hint) || eq(p.name, hint))
    if (byHint) return byHint
  }
  const owners = agyTargetOwners[target] || []
  if (owners.length === 1) {
    const byOwner = enabled.find(p => eq(p.id, owners[0]) || eq(p.name, owners[0]))
    if (byOwner) return byOwner
  }
  let hits = enabled.filter(p =>
    (p.models || []).some(m => eq(m, target)) ||
    Object.keys(p.aliases || {}).some(a => eq(a, target)) ||
    Object.values(p.aliases || {}).some(v => eq(v, target))
  )
  if (!hits.length) hits = enabled
  const def = hits.find(p => eq(p.id, cfg?.defaultProvider) || eq(p.name, cfg?.defaultProvider))
  return (def || hits[0]) || null
}
// 拉取路由器接入方式返回的模型列表（/oai；GetModelsByFormat，失败退化 GetConfig 聚合）
export async function loadAgyModelLists() {
  let models = []
  agyTargetOwners = {}
  try {
    if (api.GetModelsByFormat) {
      const r = await api.GetModelsByFormat('/oai')
      for (const m of (r?.models || [])) {
        if (!m || String(m).includes('*')) continue
        models.push(String(m))
      }
      // 权威归属优先取 entries（每项含所属服务商）；旧绑定无 entries 时按 @ 后缀解析
      const entries = r?.entries || []
      if (entries.length) {
        for (const e of entries) {
          const { clean, tag } = agySplitTargetModel(e.id)
          if (clean) agyRegisterOwner(clean, e.provider || tag)
        }
      } else {
        for (const m of models) {
          const { clean, tag } = agySplitTargetModel(m)
          if (clean && tag) agyRegisterOwner(clean, tag)
        }
      }
    }
  } catch (e) {}
  if (!models.length && api.GetConfig) {
    try {
      const cfg = await api.GetConfig()
      for (const p of (cfg?.providers || [])) {
        if (!p.enabled) continue
        const tag = p.name || p.id
        let pl = []
        if (api.GetProviderModels) { try { pl = (await api.GetProviderModels(p))?.models || [] } catch (e2) {} }
        for (const m of [...pl, ...(p.models || []), ...Object.keys(p.aliases || {})]) {
          if (m && !String(m).includes('*')) { models.push(String(m)); agyRegisterOwner(String(m), tag) }
        }
      }
    } catch (e) {}
  }
  agyRouterModels.value = [...new Set(models)].sort()
}
// 模型映射随配置窗口「保存」一并落盘：把面板映射文本回写到路由器服务商 aliases。
// 走 SaveProviderAliases 定点绑定（后端只替换该服务商的 aliases 键），不再把整份
// 配置往返传回——避免并发编辑被整体覆盖，也避开掩码字段（****）的回写与竞态。
// 返回错误消息（''=成功 / 面板未载入则跳过返回 ''），由调用方统一提示。
export async function applyAgyAliases() {
  if (!agyAliasLoaded.value) return '' // 面板未成功载入时不动 aliases，避免载入失败被误判为「清空映射」
  const parsed = [] // {k, v, prov}  prov=目标服务商 key 或 null（由目标模型解析）
  for (const line of String(agyAliasText.value || '').split('\n')) {
    if (!line.trim()) continue
    const sep = line.includes('=') ? '=' : line.includes('→') ? '→' : line.includes('->') ? '->' : null
    if (!sep) continue // 跳过无法解析的行
    const idx = line.indexOf(sep)
    const k = line.slice(0, idx).trim()
    let v = line.slice(idx + sep.length).trim()
    if (!k || !v) continue
    // 目标模型可能带 @服务商 后缀（模型下拉跨服务商重名时按此区分）：
    // 拆出 provider hint，并把别名实际值存为去后缀的干净模型名
    let provHint = null
    const at = v.lastIndexOf('@')
    if (at > 0) {
      const hint = v.slice(at + 1).trim()
      if (hint && !/^\d/.test(hint)) { provHint = hint; v = v.slice(0, at).trim() }
    }
    parsed.push({ k, v, prov: provHint })
  }
  try {
    // 只读一次配置，仅用于「目标模型 → 目标服务商」归属解析，不回写整份配置
    const cfg = (await api.GetConfig()) || {}
    const providers = cfg.providers || []
    const byProvider = {}  // key「id|name」-> {k:v}
    for (const it of parsed) {
      const p = agyResolveProviderFor(it.v, cfg, it.prov)
      const pk = p ? (p.id || p.name) : null
      if (!pk) continue
      ;(byProvider[pk] = byProvider[pk] || {})[it.k] = it.v
    }
    // 待定点回写的服务商：{pk, map}；map 为空对象 = 清空该服务商的 aliases
    const writes = []
    const hasText = String(agyAliasText.value || '').trim().length > 0
    if (!Object.keys(byProvider).length) {
      if (hasText) {
        // 有内容但无法解析到任何服务商（目标模型不在已启用服务商模型/别名中）
        return t('sync.agy.aliasProviderReq')
      }
      // 文本为空 = 清空映射：把所有已启用且当前带 aliases 的服务商 aliases 置空
      for (const p of providers) {
        if (p.enabled === false) continue
        if (p.aliases && Object.keys(p.aliases).length) writes.push({ pk: p.id || p.name, map: {} })
      }
    } else {
      const covered = new Set()
      for (const [pk, map] of Object.entries(byProvider)) {
        const p = providers.find(x => x.id === pk || x.name === pk)
        if (!p) continue
        writes.push({ pk: p.id || p.name, map })
        covered.add(p.id)
        covered.add(p.name)
      }
      if (!writes.length) return t('sync.agy.aliasProviderReq')
      // 本次文本未提及但此前带 aliases 的启用服务商：其别名行已被全部删除，
      // 一并清空，否则磁盘上残留旧映射（编辑器看起来已清空、重载却还在）
      for (const p of providers) {
        if (p.enabled === false) continue
        if (covered.has(p.id) || covered.has(p.name)) continue
        if (p.aliases && Object.keys(p.aliases).length) writes.push({ pk: p.id || p.name, map: {} })
      }
    }
    // 定点回写：后端只替换该服务商的 aliases 键，其余配置（含并发改动）不受影响
    let restart = false
    for (const w of writes) {
      const r = await api.SaveProviderAliases(w.pk, w.map)
      if (!(r && r.ok)) return (r && r.error) || '?'
      if (r.restartNeeded) restart = true
    }
    if (restart) toast(t('set.savedRestart'))
    return ''
  } catch (e) { return e?.message || '?' }
}

// 接入环境变量（官方 BYOK 契约）：仅 GOOGLE_GEMINI_BASE_URL / GEMINI_API_KEY 两个
export const AGY_ENV_KEYS = ['GOOGLE_GEMINI_BASE_URL', 'GEMINI_API_KEY']
export const agyEnvBase = ref('')   // GOOGLE_GEMINI_BASE_URL（展示当前值）
export const agyEnvKey = ref('')    // GEMINI_API_KEY（可编辑，空 = 写路由令牌）
export const agyEnvLoading = ref(false)
export const agyEnvSaving = ref(false)
// 「模型供应商」下拉展开状态。此项决定 agy 走哪条认证链路：gemini = 跳过账号登录、
// 改用 GEMINI_API_KEY（配合上面的接入地址指向本路由器）；留空 = 不写该键（CLI 默认行为）。
export const agyMpOpen = ref(false)
export function agyRouterUrl() {
  return (routerBase.value || 'http://127.0.0.1:3000') + '/router'
}
export async function loadAgyEnv() {
  agyEnvLoading.value = true
  try {
    const [b, ak] = await Promise.all(AGY_ENV_KEYS.map(k => (api.GetUserEnv ? api.GetUserEnv(k) : null)))
    if (b && b.ok && b.exists) agyEnvBase.value = String(b.value || '')
    if (ak && ak.ok && ak.exists) agyEnvKey.value = String(ak.value || '')
    agyEnvKeyLoaded.value = agyEnvKey.value
  } catch (e) {} finally { agyEnvLoading.value = false }
}
export const agyEnvKeyLoaded = ref('') // 载入时的 API Key 值：判断用户是否改动过，切换单按钮的写入/清除
// 写入两个接入环境变量；返回错误字符串（空 = 成功）。
// GOOGLE_GEMINI_BASE_URL 固定为本路由器接入地址；GEMINI_API_KEY 为空时自动回退
// 路由令牌 authToken（未设鉴权时写空值仅保证变量存在，请求无需认证）。
export async function writeAgyEnv() {
  if (!api.SetUserEnv) return t('sync.agy.envNoBind')
  const base = agyRouterUrl()
  let key = String(agyEnvKey.value || '').trim()
  if (!key) {
    try {
      const c = await api.GetConfig()
      const tk = String((c && c.authToken) || '').trim()
      if (tk && tk !== '****') key = tk
    } catch (e) {}
  }
  for (const [k, v] of [['GOOGLE_GEMINI_BASE_URL', base], ['GEMINI_API_KEY', key]]) {
    const r = await api.SetUserEnv(k, v)
    if (!(r && r.ok)) return (r && r.error) || (k + ' 写入失败')
  }
  agyEnvBase.value = base
  // 同步已保存值：写入成功后按钮从「写入环境变量」回到「清除接入配置」
  agyEnvKeyLoaded.value = agyEnvKey.value
  return ''
}
export async function clearAgyEnv() {
  if (!api.DelUserEnv) return t('sync.agy.envClearNoBind')
  const errs = []
  for (const k of AGY_ENV_KEYS) {
    try {
      const r = await api.DelUserEnv(k)
      if (!(r && r.ok)) errs.push((r && r.error) || k)
    } catch (e) { errs.push(k) }
  }
  if (errs.length) return t('sync.agy.envClearFail', { list: errs.join('、') })
  agyEnvBase.value = ''; agyEnvKey.value = ''; agyEnvKeyLoaded.value = ''
  return ''
}

export function agySettingsPreset() {
  return {
    modelProvider: '',
    baseUrl: '',
    apiKey: '',
    model: '',
    models: [{ id: '', name: '', description: '' }],
    systemInstruction: '',
    parameters: { temperature: null, maxOutputTokens: null, topP: null, topK: null },
    artifactReviewPolicy: '',
    permissions: { toolPermission: '', allow: [], ask: [], deny: [] },
    enableTerminalSandbox: null,
    allowNonWorkspaceAccess: null,
    useG1Credits: null,
    enableTelemetry: null,
    notifications: null,
    showTips: null,
    showFeedbackSurvey: null,
    colorScheme: '',
    altScreenMode: '',
    runningLightSpeed: '',
    verbosity: '',
    editor: '',
    subagents: { enabled: null, maxSteps: null, autoExecuteTools: null },
  }
}
export const agyForm = ref({
  modelProvider: '', baseUrl: '', apiKey: '', model: '',
  systemInstruction: '',
  temperature: '', maxOutputTokens: '', topP: '', topK: '',
  toolPermission: '',
  artifactReviewPolicy: '', // 代码审查策略（官方：asks-for-review / agent-decides / always-proceed）
  // 「执行所有命令」胶囊：'' 默认不接管 / 'allow' 允许 / 'ask' 询问 / 'deny' 拒绝
  // 对应 permissions.{allow,ask,deny} 里的 command(*) 通配规则（官方优先级 deny > ask > allow）
  allCommands: '',
  enableTerminalSandbox: '', allowNonWorkspaceAccess: '', useG1Credits: '', enableTelemetry: '',
  // 通知与界面提示（官方键：notifications 默认 false，showTips / showFeedbackSurvey 默认 true）
  notifications: '', showTips: '', showFeedbackSurvey: '',
  colorScheme: '', altScreenMode: '', runningLightSpeed: '', verbosity: '', editor: '',
  subagentsEnabled: '', subagentsMaxSteps: '', subagentsAutoExecuteTools: '',
  modelsText: '',
})
export function initAgyForm() {
  const c = cfgObj.value || {}
  const obj = v => (v && typeof v === 'object' && !Array.isArray(v)) ? v : {}
  const params = obj(c.parameters), perms = obj(c.permissions), sub = obj(c.subagents)
  const numOr = v => (v !== null && v !== undefined && v !== '' && isFinite(Number(v))) ? String(v) : ''
  const triOr = v => (v === true || v === false) ? String(v) : ''
  agyForm.value = {
    modelProvider: c.modelProvider || '',
    baseUrl: c.baseUrl || '',
    apiKey: c.apiKey || '',
    model: c.model || '',
    systemInstruction: c.systemInstruction || '',
    temperature: numOr(params.temperature),
    maxOutputTokens: numOr(params.maxOutputTokens),
    topP: numOr(params.topP),
    topK: numOr(params.topK),
    toolPermission: perms.toolPermission || '',
    artifactReviewPolicy: c.artifactReviewPolicy || '',
    allCommands: (Array.isArray(perms.allow) && perms.allow.includes(AGY_ALL_CMD_RULE)) ? 'allow'
      : (Array.isArray(perms.ask) && perms.ask.includes(AGY_ALL_CMD_RULE)) ? 'ask'
      : (Array.isArray(perms.deny) && perms.deny.includes(AGY_ALL_CMD_RULE)) ? 'deny' : '',
    enableTerminalSandbox: triOr(c.enableTerminalSandbox),
    allowNonWorkspaceAccess: triOr(c.allowNonWorkspaceAccess),
    useG1Credits: triOr(c.useG1Credits),
    enableTelemetry: triOr(c.enableTelemetry),
    notifications: triOr(c.notifications),
    showTips: triOr(c.showTips),
    showFeedbackSurvey: triOr(c.showFeedbackSurvey),
    colorScheme: c.colorScheme || '',
    altScreenMode: c.altScreenMode || '',
    runningLightSpeed: c.runningLightSpeed || '',
    verbosity: c.verbosity || '',
    editor: c.editor || '',
    subagentsEnabled: triOr(sub.enabled),
    subagentsMaxSteps: numOr(sub.maxSteps),
    subagentsAutoExecuteTools: triOr(sub.autoExecuteTools),
    modelsText: JSON.stringify(Array.isArray(c.models) ? c.models : [], null, 2),
  }
}
// 合并写回：只覆盖表单管控的字段，其余（mcpServers 等）原样保留
export function agyApplyToObj() {
  const c = cfgObj.value
  if (!c || typeof c !== 'object') return
  const f = agyForm.value
  const strTrim = v => String(v ?? '').trim()
  const setStr = (k) => {
    const v = strTrim(f[k])
    if (v) c[k] = v
    else if (k in c) delete c[k]
  }
  setStr('modelProvider'); setStr('baseUrl'); setStr('apiKey'); setStr('model')
  setStr('systemInstruction')
  setStr('artifactReviewPolicy')
  setStr('colorScheme'); setStr('altScreenMode'); setStr('runningLightSpeed')
  setStr('verbosity'); setStr('editor')
  // parameters：留空删除，合法数值写入
  const params = (c.parameters && typeof c.parameters === 'object' && !Array.isArray(c.parameters)) ? c.parameters : {}
  const numOr = v => (String(v ?? '').trim() === '' || !isFinite(Number(v))) ? null : Number(v)
  const pv = numOr(f.temperature); const po = numOr(f.maxOutputTokens)
  const pp = numOr(f.topP); const pk = numOr(f.topK)
  if (pv != null) params.temperature = pv; else if ('temperature' in params) delete params.temperature
  if (po != null) params.maxOutputTokens = po; else if ('maxOutputTokens' in params) delete params.maxOutputTokens
  if (pp != null) params.topP = pp; else if ('topP' in params) delete params.topP
  if (pk != null) params.topK = pk; else if ('topK' in params) delete params.topK
  if (Object.keys(params).length) c.parameters = params
  else if ('parameters' in c) delete c.parameters
  // permissions：toolPermission + 「执行所有命令」胶囊（仅定点维护 command(*) 通配规则，
  // 允许=入 allow、拒绝=入 deny、默认=移除；其余 allow/deny 规则原样保留，可在 JSON 源码页编辑）
  const perms = (c.permissions && typeof c.permissions === 'object' && !Array.isArray(c.permissions)) ? c.permissions : {}
  const tp = strTrim(f.toolPermission)
  if (tp) perms.toolPermission = tp; else if ('toolPermission' in perms) delete perms.toolPermission
  // 官方细粒度权限是 allow / ask / deny 三张表（优先级 deny > ask > allow），command(*) 只落一张
  const arrOr = v => Array.isArray(v) ? v.filter(x => String(x).trim() !== AGY_ALL_CMD_RULE) : []
  const allow = arrOr(perms.allow), deny = arrOr(perms.deny), ask = arrOr(perms.ask)
  const ac = strTrim(f.allCommands)
  if (ac === 'allow') allow.push(AGY_ALL_CMD_RULE)
  else if (ac === 'ask') ask.push(AGY_ALL_CMD_RULE)
  else if (ac === 'deny') deny.push(AGY_ALL_CMD_RULE)
  if (allow.length) perms.allow = allow; else if ('allow' in perms) delete perms.allow
  if (ask.length) perms.ask = ask; else if ('ask' in perms) delete perms.ask
  if (deny.length) perms.deny = deny; else if ('deny' in perms) delete perms.deny
  if (Object.keys(perms).length) c.permissions = perms
  else if ('permissions' in c) delete c.permissions
  // 布尔开关三态：true / false 显式写入；'' 不写（移除已有键）
  const tri = (k) => {
    const v = f[k]
    if (v === 'true') c[k] = true
    else if (v === 'false') c[k] = false
    else if (k in c) delete c[k]
  }
  tri('enableTerminalSandbox'); tri('allowNonWorkspaceAccess')
  tri('useG1Credits'); tri('enableTelemetry')
  tri('notifications'); tri('showTips'); tri('showFeedbackSurvey')
  // subagents 三态 + maxSteps
  const sub = (c.subagents && typeof c.subagents === 'object' && !Array.isArray(c.subagents)) ? c.subagents : {}
  if (f.subagentsEnabled === 'true') sub.enabled = true
  else if (f.subagentsEnabled === 'false') sub.enabled = false
  else if ('enabled' in sub) delete sub.enabled
  const steps = String(f.subagentsMaxSteps ?? '').trim()
  if (steps !== '' && isFinite(Number(steps))) sub.maxSteps = Number(steps)
  else if ('maxSteps' in sub) delete sub.maxSteps
  if (f.subagentsAutoExecuteTools === 'true') sub.autoExecuteTools = true
  else if (f.subagentsAutoExecuteTools === 'false') sub.autoExecuteTools = false
  else if ('autoExecuteTools' in sub) delete sub.autoExecuteTools
  if (Object.keys(sub).length) c.subagents = sub
  else if ('subagents' in c) delete c.subagents
  // models：JSON 源码解析，缺失字段补空
  let models = []
  try { const v = JSON.parse(f.modelsText || '[]'); if (Array.isArray(v)) models = v.filter(m => String(strTrim(m.id || m.name || '')).trim()) } catch (e) { models = [] }
  if (models.length) c.models = models
  else if ('models' in c) delete c.models
}

// watchers 在 init 中注册（组件 setup 上下文内）
export function initAgy() {
  watch(agyForm, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'agy' || jsonEcho) return
    try { agyApplyToObj(); syncToJson() } catch (e) {}
  }, { deep: true })
}