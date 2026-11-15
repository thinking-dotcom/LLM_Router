// Clients.vue 拆分出的中央状态与通用辅助（对话框、目标列表、模型候选、接入地址）。
// 不依赖任何客户端模块；客户端模块与 controller 均从本文件命名导入。
import { ref } from 'vue'
import { toast } from '../../toast'
import { t } from '../../i18n'
import api from '../../api'
import { getPath, setPath } from './shared'

// ============ 对话框基础状态 ============
export const cfgEdit = ref(false)
export const cfgTarget = ref(null)
export const cfgLoading = ref(false)
export const cfgSaving = ref(false)
export const cfgMode = ref('form')
export const cfgFormat = ref('json')
export const cfgFile = ref('')
export const cfgExists = ref(true)
export const cfgContent = ref('')
export const cfgObj = ref({})

// ============ 目标客户端列表 ============
export const targets = ref([])
export const busy = ref({})
export const routerBase = ref('http://127.0.0.1:3000')
export async function refreshRouterBase() {
  try {
    if (!api.GetRouterBase) return
    const r = await api.GetRouterBase()
    if (r) routerBase.value = String(r).replace(/\/+$/, '') || routerBase.value
  } catch (e) {}
}
export async function loadTargets() {
  try {
    await refreshRouterBase()
    const r = await api.ListTargets()
    targets.value = Array.isArray(r) ? r : []
  } catch (e) {}
}

// ============ 模型候选 ============
export const modelOptions = ref([])
export const modelLoading = ref(false)
export function providerModelsPath() { return 'model' }
export function providerModelsValue() {
  const v = getPath(cfgObj.value, providerModelsPath())
  if (Array.isArray(v)) return v
  if (typeof v === 'string' && v) return [v]
  return []
}
export function setProviderModels(arr) {
  const p = providerModelsPath()
  if (arr.length === 0) setPath(cfgObj.value, p, '')
  else if (arr.length === 1) setPath(cfgObj.value, p, arr[0])
  else setPath(cfgObj.value, p, arr)
}
export function addModel(m) {
  if (!m) return
  const cur = providerModelsValue()
  if (!cur.includes(m)) cur.push(m)
  setProviderModels(cur)
}
export async function loadModelOptions() {
  modelLoading.value = true
  try {
    let models = []
    try {
      if (api.GetModelsByFormat) {
        const r = await api.GetModelsByFormat(currentAccessFmt() || (cfgTarget.value?.apiFormat || '/oai'))
        for (const m of (r?.models || [])) if (m && !String(m).includes('*')) models.push(String(m))
      }
    } catch (e) {}
    if (!models.length && api.GetConfig) {
      try {
        const cfg = await api.GetConfig()
        const provs = cfg?.providers || []
        const fmt = currentAccessFmt() || '/oai'
        const allowType = fmt === '/aai' ? ['anthropic'] : fmt === '/gai' ? ['gemini'] : null
        for (const p of provs) {
          if (!p.enabled) continue
          if (allowType && !allowType.includes(p.type)) continue
          let list = []
          if (api.GetProviderModels) { try { list = (await api.GetProviderModels(p))?.models || [] } catch (e) {} }
          for (const m of list) if (m && !String(m).includes('*')) models.push(String(m))
          for (const m of (p.models || [])) if (m && !String(m).includes('*')) models.push(String(m))
          for (const k of Object.keys(p.aliases || {})) if (k && !String(k).includes('*')) models.push(String(k))
        }
      } catch (e) {}
    }
    modelOptions.value = [...new Set(models)].sort()
  } finally { modelLoading.value = false }
}

// ============ 接入地址 / 页签辅助 ============
// 统一接入：所有客户端 base_url 均为路由器根地址，协议路径由客户端 SDK 自行追加，
// 路由器按路径识别协议并归一化，再按实际上游格式转换。
export function currentAccessFmt() {
  const id = cfgTarget.value?.id
  if (id === 'claude-code') return '/aai'
  return '/oai'
}
export function routerHintForTarget() {
  return (routerBase.value || 'http://127.0.0.1:3000') + '/router'
}
export function derivedBaseUrl(_accessMode) {
  return (routerBase.value || 'http://127.0.0.1:3000') + '/router'
}
export function syncToJson() {
  try { cfgContent.value = JSON.stringify(cfgObj.value, null, 2) } catch (e) { cfgContent.value = '{}' }
}
// 源码页签底部：以文件名为按钮，点击在系统文件管理器中定位真实配置文件
export function srcBarName(p) {
  const s = String(p || '')
  const i = Math.max(s.lastIndexOf('/'), s.lastIndexOf('\\'))
  return i >= 0 ? s.slice(i + 1) : s
}
export async function revealFile(p) {
  if (!p) return
  try {
    const r = await api.OpenFileLocation(p)
    if (!(r && r.ok)) toast(t('common.fail', { msg: r?.error || '?' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) }
}
// 各客户端真实配置路径清单（来自 GetClientRealPaths / client-paths.json），
// 源码页签文件名按钮据此打开真实所在目录，而非参考模板目录
export const targetRealPaths = ref({})
let clientPathsLoading = false
export async function ensureClientPaths() {
  if (clientPathsLoading || Object.keys(targetRealPaths.value).length) return
  clientPathsLoading = true
  try {
    const r = await api.GetClientRealPaths()
    if (r && r.ok && r.paths) targetRealPaths.value = r.paths
  } catch (e) {}
}
export function targetRealFile() {
  const tg = cfgTarget.value
  if (!tg) return ''
  const p = targetRealPaths.value[tg.id] || {}
  return p.config || tg.mcpFile || tg.configFile || ''
}
export function syncToObj() {
  try {
    const v = JSON.parse(cfgContent.value || '{}')
    cfgObj.value = v && typeof v === 'object' ? v : {}
    return true
  } catch (e) { toast(t('sync.jsonError')); return false }
}

// ============ 双向实时联动（通用 JSON 路径：claude-code 等） ============
// genericMuted：模板载入期间静默，避免载入赋值触发联动打乱源码键序
export let genericMuted = false
export function setGenericMuted(v) { genericMuted = v }
// jsonEcho：JSON 源码反向回填期间静默，防止回填触发的表单 watcher 再把源码覆写（避免光标跳动/输入被打断）
export let jsonEcho = false
export function setJsonEcho(v) { jsonEcho = v }
// 有专属联动的客户端不走通用 watcher（各家已有 deep watcher 或 TOML/YAML 原文流）；
// copilot 走「配置选项」BYOK 面板、不发主配置表单，同样排除避免通用表单污染 mcp-config.json
export const GENERIC_EXCLUDED_IDS = ['pi', 'opencode', 'grok-build', 'codex', 'commandcode', 'copilot', 'kimi', 'mimocode', 'agy', 'codebuddy', 'deepcode']