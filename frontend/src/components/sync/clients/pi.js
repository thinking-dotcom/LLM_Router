// pi（~/.pi/）：models.json（表单+源码）、settings.json、mcp.json、AGENTS.md
// 受管键与模版文件一一映射；base_url 自动指向路由器。
import { ref, watch, nextTick } from 'vue'
import { toast } from '../../../toast'
import { t } from '../../../i18n'
import api from '../../../api'
import { srcObj, deepMerge } from '../shared'
import { cfgContent, cfgObj, cfgEdit, cfgTarget, genericMuted, jsonEcho, routerBase, derivedBaseUrl } from '../store'

export const PI_CONTEXT_OPTIONS = [4096, 8192, 16384, 32768, 65536, 128000, 200000, 256000, 1000000]
export const PI_SETTINGS_THINKING_OPTIONS = ['minimal', 'low', 'medium', 'high', 'max']
export const PI_THEME_OPTIONS = ['dark', 'light']
export const PI_STEERING_OPTIONS = ['enter', 'alt-enter']
export const PI_TRUST_OPTIONS = ['ask', 'always', 'never']
export const PI_PROVIDER_API_OPTIONS = [
  { value: 'openai-completions', label: () => 'openai-completions（' + t('sync.optOpenaiCompat') + '）' },
  { value: 'anthropic', label: () => 'anthropic' },
  { value: 'google-generative-ai', label: () => 'google-generative-ai' },
]

// ============ pi 全局设置（~/.pi/agent/settings.json） ============
export function piSettingsPreset() {
  return {
    lastChangelogVersion: '0.74.0',
    defaultProvider: 'LLM',
    defaultModel: 'x-preview-f-free',
    defaultThinkingLevel: 'high',
    theme: 'dark',
    steeringMode: 'enter',
    followUpMode: 'alt-enter',
    defaultProjectTrust: 'ask',
  }
}
export const piSettings = ref({
  lastChangelogVersion: '', defaultProvider: '', defaultModel: '',
  defaultThinkingLevel: 'high', theme: 'dark', steeringMode: 'enter',
  followUpMode: 'alt-enter', defaultProjectTrust: 'ask',
})
export const piSettingsContent = ref('') // 模版文件源码（编辑源）
export const piSettingsExists = ref(false)
export const piSettingsFile = ref('~/.pi/agent/settings.json')
// pi mcp.json（~/.pi/agent/mcp.json，MCP 服务配置源码直编）
export const piMcpContent = ref('')
export const piMcpExists = ref(false)
export const piMcpFile = ref('~/.pi/agent/mcp.json')
export function initPiSettingsForm(c = {}) {
  piSettings.value = {
    lastChangelogVersion: c.lastChangelogVersion || '',
    defaultProvider: c.defaultProvider || '',
    defaultModel: c.defaultModel || '',
    defaultThinkingLevel: c.defaultThinkingLevel || 'high',
    theme: c.theme || 'dark',
    steeringMode: c.steeringMode || 'enter',
    followUpMode: c.followUpMode || 'alt-enter',
    defaultProjectTrust: c.defaultProjectTrust || 'ask',
  }
}
export function piSettingsBuildText() {
  const f = piSettings.value
  const generated = {}
  for (const k of ['defaultProvider', 'defaultModel', 'defaultThinkingLevel', 'theme', 'steeringMode', 'followUpMode', 'defaultProjectTrust', 'lastChangelogVersion']) {
    if (String(f[k]).trim()) generated[k] = String(f[k]).trim()
  }
  // 合并模式：保留 settings.json 里面板未覆盖的额外键
  return JSON.stringify(deepMerge(srcObj(piSettingsContent.value), generated), null, 2)
}
export function syncSettingsFromSource() {
  try {
    const v = JSON.parse(piSettingsContent.value || '{}')
    initPiSettingsForm(v && typeof v === 'object' ? v : {})
    return true
  } catch (e) { toast(t('sync.jsonError')); return false }
}

// pi AGENTS.md（全局编码规范，Markdown 源码直编）
export const piAgentsContent = ref('')
export const piAgentsExists = ref(false)
export const piAgentsFile = ref('~/.pi/agent/AGENTS.md')

// ============ pi models.json（提供商与模型，字段与模版文件一一映射） ============
export const piM = ref({
  key: 'LLM',
  api: 'openai-completions',
  baseUrl: 'http://127.0.0.1:3000',
  apiKey: '',
  models: [],
})
export function newPiModelRow(src = {}) {
  const m = src && typeof src === 'object' ? src : {}
  return {
    id: m.id || '',
    name: m.name || '',
    // 原生思考深度：pi 仅接受 low/high/max（minimal/medium 会报 [1210]）
    thinkingLevel: m.thinkingLevel || '',
    contextWindow: m.contextWindow != null ? String(m.contextWindow) : '',
    reasoning: m.reasoning === true,
    supportsTools: m.supportsTools !== false,
    supportsVision: m.supportsVision === true,
  }
}
export function initPiForm() {
  const provs = cfgObj.value?.providers || {}
  const keys = Object.keys(provs)
  const key = keys[0] || 'LLM'
  const p = provs[key] || {}
  piM.value = {
    key,
    api: p.api || 'openai-completions',
    baseUrl: p.baseUrl || (routerBase.value || 'http://127.0.0.1:3000') + '/router',
    apiKey: p.apiKey || '',
    models: Array.isArray(p.models) ? p.models.map(newPiModelRow) : [newPiModelRow()],
  }
}
export function piBuildModelsText() {
  const key = String(piM.value.key || '').trim() || 'LLM'
  // 合并写回（无模板中转）：仅更新受管的单个 provider（piM.key），
  // 其余 provider、模型条目上的未管字段（cost 等）原样保留
  const managedModelKeys = ['id', 'name', 'contextWindow', 'input', 'reasoning', 'supportsTools', 'supportsVision', 'thinkingLevel']
  const c = srcObj(cfgContent.value)
  if (!c.providers || typeof c.providers !== 'object') c.providers = {}
  const prev = (c.providers[key] && typeof c.providers[key] === 'object') ? c.providers[key] : {}
  const prevModels = Array.isArray(prev.models) ? prev.models : []
  const prevById = new Map(prevModels
    .filter(m => m && typeof m === 'object' && String(m.id || '').trim())
    .map(m => [String(m.id).trim(), m]))
  const models = []
  for (const m of piM.value.models) {
    const id = String(m.id || '').trim()
    if (!id) continue
    const old = prevById.get(id) || {}
    const o = {}
    // 旧条目未管字段原样保留在前
    for (const k of Object.keys(old)) if (!managedModelKeys.includes(k)) o[k] = old[k]
    o.id = id
    o.name = (String(m.name || '').trim()) ? String(m.name).trim() : id
    o.contextWindow = Number(m.contextWindow) || (Number(old.contextWindow) || 128000)
    o.input = Array.isArray(old.input) ? old.input : ['text']
    o.reasoning = m.reasoning === true
    o.supportsTools = m.supportsTools !== false
    o.supportsVision = m.supportsVision === true
    if (String(m.thinkingLevel || '').trim()) o.thinkingLevel = String(m.thinkingLevel).trim()
    models.push(o)
  }
  const entry = { ...prev } // provider 级未管键原样保留
  entry.api = piM.value.api || 'openai-completions'
  entry.apiKey = piM.value.apiKey
  entry.baseUrl = piM.value.baseUrl
  entry.models = models
  c.providers = { ...c.providers, [key]: entry }
  return JSON.stringify(c, null, 2)
}

// ---- pi：模型列表按填入的 baseUrl 实时拉取（600ms 防抖） ----
export const piBaseModels = ref([])
export const piFetchLoading = ref(false)
let piFetchTimer = null
let piFetchSeq = 0
export function piApiToType(api) {
  if (api === 'anthropic') return 'anthropic'
  if (api === 'google-generative-ai') return 'gemini'
  return 'openai'
}
export async function fetchPiBaseModels() {
  // 弹窗已关闭 / 切走时作废在途的防抖拉取，避免关窗后仍发请求
  if (!cfgEdit.value || cfgTarget.value?.id !== 'pi') {
    piBaseModels.value = []
    piFetchLoading.value = false
    return
  }
  const url = String(piM.value.baseUrl || '').trim()
  if (!url) { piBaseModels.value = []; piFetchLoading.value = false; return }
  const seq = ++piFetchSeq
  piFetchLoading.value = true
  try {
    const provider = {
      name: String(piM.value.key || '').trim() || 'pi-custom',
      type: piApiToType(piM.value.api),
      baseUrl: url,
      apiKey: String(piM.value.apiKey || '').trim(),
    }
    const r = await api.FetchModels(provider)
    if (seq !== piFetchSeq) return // 过期响应丢弃
    if (r && r.ok && Array.isArray(r.models) && r.models.length) piBaseModels.value = r.models.slice()
  } catch (e) {} finally {
    if (seq === piFetchSeq) piFetchLoading.value = false
  }
}

// 表单 → 模版 JSON 实时联动：填写面板时，源码文本框实时更新（watchers 在 init 中注册，
// 保证在组件 setup 上下文内建立，随组件生命周期清理）
export function initPi() {
  // 表单 → 模版 JSON 实时联动：填写可视化面板时，settings.json 源码文本框实时更新
  watch(piSettings, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'pi' || genericMuted || jsonEcho) return
    try { piSettingsContent.value = piSettingsBuildText() } catch (e) {}
  }, { deep: true })
  // 表单 → 模版 JSON 实时联动：填写 models 面板时，源码文本框实时更新
  // genericMuted：载入/切换页签期间 initPiForm 整体替换 piM 会触发本 watcher，
  // 若不静默会用仅含受管字段的文本覆写 cfgContent，丢失多 provider 与未管字段
  watch(piM, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'pi' || genericMuted || jsonEcho) return
    try { cfgContent.value = piBuildModelsText() } catch (e) {}
  }, { deep: true })
  // 模型列表按 base_url 实时拉取（600ms 防抖）
  watch(
    () => [piM.value.baseUrl, piM.value.api, piM.value.apiKey],
    () => {
      if (!cfgEdit.value || cfgTarget.value?.id !== 'pi') return
      clearTimeout(piFetchTimer)
      piFetchTimer = setTimeout(fetchPiBaseModels, 600)
    }
  )
}