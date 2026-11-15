// CodeBuddy CLI（codebuddy，~/.codebuddy/settings.json，/oai 接入）：
// settings.json 表单 + models.json 端点行 + mcp.json 源码，界面仿 agy。
//
// 主配置 settings.json（官方字段，权限/沙箱/模型/深度推理等）直读直写真实文件：
//   model / reasoningEffort / alwaysThinkingEnabled /
//   variantModels{ lite, reasoning } / cleanupPeriodDays /
//   includeCoAuthoredBy / trustedDirectories[] /
//   permissions{ defaultMode, allow[], ask[], deny[] } /
//   sandbox{ enabled, autoAllowBashIfSandboxed, excludedCommands[],
//            network{ allowUnixSockets[], allowLocalBinding } }
// 其余（env / hooks / memory / statusLine / subagents / autoMode …）非受管，
// 由 JSON 源码页签维护，保存原样保留。
//
// 接入本路由器靠 ~/.codebuddy/models.json 的端点模型行（官方机制）：
//   每行 { id, name, vendor, url, apiKey, supportsToolCall, supportsImages,
//          supportsReasoning }
//   把 url 指向本路由器 /router、apiKey 填路由令牌即可让 CLI 请求走路由器。
// 表单将连接收敛为顶部「端点地址 + 密钥」一组共享值：保存时统一铺写每个模型行的
// url / apiKey（models.json 行级字段），行内仅保留模型配置；行级差异化走 JSON 源码页签。
// models.json 顶层其余键（availableModels 等）原样保留。
//
// MCP 声明在 ~/.codebuddy/mcp.json（mcpServers 对象），源码页签直编。
import { ref, watch } from 'vue'
import { cfgObj, cfgEdit, cfgTarget, jsonEcho, syncToJson, routerBase } from '../store'

// ---- mcp.json 源码（~/.codebuddy/mcp.json）----
export const cbMcpContent = ref('')
export const cbMcpExists = ref(false)
export const cbMcpFile = ref('~/.codebuddy/mcp.json')

// ---- models.json（~/.codebuddy/models.json）：端点模型清单 ----
// cbModelsObj 为整个文件的对象（models 数组为主管字段，顶层其余键保留）；
// cbModelsContent 为源码文本，进入/离开源码页签时经 build/parse 双向同步。
export const cbModelsObj = ref({ models: [] })
export const cbModelsContent = ref('')
export const cbModelsExists = ref(false)
export const cbModelsFile = ref('~/.codebuddy/models.json')

// 共享连接（面板顶部）：端点地址 cbConnUrl + 密钥 cbConnKey。
// models.json 每行的 url / apiKey 由这两值统一铺写；行内不再各自重复配置。
export const cbConnUrl = ref('')
export const cbConnKey = ref('')

const modelsRows = () => {
  const o = cbModelsObj.value
  return Array.isArray(o && o.models) ? o.models : []
}
// 行 → 顶部：取首个非空 url / apiKey（按加载顺序）；全空则回退本路由器接入地址
function syncConnFromRows() {
  let u = '', k = ''
  for (const r of modelsRows()) {
    if (!u) u = String((r && r.url) || '').trim()
    if (!k) k = String((r && r.apiKey) || '').trim()
    if (u && k) break
  }
  cbConnUrl.value = u || cbRouterUrl()
  cbConnKey.value = k
}
// 顶部 → 行：url / apiKey 为受管共享键，非空则铺写每个模型行；
// 空值不干预（行级既有差异保留，仅在 JSON 源码页签可见/维护）
function applyConnToRows() {
  const u = String(cbConnUrl.value || '').trim()
  const k = String(cbConnKey.value || '').trim()
  if (!u && !k) return
  for (const r of modelsRows()) {
    if (!r || typeof r !== 'object') continue
    if (u) r.url = u
    if (k) r.apiKey = k
  }
}

// 从源码文本解析进 cbModelsObj（含 models 数组缺省兜底）；非法 JSON 返回 false
export function cbModelsFromText(content) {
  try {
    const v = JSON.parse(String(content || '{}'))
    if (!v || typeof v !== 'object' || Array.isArray(v)) return false
    if (!Array.isArray(v.models)) v.models = []
    // 归一能力开关：supportsReasoning 缺失的旧行补默认 false，保证行内下拉有选中态
    // 同时给每行挂稳定内部键 dbk（列表 :key 用）：id 为空或多个行同名时仍能区分行，
    // 避免删除/重排后 v-for 复用错位；构建写盘文本前会被剔除，不落盘。
    let n = 0
    for (const r of v.models) {
      if (r && typeof r === 'object') {
        if (typeof r.supportsReasoning !== 'boolean') r.supportsReasoning = false
        Object.defineProperty(r, 'dbk', { value: 'cbk' + (n++), enumerable: false, writable: true, configurable: true })
      } else n++
    }
    cbModelsObj.value = v
    syncConnFromRows()
    return true
  } catch (e) { return false }
}
// cbModelsObj → 源码文本（先把顶部共享连接铺到各模型行，再序列化，顶层其它键一并保留）
export function cbModelsBuildText() {
  applyConnToRows()
  const o = cbModelsObj.value || {}
  return JSON.stringify({ models: Array.isArray(o.models) ? o.models : [], ...dropKey(o, 'models') }, null, 2) + '\n'
}
function dropKey(o, key) {
  const out = {}
  for (const [k, v] of Object.entries(o || {})) if (k !== key) out[k] = v
  return out
}
// 列表内部键（:key 用）序号：仅进程内递增，不参与序列化（dbk 定义为不可枚举）
let cbKeySeq = 1
// 新行骨架：仅模型配置；端点 url / apiKey 由顶部共享连接统一铺写
export function cbNewModelRow() {
  const row = { id: '', name: '', vendor: 'OpenAI', supportsToolCall: true, supportsImages: false, supportsReasoning: false }
  Object.defineProperty(row, 'dbk', { value: 'cbk' + (cbKeySeq++), enumerable: false, writable: true, configurable: true })
  return row
}
export function cbAddModel() {
  const o = cbModelsObj.value
  if (!Array.isArray(o.models)) o.models = []
  o.models.push(cbNewModelRow())
}
export function cbDelModel(i) {
  const o = cbModelsObj.value
  if (Array.isArray(o.models)) o.models.splice(i, 1)
}

// 接入地址：统一 /router（与其它客户端一致）
export function cbRouterUrl() {
  return (routerBase.value || 'http://127.0.0.1:3000') + '/router'
}

// ---- settings.json 表单（cfgObj 受管子集，其余键保留）----
export function codebuddyPreset() {
  return {
    model: '',
    reasoningEffort: '',
    alwaysThinkingEnabled: null,
    variantModels: { lite: '', reasoning: '' },
    permissions: { defaultMode: 'default', allow: [], ask: [], deny: [] },
    sandbox: { enabled: null, autoAllowBashIfSandboxed: null, excludedCommands: [], network: { allowLocalBinding: null, allowUnixSockets: [] } },
    cleanupPeriodDays: null,
    includeCoAuthoredBy: null,
    trustedDirectories: [],
  }
}
export const cbForm = ref({
  model: '',
  reasoningEffort: '',  // ''=auto（跟随产品默认）/ low / medium / high / xhigh
  alwaysThinking: '',   // '' 默认 / true / false（alwaysThinkingEnabled）
  liteModel: '',
  reasoningModel: '',
  cleanupDays: '',
  coauth: '',          // '' 默认 / true / false
  trustedDirsText: '', // 每行一个目录
  defaultMode: 'default',
  sandboxEnabled: '',
  autoAllowBash: '',
  excludedCmdsText: '',
  allowLocalBinding: '',
  unixSocketsText: '',
})
// 权限规则行（统一 allow/ask/deny 行胶囊 UI）：仅写盘时分组回 permissions
export const cbPermRows = ref([])

// 「所有命令」三态：以裸工具名 Bash 作通配规则（= 该工具全部终端命令，与
// Claude Code / CodeBuddy 官方规则语法一致）。面板上不再提供逐条自定义命令，
// 命令行为只由 允许/询问/拒绝 三档控制；点选即把 Bash 行落到对应档位列表。
export const CB_CMD_RULE = 'Bash'
export function cbCmdMode() {
  const r = (cbPermRows.value || []).find(x => String(x.text || '').trim() === CB_CMD_RULE)
  return r ? r.mode : ''
}
export function cbCmdSet(mode) {
  const rows = (cbPermRows.value || []).slice()
  const i = rows.findIndex(x => String(x.text || '').trim() === CB_CMD_RULE)
  if (i >= 0) rows[i] = { ...rows[i], mode }
  else rows.push({ text: CB_CMD_RULE, mode })
  cbPermRows.value = rows
}

const arrLines = v => String(v || '').split(/\r?\n/).map(s => s.trim()).filter(Boolean)
const linesText = arr => Array.isArray(arr) ? arr.join('\n') : ''
const numOr = v => (v !== null && v !== undefined && v !== '' && isFinite(Number(v))) ? String(v) : ''
const triOr = v => (v === true || v === false) ? String(v) : ''
const objOr = v => (v && typeof v === 'object' && !Array.isArray(v)) ? v : {}

export function initCodebuddyForm() {
  const c = cfgObj.value || {}
  const perms = objOr(c.permissions)
  const sandbox = objOr(c.sandbox)
  const net = objOr(sandbox.network)
  const variant = objOr(c.variantModels)
  cbForm.value = {
    model: c.model || '',
    // 官方语义：auto 等价于清空该项 → 归一为空（下拉空选项即 auto）
    reasoningEffort: c.reasoningEffort === 'auto' ? '' : (c.reasoningEffort || ''),
    alwaysThinking: triOr(c.alwaysThinkingEnabled),
    liteModel: variant.lite || '',
    reasoningModel: variant.reasoning || '',
    cleanupDays: numOr(c.cleanupPeriodDays),
    coauth: triOr(c.includeCoAuthoredBy),
    trustedDirsText: linesText(c.trustedDirectories),
    defaultMode: perms.defaultMode || 'default',
    sandboxEnabled: triOr(sandbox.enabled),
    autoAllowBash: triOr(sandbox.autoAllowBashIfSandboxed),
    excludedCmdsText: linesText(sandbox.excludedCommands),
    allowLocalBinding: triOr(net.allowLocalBinding),
    unixSocketsText: linesText(net.allowUnixSockets),
  }
  const rows = []
  for (const mode of ['allow', 'ask', 'deny']) {
    for (const it of (Array.isArray(perms[mode]) ? perms[mode] : [])) {
      const text = String(it).trim()
      if (text) rows.push({ text, mode })
    }
  }
  cbPermRows.value = rows
}

const strTrim = v => String(v ?? '').trim()
const setStr = (c, f, k) => {
  const v = strTrim(f[k])
  if (v) c[k] = v
  else if (k in c) delete c[k]
}

export function codebuddyApplyToObj() {
  const c = cfgObj.value
  if (!c || typeof c !== 'object') return
  const f = cbForm.value
  setStr(c, f, 'model')
  // reasoningEffort（官方 low/medium/high/xhigh；空=auto/跟随产品默认，删键）
  const re = strTrim(f.reasoningEffort)
  if (re) c.reasoningEffort = re
  else if ('reasoningEffort' in c) delete c.reasoningEffort
  // alwaysThinkingEnabled：总开思考模式（三态，空=删键跟随默认）
  const at = f.alwaysThinking
  if (at === 'true') c.alwaysThinkingEnabled = true
  else if (at === 'false') c.alwaysThinkingEnabled = false
  else if ('alwaysThinkingEnabled' in c) delete c.alwaysThinkingEnabled
  // variantModels：仅维护 lite / reasoning，其余键保留
  const variant = objOr(c.variantModels)
  const vl = strTrim(f.liteModel), vr = strTrim(f.reasoningModel)
  if (vl) variant.lite = vl; else if ('lite' in variant) delete variant.lite
  if (vr) variant.reasoning = vr; else if ('reasoning' in variant) delete variant.reasoning
  if (Object.keys(variant).length) c.variantModels = variant
  else if ('variantModels' in c) delete c.variantModels
  // cleanupPeriodDays / includeCoAuthoredBy
  const cd = String(f.cleanupDays ?? '').trim()
  if (cd !== '' && isFinite(Number(cd))) c.cleanupPeriodDays = Number(cd)
  else if ('cleanupPeriodDays' in c) delete c.cleanupPeriodDays
  const ca = f.coauth
  if (ca === 'true') c.includeCoAuthoredBy = true
  else if (ca === 'false') c.includeCoAuthoredBy = false
  else if ('includeCoAuthoredBy' in c) delete c.includeCoAuthoredBy
  // trustedDirectories：每行一个目录
  const dirs = arrLines(f.trustedDirsText)
  if (dirs.length) c.trustedDirectories = dirs
  else if ('trustedDirectories' in c) delete c.trustedDirectories
  // permissions：defaultMode + allow/ask/deny 规则（其余权限键保留）
  const perms = objOr(c.permissions)
  const dm = strTrim(f.defaultMode)
  if (dm) perms.defaultMode = dm
  else if ('defaultMode' in perms) delete perms.defaultMode
  const buckets = { allow: [], ask: [], deny: [] }
  for (const r of cbPermRows.value || []) {
    const text = String(r.text || '').trim()
    const mode = (r.mode === 'ask' || r.mode === 'deny') ? r.mode : 'allow'
    if (text) buckets[mode].push(text)
  }
  for (const m of Object.keys(buckets)) {
    if (buckets[m].length) perms[m] = buckets[m]
    else if (m in perms) delete perms[m]
  }
  if (Object.keys(perms).length) c.permissions = perms
  else if ('permissions' in c) delete c.permissions
  // sandbox（含 network 子对象；非受管键原样保留）
  const sb = objOr(c.sandbox)
  const net = objOr(sb.network)
  const triSet = (k, o) => {
    const v = f[k]
    if (v === 'true') o = true
    else if (v === 'false') o = false
    else o = null
    return o
  }
  const en = triSet('sandboxEnabled', sb.enabled)
  if (en != null) sb.enabled = en
  else if ('enabled' in sb) delete sb.enabled
  const ab = triSet('autoAllowBash', sb.autoAllowBashIfSandboxed)
  if (ab != null) sb.autoAllowBashIfSandboxed = ab
  else if ('autoAllowBashIfSandboxed' in sb) delete sb.autoAllowBashIfSandboxed
  const ex = arrLines(f.excludedCmdsText)
  if (ex.length) sb.excludedCommands = ex
  else if ('excludedCommands' in sb) delete sb.excludedCommands
  const lb = triSet('allowLocalBinding', net.allowLocalBinding)
  if (lb != null) net.allowLocalBinding = lb
  else if ('allowLocalBinding' in net) delete net.allowLocalBinding
  const us = arrLines(f.unixSocketsText)
  if (us.length) net.allowUnixSockets = us
  else if ('allowUnixSockets' in net) delete net.allowUnixSockets
  if (Object.keys(net).length) sb.network = net
  else if ('network' in sb) delete sb.network
  if (Object.keys(sb).length) c.sandbox = sb
  else if ('sandbox' in c) delete c.sandbox
}

// watchers 在 init 中注册（组件 setup 上下文内）
export function initCodebuddy() {
  watch([cbForm, cbPermRows], () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'codebuddy' || jsonEcho) return
    try { codebuddyApplyToObj(); syncToJson() } catch (e) {}
  }, { deep: true })
}
