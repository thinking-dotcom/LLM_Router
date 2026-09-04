// Command Code（~/.commandcode/config.json 编辑源）
// 官方字段参考 commandcode.ai/docs/settings 与 /docs/byok：
// config.json：provider / model / theme / compactMode / reasoningEffort（按模型 id 键控，
//   档位 low / medium / high / xhigh / max）；
// settings.json：model / effort / permissions（allow / ask / deny / defaultMode /
//   additionalDirectories / disableBypass），规则列表跨作用域并集生效；
// providers.json：BYOK 服务商（api 协议 + baseURL + apiKey 引用 + models）。
import { ref, watch, nextTick } from 'vue'
import { t } from '../../i18n'
import api from '../../api'
import { srcObj, permRowsFromLists, permRowsToLists } from './shared'
import { cfgObj, cfgEdit, cfgTarget, cfgContent, cfgMode, derivedBaseUrl, routerHintForTarget, jsonEcho, syncToJson, modelOptions } from './store'

export const CC_EFFORT_LEVELS = ['low', 'medium', 'high', 'xhigh', 'max']
export const CC_PERM_MODES = ['default', 'auto-accept', 'plan', 'bypass']
// apiKey 留空时的默认引用：本地 provider 保存时自动写入的免登录环境变量（cmdc 只认引用，
// false / 字面量会被判为无 key 报错 "Provider has no API key"）
export const CC_DEFAULT_KEY_REF = '$COMMAND_CODE_API_KEY'
// 官方 apiKey 仅接受引用形式（$VAR / {env:VAR} / !command），其余（字面量/false/空）按未填写处理
export function ccIsApiKeyRef(k) { return /^(\$|\{env:|\!)/.test(String(k || '')) }
// 权限规则快选：Command Code / Claude Code / Codex 同族语法（Claude Code 官方工具）。
// 紧凑胶囊多列排布，点击加入对应文本框（再点取消）；自定义规则（细粒度通配）仍在文本框手填
export const PERM_COMMON_RULES = [
  { v: 'Bash', tip: () => t('sync.ruleBash') },
  { v: 'Bash(git:*)', tip: () => 'Bash(git:*) — Git' },
  { v: 'Bash(npm:*)', tip: () => 'Bash(npm:*) — npm' },
  { v: 'Bash(pnpm:*)', tip: () => 'Bash(pnpm:*) — pnpm' },
  { v: 'Bash(go:*)', tip: () => 'Bash(go:*) — go' },
  { v: 'Bash(docker:*)', tip: () => 'Bash(docker:*) — docker' },
  { v: 'Read', tip: () => t('sync.ruleRead') },
  { v: 'Edit', tip: () => t('sync.ruleEdit') },
  { v: 'Grep', tip: () => t('sync.ruleGrep') },
  { v: 'Glob', tip: () => t('sync.ruleGlob') },
  { v: 'WebFetch', tip: () => t('sync.ruleFetch') },
  { v: 'WebSearch', tip: () => t('sync.ruleSearch') },
  { v: 'Task', tip: () => t('sync.ruleTask') },
  { v: 'Write', tip: () => t('sync.ruleWrite') },
  { v: 'Mcp', tip: () => t('sync.ruleMcp') },
]
// 权限规则快选文本域（按行去重切换）：与 CommandCode 面板同一套胶囊交互
export function ccAddPermRule(field, rule) {
  if (!rule) return
  const cur = String(ccForm.value[field] || '').trim()
  const arr = cur ? cur.split(/\r?\n/).map(s => s.trim()).filter(Boolean) : []
  const i = arr.indexOf(rule)
  if (i >= 0) arr.splice(i, 1)
  else arr.push(rule)
  ccForm.value[field] = arr.join('\n')
}
export function ccRuleOn(field, rule) {
  return String(ccForm.value[field] || '').split(/\r?\n/).some(s => s.trim() === rule)
}
// BYOK 模型 id 归一化：剥掉手填的「服务商id/」前缀（cmdc 对 providers.json 声明的键原样发送，
// 带前缀会被路由器拒绝）；command-code 官方云端模型保持原名
export function ccNativeModel(id, pid) {
  const s = String(id || '').trim()
  if (!s) return ''
  const pre = String(pid || '').trim() + '/'
  return s.startsWith(pre) ? s.slice(pre.length) : s
}
export function ccPickPid() { return String(ccForm.value.endpointId || '').trim() || 'llmrouter' }
export let ccModelsSeeded = false
export function setCcModelsSeeded(v) { ccModelsSeeded = v }
export let ccModelsReady = false
export function setCcModelsReady(v) { ccModelsReady = v }
// 模型清单后台自动同步：以接入方式返回的模型列表为准覆盖清单，无需手动「从模型列表填充」
export function ccSyncModelsFromList() {
  if (!ccModelsReady || !modelOptions.value.length) return
  const next = [...new Set(modelOptions.value.map(m => ccNativeModel(m, ccPickPid())).filter(Boolean))].sort().join('\n')
  if (next && ccForm.value.endpointModelsText !== next) ccForm.value.endpointModelsText = next
}
export function ccSeedModelsFromList() {
  if (ccModelsSeeded) return
  ccSyncModelsFromList()
  ccModelsSeeded = true
}
export const ccQuickOpen = ref('')
export const ccQuickQFocus = ref('')
export const ccForm = ref({
  provider: 'llmrouter', model: '', globalEffort: '', effortRows: [],
  endpointId: 'llmrouter', endpointApi: 'openai-completions', endpointKey: '', endpointModelsText: '',
  permMode: 'default',
})
export const ccPermRows = ref([]) // [{ text, mode }] 权限规则点选列表（allow/ask/deny 统一点选）
export const ccSettingsFile = ref(''); export const ccSettingsExists = ref(false); export const ccSettingsContent = ref('')
export const ccProvidersFile = ref(''); export const ccProvidersExists = ref(false); export const ccProvidersContent = ref('')
// 启动免登录开关：写/删用户环境变量 COMMAND_CODE_API_KEY（占位值），绕过 cmdc 启动账号校验
export const ccNoLogin = ref(false)
export const ccNoLoginMsg = ref('')
export async function ccLoadNoLogin() {
  try { ccNoLogin.value = !!(api.GetCommandcodeNoLogin && await api.GetCommandcodeNoLogin()) } catch { ccNoLogin.value = false }
}
export async function ccToggleNoLogin() {
  const want = ccNoLogin.value
  ccNoLoginMsg.value = ''
  if (!api.SetCommandcodeNoLogin) { ccNoLoginMsg.value = t('sync.ccNoLoginUnsupported'); await ccLoadNoLogin(); return }
  let err = ''
  try { err = await api.SetCommandcodeNoLogin(want) } catch (e) { err = String(e?.message || e) }
  if (err) { ccNoLoginMsg.value = t('sync.ccNoLoginFail', { msg: err }) }
  await ccLoadNoLogin()
}
export const ccModelOpen = ref(-1) // 默认模型下拉展开态（-2 表示展开）
export const ccEffortOpen = ref(-1) // 推理深度映射行的模型下拉展开态（行索引）
export const ccEffortPick = (i, m) => { if (ccForm.value.effortRows[i]) ccForm.value.effortRows[i].model = m; ccEffortOpen.value = -1 }
export let ccMuted = false
export function setCcMuted(v) { ccMuted = v }

export function initCcForm() {
  const c = cfgObj.value || {}
  const eff = (c.reasoningEffort && typeof c.reasoningEffort === 'object' && !Array.isArray(c.reasoningEffort)) ? c.reasoningEffort : {}
  const effKeys = Object.keys(eff)
  const model = String(c.model || '')
  const effRows = effKeys.map(k => ({ model: k, level: String(eff[k] || '') }))
  // 端点：取 providers.json 第一个 provider 条目（{"provider": {id: {...}}}）
  const psrc = srcObj(ccProvidersContent.value)
  const provs = (psrc.provider && typeof psrc.provider === 'object') ? psrc.provider : {}
  const pid = Object.keys(provs)[0] || 'llmrouter'
  const pv = (provs[pid] && typeof provs[pid] === 'object') ? provs[pid] : {}
  const ms = (pv.models && typeof pv.models === 'object' && !Array.isArray(pv.models)) ? pv.models : {}
  // 权限：settings.json permissions（本地只编辑本层，其余层不参与合并）
  const sset = srcObj(ccSettingsContent.value)
  const perm = (sset.permissions && typeof sset.permissions === 'object') ? sset.permissions : {}
  ccForm.value = {
    provider: String(c.provider || pid),
    model,
    globalEffort: String(sset.effort || ''),
    effortRows: effRows.length ? effRows : [{ model: '', level: '' }],
    endpointId: pid,
    endpointApi: String(pv.api || 'openai-completions'),
    endpointKey: (typeof pv.apiKey === 'string' && ccIsApiKeyRef(pv.apiKey)) ? pv.apiKey : '',
    endpointModelsText: Object.keys(ms).join('\n'),
    permMode: String(perm.defaultMode || 'default'),
  }
  ccPermRows.value = permRowsFromLists(perm)
}
// 主配置（config.json）：只覆盖 provider / model / reasoningEffort 三个受管键，
// theme / compactMode / featureModels 等其余字段原样保留
export function ccApplyToObj() {
  const c = cfgObj.value
  if (!c || typeof c !== 'object') return
  const f = ccForm.value
  // provider 留空时自动指向本地 BYOK 服务商（免登录直连）；填 command-code 则走云端订阅（需登录）
  const prov = String(f.provider || '').trim() || String(f.endpointId || '').trim() || 'llmrouter'
  c.provider = prov
  let model = String(f.model || '').trim()
  // BYOK 模型 id 用路由器原生形式（如 deepseek-v4-flash@go）：cmdc 对 providers.json 声明的
  // 键原样发送，带「服务商id/」前缀会被路由器拒绝（400 no provider found），故剥掉手填前缀
  if (model && prov !== 'command-code') model = ccNativeModel(model, prov)
  if (model) c.model = model
  // reasoningEffort：多行映射即完整清单（所见即所得）；行模型留空则该行不写入
  const next = {}
  for (const row of f.effortRows) {
    const em = String(row.model || '').trim()
    const el = String(row.level || '').trim()
    if (em && el) next[em] = el
  }
  if (Object.keys(next).length) c.reasoningEffort = next
  else delete c.reasoningEffort
}
// settings.json：permissions 覆盖五个受管键 + 全局 effort + 默认 model 同步；其余键保留
export function ccApplyToSettings() {
  const c = srcObj(ccSettingsContent.value)
  if (!c || typeof c !== 'object') return
  const f = ccForm.value
  const prev = (c.permissions && typeof c.permissions === 'object') ? c.permissions : {}
  const lists = permRowsToLists(ccPermRows.value)
  // additionalDirectories 不在本面板管理范围：原样保留（经 ...prev 透传）
  c.permissions = {
    ...prev,
    defaultMode: CC_PERM_MODES.includes(f.permMode) ? f.permMode : 'default',
    allow: lists.allow,
    ask: lists.ask,
    deny: lists.deny,
  }
  const el = String(f.globalEffort || '').trim()
  if (el) c.effort = el
  else delete c.effort
  // 与 ccApplyToObj 相同的 BYOK 原生 id 规则，保持两层配置一致
  const prov = String(f.provider || '').trim() || String(f.endpointId || '').trim() || 'llmrouter'
  let m = String(f.model || '').trim()
  if (m && prov !== 'command-code') m = ccNativeModel(m, prov)
  if (m) c.model = m
  else delete c.model
  ccSettingsContent.value = JSON.stringify(c, null, 2)
}
// providers.json：只 upsert 表单指向的服务商条目（baseUrl 统一路由器），
// 其余服务商条目与已声明模型字段（name / contextWindow / cost 等）原样保留
export function ccApplyToProviders() {
  const c = srcObj(ccProvidersContent.value)
  if (!c || typeof c !== 'object') return
  if (!c.provider || typeof c.provider !== 'object') c.provider = {}
  const f = ccForm.value
  const pid = String(f.endpointId || '').trim() || 'llmrouter'
  const prev = (c.provider[pid] && typeof c.provider[pid] === 'object') ? c.provider[pid] : {}
  const models = {}
  for (const line of String(f.endpointModelsText || '').split('\n')) {
    const id = ccNativeModel(line, pid)
    if (!id) continue
    const old = (prev.models && prev.models[id] && typeof prev.models[id] === 'object') ? prev.models[id] : {}
    models[id] = { ...old }
  }
  const entry = { ...prev }
  if (String(f.endpointApi || '').trim()) entry.api = String(f.endpointApi).trim()
  entry.baseURL = derivedBaseUrl()
  const key = String(f.endpointKey || '').trim()
  // 官方仅接受引用："$VAR" / "{env:VAR}" / "!command"；留空默认写免登录环境变量引用
  //（$COMMAND_CODE_API_KEY，本地 provider 保存时自动写入），false / 字面量会被 cmdc 判为无 key
  entry.apiKey = key || CC_DEFAULT_KEY_REF
  entry.models = models
  c.provider[pid] = entry
  ccProvidersContent.value = JSON.stringify(c, null, 2)
}

// watchers 在 init 中注册（组件 setup 上下文内）
export function initCommandCode() {
  // 表单改动 → config.json 源码页签实时序列化；settings.json / providers.json 页签同步刷新
  //（正在编辑对应源码页签时跳过，避免打断输入/覆写手改内容）
  watch(ccForm, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'commandcode' || ccMuted || jsonEcho) return
    try { ccApplyToObj(); syncToJson() } catch (e) {}
    try { if (cfgMode.value !== 'ccsettings') ccApplyToSettings() } catch (e) {}
    try { if (cfgMode.value !== 'ccproviders') ccApplyToProviders() } catch (e) {}
  }, { deep: true })
  // 权限规则点选 → settings.json 源码页签实时刷新
  watch(ccPermRows, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'commandcode' || ccMuted || jsonEcho) return
    try { if (cfgMode.value !== 'ccsettings') ccApplyToSettings() } catch (e) {}
  }, { deep: true })
  // 模型列表在后台刷新（如拉取完成/服务商模型变化）时，实时同步进模型清单
  watch(modelOptions, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'commandcode' || ccMuted || jsonEcho) return
    ccSyncModelsFromList()
  })
}