// Command Code（~/.commandcode/config.json 编辑源）
// 官方字段参考 commandcode.ai/docs/settings 与 /docs/byok：
// config.json：provider / model / theme / compactMode / reasoningEffort（按模型 id 键控，
//   档位 low / medium / high / xhigh / max）；
// settings.json：model / effort / permissions（allow / ask / deny / defaultMode /
//   additionalDirectories / disableBypass），规则列表跨作用域并集生效；
// providers.json：BYOK 服务商（api 协议 + baseURL + apiKey 引用 + models）。
import { ref, watch, nextTick } from 'vue'
import { t } from '../../../i18n'
import api from '../../../api'
import { srcObj, permRowsFromLists, permRowsToLists } from '../shared'
import { cfgObj, cfgEdit, cfgTarget, cfgContent, cfgMode, derivedBaseUrl, routerHintForTarget, jsonEcho, syncToJson, modelOptions } from '../store'

export const CC_EFFORT_LEVELS = ['low', 'medium', 'high', 'xhigh', 'max']
export const CC_PERM_MODES = ['default', 'auto-accept', 'plan', 'bypass']
// 通用设置枚举（官方 settings 文档）：theme/compactMode/treeFilterMode/defaultExportFormat
export const CC_THEMES = ['dark', 'light', 'auto']
export const CC_COMPACT_MODES = ['default', 'fast']
export const CC_TREE_FILTERS = ['default', 'no-tools', 'user-only', 'labeled-only', 'all']
export const CC_EXPORT_FORMATS = ['html', 'jsonl', 'md']
// 三态归一：true/'true'→'true'，false/'false'→'false'，其余→''（不写键）
const ccTriStr = v => v === true || v === 'true' ? 'true' : (v === false || v === 'false') ? 'false' : ''
const ccSetOrDel = (obj, k, v) => { const t = String(v || '').trim(); if (t) obj[k] = t; else delete obj[k] }
const ccBoolOrDel = (obj, k, v) => { const t = ccTriStr(v); if (t === 'true') obj[k] = true; else if (t === 'false') obj[k] = false; else delete obj[k] }
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
  permMode: 'default', disableBypass: false,
  theme: '', compactMode: '', collapsePastedText: '', tasteLearning: '', treeFilterMode: '', exportFormat: '',
  disableScratchpad: '', disableSkillShell: '',
  attribution: '', ideContext: '', autoInstallExt: '',
})
export const ccPermRows = ref([]) // [{ text, mode }] 权限规则点选列表（allow/ask/deny 统一点选）
// featureModels（config.json 官方键）：后台功能模型覆盖，6 个官方功能键。
// 表单只管这 6 键（空=删键恢复默认）；磁盘上其它未知功能键原样保留。
export const CC_FEATURE_KEYS = ['titleGeneration', 'compaction', 'toolDescription', 'tasteOnboarding', 'tasteLearning', 'branchSummarization']
export const ccFeatModels = ref({}) // { [featureKey]: modelId }
// 「执行所有命令」三态：以裸工具名 Bash 作通配规则（与 Claude Code / CodeBuddy 官方
// 规则语法一致，= 该工具全部终端命令）。点选即把 Bash 行落到对应档位列表；
// 再点当前档取消（Bash 行移除，回到逐项询问）。其余既有规则行原样保留写回。
export const CC_CMD_RULE = 'Bash'
export function ccCmdMode() {
  const r = (ccPermRows.value || []).find(x => String(x.text || '').trim() === CC_CMD_RULE)
  return r ? r.mode : ''
}
export function ccCmdSet(mode) {
  const rows = (ccPermRows.value || []).slice()
  const i = rows.findIndex(x => String(x.text || '').trim() === CC_CMD_RULE)
  if (i >= 0 && rows[i].mode === mode) rows.splice(i, 1)
  else if (i >= 0) rows[i] = { ...rows[i], mode }
  else rows.push({ text: CC_CMD_RULE, mode })
  ccPermRows.value = rows
}
export const ccSettingsFile = ref(''); export const ccSettingsExists = ref(false); export const ccSettingsContent = ref('')
export const ccProvidersFile = ref(''); export const ccProvidersExists = ref(false); export const ccProvidersContent = ref('')
// 启动免登录开关：写/删用户环境变量 COMMAND_CODE_API_KEY（占位值），绕过 cmdc 启动账号校验
export const ccNoLogin = ref(false)
// 用户是否手动动过免登录开关：保存时「本地 provider 自动开启免登录」的便利
// 只在用户从未手动关闭时生效，否则会静默覆盖用户明确关掉的意图
export const ccNoLoginTouched = ref(false)
export const ccNoLoginMsg = ref('')
// keepTouched=true：保留「用户手动改过」的标记。用户点完开关后会回读真实状态，
// 回读若顺手把标记清掉，保存时又会把它当成「从未手动操作」而自动开启，开关等于关不掉。
export async function ccLoadNoLogin(keepTouched = false) {
  try { ccNoLogin.value = !!(api.GetCommandcodeNoLogin && await api.GetCommandcodeNoLogin()) } catch { ccNoLogin.value = false }
  if (!keepTouched) ccNoLoginTouched.value = false
}
export async function ccToggleNoLogin() {
  const want = ccNoLogin.value
  ccNoLoginTouched.value = true
  ccNoLoginMsg.value = ''
  if (!api.SetCommandcodeNoLogin) { ccNoLoginMsg.value = t('sync.ccNoLoginUnsupported'); await ccLoadNoLogin(true); return }
  let err = ''
  try { err = await api.SetCommandcodeNoLogin(want) } catch (e) { err = String(e?.message || e) }
  if (err) { ccNoLoginMsg.value = t('sync.ccNoLoginFail', { msg: err }) }
  await ccLoadNoLogin(true)
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
    // 勾选框两态：仅官方布尔 true 视为勾选；"disable" 字符串/缺省均视为不勾（保存时删键）
    disableBypass: perm.disableBypass === true,
    theme: String(c.theme || ''),
    compactMode: String(c.compactMode || ''),
    collapsePastedText: ccTriStr(c.collapsePastedText),
    tasteLearning: ccTriStr(c.tasteLearning),
    treeFilterMode: String(c.treeFilterMode || ''),
    exportFormat: String(c.defaultExportFormat || ''),
    disableScratchpad: ccTriStr(sset.disableScratchpad),
    disableSkillShell: ccTriStr(sset.disableSkillShellExecution),
    // 提交署名：磁盘空串显示为 ""（两引号），保存时输入 "" 即写空串关闭尾注
    attribution: (sset.attribution && typeof sset.attribution === 'object' && sset.attribution.commit === '') ? '""' : String((sset.attribution && typeof sset.attribution === 'object') ? sset.attribution.commit || '' : ''),
    ideContext: ccTriStr(c.ideContextEnabled),
    autoInstallExt: ccTriStr(c.autoInstallExtension),
  }
  // featureModels：只取官方 6 键；未知功能键不进表单（写回时原样保留）
  const fm = (c.featureModels && typeof c.featureModels === 'object' && !Array.isArray(c.featureModels)) ? c.featureModels : {}
  ccFeatModels.value = Object.fromEntries(CC_FEATURE_KEYS.map(k => [k, String(fm[k] || '')]))
  ccPermRows.value = permRowsFromLists(perm)
}
// 主配置（config.json）：覆盖 provider / model / reasoningEffort / featureModels 及通用偏好受管键，
// 其余字段（如机器写入的 installed 等）原样保留
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
  // featureModels：官方 6 键按表单写（空=删键恢复官方默认），其余未知功能键原样保留
  const fmPrev = (c.featureModels && typeof c.featureModels === 'object' && !Array.isArray(c.featureModels)) ? c.featureModels : {}
  const fmOut = { ...fmPrev }
  for (const k of CC_FEATURE_KEYS) {
    const v = String((ccFeatModels.value || {})[k] || '').trim()
    if (v) fmOut[k] = v
    else delete fmOut[k]
  }
  if (Object.keys(fmOut).length) c.featureModels = fmOut
  else delete c.featureModels
  // 通用偏好（官方 config.json 受管键）：所见即所得，空=删键
  ccSetOrDel(c, 'theme', f.theme)
  ccSetOrDel(c, 'compactMode', f.compactMode)
  ccSetOrDel(c, 'treeFilterMode', f.treeFilterMode)
  ccSetOrDel(c, 'defaultExportFormat', f.exportFormat)
  ccBoolOrDel(c, 'collapsePastedText', f.collapsePastedText)
  ccBoolOrDel(c, 'tasteLearning', f.tasteLearning)
  ccBoolOrDel(c, 'ideContextEnabled', f.ideContext)
  ccBoolOrDel(c, 'autoInstallExtension', f.autoInstallExt)
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
  // disableBypass 勾选框：勾=写 true（禁 bypass/--yolo）；不勾=删键回官方默认（"disable" 字符串等非布尔值一并清除）
  if (f.disableBypass === true) c.permissions.disableBypass = true
  else delete c.permissions.disableBypass
  // 行为开关（官方 settings.json 顶层键）：空=删键恢复默认
  ccBoolOrDel(c, 'disableScratchpad', f.disableScratchpad)
  ccBoolOrDel(c, 'disableSkillShellExecution', f.disableSkillShell)
  // attribution.commit（提交署名尾注）：留空=删键回默认 CommandCodeBot；输入 ""（两引号）=写空串关闭；
  // 其余原样写（非法格式官方会忽略）。attribution 未知子键原样保留。
  const attrPrev = (c.attribution && typeof c.attribution === 'object' && !Array.isArray(c.attribution)) ? c.attribution : {}
  const attrOut = { ...attrPrev }
  const attrRaw = String(f.attribution || '').trim()
  if (attrRaw === '""') attrOut.commit = ''
  else if (attrRaw) attrOut.commit = attrRaw
  else delete attrOut.commit
  if (Object.keys(attrOut).length) c.attribution = attrOut
  else delete c.attribution
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