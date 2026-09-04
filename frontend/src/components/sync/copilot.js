// Copilot CLI 接入面板：端点三环境变量（BYOK）+ settings.json（模型/推理深度/URL 权限/
// 沙箱）+ permissions-config.json（工具审批，源码直编）
import { ref } from 'vue'
import { toast } from '../../toast'
import { t } from '../../i18n'
import api from '../../api'
import { routerBase, modelOptions } from './store'

// Copilot CLI 官方 BYOK：端点靠用户环境变量，模型/推理深度/权限靠 settings.json：
//   COPILOT_PROVIDER_BASE_URL → 本路由器接入地址 /router（OpenAI 兼容）
//   COPILOT_PROVIDER_TYPE     → openai（默认）
//   COPILOT_PROVIDER_API_KEY  → 路由令牌（可选，未设鉴权可不填）
//   settings.json：model（等价持久化 COPILOT_MODEL）、effortLevel（推理深度）、
//   allowedUrls/deniedUrls（URL 权限）、permissions.disableBypassPermissionsMode（提权限制）、
//   sandbox.*（OS 沙箱：enabled / allowBypass / userPolicy.network.allowLocalNetwork）
export const COPILOT_ENV_KEYS = ['COPILOT_PROVIDER_BASE_URL', 'COPILOT_PROVIDER_TYPE', 'COPILOT_PROVIDER_API_KEY']
export const copilotBase = ref('')     // 仅展示：实际写入时取路由器地址
export const copilotType = ref('')     // 仅展示：恒为 openai
export const copilotModel = ref('')    // 模型（可编辑，写入 settings.json model 键）
export const copilotApiKey = ref('')   // API Key（可编辑，空 = 用路由令牌）
export const copilotEffort = ref('')   // 推理深度 effortLevel（settings.json）
export const copilotBypass = ref('')   // permissions.disableBypassPermissionsMode（提权限制）
export const copilotAllow = ref('')    // allowedUrls（文本域，每行一条）
export const copilotDeny = ref('')     // deniedUrls（文本域，每行一条）
export const copilotSandboxEna = ref('')     // sandbox.enabled（''/true/false 三态）
export const copilotSandboxBypass = ref('')  // sandbox.allowBypass（''/true/false 三态）
export const copilotSandboxNet = ref('')     // sandbox.userPolicy.network.allowLocalNetwork
export const copilotLoading = ref(false)
export const copilotSaving = ref(false)
export const copilotModelOpen = ref(false)
export const COPILOT_EFFORT_OPTIONS = ['low', 'medium', 'high', 'xhigh']
export const COPILOT_BYPASS_OPTIONS = ['disable', 'allow-auto-only']
// 工具审批权限（~/.copilot/permissions-config.json，按项目位置保存）源码直编
export const copilotPermContent = ref('')
export const copilotPermExists = ref(false)
export const copilotPermFile = ref('~/.copilot/permissions-config.json')
// 工具审批（tool_approvals）点选列表：每条规则 { text, mode }，mode ∈ allow/ask/deny
export const copilotTools = ref([])
// allowed_directories 目录白名单（换行文本）
export const copilotDirs = ref('')
// 从 copilotPermContent 源码解析出结构化点选列表（打开时调用）
export function copilotPermFromContent() {
  let doc = {}
  try { doc = JSON.parse(copilotPermContent.value || '{}') } catch (e) { doc = {} }
  const ta = (doc.tool_approvals && typeof doc.tool_approvals === 'object' && !Array.isArray(doc.tool_approvals)) ? doc.tool_approvals : {}
  const rows = []
  for (const [pat, val] of Object.entries(ta)) {
    const mode = (val === 'ask' || val === 'deny') ? val : (val === 'allow' ? 'allow' : '')
    rows.push({ text: pat, mode })
  }
  copilotTools.value = rows
  copilotDirs.value = (Array.isArray(doc.allowed_directories) ? doc.allowed_directories : []).join('\n')
}
// 从点选列表生成 permissions-config.json 源码（保存时调用）
function copilotPermDoc() {
  const approvals = {}
  for (const r of copilotTools.value) {
    const p = String(r.text || '').trim()
    if (!p) continue
    approvals[p] = (r.mode === 'ask' || r.mode === 'deny') ? r.mode : 'allow'
  }
  const doc = { tool_approvals: approvals }
  const dirs = String(copilotDirs.value || '').split('\n').map(s => s.trim()).filter(Boolean)
  if (dirs.length) doc.allowed_directories = dirs
  return JSON.stringify(doc, null, 2) + '\n'
}
export function copilotRouterUrl() {
  return (routerBase.value || 'http://127.0.0.1:3000') + '/router'
}
async function copilotReadVars() {
  // 三个端点环境变量回显到 state
  try {
    const [b, tp, ak] = await Promise.all(COPILOT_ENV_KEYS.map(k => (api.GetUserEnv ? api.GetUserEnv(k) : null)))
    if (b && b.ok && b.exists) copilotBase.value = String(b.value || '')
    if (tp && tp.ok && tp.exists) copilotType.value = String(tp.value || '')
    if (ak && ak.ok && ak.exists) copilotApiKey.value = String(ak.value || '')
  } catch (e) {}
}
async function copilotReadSettings() {
  // settings.json 面板管理的全部键一次读回（JSONC 宽容），缺失回退模型列表首个
  try {
    if (!api.GetCopilotSettings) return
    const r = await api.GetCopilotSettings()
    if (!(r && r.ok)) {
      if (r && r.error) toast(t('common.fail', { msg: r.error }))
      return
    }
    const s = (r && r.settings) || {}
    if (s.model) copilotModel.value = String(s.model)
    if (s.effortLevel) copilotEffort.value = String(s.effortLevel)
    if (s.disableBypassPermissionsMode) copilotBypass.value = String(s.disableBypassPermissionsMode)
    if (s.sandboxEnabled) copilotSandboxEna.value = String(s.sandboxEnabled)
    if (s.sandboxAllowBypass) copilotSandboxBypass.value = String(s.sandboxAllowBypass)
    if (s.sandboxAllowLocal) copilotSandboxNet.value = String(s.sandboxAllowLocal)
    if (Array.isArray(s.allowedUrls)) copilotAllow.value = s.allowedUrls.join('\n')
    if (Array.isArray(s.deniedUrls)) copilotDeny.value = s.deniedUrls.join('\n')
  } catch (e) {}
}
export async function loadCopilotEnv() {
  copilotLoading.value = true
  try {
    await Promise.all([copilotReadVars(), copilotReadSettings()])
    // 尚未设置模型时默认选路由器模型列表第一个，避免留空
    if (!copilotModel.value && modelOptions.value.length) copilotModel.value = modelOptions.value[0]
  } finally { copilotLoading.value = false }
}
// 文本域（每行一条）转 string[]
export function copilotUrlList(v) {
  return String(v || '').split('\n').map(s => s.trim()).filter(Boolean)
}
export async function loadCopilotPerm() {
  copilotPermExists.value = false
  copilotPermContent.value = ''
  try {
    if (!api.GetCopilotPermissions) return
    const r = await api.GetCopilotPermissions()
    copilotPermExists.value = !!(r && r.ok && r.exists)
    if (r && r.file) copilotPermFile.value = r.file
    if (r && r.ok && r.content) copilotPermContent.value = r.content
    copilotPermFromContent()
  } catch (e) {}
}
export async function saveCopilotPerm() {
  if (!api.SaveCopilotPermissions) { toast(t('common.fail', { msg: t('sync.copilot.permNoBind') })); return }
  try {
    const r = await api.SaveCopilotPermissions(copilotPermDoc())
    if (r && r.ok) { copilotPermExists.value = true; toast(t('sync.configSaved')) }
    else toast(t('common.fail', { msg: (r && r.error) || '?' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) }
}
// 写入：三个端点环境变量 + settings.json 全部面板键（空 = 移除）；任一步失败返回错误信息
async function copilotWriteVars() {
  if (!String(copilotModel.value || '').trim()) return t('sync.copilot.modelReq')
  const base = copilotRouterUrl()
  const type = 'openai'
  let apiKey = String(copilotApiKey.value || '').trim()
  if (!apiKey) {
    // 空 Key 自动回退路由令牌：未设鉴权时写空值仅保证变量存在，请求无需认证
    try {
      const c = await api.GetConfig()
      const tk = String((c && c.authToken) || '').trim()
      if (tk && tk !== '****') apiKey = tk
    } catch (e) {}
  }
  for (const [k, v] of [
    ['COPILOT_PROVIDER_BASE_URL', base],
    ['COPILOT_PROVIDER_TYPE', type],
    ['COPILOT_PROVIDER_API_KEY', apiKey],
  ]) {
    if (!api.SetUserEnv) return t('sync.copilot.noBind')
    const r = await api.SetUserEnv(k, v)
    if (!(r && r.ok)) return (r && r.error) || (k + ' 写入失败')
  }
  if (!api.SaveCopilotSettings) return t('sync.copilot.noModelBind')
  const ss = await api.SaveCopilotSettings({
    model: String(copilotModel.value || '').trim(),
    effortLevel: String(copilotEffort.value || '').trim(),
    allowedUrls: copilotUrlList(copilotAllow.value),
    deniedUrls: copilotUrlList(copilotDeny.value),
    disableBypassPermissionsMode: String(copilotBypass.value || '').trim(),
    sandboxEnabled: String(copilotSandboxEna.value || '').trim(),
    sandboxAllowBypass: String(copilotSandboxBypass.value || '').trim(),
    sandboxAllowLocal: String(copilotSandboxNet.value || '').trim(),
  })
  if (!(ss && ss.ok)) return (ss && ss.error) || t('sync.copilot.modelWriteFail')
  copilotBase.value = base
  copilotType.value = type
  // 已写入的 API Key 只做占位展示不回显明文
  return ''
}
export async function writeCopilotEnv() {
  copilotSaving.value = true
  try {
    const err = await copilotWriteVars()
    if (!err) { toast(t('sync.copilot.written')); return }
    toast(t('common.fail', { msg: err }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { copilotSaving.value = false }
}
export async function clearCopilotEnv() {
  // 确认交互由调用方的应用内弹窗完成（替代原生 confirm），此处只执行清除
  if (!api.DelUserEnv) { toast(t('common.fail', { msg: t('sync.copilot.clearNoBind') })); return }
  const errs = []
  for (const k of COPILOT_ENV_KEYS) {
    try {
      const r = await api.DelUserEnv(k)
      if (!(r && r.ok)) errs.push((r && r.error) || k)
    } catch (e) { errs.push(k) }
  }
  // 移除 settings.json 的 model 键（清除接入配置；effortLevel / 权限等用户偏好保留）
  if (api.SaveCopilotSettings) {
    try {
      const sm = await api.SaveCopilotSettings({ model: '' })
      if (!(sm && sm.ok)) errs.push(String((sm && sm.error) || 'model'))
    } catch (e) { errs.push('model') }
  }
  if (errs.length) { toast(t('common.fail', { msg: t('sync.copilot.clearFail', { list: errs.join('、') }) })); return }
  copilotBase.value = ''; copilotType.value = ''
  copilotApiKey.value = ''
  if (modelOptions.value.length) copilotModel.value = modelOptions.value[0]
  else copilotModel.value = ''
  toast(t('sync.copilot.cleared'))
}