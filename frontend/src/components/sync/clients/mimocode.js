// MimoCode（小米 MiMo Code，~/.config/mimocode/mimocode.jsonc，JSONC 结构化表单）
// 接入方式与 OpenCode 同族。受管键（字段以官方 JSON Schema 为准）：
// - provider.<key>：api（API 类型）/ name（显示名）/ options.{baseURL, apiKey,
//   timeout, headerTimeout, chunkTimeout, setCacheKey} / models 模型条目表
// - 顶层默认模型 model / small_model（"服务商/模型" 格式）
// 其余顶层键（$schema / mcp / skills 等）原样保留。mcp 只读展示
//（MCP 由 MCP 页统一管理）。
// permission：面板只暴露「权限模式」预设（'*' / edit / bash 三键组合）与
// 「所有命令」（bash）；其余工具键与对象规则原样保留。
import { computed, ref, watch } from 'vue'
import { cfgObj, cfgContent, cfgEdit, cfgTarget, derivedBaseUrl, jsonEcho, syncToJson } from '../store'
import { parseJsonc } from '../shared'
import { toast } from '../../../toast'
import { t } from '../../../i18n'

export const mimoProviderKey = ref('llm') // provider 对象键（可自取）
export const mimoApiKey = ref('')
export const mimoApi = ref('') // API 类型（schema: provider.<id>.api，如 openai / anthropic）
export const mimoName = ref('') // 显示名
// options 运行选项：数字毫秒或 'false'（禁用）；空 = 不写入（保留原值）
export const mimoTimeout = ref('')
export const mimoHeaderTimeout = ref('')
export const mimoChunkTimeout = ref('')
// setCacheKey：'' / 'true' / 'false' 三态
export const mimoSetCacheKey = ref('')
// 全局推理档位（provider.<key>.options.reasoningEffort，透传 SDK；'' = 不写入）
export const mimoEffort = ref('')
// 模型条目表：[{id,name,reasoning,toolCall,attachment,limitContext,limitOutput,cacheTTL}]
export const mimoModels = ref([])

// ============ 权限配置（顶层 permission 键，allow/ask/deny；工具键以官方「权限」文档为准） ============
// 受管键：'*' 全局兜底 + 工具类 + 安全防护项；对象规则（细粒度通配）原样保留不覆盖
export const MIMO_PERM_KEYS = [
  { k: 'read', hint: '读取文件' },
  { k: 'edit', hint: '文件修改' },
  { k: 'glob', hint: '文件通配' },
  { k: 'grep', hint: '内容搜索' },
  { k: 'bash', hint: '终端命令' },
  { k: 'task', hint: '启动子代理' },
  { k: 'skill', hint: '加载技能' },
  { k: 'lsp', hint: 'LSP 查询' },
  { k: 'webfetch', hint: '获取 URL' },
  { k: 'websearch', hint: '网页搜索' },
  { k: 'codesearch', hint: '代码搜索' },
  { k: 'external_directory', hint: '工作目录外路径' },
  { k: 'doom_loop', hint: '重复调用护栏' },
]
// 权限模式预设：每个预设一次性写出「'*' 全局默认 / edit / bash」三键的组合，
// 与右侧「所有命令」（bash）行状态保持一致——只点预设不会留下互相打架的旧值
// （如选「询问」却仍残留 bash=allow）。文案复用各客户端通用的四档模式名。
// 其余工具键（read/glob/grep/task/skill/lsp/webfetch/websearch/codesearch/
// external_directory/doom_loop）面板不展示，原样保留，不由预设改写。
export const MIMO_PERM_MODES = [
  { id: 'ask', label: () => t('sync.permDefaultName'), star: 'ask', edit: '', bash: 'ask' },
  { id: 'autoedit', label: () => t('sync.permAcceptEditsName'), star: 'ask', edit: 'allow', bash: 'ask' },
  { id: 'allow', label: () => t('sync.permBypassName'), star: 'allow', edit: '', bash: 'allow' },
  { id: 'plan', label: () => t('sync.permPlanName'), star: 'ask', edit: 'deny', bash: 'deny' },
]
export const mimoPerms = ref({}) // key -> { mode, obj }，obj 标记原值是对象规则（保留）
// 当前预设：由 '*'/edit/bash 三项现值反推（computed，保证两排胶囊状态永远一致；
// 手动把「所有命令」改成非预设组合时自动回到「无预设」= 自定义）
export const mimoPermMode = computed(() => detectPermMode(mimoPerms.value))
// 「所有命令」三态单选（允许/询问/拒绝）
export const MIMO_BASH_OPTS = [
  { v: 'allow', label: 'sync.allow' },
  { v: 'ask', label: 'sync.ask' },
  { v: 'deny', label: 'sync.deny' },
]

export function initMimoPerms(perm) {
  const o = {}
  const sv = perm ? perm['*'] : undefined
  if (typeof sv === 'string') o['*'] = { mode: sv, obj: false }
  else if (sv && typeof sv === 'object') o['*'] = { mode: '', obj: true }
  else o['*'] = { mode: '', obj: false }
  for (const { k } of MIMO_PERM_KEYS) {
    const v = perm ? perm[k] : undefined
    if (typeof v === 'string') o[k] = { mode: v, obj: false }
    else if (v && typeof v === 'object') o[k] = { mode: '', obj: true }
    else o[k] = { mode: '', obj: false }
  }
  mimoPerms.value = o
}

// 由 '*'/edit/bash 三项现值反推预设（供回显）；不匹配任何预设 = 自定义。
// 只看这三个面板可见键：其余工具键（read/glob/... 与对象规则）面板不展示、
// 也不由预设改写，故不参与判定——否则配置里任意一个工具键都会让预设高亮丢失。
function detectPermMode(o) {
  const m = (k) => (o[k] && !o[k].obj ? o[k].mode : '')
  const star = m('*'), edit = m('edit'), bash = m('bash')
  for (const p of MIMO_PERM_MODES) {
    if (star === p.star && edit === p.edit && bash === p.bash) return p.id
  }
  return ''
}

// 点选预设：一次性写出该预设的三键组合（'*' / edit / bash），
// 与「所有命令」行保持同步，不再残留互相矛盾的旧值
export function mimoPickMode(id) {
  const preset = MIMO_PERM_MODES.find(p => p.id === id)
  if (!preset) return
  const o = mimoPerms.value
  const set = (k, v) => { if (o[k] && !o[k].obj) o[k].mode = v }
  set('*', preset.star)
  set('edit', preset.edit)
  set('bash', preset.bash)
}

export function mimoCyclePerm(key) {
  const st = mimoPerms.value[key]
  if (!st || st.obj) return
  const cycle = { '': 'allow', allow: 'ask', ask: 'deny', deny: '' }
  st.mode = cycle[st.mode] || ''
}

// 权限写回：allow/ask/deny 直写覆盖；空值且原是对象规则保留；
// 空值且非对象删除该键；'*' 全空且原无 permission 时不创建顶层键
export function mimoApplyPerms(c) {
  const hadPerm = !!(c.permission && typeof c.permission === 'object')
  const perm = hadPerm ? c.permission : {}
  for (const { k } of MIMO_PERM_KEYS) {
    const st = mimoPerms.value[k]
    if (!st) continue
    if (st.mode) { perm[k] = st.mode; continue }
    if (st.obj) continue
    delete perm[k]
  }
  const star = mimoPerms.value['*']
  if (star && star.mode) perm['*'] = star.mode
  else if (star && !star.obj) delete perm['*']
  if (!hadPerm && !Object.keys(perm).length) return
  c.permission = perm
}
// 顶层默认模型（"mimoProviderKey/模型ID" 格式；空 = 未设置）
export const mimoModel = ref('')
export const mimoSmallModel = ref('')

// 原始 provider 键：表单改动键名时，写回需同步移除旧键条目（避免 provider.<oldKey> 残留）
let mimoProviderKeyOrig = ''
export function mimoProviderKeyOriginal() { return mimoProviderKeyOrig }
// 模型下拉（行内 / 顶层默认模型）
export const mimoModelRowOpen = ref(-1)
export const mimoModelOpen = ref(false)
export const mimoSmallOpen = ref(false)

// 数字或 'false' 文本 → number | false | undefined。
// schema：timeout / headerTimeout / chunkTimeout 均为 integer(>0)，前两者可写 false；
// chunkTimeout 不接受 false。非法输入（0 / 负数 / 非数字 / 不允许的 false）
// 一律按 undefined 处理（保留原值），避免写出 schema 校验不过的配置。
function parseTimeoutOpt(v, allowFalse) {
  const s = String(v || '').trim().toLowerCase()
  if (!s) return undefined
  if (s === 'false') return allowFalse ? false : undefined
  const n = Number(s)
  return Number.isFinite(n) && n > 0 ? n : undefined
}

export function initMimoForm() {
  const c = cfgObj.value || {}
  const provs = (c.provider && typeof c.provider === 'object') ? c.provider : {}
  const keys = Object.keys(provs).filter(k => String(k || '').trim())
  const key = keys[0] || 'llm'
  const prov = (provs[key] && typeof provs[key] === 'object') ? provs[key] : {}
  const opts = prov.options || {}
  mimoProviderKey.value = key
  mimoProviderKeyOrig = key
  mimoApiKey.value = opts.apiKey || ''
  mimoApi.value = String(prov.api || '')
  mimoName.value = String(prov.name || '')
  const fmtT = (v) => (v === false ? 'false' : (v === 0 ? '0' : (v == null ? '' : String(v))))
  mimoTimeout.value = fmtT(opts.timeout)
  mimoHeaderTimeout.value = fmtT(opts.headerTimeout)
  mimoChunkTimeout.value = fmtT(opts.chunkTimeout)
  mimoSetCacheKey.value = opts.setCacheKey === true ? 'true' : opts.setCacheKey === false ? 'false' : ''
  mimoEffort.value = String(opts.reasoningEffort || '')
  const perm = (c.permission && typeof c.permission === 'object') ? c.permission
    : (typeof c.permission === 'string' ? { '*': c.permission } : null)
  initMimoPerms(perm)
  const ms = (prov.models && typeof prov.models === 'object') ? prov.models : {}
  mimoModels.value = Object.keys(ms).map(id => {
    const e = (ms[id] && typeof ms[id] === 'object') ? ms[id] : {}
    const lim = (e.limit && typeof e.limit === 'object') ? e.limit : {}
    const eo = (e.options && typeof e.options === 'object') ? e.options : {}
    return {
      id,
      name: e.name || '',
      reasoning: e.reasoning === true ? 'true' : e.reasoning === false ? 'false' : '',
      toolCall: e.tool_call === true ? 'true' : e.tool_call === false ? 'false' : '',
      attachment: e.attachment === true ? 'true' : e.attachment === false ? 'false' : '',
      effort: String(eo.reasoningEffort || ''),
      limitContext: lim.context == null ? '' : String(lim.context),
      limitOutput: lim.output == null ? '' : String(lim.output),
      cacheTTL: e.cachePromptTTL === '1h' ? '1h' : e.cachePromptTTL === '5m' ? '5m' : '',
    }
  })
  mimoModel.value = String(c.model || '')
  mimoSmallModel.value = String(c.small_model || '')
}

export function mimoProviderPrefix() {
  return String(mimoProviderKey.value || '').trim() || 'llm'
}
export function mimoQualifyModel(m) {
  return (m && !String(m).includes('/')) ? mimoProviderPrefix() + '/' + String(m) : String(m || '')
}

// 三态文本 → boolean | undefined
function tri(v) {
  return v === 'true' ? true : v === 'false' ? false : undefined
}

// 模型条目里由表单接管的键（其余 schema 已知键原样保留，见 mimoApplyToObj）
const MIMO_MODEL_OWNED = new Set(['name', 'reasoning', 'tool_call', 'attachment', 'limit', 'cachePromptTTL', 'options'])

export function mimoApplyToObj() {
  const c = cfgObj.value
  if (!c || typeof c !== 'object') return
  if (!c.provider || typeof c.provider !== 'object') c.provider = {}
  const pk = mimoProviderPrefix()
  // provider 键被表单改名时，原值来源是「旧键条目」：否则新键查不到旧条目，
  // models 里未被表单接管的键（family/cost/modalities 等）与 options 其它键会丢
  const renameFrom = (mimoProviderKeyOrig && mimoProviderKeyOrig !== pk) ? mimoProviderKeyOrig : ''
  const asObj = (k) => (k && c.provider[k] && typeof c.provider[k] === 'object') ? c.provider[k] : null
  const prev = asObj(pk) || asObj(renameFrom) || {}
  const prevOpts = (prev.options && typeof prev.options === 'object') ? prev.options : {}
  const prevModels = (prev.models && typeof prev.models === 'object') ? prev.models : {}
  // options：baseURL/apiKey 必写；超时类仅在表单有值时写入（含 'false'），空值保留原值
  const options = { ...prevOpts, baseURL: derivedBaseUrl(), apiKey: mimoApiKey.value || '' }
  const t = parseTimeoutOpt(mimoTimeout.value, true); if (t !== undefined) options.timeout = t
  const ht = parseTimeoutOpt(mimoHeaderTimeout.value, true); if (ht !== undefined) options.headerTimeout = ht
  // chunkTimeout 在 schema 里不允许 false（只有整数）
  const ct = parseTimeoutOpt(mimoChunkTimeout.value, false); if (ct !== undefined) options.chunkTimeout = ct
  const sck = tri(mimoSetCacheKey.value); if (sck !== undefined) options.setCacheKey = sck
  if (String(mimoEffort.value).trim()) options.reasoningEffort = String(mimoEffort.value).trim()
  else delete options.reasoningEffort
  const provEntry = { ...prev, options }
  // api / name：非空写入，清空移除
  if (String(mimoApi.value).trim()) provEntry.api = String(mimoApi.value).trim()
  else delete provEntry.api
  if (String(mimoName.value).trim()) provEntry.name = String(mimoName.value).trim()
  else delete provEntry.name
  // 模型条目：schema 里 additionalProperties=false ⇒ 只写 schema 已知键。
  // 表单接管的键见 MIMO_MODEL_OWNED，其余合法键（id/family/release_date/
  // temperature/interleaved/cost/modalities/experimental/status/provider/headers/
  // variants）从原条目原样带过，避免改一个字段就把整条模型定义抹掉。
  const models = {}
  for (const row of mimoModels.value) {
    const id = String(row.id || '').trim()
    if (!id) continue
    const entry = {}
    const old = (prevModels[id] && typeof prevModels[id] === 'object') ? prevModels[id] : {}
    for (const k of Object.keys(old)) if (!MIMO_MODEL_OWNED.has(k)) entry[k] = old[k]
    // 推理档位（模型级覆盖）：写 options.reasoningEffort，保留 options 其余键
    const oldOpts = (old.options && typeof old.options === 'object') ? old.options : {}
    const opts = {}
    for (const k of Object.keys(oldOpts)) if (k !== 'reasoningEffort') opts[k] = oldOpts[k]
    if (String(row.effort || '').trim()) opts.reasoningEffort = String(row.effort).trim()
    if (Object.keys(opts).length) entry.options = opts
    else delete entry.options
    if (String(row.name || '').trim()) entry.name = String(row.name).trim()
    const r = tri(row.reasoning); if (r !== undefined) entry.reasoning = r
    const tc = tri(row.toolCall); if (tc !== undefined) entry.tool_call = tc
    const at = tri(row.attachment); if (at !== undefined) entry.attachment = at
    // limit：schema 要求 context 与 output 同时存在（required，additionalProperties=false）。
    // 表单两格独立填写，故只填一半时沿用原值的另一半；凑不齐整条不写，
    // 绝不写出非法 limit（会让 MiMo Code 配置校验失败）。清空两格 = 保留原 limit。
    const ctx = parseInt(String(row.limitContext).trim(), 10)
    const out = parseInt(String(row.limitOutput).trim(), 10)
    const lim = (old.limit && typeof old.limit === 'object') ? { ...old.limit } : {}
    if (Number.isFinite(ctx)) lim.context = ctx
    if (Number.isFinite(out)) lim.output = out
    if (Number.isFinite(lim.context) && Number.isFinite(lim.output)) entry.limit = lim
    if (row.cacheTTL === '5m' || row.cacheTTL === '1h') entry.cachePromptTTL = row.cacheTTL
    models[id] = entry
  }
  if (Object.keys(models).length) provEntry.models = models
  else delete provEntry.models
  c.provider[pk] = provEntry
  // 表单改变了 provider 键名时：移除旧键条目避免残留，并同步顶层默认模型的
  // 引用前缀（model / small_model 为「键/模型ID」格式，不同步就会指向已不存在的
  // provider，导致默认模型解析失败）
  if (renameFrom) {
    const oldPk = renameFrom
    const rePrefix = (v) => {
      const s = String(v || '').trim()
      return s.startsWith(oldPk + '/') ? pk + s.slice(oldPk.length) : s
    }
    mimoModel.value = rePrefix(mimoModel.value)
    mimoSmallModel.value = rePrefix(mimoSmallModel.value)
    delete c.provider[oldPk]
    mimoProviderKeyOrig = pk
  }
  const setModel = (key, v) => {
    const s = String(v || '').trim()
    if (s) c[key] = s
    else delete c[key]
  }
  setModel('model', mimoModel.value)
  setModel('small_model', mimoSmallModel.value)
  mimoApplyPerms(c)
}

// mcp 只读展示：MiMo Code 的 MCP 声明在顶层 mcp 键（对象形态），返回已配置服务器名
export function mimoMcpServerNames() {
  const m = cfgObj.value && typeof cfgObj.value === 'object' ? cfgObj.value.mcp : undefined
  if (!m || typeof m !== 'object' || Array.isArray(m)) return []
  return Object.keys(m).filter(k => {
    const e = m[k]
    return e !== false && (!e || typeof e !== 'object' || e.enabled !== false)
  })
}

export let mimoMuted = false
export function setMimoMuted(v) { mimoMuted = v }

// JSONC 源码 → cfgObj 回填（switchMode json→form 时调用；JSONC 宽容解析）
export function mimoFromSource() {
  try {
    const t = String(cfgContent.value || '{}')
    const parsed = parseJsonc(t)
    cfgObj.value = parsed && typeof parsed === 'object' ? parsed : {}
    return true
  } catch (e) { toast(t('sync.jsonError')); return false }
}

export function initMimo() {
  watch([mimoProviderKey, mimoApiKey, mimoApi, mimoName, mimoTimeout, mimoHeaderTimeout, mimoChunkTimeout,
    mimoSetCacheKey, mimoEffort, mimoModels, mimoModel, mimoSmallModel, mimoPerms], () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'mimocode' || mimoMuted || jsonEcho) return
    try { mimoApplyToObj(); syncToJson() } catch (e) {}
  }, { deep: true })
}
