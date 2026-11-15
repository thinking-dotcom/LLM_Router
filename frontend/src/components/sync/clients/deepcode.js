// Deep Code CLI（~/.deepcode/settings.json，固定 /oai 接入）。
// 字段口径依据官方文档 docs/configuration.md + docs/permission.md（lessweb/deepcode-cli）。
//   - 接入地址/密钥只有 env.BASE_URL / env.API_KEY 这一处（CLI 自行追加 /chat/completions）；
//   - 模型：CLI 读序为 systemEnv > 项目级 model > 项目级 env.MODEL > 用户级 model >
//     用户级 env.MODEL，且它自己的 /model 命令写的也是用户级顶层 model，
//     所以面板统一写顶层 model（只写 env.MODEL 会被顶层 model 盖过）；
//   - thinkingEnabled(bool) + reasoningEffort(仅 low|high|max，其它值回落 max) 为顶层键，
//     请求侧落成 thinking:{type} + reasoning_effort；
//   - 采样/上下文：temperature(0~2)、contextWindow 与 autoCompactWindow（正整数 token，
//     或 "128K"/"1M" 这类带后缀字符串，1K=1024；未设压缩阈值时取上下文窗口的 50%）；
//     普通模型默认上下文 256K，DeepSeek V4 系列 1M；
//   - multimodal(default|on|off)：default=按内置模型列表自动判定，on=图片以 image_url 内联，
//     off=交给模型用识图工具读；
//   - debugLogEnabled(false) / telemetryEnabled(true) / notify(脚本绝对路径) /
//     webSearchTool(自定义搜索脚本绝对路径)；
//   - Files API（filesApiEnabled + filesApiTimeoutMs，1000~600000）：**仅当 BASE_URL 为
//     https://api.deepseek.com 才生效**，接本路由器时不会用到（面板只留开关与超时，
//     其余阈值 fileExpiresAfterSeconds/fileRefreshMarginSeconds/fileQuotaCleanupBatch/
//     maxRequestFilesBytes 走 JSON 源码页签）；
//   - permissions：{allow,deny,ask,defaultMode,addWorkingDirs}，范围共 12 种
//     （read/write/delete × in-cwd|in-tmp|out-cwd、query-git-log、mutate-git-log、
//     network、mcp），优先级 deny > ask > allow > defaultMode（allowAll|askAll，默认 allowAll），
//     unknown 范围总是询问、不可配置；
//   - 受管键之外的 settings.json 内容（mcpServers / enabledSkills / statusline /
//     permissions 里的 addWorkingDirs 等）一律原样保留，只做定点赋值，不重建整个配置对象。
// 全空的三态（''）一律「不写该键」，交由 CLI 默认值，避免把默认值刷进用户配置。
import { ref, watch } from 'vue'
import { cfgObj, cfgEdit, cfgTarget, derivedBaseUrl, jsonEcho, syncToJson } from '../store'
import { toast } from '../../../toast'
import { t } from '../../../i18n'
import api from '../../../api'

// CLI 的 /model 菜单支持的推理强度（resolveReasoningEffort 只认这三个，其余回落 max）
export const DEEPCODE_EFFORTS = ['max', 'high', 'low']

// CLI 内置模型 ID（0.4.0 包内 DEEPSEEK_V4_MODELS 核对；/model 菜单默认就是这几项）。
// 注意 CLI **不限制**模型：MODEL 只是一个字符串，原样作为请求的 model 发往 BASE_URL
// （README：「任何其他 OpenAI 兼容模型」）。所以这里既并入内置 ID、也保留路由器模型池，
// 两边都能选：选内置 ID 时需在路由器侧把它配成服务商模型或别名才路由得到。
export const DEEPCODE_BUILTIN_MODELS = [
  'deepseek-flash', 'deepseek-v4-pro', 'deepseek-v4-flash', 'deepseek-v4-flash-vision-exp',
]

// multimodal：default=按内置模型列表自动判定 / on=强制图片内联 / off=强制走识图工具
export const DEEPCODE_MULTIMODAL = ['default', 'on', 'off']
// 官方 permission.md 定义的 12 个权限范围（另有 unknown：无法归类副作用，总是询问、不可配置）。
// 面板按语义分 4 组展示（读 / 写 / 删 / 其它），组内定宽网格对齐；写盘顺序仍按扁平列表。
export const DEEPCODE_PERM_GROUPS = [
  { key: 'read', scopes: ['read-in-cwd', 'read-in-tmp', 'read-out-cwd'] },
  { key: 'write', scopes: ['write-in-cwd', 'write-in-tmp', 'write-out-cwd'] },
  { key: 'delete', scopes: ['delete-in-cwd', 'delete-out-cwd'] },
  { key: 'other', scopes: ['query-git-log', 'mutate-git-log', 'network', 'mcp'] },
]
export const DEEPCODE_PERM_SCOPES = DEEPCODE_PERM_GROUPS.flatMap(g => g.scopes)

export const deepcodeForm = ref({
  baseUrl: '', apiKey: '', model: '', thinkingEnabled: true, reasoningEffort: 'max',
  temperature: '',      // 顶层 temperature（0~2）；空 = 不写
  contextWindow: '',    // 正整数 token 或 "128K" / "1M"
  autoCompactWindow: '',
  multimodal: '',       // '' | default | on | off
  debugLog: '',         // '' | on | off（debugLogEnabled）
  telemetry: '',        // '' | on | off（telemetryEnabled）
  filesApi: '',         // '' | on | off（filesApiEnabled，仅官方 BASE_URL 生效）
  filesApiTimeoutMs: '',
  notify: '',           // 任务完成通知脚本绝对路径
  webSearchTool: '',    // 自定义联网搜索脚本绝对路径
  permMode: '',         // '' | allowAll | askAll
  perm: {},             // { 范围: 'allow' | 'deny' | 'ask' }，空 = 不写
})
// 统一模型下拉（pi-sel）：候选来自路由器聚合模型（modelGroupsByLetter 的默认池）
export const deepcodeModelOpen = ref(false)
export function pickDeepcodeModel(m) { deepcodeForm.value.model = m; deepcodeModelOpen.value = false }

// 权限范围胶囊点按：未设 → 允许 → 拒绝 → 询问 → 未设（决策优先级 deny > ask > allow）
export function deepcodePermCycle(s) {
  const cur = (deepcodeForm.value.perm && deepcodeForm.value.perm[s]) || ''
  const next = cur === '' ? 'allow' : cur === 'allow' ? 'deny' : cur === 'deny' ? 'ask' : ''
  const perm = { ...(deepcodeForm.value.perm || {}) }
  if (next) perm[s] = next
  else delete perm[s]
  deepcodeForm.value = { ...deepcodeForm.value, perm }
}

// 读回权限配置（permissions 的三个数组 → 表单 {范围: 决策}）
function deepcodeReadPerms(c) {
  const p = (c.permissions && typeof c.permissions === 'object') ? c.permissions : {}
  const perm = {}
  for (const s of DEEPCODE_PERM_SCOPES) {
    if (Array.isArray(p.allow) && p.allow.includes(s)) perm[s] = 'allow'
    else if (Array.isArray(p.deny) && p.deny.includes(s)) perm[s] = 'deny'
    else if (Array.isArray(p.ask) && p.ask.includes(s)) perm[s] = 'ask'
  }
  const mode = p.defaultMode === 'allowAll' || p.defaultMode === 'askAll' ? p.defaultMode : ''
  return { mode, perm }
}

// 从 cfgObj（真实 settings.json）回填表单
export function initDeepcodeForm() {
  const c = cfgObj.value && typeof cfgObj.value === 'object' ? cfgObj.value : {}
  const env = c.env && typeof c.env === 'object' ? c.env : {}
  const pm = deepcodeReadPerms(c)
  deepcodeForm.value = {
    baseUrl: typeof env.BASE_URL === 'string' ? env.BASE_URL : '',
    apiKey: typeof env.API_KEY === 'string' ? env.API_KEY : '',
    model: typeof c.model === 'string' && c.model
      ? c.model
      : (typeof env.MODEL === 'string' ? env.MODEL : ''),
    thinkingEnabled: c.thinkingEnabled !== false, // 缺省视为启用（CLI 对 deepseek-v4 默认开思考）
    reasoningEffort: DEEPCODE_EFFORTS.includes(c.reasoningEffort) ? c.reasoningEffort : 'max',
    temperature: typeof c.temperature === 'number' ? String(c.temperature) : '',
    contextWindow: c.contextWindow == null ? '' : String(c.contextWindow),
    autoCompactWindow: c.autoCompactWindow == null ? '' : String(c.autoCompactWindow),
    multimodal: DEEPCODE_MULTIMODAL.includes(c.multimodal) ? c.multimodal : '',
    debugLog: typeof c.debugLogEnabled === 'boolean' ? (c.debugLogEnabled ? 'on' : 'off') : '',
    telemetry: typeof c.telemetryEnabled === 'boolean' ? (c.telemetryEnabled ? 'on' : 'off') : '',
    filesApi: typeof c.filesApiEnabled === 'boolean' ? (c.filesApiEnabled ? 'on' : 'off') : '',
    filesApiTimeoutMs: typeof c.filesApiTimeoutMs === 'number' ? String(c.filesApiTimeoutMs) : '',
    notify: typeof c.notify === 'string' ? c.notify : '',
    webSearchTool: typeof c.webSearchTool === 'string' ? c.webSearchTool : '',
    permMode: pm.mode,
    perm: pm.perm,
  }
}

// ---- 写盘基元：三态（'' = 不写，交给 CLI 默认值）/ 数值 / 上下文窗口 ----
function dcSetBool(c, key, v) {
  if (v === 'on') c[key] = true
  else if (v === 'off') c[key] = false
  else delete c[key]
}
function dcSetStr(c, key, v) {
  const s = String(v == null ? '' : v).trim()
  if (s) c[key] = s
  else delete c[key]
}
function dcSetNum(c, key, v, lo, hi) {
  const s = String(v == null ? '' : v).trim()
  if (!s) { delete c[key]; return }
  const n = Number(s)
  if (!Number.isFinite(n)) { delete c[key]; return }
  let x = n
  if (lo != null) x = Math.max(lo, x)
  if (hi != null) x = Math.min(hi, x)
  c[key] = x
}
// contextWindow / autoCompactWindow：纯数字写 number，带 K/M 后缀的原样写字符串（1K=1024）
function dcSetWin(c, key, v) {
  const s = String(v == null ? '' : v).trim()
  if (!s) { delete c[key]; return }
  if (/^\d+$/.test(s)) { c[key] = Number(s); return }
  c[key] = s
}
// permissions：重写本面板的 12 个范围；未展示的自定义条目与 addWorkingDirs 原样保留
function dcSetPerms(c, f) {
  const cur = (c.permissions && typeof c.permissions === 'object') ? c.permissions : {}
  const next = { ...cur }
  const want = { allow: [], deny: [], ask: [] }
  for (const s of DEEPCODE_PERM_SCOPES) {
    const d = (f.perm && f.perm[s]) || ''
    if (want[d]) want[d].push(s)
  }
  const known = new Set(DEEPCODE_PERM_SCOPES)
  for (const k of ['allow', 'deny', 'ask']) {
    const extra = Array.isArray(cur[k]) ? cur[k].filter(x => typeof x === 'string' && !known.has(x)) : []
    const all = want[k].concat(extra)
    if (all.length) next[k] = all
    else delete next[k]
  }
  if (f.permMode === 'allowAll' || f.permMode === 'askAll') next.defaultMode = f.permMode
  else delete next.defaultMode
  const keep = next.allow || next.deny || next.ask || next.defaultMode || next.addWorkingDirs
  if (keep) c.permissions = next
  else delete c.permissions
}

// 表单 → cfgObj（只覆盖受管键，其余键原样保留）
export function deepcodeApplyToObj() {
  const c = cfgObj.value
  if (!c || typeof c !== 'object') return
  const f = deepcodeForm.value
  if (!c.env || typeof c.env !== 'object') c.env = {}
  c.env.BASE_URL = f.baseUrl || derivedBaseUrl()
  c.env.API_KEY = f.apiKey || ''
  const m = String(f.model || '').trim()
  if (m) c.model = m
  else delete c.model // 留空则不写顶层 model，交由 CLI 用 env.MODEL / 默认模型
  c.thinkingEnabled = !!f.thinkingEnabled
  c.reasoningEffort = DEEPCODE_EFFORTS.includes(f.reasoningEffort) ? f.reasoningEffort : 'max'
  dcSetNum(c, 'temperature', f.temperature, 0, 2)
  dcSetWin(c, 'contextWindow', f.contextWindow)
  dcSetWin(c, 'autoCompactWindow', f.autoCompactWindow)
  if (DEEPCODE_MULTIMODAL.includes(f.multimodal)) c.multimodal = f.multimodal
  else delete c.multimodal
  dcSetBool(c, 'debugLogEnabled', f.debugLog)
  dcSetBool(c, 'telemetryEnabled', f.telemetry)
  dcSetBool(c, 'filesApiEnabled', f.filesApi)
  dcSetNum(c, 'filesApiTimeoutMs', f.filesApiTimeoutMs, 1, 600000)
  dcSetStr(c, 'notify', f.notify)
  dcSetStr(c, 'webSearchTool', f.webSearchTool)
  dcSetPerms(c, f)
}

// 模板载入期间静默：否则 initDeepcodeForm 的赋值会触发 watcher 立刻回写并重排源码框
export let deepcodeMuted = false
export function setDeepcodeMuted(v) { deepcodeMuted = v }

export function initDeepcode() {
  watch(deepcodeForm, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'deepcode' || deepcodeMuted || jsonEcho) return
    try { deepcodeApplyToObj(); syncToJson() } catch (e) {}
  }, { deep: true })
}

// ============ 内置 ID → 目标模型 映射（写路由器服务商 aliases）============
// 动机：CLI 的 /model 菜单只列 4 个内置 ID，且在菜单里选中就会把顶层 model 改成该 ID。
// 把这 4 个 ID 在路由器侧配成别名，菜单里任选一项都能路由到真实上游模型（与 agy 同思路）。
// 与 agy 的差别：deepcode 只维护这 4 个内置键，其它别名键（别的客户端别名、通配别名）
// 一并原样保留——保存时是「读现值 → 增删我方 4 个键 → 定点回写（SaveProviderAliases）」，
// 不做整份 aliases 文本往返。
export const deepcodeAliasMap = ref({})       // {内置ID: 目标模型}（值可带 @服务商 消歧后缀）
export const deepcodeAliasLoaded = ref(false) // 是否已成功从磁盘载入：false 时保存跳过回写，防止误清
export const deepcodeRouterModels = ref([])   // 映射目标下拉候选（接入方式返回的模型池）
export const deepcodeToOpenFor = ref('')      // 当前展开下拉的内置 ID（同一时刻仅一个）
export const deepcodeToFilter = ref('')       // 下拉过滤词

function emptyAliasMap() {
  const m = {}
  for (const id of DEEPCODE_BUILTIN_MODELS) m[id] = ''
  return m
}
export function deepcodeSetAlias(k, v) {
  deepcodeAliasMap.value = { ...deepcodeAliasMap.value, [k]: String(v || '') }
}
// 映射目标下拉：剔除内置 ID 自身（内置 ID 已作为别名混进模型池，留着会形成自映射选项）
export function deepcodeToGroups() {
  const s = String(deepcodeToFilter.value || '').toLowerCase().trim()
  const builtin = new Set(DEEPCODE_BUILTIN_MODELS)
  const list = deepcodeRouterModels.value.slice().sort().filter(m => {
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
export function pickDeepcodeTo(k, m) {
  deepcodeSetAlias(k, m)
  deepcodeToOpenFor.value = ''
}
// 字母键定位当前字母分组（与其它客户端 picker 一致）
export function deepcodeToKeyNav(e) {
  if (!/^[a-zA-Z0-9]$/.test(e.key)) return
  const drop = e.currentTarget.closest('.pi-sel')?.querySelector('.pi-sel-drop')
  if (!drop) return
  const L = e.key.toUpperCase()
  for (const el of drop.querySelectorAll('[data-letter]')) {
    if (el.dataset.letter.toUpperCase() === L) { el.scrollIntoView({ block: 'start' }); e.preventDefault(); return }
  }
}
// 映射目标候选：优先接入方式返回的模型列表（/oai），拿不到再退回配置聚合
export async function loadDeepcodeModelLists() {
  let models = []
  try {
    if (api.GetModelsByFormat) {
      const r = await api.GetModelsByFormat('/oai')
      for (const m of (r?.models || [])) {
        if (m && !String(m).includes('*')) models.push(String(m))
      }
    }
  } catch (e) {}
  if (!models.length && api.GetConfig) {
    try {
      const cfg = await api.GetConfig()
      for (const p of (cfg?.providers || [])) {
        if (!p.enabled) continue
        for (const m of [...(p.models || []), ...Object.keys(p.aliases || {})]) {
          if (m && !String(m).includes('*')) models.push(String(m))
        }
      }
    } catch (e) {}
  }
  deepcodeRouterModels.value = [...new Set(models)].sort()
}
// 载入映射面板：从已启用服务商的 aliases 里挑出这 4 个内置键回显
export async function loadDeepcodeAliasPanel() {
  deepcodeAliasLoaded.value = false
  deepcodeAliasMap.value = emptyAliasMap()
  try {
    const cfg = (await api.GetConfig()) || {}
    const builtin = new Set(DEEPCODE_BUILTIN_MODELS)
    for (const p of (cfg.providers || [])) {
      if (p.enabled === false) continue
      for (const [k, v] of Object.entries(p.aliases || {})) {
        if (builtin.has(k)) deepcodeAliasMap.value[k] = String(v || '')
      }
    }
    deepcodeAliasLoaded.value = true
  } catch (e) {}
  try { await loadDeepcodeModelLists() } catch (e) {}
}
function eqStr(a, b) { return String(a || '').toLowerCase() === String(b || '').toLowerCase() }
// 目标模型归属服务商：@后缀 > 模型/别名命中 > 默认服务商 > 首个启用服务商
function resolveDeepcodeTargetProvider(target, hint, cfg, providers) {
  if (hint) {
    const p = providers.find(x => eqStr(x.id, hint) || eqStr(x.name, hint))
    if (p) return p
  }
  let hits = providers.filter(p =>
    (p.models || []).some(m => eqStr(m, target)) ||
    Object.keys(p.aliases || {}).some(a => eqStr(a, target)) ||
    Object.values(p.aliases || {}).some(v => eqStr(v, target)))
  if (!hits.length) hits = providers
  const def = hits.find(p => eqStr(p.id, cfg?.defaultProvider) || eqStr(p.name, cfg?.defaultProvider))
  return def || hits[0] || null
}
// 映射随配置窗口「保存」一并落盘：把面板上的目标模型写回对应服务商的 aliases。
// 返回错误消息（'' = 成功；面板未成功载入则跳过，返回 ''）。
export async function applyDeepcodeAliases() {
  if (!deepcodeAliasLoaded.value) return ''
  try {
    const cfg = (await api.GetConfig()) || {}
    const providers = (cfg.providers || []).filter(p => p.enabled !== false)
    // 本次期望写入：{ 服务商key: { 内置ID: 干净目标模型 } }
    const want = {}
    for (const id of DEEPCODE_BUILTIN_MODELS) {
      let v = String(deepcodeAliasMap.value[id] || '').trim()
      if (!v) continue
      let hint = ''
      const at = v.lastIndexOf('@')
      if (at > 0) {
        const h = v.slice(at + 1).trim()
        if (h && !/^\d/.test(h)) { hint = h; v = v.slice(0, at).trim() }
      }
      if (!v) continue
      const p = resolveDeepcodeTargetProvider(v, hint, cfg, providers)
      if (!p) continue
      const pk = p.id || p.name
      ;(want[pk] = want[pk] || {})[id] = v
    }
    // 回写范围：本次要写的服务商 + 磁盘上仍残留我方键的服务商（清空映射时要能删掉）
    const builtin = new Set(DEEPCODE_BUILTIN_MODELS)
    const writes = []
    for (const p of providers) {
      const pk = p.id || p.name
      const cur = p.aliases || {}
      const hasMine = Object.keys(cur).some(k => builtin.has(k))
      const add = want[pk] || {}
      if (!hasMine && !Object.keys(add).length) continue
      const next = {}
      for (const [k, v] of Object.entries(cur)) if (!builtin.has(k)) next[k] = v // 其它别名原样保留
      Object.assign(next, add)
      writes.push({ pk, map: next })
    }
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
