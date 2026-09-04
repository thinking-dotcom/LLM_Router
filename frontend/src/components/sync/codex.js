// Codex（~/.codex/config.toml，固定 /oai 接入，TOML 结构化表单）
// 受管键：model、model_provider、model_reasoning_effort、model_temperature、model_top_p、
// approval_policy（untrusted / on-request / never，on-failure 已弃用）、
// [model_providers.<key>] 的 name/base_url/wire_api、可选的重试与超时三键、二选一的鉴权
// （api_key 明文 → [model_providers.<key>.http_headers] Authorization；api_key 留空且 env_key 非空
// → env_key = <环境变量名>）、以及 [permissions] 审批规则段（allow/deny/ask/disable 数组）。
// 其余内容（[projects]、[windows] 等用户自留节）原样保留。base_url 自动取路由器地址。
import { ref, watch } from 'vue'
import { toast } from '../../toast'
import { t } from '../../i18n'
import { permRowsFromLists, permRowsToLists, permToggleRule, permRuleOn, tq } from './shared'
import { cfgContent, cfgEdit, cfgTarget, cfgFormat, derivedBaseUrl } from './store'

export const codexAgentsContent = ref('')
export const codexAgentsExists = ref(false)
export const codexAgentsFile = ref('~/.codex/AGENTS.md')

export const codexForm = ref({ model: '', reasoning: '', temperature: '', topP: '', apiKey: '', providerKey: '', name: '', wireApi: 'responses', envKey: '', reqRetry: '', streamRetry: '', idleTimeout: '', approvalPolicy: '', permDisable: '' })
export const codexPermRows = ref([]) // [{ text, mode }] 权限规则点选列表（allow/ask/deny 统一点选，disable 单独保留）
// Codex 权限规则快选（与 CommandCode 同款胶囊下拉）：allow/ask/deny 三个文本域（disable 语义不同，保留纯文本框）
export const codexQuickOpen = ref('')
export const codexQuickFocus = ref('')
export function codexToggleRule(field, rule) { codexForm.value[field] = permToggleRule(codexForm.value[field], rule) }
export function codexRuleOn(field, rule) { return permRuleOn(codexForm.value[field], rule) }
// Codex 内置保留 provider ID，不能用作自定义 model_providers 键（会被 Codex 忽略）
export const CODEX_RESERVED_PROVIDERS = ['openai', 'ollama', 'lmstudio']
export const codexModelOpen = ref(false)
export let codexMuted = false
export function setCodexMuted(v) { codexMuted = v }
export function codexTomlQuote(v) { return '"' + String(v ?? '').replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"' }
// 从 TOML 数组字面量中提取全部引号字符串（支持多行数组），每项一行
function parseTomlArray(v) {
  const s = String(v ?? '')
  const m = s.match(/\[(.*)\]/s)
  if (!m) return ''
  const items = []
  const re = /"((?:[^"\\]|\\.)*)"|'([^']*)'/g
  let mm
  while ((mm = re.exec(m[1]))) items.push(mm[1] != null ? mm[1] : mm[2])
  return items.join('\n')
}
const capKey = k => k.charAt(0).toUpperCase() + k.slice(1)
export function codexInitFrom(text) {
  const f = { model: '', reasoning: '', temperature: '', topP: '', apiKey: '', providerKey: '', name: '', wireApi: 'responses', envKey: '', reqRetry: '', streamRetry: '', idleTimeout: '', approvalPolicy: '', permDisable: '' }
  let inProv = false
  let inHeaders = false
  let inPerm = false
  let curPermKey = ''
  let curPermBuf = ''
  const PERM_KEYS = ['allow', 'deny', 'ask', 'disable']
  const permAcc = { allow: [], ask: [], deny: [] }
  for (const ln of String(text || '').split(/\r?\n/)) {
    const t = ln.trim()
    if (curPermKey) {
      curPermBuf += t
      if (t.includes(']')) {
        const raw = String(parseTomlArray(curPermBuf) || '')
        if (curPermKey === 'disable') f.permDisable = raw
        else if (permAcc[curPermKey]) permAcc[curPermKey] = raw.split('\n').map(s => s.trim()).filter(Boolean)
        curPermKey = ''
        curPermBuf = ''
      }
      continue
    }
    if (/^\[permissions\]$/.test(t)) { inPerm = true; inProv = false; inHeaders = false; continue }
    if (/^\[model_providers\.[^\]]*\.http_headers\]$/.test(t)) { inHeaders = true; continue }
    const h = t.match(/^\[model_providers\.(.+?)\]$/)
    if (h) { inProv = true; inHeaders = false; inPerm = false; f.providerKey = h[1].replace(/^"|"$/g, ''); continue }
    if (/^\[/.test(t)) { inProv = false; inHeaders = false; inPerm = false; continue }
    const kv = t.match(/^([A-Za-z0-9_.-]+)\s*=\s*(.+)$/)
    if (!kv) continue
    const val = kv[2].trim().replace(/^"(.*)"$/, '$1')
    if (!inProv && kv[1] === 'model') f.model = val
    else if (!inProv && kv[1] === 'model_reasoning_effort') f.reasoning = val
    else if (!inProv && kv[1] === 'model_temperature') f.temperature = val
    else if (!inProv && kv[1] === 'model_top_p') f.topP = val
    else if (!inProv && kv[1] === 'approval_policy') f.approvalPolicy = val
    else if (inHeaders && kv[1] === 'Authorization') f.apiKey = val.replace(/^Bearer\s+/i, '')
    else if (inProv && kv[1] === 'name') f.name = val
    else if (inProv && kv[1] === 'wire_api') f.wireApi = val || 'responses'
    else if (inProv && kv[1] === 'env_key') f.envKey = val
    else if (inProv && kv[1] === 'request_max_retries') f.reqRetry = val
    else if (inProv && kv[1] === 'stream_max_retries') f.streamRetry = val
    else if (inProv && kv[1] === 'stream_idle_timeout_ms') f.idleTimeout = val
    else if (inPerm && PERM_KEYS.includes(kv[1])) {
      if (val.includes('[') && !val.includes(']')) { curPermKey = kv[1]; curPermBuf = val }
      else {
        const raw = String(parseTomlArray(val) || '')
        if (kv[1] === 'disable') f.permDisable = raw
        else if (permAcc[kv[1]]) permAcc[kv[1]] = raw.split('\n').map(s => s.trim()).filter(Boolean)
      }
    }
  }
  codexPermRows.value = permRowsFromLists(permAcc)
  codexForm.value = f
  return f
}
export function codexWriteToml(text) {
  const f = codexForm.value
  const key = (f.providerKey || '').trim() || 'llmrouter'
  const lines = String(text || '').split('\n')
  const lead = []   // 头部注释/空行：受管块插在它们之后
  const rest = []   // 其余未受管内容
  let inLead = true
  let inProv = false
  let inPerm = false
  let justCut = false
  for (const ln of lines) {
    const t = ln.trim()
    if (inLead && (t === '' || t.startsWith('#'))) { lead.push(ln); continue }
    inLead = false
    if (/^\[model_providers\.[^\]]+\]$/.test(t)) { inProv = true; justCut = true; continue } // 旧受管节：剔除
    if (inProv) {
      if (!/^\[/.test(t)) continue // 节内受管行：剔除（不清 justCut，便于吃掉随后的孤立空行）
      inProv = false               // 新节开始：该行落到通用逻辑
    }
    if (/^\[permissions\]$/.test(t)) { inPerm = true; justCut = true; continue } // 旧受管权限段：剔除
    if (inPerm) {
      if (!/^\[/.test(t)) continue
      inPerm = false
    }
    if (/^\[/.test(t)) { justCut = false; rest.push(ln); continue }
    if (!/^\[/.test(t) && /^(model|model_provider|model_reasoning_effort|model_temperature|model_top_p|approval_policy)\s*=/.test(t)) { justCut = true; continue } // 旧受管键：剔除
    if (t === '' && justCut) { justCut = false; continue } // 吞掉被删内容后的孤立空行，保证往返稳定
    justCut = false
    rest.push(ln)
  }
  const block = [
    'model = ' + codexTomlQuote(f.model),
    'model_provider = ' + codexTomlQuote(key),
    ...((f.reasoning || '').trim() ? ['model_reasoning_effort = ' + codexTomlQuote(f.reasoning.trim())] : []),
    ...((f.approvalPolicy || '').trim() ? ['approval_policy = ' + codexTomlQuote(f.approvalPolicy.trim())] : []),
  ]
  // 可选采样参数：留空或非数字不写入（TOML 浮点/整数，不加引号）
  const numFloat = v => (/^\d+(\.\d+)?$/.test(String(v ?? '').trim()) ? String(v).trim() : '')
  const tm = numFloat(f.temperature), tp = numFloat(f.topP)
  if (tm) block.push('model_temperature = ' + tm)
  if (tp) block.push('model_top_p = ' + tp)
  block.push(
    '',
    '[model_providers.' + key + ']',
    'name = ' + codexTomlQuote(f.name),
    'base_url = ' + codexTomlQuote(derivedBaseUrl()),
    // wire_api：新版 Codex 已移除 chat 支持，固定写 responses
    'wire_api = "responses"',
  )
  // 可选的重试与超时三键：留空或非纯数字不写入（TOML 整数，不加引号）
  const numOrEmpty = v => (/^\d+$/.test(String(v ?? '').trim()) ? String(v).trim() : '')
  const rr = numOrEmpty(f.reqRetry), sr = numOrEmpty(f.streamRetry), it = numOrEmpty(f.idleTimeout)
  if (rr) block.push('request_max_retries = ' + rr)
  if (sr) block.push('stream_max_retries = ' + sr)
  if (it) block.push('stream_idle_timeout_ms = ' + it)
  const ak = (f.apiKey || '').trim()
  if (ak) {
    // 密钥明文直写配置文件（http_headers 静态头），优先于 env_key
    block.push('', '[model_providers.' + key + '.http_headers]', 'Authorization = ' + codexTomlQuote('Bearer ' + ak))
  } else if ((f.envKey || '').trim()) {
    block.push('env_key = ' + codexTomlQuote(f.envKey.trim()))
  }
  // [permissions] 审批规则段：allow/ask/deny 来自点选列表按类别分组；disable 单独保留（语义不同）
  const permLines = []
  const lists = permRowsToLists(codexPermRows.value)
  for (const k of ['allow', 'ask', 'deny']) {
    const items = lists[k]
    if (items.length) permLines.push(k + ' = [' + items.map(i => codexTomlQuote(i)).join(', ') + ']')
  }
  {
    const items = String(f.permDisable || '').split('\n').map(s => s.trim()).filter(Boolean)
    if (items.length) permLines.push('disable = [' + items.map(i => codexTomlQuote(i)).join(', ') + ']')
  }
  if (permLines.length) block.push('', '[permissions]', ...permLines)
  const outLines = [...lead, ...block]
  if (rest.length && rest[0].trim() !== '') outLines.push('') // 受管块与后续内容空一行分隔
  outLines.push(...rest)
  let s = outLines.join('\n')
  if (!s.endsWith('\n')) s += '\n'
  return s
}
export function codexTomlPreset() {
  return `# ~/.codex/config.toml
# Codex CLI 参考模版：模型提供商指向本路由器

[model_providers.llmrouter]
name = "LLM Router"
base_url = "` + derivedBaseUrl() + `"
wire_api = "responses"
`
}

// watchers 在 init 中注册（组件 setup 上下文内）
export function initCodex() {
  watch(codexForm, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'codex' || codexMuted) return
    if (cfgFormat.value !== 'toml') return
    try { cfgContent.value = codexWriteToml(cfgContent.value) } catch (e) {}
  }, { deep: true })
}