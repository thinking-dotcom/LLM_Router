// Cline（cline_mcp_settings.json，独立版桌面端）：models.json / providers.json /
// globalState.json 表单与源码；MCP 服务由统一「MCP」页签注入管理。
import { ref, computed, watch, nextTick } from 'vue'
import { toast } from '../../toast'
import { t } from '../../i18n'
import { srcObj, srcObjValid } from './shared'
import { cfgEdit, cfgTarget, cfgMode, derivedBaseUrl, modelOptions, loadModelOptions } from './store'

export const clineModelsContent = ref('')
export const clineModelsExists = ref(false)
export const clineModelsFile = ref('~/.cline/data/settings/models.json')
export const clineProvidersContent = ref('')
export const clineProvidersExists = ref(false)
export const clineProvidersFile = ref('~/.cline/data/settings/providers.json')
export const clineAgentsContent = ref('')
export const clineAgentsExists = ref(false)
export const clineAgentsFile = ref('~/.agents/AGENTS.md')
export const clineGlobalStateContent = ref('')
export const clineGlobalStateExists = ref(false)
export const clineGlobalStateFile = ref('~/.cline/data/globalState.json')

// ---- Cline models.json（模型清单，官方字段：providers.<key>.provider{name,baseUrl,defaultModelId}
// + models.<id>{id,name,contextWindow,maxInputTokens,capabilities[],reasoning{enabled,effort}}）----
export const CLINE_CAP_KEYS = ['streaming', 'tools', 'images', 'vision', 'reasoning', 'audio']
export const CLINE_REASONING_EFFORT_OPTIONS = ['minimal', 'low', 'medium', 'high']
export function newClineModelRow() {
  return { id: '', name: '', contextWindow: '', maxInputTokens: '', caps: ['streaming'], reasoningEnabled: true, reasoningEffort: 'high' }
}
export function toggleClineCap(row, cap) {
  const s = new Set(row.caps)
  if (s.has(cap)) s.delete(cap)
  else s.add(cap)
  row.caps = [...s]
}
export const clineModelsForm = ref({ key: '', providerName: '', baseUrl: '', defaultModelId: '', models: [] })
export const clineDefModelOpen = ref(false) // 服务商默认模型下拉
export const clineModelSel = ref(null)      // 模型行 ID 下拉（记录行对象而非下标，删除行后不错位）
export const clinePanel = ref('models')      // Cline 编辑页：models.json / providers.json / globalState.json 三面板展开态（''=全部收起）
export const clinePanelOrder = ref(['models', 'caps', 'providers', 'globalstate']) // 面板固定显示顺序（不随展开换位）
export function clineTogglePanel(id) {
  // 原地展开/收起：不改变面板顺序，避免点击后其它面板跳动到底部
  clinePanel.value = clinePanel.value === id ? '' : id
}
export function clinePanelIdx(id) { return clinePanelOrder.value.indexOf(id) }
// Cline 连接凭据与默认（providers.json 首个 provider 的 settings）：
// apiKey / 默认模型 / headers / options / 推理兜底；baseUrl 为连接地址，表单只读展示
export const clineConnModelOpen = ref(false)
export const clineConnForm = ref({ apiKey: '', model: '', headersText: '', optionsText: '', reasoningEnabled: false, reasoningEffort: '', tokenSource: '', updatedAt: '' })
// 最近使用提供方（providers.json 顶层 lastUsedProvider）
export const clineLastUsed = ref('')
export const clineProviderKeys = computed(() => {
  const c = srcObj(clineProvidersContent.value)
  const provs = (c.providers && typeof c.providers === 'object') ? c.providers : {}
  return Object.keys(provs)
})
export const connBaseUrlText = computed(() => {
  const c = srcObj(clineProvidersContent.value)
  const provs = (c.providers && typeof c.providers === 'object') ? c.providers : {}
  const key = Object.keys(provs)[0] || ''
  const s = (provs[key] && provs[key].settings && typeof provs[key].settings === 'object') ? provs[key].settings : {}
  return String(s.baseUrl || '').trim() || derivedBaseUrl()
})
export function initClineConnForm() {
  const c = srcObj(clineProvidersContent.value)
  const provs = (c.providers && typeof c.providers === 'object') ? c.providers : {}
  const key = Object.keys(provs)[0] || ''
  const p = (provs[key] && typeof provs[key] === 'object') ? provs[key] : {}
  const s = (p.settings && typeof p.settings === 'object') ? p.settings : {}
  clineConnForm.value = {
    apiKey: s.apiKey || '',
    model: s.model || '',
    headersText: (s.headers && typeof s.headers === 'object') ? JSON.stringify(s.headers, null, 2) : '',
    optionsText: (s.options && typeof s.options === 'object') ? JSON.stringify(s.options, null, 2) : '',
    reasoningEnabled: !!(s.reasoning && s.reasoning.enabled),
    reasoningEffort: (s.reasoning && s.reasoning.effort) || '',
    tokenSource: String(p.tokenSource || ''),
    updatedAt: String(p.updatedAt || ''),
  }
  clineLastUsed.value = String(c.lastUsedProvider || '')
}
export let clineConnMuted = false
export function setClineConnMuted(v) { clineConnMuted = v }
// Cline 全局默认（globalState.json）：act/plan 模式默认模型/档位 + 常用运行时开关；
// 其余字段走源码页签
export const clineGlobalActOpen = ref(false)
export const clineGlobalPlanOpen = ref(false)
const triStr = v => v === true || v === 'true' ? 'true' : (v === false || v === 'false') ? 'false' : ''
export const CLINE_GLOBAL_TERMINAL_OPTIONS = ['cmd', 'powershell', 'bash', 'pwsh', 'zsh']
export const CLINE_GLOBAL_PROVIDER_OPTIONS = ['openai', 'anthropic', 'ollama', 'lmstudio']
export const CLINE_GLOBAL_TERMINAL_EXEC_OPTIONS = ['backgroundExec', 'foreground']
// autoApprovalSettings.actions 的官方操作键：以「卡片名称 + true/false 双态点选」展示
export const CLINE_AA_ACTIONS = ['editFiles', 'editFilesExternally', 'executeAllCommands', 'executeSafeCommands', 'readFiles', 'readFilesExternally', 'useBrowser', 'useMcp']
export const clineGlobalForm = ref({
  actModel: '', planModel: '', actEffort: '', planEffort: '',
  actProvider: '', planProvider: '', vscodeTerminalExec: '', maxRequests: '', liteBaseUrl: '',
  mode: '', defaultTerminalProfile: '', preferredLanguage: '', mcpDisplayMode: '',
  telemetrySetting: '', subagentsEnabled: '', backgroundEditEnabled: '', showFeatureTips: '', autoApproveEnabled: '',
})
// 逐项自动审批：{ 操作键: ''|'true'|'false' }，卡片点选 true/false（再点取消）
export const clineGlobalActions = ref({})
export function initClineGlobalForm() {
  const c = srcObj(clineGlobalStateContent.value)
  const aas = c.autoApprovalSettings && typeof c.autoApprovalSettings === 'object' ? c.autoApprovalSettings : {}
  clineGlobalForm.value = {
    actModel: String(c.actModeApiModelId || ''),
    planModel: String(c.planModeApiModelId || ''),
    actEffort: String(c.actModeReasoningEffort || ''),
    planEffort: String(c.planModeReasoningEffort || ''),
    actProvider: String(c.actModeApiProvider || ''),
    planProvider: String(c.planModeApiProvider || ''),
    vscodeTerminalExec: String(c.vscodeTerminalExecutionMode || ''),
    maxRequests: aas.maxRequests != null ? String(aas.maxRequests) : '',
    liteBaseUrl: String(c.liteLlmBaseUrl || ''),
    mode: String(c.mode || ''),
    defaultTerminalProfile: String(c.defaultTerminalProfile || ''),
    preferredLanguage: String(c.preferredLanguage || ''),
    mcpDisplayMode: String(c.mcpDisplayMode || ''),
    telemetrySetting: String(c.telemetrySetting || ''),
    subagentsEnabled: triStr(c.subagentsEnabled),
    backgroundEditEnabled: triStr(c.backgroundEditEnabled),
    showFeatureTips: triStr(c.showFeatureTips),
    autoApproveEnabled: triStr(aas.enabled),
  }
  const acts = aas.actions && typeof aas.actions === 'object' ? aas.actions : {}
  clineGlobalActions.value = {}
  for (const k of CLINE_AA_ACTIONS) clineGlobalActions.value[k] = triStr(acts[k])
}
export let clineGlobalMuted = false
export function setClineGlobalMuted(v) { clineGlobalMuted = v }
export function clineModelsRow(src = {}) {
  const s = src && typeof src === 'object' ? src : {}
  const r = (s.reasoning && typeof s.reasoning === 'object') ? s.reasoning : {}
  return {
    id: s.id || '',
    name: s.name || '',
    contextWindow: s.contextWindow != null ? String(s.contextWindow) : '',
    maxInputTokens: s.maxInputTokens != null ? String(s.maxInputTokens) : '',
    caps: Array.isArray(s.capabilities) ? s.capabilities.map(String) : [],
    reasoningEnabled: (s.reasoning && typeof s.reasoning === 'object') ? !!(r.enabled) : true,
    reasoningEffort: r.effort || 'high',
  }
}
// Cline 模型清单点选：从接入方式（路由器模型列表 /oai）一次展示全部模型 id，
// 点选加入/移出当前服务商（clineModelsForm.key）的 models 条目
export const clinePickOpen = ref(false)
export const clinePickQ = ref('')
export function clinePickModels() {
  const q = String(clinePickQ.value || '').toLowerCase().trim()
  return [...new Set(modelOptions.value.map(String).filter(Boolean))].sort().filter(m => !q || String(m).toLowerCase().includes(q))
}
export function clineModelOn(m) {
  return clineModelsForm.value.models.some(r => String(r.id || '').trim() === m)
}
export function clineToggleModel(m) {
  const rows = clineModelsForm.value.models
  const i = rows.findIndex(r => String(r.id || '').trim() === m)
  if (i >= 0) rows.splice(i, 1)
  else rows.push({ ...newClineModelRow(), id: m })
}
export async function clineFillModelsFromList() {
  if (!modelOptions.value.length) await loadModelOptions()
  const list = [...new Set(modelOptions.value.map(String).filter(Boolean))].sort()
  if (!list.length) { toast(t('sync.modelsEmptyToast')); return }
  const ids = new Set(clineModelsForm.value.models.map(r => String(r.id || '').trim()).filter(Boolean))
  let added = 0
  for (const m of list) {
    if (!ids.has(m)) { clineModelsForm.value.models.push({ ...newClineModelRow(), id: m }); ids.add(m); added++ }
  }
  toast(added ? t('sync.fillModelsToast', { added, total: clineModelsForm.value.models.length }) : t('sync.fillModelsAll'))
}
// 模型清单自动填充：打开配置窗口时按提供商（路由器 /oai 模型列表）自动并入全部模型，
// 无需手动点选，保留用户已有条目；有新增才写回源码（静默避免来回触发）
export async function clineAutoFillModels() {
  if (!modelOptions.value.length) { try { await loadModelOptions() } catch (e) { return } }
  const list = [...new Set(modelOptions.value.map(String).filter(Boolean))].sort()
  if (!list.length) return
  const rows = clineModelsForm.value.models
  const ids = new Set(rows.map(r => String(r.id || '').trim()).filter(Boolean))
  let added = 0
  for (const m of list) {
    if (!ids.has(m)) { rows.push({ ...newClineModelRow(), id: m }); ids.add(m); added++ }
  }
  if (!added) return
  const prevMuted = clineModelsMuted
  clineModelsMuted = true
  try { applyClineModelsForm() } catch (e) {}
  nextTick(() => { clineModelsMuted = prevMuted || false })
}
export function initClineModelsForm() {
  const c = srcObj(clineModelsContent.value)
  const provs = (c.providers && typeof c.providers === 'object') ? c.providers : {}
  const key = Object.keys(provs)[0] || 'openai-compatible'
  const p = (provs[key] && typeof provs[key] === 'object') ? provs[key] : {}
  const pv = (p.provider && typeof p.provider === 'object') ? p.provider : {}
  const ms = (p.models && typeof p.models === 'object' && !Array.isArray(p.models)) ? p.models : {}
  clineModelsForm.value = {
    key,
    providerName: pv.name || '',
    baseUrl: pv.baseUrl || '',
    defaultModelId: pv.defaultModelId || '',
    models: Object.keys(ms).map(id => clineModelsRow({ ...(ms[id] || {}), id })),
  }
  initClineCaps()
}
// ----「能力」统一面板：不再按模型单独设置，一个面板控制所有模型的 capabilities ----
export let clineCapsMuted = false
export function setClineCapsMuted(v) { clineCapsMuted = v }
export const clineCaps = ref([]) // 当前激活的能力键（多选）
function initClineCaps() {
  const set = new Set()
  for (const r of clineModelsForm.value.models) for (const c of (r.caps || [])) set.add(typeof c === 'string' ? c : String(c))
  clineCaps.value = CLINE_CAP_KEYS.filter(k => set.has(k))
}
export function clineToggleCap(k) {
  const i = clineCaps.value.indexOf(k)
  if (i >= 0) clineCaps.value.splice(i, 1)
  else clineCaps.value.push(k)
}
export function applyClineModelsForm() {
  const c = srcObj(clineModelsContent.value)
  if (!c.providers || typeof c.providers !== 'object') c.providers = {}
  const key = String(clineModelsForm.value.key || '').trim() || 'openai-compatible'
  const prev = (c.providers[key] && typeof c.providers[key] === 'object') ? c.providers[key] : {}
  const pv = {}
  if (String(clineModelsForm.value.providerName || '').trim()) pv.name = String(clineModelsForm.value.providerName).trim()
  if (String(clineModelsForm.value.baseUrl || '').trim()) pv.baseUrl = String(clineModelsForm.value.baseUrl).trim()
  if (String(clineModelsForm.value.defaultModelId || '').trim()) pv.defaultModelId = String(clineModelsForm.value.defaultModelId).trim()
  const models = {}
  for (const row of clineModelsForm.value.models) {
    const id = String(row.id || '').trim()
    if (!id) continue
    const entry = { id }
    if (String(row.name || '').trim()) entry.name = String(row.name).trim()
    if (String(row.contextWindow || '').trim()) entry.contextWindow = Number(row.contextWindow)
    if (String(row.maxInputTokens || '').trim()) entry.maxInputTokens = Number(row.maxInputTokens)
    if (row.caps.length) entry.capabilities = row.caps.slice()
    if (row.reasoningEnabled || String(row.reasoningEffort || '').trim()) {
      const r = {}
      if (row.reasoningEnabled) r.enabled = true
      if (String(row.reasoningEffort || '').trim()) r.effort = String(row.reasoningEffort).trim()
      entry.reasoning = r
    }
    models[id] = entry
  }
  c.providers[key] = { ...prev, ...(Object.keys(pv).length ? { provider: pv } : {}), models }
  clineModelsContent.value = JSON.stringify(c, null, 2) + '\n'
}
export let clineModelsMuted = false
export function setClineModelsMuted(v) { clineModelsMuted = v }

// watchers 在 init 中注册（组件 setup 上下文内）
let clineAutoFilled = false
export function initCline() {
  // 路由器模型列表就绪后，自动按提供商补填模型清单（首次生效，之后不再重复）
  watch(modelOptions, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'cline' || clineAutoFilled) return
    if (!modelOptions.value.length) return
    clineAutoFilled = true
    clineAutoFillModels()
  })
  watch([clineConnForm, clineLastUsed], () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'cline' || clineConnMuted) return
    if (!srcObjValid(clineProvidersContent.value)) return // 源码非法：不回写，待源码页签修复
    const c = srcObj(clineProvidersContent.value)
    if (!c.providers || typeof c.providers !== 'object') c.providers = {}
    let key = Object.keys(c.providers)[0]
    if (!key) {
      // providers.json 为空/不存在时自动建默认服务商条目，避免连接凭据表单的编辑被静默丢弃
      key = 'openai-compatible'
      c.providers[key] = { settings: {} }
    }
    const p = (c.providers[key] && typeof c.providers[key] === 'object') ? c.providers[key] : {}
    const s = (p.settings && typeof p.settings === 'object') ? p.settings : {}
    const f = clineConnForm.value
    s.apiKey = String(f.apiKey || '')
    const m = String(f.model || '').trim()
    if (m) s.model = m
    else delete s.model
    // 解析 JSON 对象文本：''→删除标记；非法→null（保留旧值）；合法对象→对象
    const parseObj = t => {
      const txt = String(t || '').trim()
      if (!txt) return ''
      try { const v = JSON.parse(txt); return (v && typeof v === 'object' && !Array.isArray(v)) ? v : '' } catch (e) { return null }
    }
    const h = parseObj(f.headersText)
    if (h === null) { /* 非法源码：保留旧值 */ }
    else if (h === '') delete s.headers
    else s.headers = h
    const o = parseObj(f.optionsText)
    if (o === null) { /* 非法源码：保留旧值 */ }
    else if (o === '') delete s.options
    else s.options = o
    const r = {}
    if (f.reasoningEnabled) r.enabled = true
    if (String(f.reasoningEffort || '').trim()) r.effort = String(f.reasoningEffort).trim()
    if (Object.keys(r).length) s.reasoning = r
    else delete s.reasoning
    // tokenSource 位于 provider 条目层（非 settings）；updatedAt 只读展示、不写回
    const ts = String(f.tokenSource || '').trim()
    if (ts) p.tokenSource = ts
    else delete p.tokenSource
    c.providers[key] = { ...p, settings: s }
    const lu = String(clineLastUsed.value || '').trim()
    if (lu) c.lastUsedProvider = lu
    else delete c.lastUsedProvider
    clineProvidersContent.value = JSON.stringify(c, null, 2) + '\n'
  }, { deep: true })
  // 反向：providers.json 源码页签输入 → 实时回填连接凭据表单（静默避免互相触发/打断输入）
  watch(clineProvidersContent, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'cline' || clineConnMuted || cfgMode.value !== 'clineproviders') return
    clineConnMuted = true
    initClineConnForm()
    nextTick(() => { clineConnMuted = false })
  })
  watch([clineGlobalForm, clineGlobalActions], () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'cline' || clineGlobalMuted) return
    if (!srcObjValid(clineGlobalStateContent.value)) return // 源码非法：不回写，待源码页签修复
    const c = srcObj(clineGlobalStateContent.value)
    const f = clineGlobalForm.value
    const setOrDel = (k, v) => { const t = String(v || '').trim(); if (t) c[k] = t; else delete c[k] }
    const boolOrDel = (k, v) => { const t = String(v || '').trim(); if (t === 'true') c[k] = true; else if (t === 'false') c[k] = false; else delete c[k] }
    setOrDel('actModeApiModelId', f.actModel)
    setOrDel('planModeApiModelId', f.planModel)
    setOrDel('actModeReasoningEffort', f.actEffort)
    setOrDel('planModeReasoningEffort', f.planEffort)
    setOrDel('actModeApiProvider', f.actProvider)
    setOrDel('planModeApiProvider', f.planProvider)
    setOrDel('vscodeTerminalExecutionMode', f.vscodeTerminalExec)
    setOrDel('mode', f.mode)
    setOrDel('defaultTerminalProfile', f.defaultTerminalProfile)
    setOrDel('preferredLanguage', f.preferredLanguage)
    setOrDel('mcpDisplayMode', f.mcpDisplayMode)
    setOrDel('telemetrySetting', f.telemetrySetting)
    boolOrDel('subagentsEnabled', f.subagentsEnabled)
    boolOrDel('backgroundEditEnabled', f.backgroundEditEnabled)
    boolOrDel('showFeatureTips', f.showFeatureTips)
    // autoApprovalSettings 保留其它子键，仅重建 enabled / maxRequests / actions
    const aas = (c.autoApprovalSettings && typeof c.autoApprovalSettings === 'object') ? { ...c.autoApprovalSettings } : {}
    const at = String(f.autoApproveEnabled || '').trim()
    if (at === 'true') aas.enabled = true
    else if (at === 'false') aas.enabled = false
    else delete aas.enabled
    const mr = String(f.maxRequests || '').trim()
    if (mr) { const n = Number(mr); if (Number.isFinite(n)) aas.maxRequests = n }
    else delete aas.maxRequests
    const actions = (aas.actions && typeof aas.actions === 'object') ? { ...aas.actions } : {}
    for (const k of CLINE_AA_ACTIONS) {
      const t = String(clineGlobalActions.value[k] || '').trim()
      if (t === 'true') actions[k] = true
      else if (t === 'false') actions[k] = false
      else delete actions[k]
    }
    if (Object.keys(actions).length) aas.actions = actions
    else delete aas.actions
    if (Object.keys(aas).length) c.autoApprovalSettings = aas
    else delete c.autoApprovalSettings
    clineGlobalStateContent.value = JSON.stringify(c, null, 2) + '\n'
  }, { deep: true })
  // 反向：globalState.json 源码页签输入 → 实时回填全局默认表单（静默避免互相触发/打断输入）
  watch(clineGlobalStateContent, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'cline' || clineGlobalMuted || cfgMode.value !== 'clineglobalstate') return
    clineGlobalMuted = true
    initClineGlobalForm()
    nextTick(() => { clineGlobalMuted = false })
  })
  watch(clineModelsForm, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'cline' || clineModelsMuted) return
    if (!srcObjValid(clineModelsContent.value)) return // 源码非法：不回写，待源码页签修复
    try { applyClineModelsForm() } catch (e) {}
  }, { deep: true })
  // 「能力」统一面板 → 同步到所有模型的 capabilities 数组
  watch(clineCaps, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'cline' || clineCapsMuted) return
    const active = new Set(clineCaps.value)
    for (const row of clineModelsForm.value.models) {
      const caps = new Set(row.caps)
      for (const k of CLINE_CAP_KEYS) {
        if (active.has(k)) caps.add(k)
        else caps.delete(k)
      }
      row.caps = [...caps]
    }
  }, { deep: true })
  // 反向：models.json 源码页签输入 → 实时回填表单（静默避免互相触发）
  watch(clineModelsContent, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'cline' || clineModelsMuted || cfgMode.value !== 'clinemodels') return
    clineModelsMuted = true
    clineCapsMuted = true
    initClineModelsForm()
    nextTick(() => { clineCapsMuted = false; clineModelsMuted = false })
  })
}

// ---- Cline providers.json（源码页签编辑；连接凭据 apiKey/baseUrl 等官方 settings 字段
// 不再提供表单，需要时走「providers.json」源码页签手改）----