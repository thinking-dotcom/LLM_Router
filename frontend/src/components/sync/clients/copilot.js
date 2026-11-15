// Copilot CLI 接入面板：端点三环境变量（BYOK）+ settings.json（模型/推理深度/URL 权限/
// 沙箱）+ permissions-config.json（工具审批，源码直编）
import { ref, computed, watch, nextTick } from 'vue'
import { toast } from '../../../toast'
import { t } from '../../../i18n'
import api from '../../../api'
import { routerBase, modelOptions } from '../store'

// Copilot CLI 官方 BYOK：端点与模型靠用户环境变量，偏好靠 settings.json：
//   COPILOT_PROVIDER_BASE_URL → 本路由器接入地址 /router（OpenAI 兼容）
//   COPILOT_PROVIDER_TYPE     → openai（默认）
//   COPILOT_PROVIDER_API_KEY  → 路由令牌（可选，未设鉴权可不填）
//   COPILOT_MODEL             → BYOK 必需（官方：COPILOT_MODEL / PROVIDER_MODEL_ID /
//                               --model 三选一，settings.json 的 model 键不满足 BYOK 校验；
//                               同时写 settings.json 便于非 BYOK 场景与面板回显）
//   settings.json：effortLevel（推理深度）、allowedUrls/deniedUrls（URL 权限）、
//   permissions.disableBypassPermissionsMode（提权限制）、
//   sandbox.*（OS 沙箱：enabled / allowBypass / userPolicy.network.allowLocalNetwork）
export const COPILOT_ENV_KEYS = ['COPILOT_PROVIDER_BASE_URL', 'COPILOT_PROVIDER_TYPE', 'COPILOT_PROVIDER_API_KEY', 'COPILOT_MODEL']
export const copilotBase = ref('')     // 仅展示：实际写入时取路由器地址
export const copilotType = ref('')     // 仅展示：恒为 openai
export const copilotModel = ref('')    // 模型（可编辑，写入 settings.json model 键）
export const copilotApiKey = ref('')   // API Key（可编辑，空 = 用路由令牌）
export const copilotEffort = ref('')   // 推理深度 effortLevel（settings.json）
export const copilotBypass = ref('')   // permissions.disableBypassPermissionsMode（提权限制）
export const copilotAllow = ref('')    // allowedUrls（文本域，每行一条）
export const copilotDeny = ref('')     // deniedUrls（文本域，每行一条）
// URL 白/黑名单紧凑按钮：弹层开关 + 条数徽标（按钮点击弹小提示框编辑，弹层内 textarea）
export const copilotAllowOpen = ref(false)
export const copilotDenyOpen = ref(false)
export const copilotAllowCount = computed(() => copilotAllow.value.split('\n').filter(s => s.trim()).length)
export const copilotDenyCount = computed(() => copilotDeny.value.split('\n').filter(s => s.trim()).length)
export const copilotSandboxEna = ref('')     // sandbox.enabled（''/true/false 三态）
export const copilotSandboxBypass = ref('')  // sandbox.allowBypass（''/true/false 三态）
export const copilotSandboxNet = ref('')     // sandbox.userPolicy.network.allowLocalNetwork
// 通用设置（settings.json 官方偏好键，'' = 删键回官方默认）：4 枚举 + 16 布尔三态（含 ide.* 嵌套）
export const copilotGeneral = ref({
  theme: '', banner: '', keepAlive: '', autoUpdatesChannel: '',
  stream: '', compactPaste: '', notifications: '', beep: '', autoUpdate: '',
  memory: '', askUser: '', includeCoAuthoredBy: '', continueOnAutoMode: '',
  experimental: '', mouse: '', renderMarkdown: '', updateTerminalTitle: '', terminalProgress: '',
  ideAutoConnect: '', ideOpenDiff: '',
})
export const COPILOT_THEMES = ['default', 'github', 'dim', 'high-contrast', 'colorblind']
export const COPILOT_BANNERS = ['always', 'once', 'never']
export const COPILOT_KEEPALIVE = ['off', 'on', 'busy']
export const COPILOT_CHANNELS = ['stable', 'prerelease']
export const COPILOT_GENERAL_BOOLS = ['stream', 'compactPaste', 'notifications', 'beep', 'autoUpdate', 'memory', 'askUser', 'includeCoAuthoredBy', 'continueOnAutoMode', 'experimental', 'mouse', 'renderMarkdown', 'updateTerminalTitle', 'terminalProgress', 'ideAutoConnect', 'ideOpenDiff']
export const copilotLoading = ref(false)
export const copilotSaving = ref(false)
export const copilotModelOpen = ref(false)
// 清除态：「清除接入」成功（或面板载入时接入环境变量全部不存在）后置 true。
// 此时 ✓ 保存 = 维持清除，不回写接入——否则空 Key 自动回退路由令牌 + 接入地址恒取
// 路由地址，会把刚清掉的接入整体复活（表现为「清除了却保存不上」）。
// 用户再次编辑任意接入字段即视为重新接入，恢复正常保存。
export const copilotCleared = ref(false)
let copilotMuted = false // 程序化回填（载入/清除）期间抑制编辑侦听，回填不算用户重新接入
watch(
  [copilotModel, copilotApiKey, copilotEffort, copilotAllow, copilotDeny, copilotGeneral],
  () => { if (copilotMuted) return; copilotCleared.value = false },
  { deep: true },
)
export const COPILOT_EFFORT_OPTIONS = ['low', 'medium', 'high', 'xhigh']
// 工具审批权限（~/.copilot/permissions-config.json，按项目位置保存）源码直编
export const copilotPermContent = ref('')
export const copilotPermExists = ref(false)
export const copilotPermFile = ref('~/.copilot/permissions-config.json')
// 「作用位置」候选（config.json trustedFolders ∪ permissions 已有 locations 键）+ 下拉开关
export const copilotLocCands = ref([])
export const copilotLocOpen = ref(false)
export async function loadCopilotLocCands() {
  try {
    const r = api.GetCopilotPermLocCandidates ? await api.GetCopilotPermLocCandidates() : null
    if (r && r.ok && Array.isArray(r.folders)) copilotLocCands.value = r.folders
  } catch (e) {}
}
// 候选清单（无输入框过滤，返回全部；点击列表项即选中）
export function copilotLocFiltered() {
  return copilotLocCands.value
}
export function copilotRouterUrl() {
  return (routerBase.value || 'http://127.0.0.1:3000') + '/router'
}
async function copilotReadVars() {
  // 四个 BYOK 环境变量回显到 state；COPILOT_MODEL 不直接写 copilotModel（与
  // copilotReadSettings 并发，改由 loadCopilotEnv 统一按优先级落值）。
  // 返回 { envModel, envAny }：envAny = 任一接入环境变量存在（清除态判定依据）
  try {
    const [b, tp, ak, mk] = await Promise.all(COPILOT_ENV_KEYS.map(k => (api.GetUserEnv ? api.GetUserEnv(k) : null)))
    let envAny = false
    if (b && b.ok && b.exists) { copilotBase.value = String(b.value || ''); envAny = true }
    if (tp && tp.ok && tp.exists) { copilotType.value = String(tp.value || ''); envAny = true }
    if (ak && ak.ok && ak.exists) { copilotApiKey.value = String(ak.value || ''); envAny = true }
    if (mk && mk.ok && mk.exists) return { envModel: String(mk.value || '').trim(), envAny: true }
    return { envModel: '', envAny }
  } catch (e) {}
  return { envModel: '', envAny: false }
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
    // 通用设置键：Go 端缺省回 ''，仅磁盘存在时返回实际值
    for (const k of [...COPILOT_GENERAL_BOOLS, 'theme', 'banner', 'keepAlive', 'autoUpdatesChannel']) {
      copilotGeneral.value[k] = String(s[k] ?? '')
    }
  } catch (e) {}
}
export async function loadCopilotEnv() {
  copilotLoading.value = true
  copilotMuted = true
  try {
    const [{ envModel, envAny }] = await Promise.all([copilotReadVars(), copilotReadSettings(), loadCopilotLocCands()])
    // 清除态判定：接入环境变量全部不存在（未接入 / 刚清除）——✓ 保存维持清除不回写
    copilotCleared.value = !envAny
    // 模型回显优先级：COPILOT_MODEL 环境变量（BYOK 实际生效值，settings.json 的 model
    // 键不参与 BYOK 校验）> settings.json model（copilotReadSettings 已回填）> 路由器模型列表首个
    if (envModel) copilotModel.value = envModel
    else if (!copilotModel.value && modelOptions.value.length) copilotModel.value = modelOptions.value[0]
  } finally {
    // muted 须等侦听任务跑完再撤（watch 回调在本同步块之后的微任务里执行）
    nextTick(() => { copilotMuted = false })
    copilotLoading.value = false
  }
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
  } catch (e) {}
}
// 保存 permissions-config.json：内容由源码页签直编（JSON 校验在 controller 统一做）；silent=不弹 toast
export async function saveCopilotPerm(silent = false) {
  if (!api.SaveCopilotPermissions) { if (!silent) toast(t('common.fail', { msg: t('sync.copilot.permNoBind') })); return false }
  try {
    const r = await api.SaveCopilotPermissions(copilotPermContent.value)
    if (r && r.ok) { copilotPermExists.value = true; if (!silent) toast(t('sync.configSaved')); return true }
    if (!silent) toast(t('common.fail', { msg: (r && r.error) || '?' }))
  } catch (e) { if (!silent) toast(t('common.fail', { msg: e?.message || '?' })) }
  return false
}

// ============ 权限配置胶囊条：按位置持久化批准（官方仅支持 allow；deny/ask 仅会话内） ============
// 作用位置（Git 根目录或运行目录绝对路径），approvals 键必须与 CLI 运行位置一致才生效
export const copilotPermLoc = ref('')
// 宽松解析当前源码（失败视为空对象，由源码页签负责修正）
function copilotPermDocSafe() {
  try { return JSON.parse(copilotPermContent.value || '{}') } catch (e) { return {} }
}
// 当前位置 read/write 批准派生状态
export function copilotPermState() {
  const loc = String(copilotPermLoc.value || '').trim()
  const doc = copilotPermDocSafe()
  const cur = loc && doc.locations && typeof doc.locations === 'object' ? doc.locations[loc] : null
  const ap = cur && Array.isArray(cur.tool_approvals) ? cur.tool_approvals : []
  return { hasRead: ap.some(a => a && a.kind === 'read'), hasWrite: ap.some(a => a && a.kind === 'write') }
}
// 模式派生：read+write=自动接受编辑 / 仅 read=只读 / 都没有=询问（默认）
export function copilotPermMode() {
  const s = copilotPermState()
  if (s.hasRead && s.hasWrite) return 'edit'
  if (s.hasRead) return 'read'
  return 'ask'
}
// 设定模式：结构化改写源码中该位置的 tool_approvals（保留 commands/mcp/memory 等其它批准与未知键）
export function copilotPermSetMode(mode) {
  const loc = String(copilotPermLoc.value || '').trim()
  if (!loc) { toast(t('sync.copilot.permNeedLoc')); return }
  const doc = copilotPermDocSafe()
  if (!doc.locations || typeof doc.locations !== 'object') doc.locations = {}
  const cur = (doc.locations[loc] && typeof doc.locations[loc] === 'object') ? doc.locations[loc] : {}
  const rest = (Array.isArray(cur.tool_approvals) ? cur.tool_approvals : []).filter(a => a && a.kind !== 'read' && a.kind !== 'write')
  let approvals = rest
  if (mode === 'edit') approvals = [{ kind: 'read' }, { kind: 'write' }, ...rest]
  else if (mode === 'read') approvals = [{ kind: 'read' }, ...rest]
  const next = { ...cur, tool_approvals: approvals }
  if (!approvals.length && !('allowed_directories' in next)) delete next.tool_approvals
  if (!Object.keys(next).length) delete doc.locations[loc]
  else doc.locations[loc] = next
  copilotPermContent.value = JSON.stringify(doc, null, 2) + '\n'
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
  const model = String(copilotModel.value || '').trim()
  for (const [k, v] of [
    ['COPILOT_PROVIDER_BASE_URL', base],
    ['COPILOT_PROVIDER_TYPE', type],
    ['COPILOT_PROVIDER_API_KEY', apiKey],
    ['COPILOT_MODEL', model], // BYOK 必需：settings.json 的 model 键不参与 BYOK 校验
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
    theme: String(copilotGeneral.value.theme || '').trim(),
    banner: String(copilotGeneral.value.banner || '').trim(),
    keepAlive: String(copilotGeneral.value.keepAlive || '').trim(),
    autoUpdatesChannel: String(copilotGeneral.value.autoUpdatesChannel || '').trim(),
    ...Object.fromEntries(COPILOT_GENERAL_BOOLS.map(k => [k, String(copilotGeneral.value[k] || '').trim()])),
  })
  if (!(ss && ss.ok)) return (ss && ss.error) || t('sync.copilot.modelWriteFail')
  copilotBase.value = base
  copilotType.value = type
  // 已写入的 API Key 只做占位展示不回显明文
  return ''
}
export async function writeCopilotEnv() {
  // 清除态：✓ 保存 = 维持清除（不回写接入环境变量 / settings.json model），面板直接收起。
  // 用户重新编辑任意接入字段、或点面板里的「写入环境变量」按钮后即恢复整体回写。
  if (copilotCleared.value) return true
  copilotSaving.value = true
  try {
    const err = await copilotWriteVars()
    if (!err) { copilotCleared.value = false; toast(t('sync.copilot.written')); return true }
    toast(t('common.fail', { msg: err }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { copilotSaving.value = false }
  return false
}
export async function clearCopilotEnv() {
  // 确认交互由调用方的应用内弹窗完成（替代原生 confirm），此处只执行清除
  if (!api.DelUserEnv) { toast(t('common.fail', { msg: t('sync.copilot.clearNoBind') })); return }
  copilotMuted = true
  try {
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
    // 进入清除态：✓ 保存维持清除，不回写接入（见 copilotCleared 注释）
    copilotCleared.value = true
    toast(t('sync.copilot.cleared'))
  } finally {
    nextTick(() => { copilotMuted = false })
  }
}