// Kilo（~/.config/kilo/kilo.jsonc，JSONC 结构化表单）
// 接入方式与 OpenCode 同族：受管键为 provider.<key>（npm/name/options.{baseURL,
// apiKey}/models，models 条目 id/name/reasoning/modalities）、顶层默认模型三键
// model / small_model / subagent_model（"服务商/模型" 格式）、permission 工具权限；
// 其余顶层键（$schema / disabled_providers / instructions / web_search / mcp 等）
// 原样保留。mcp 只读展示（MCP 由 MCP 页统一管理）。
import { ref, watch } from 'vue'
import { cfgObj, cfgContent, cfgEdit, cfgTarget, derivedBaseUrl, jsonEcho, syncToJson } from './store'
import { repairJsonText } from '../../utils'
import { toast } from '../../toast'
import { t } from '../../i18n'

// AGENTS.md 源码页签（~/.config/kilo/AGENTS.md，Markdown 源码直编；同 OpenCode/Kimi）
export const kiloAgentsContent = ref('')
export const kiloAgentsExists = ref(false)
export const kiloAgentsFile = ref('')

export const kiloProviderKey = ref('llm') // provider 对象键（可自取，如 go / llm / zen）
export const kiloApiKey = ref('')
export const kiloNpm = ref('@ai-sdk/openai-compatible')
export const kiloName = ref('LLM Router')
export const kiloModels = ref([]) // [{id,name,reasoning,vision}]，reasoning/vision 为 '' / 'true' / 'false' 三态
// 顶层默认模型（"kiloProviderKey/模型ID" 格式；空 = 未设置）
export const kiloModel = ref('')
export const kiloSmallModel = ref('')
export const kiloSubagentModel = ref('')
// 权限配置：Kilo 与 OpenCode 同为工具级 permission 键（* 全局兜底 + 具体工具）
export const KILO_PERM_KEYS = [
  { k: '*', hintKey: 'sync.kilo.itemFallback' },
  { k: 'read', hintKey: 'sync.kilo.itemRead' },
  { k: 'edit', hintKey: 'sync.kilo.itemEdit' },
  { k: 'glob', hintKey: 'sync.kilo.itemGlob' },
  { k: 'grep', hintKey: 'sync.kilo.itemGrep' },
  { k: 'list', hintKey: 'sync.kilo.itemList' },
  { k: 'bash', hintKey: 'sync.kilo.itemBash' },
  { k: 'task', hintKey: 'sync.kilo.itemTask' },
  { k: 'skill', hintKey: 'sync.kilo.itemSkill' },
  { k: 'lsp', hintKey: 'sync.kilo.itemLsp' },
  { k: 'question', hintKey: 'sync.kilo.itemQuestion' },
  { k: 'webfetch', hintKey: 'sync.kilo.itemWebfetch' },
  { k: 'websearch', hintKey: 'sync.kilo.itemWebsearch' },
  { k: 'semantic_search', hintKey: 'sync.kilo.itemSemanticSearch' },
  { k: 'external_directory', hintKey: 'sync.kilo.itemExternal' },
  { k: 'doom_loop', hintKey: 'sync.kilo.itemDoom' },
]
export const kiloPerms = ref({}) // key -> { mode, obj }，obj 标记该键原值是对象规则（保留）
export function kiloPickMode(key, mode) {
  const st = kiloPerms.value[key]
  if (!st) return
  st.mode = st.mode === mode ? '' : mode
}
// 原始 provider 键：表单改动键名时，写回需同步移除旧键条目（避免 provider.<oldKey> 残留）
let kiloProviderKeyOrig = ''
export function kiloProviderKeyOriginal() { return kiloProviderKeyOrig }
// 模型下拉（行内 / 顶层默认模型）
export const kiloModelRowOpen = ref(-1)
export const kiloModelOpen = ref(false)
export const kiloSmallOpen = ref(false)
export const kiloSubOpen = ref(false)
export function initKiloPerms(perm) {
  const o = {}
  for (const { k } of KILO_PERM_KEYS) {
    const v = perm ? perm[k] : undefined
    if (typeof v === 'string') o[k] = { mode: v, obj: false }
    else if (v && typeof v === 'object') o[k] = { mode: '', obj: true }
    else o[k] = { mode: '', obj: false }
  }
  kiloPerms.value = o
}
export function initKiloForm() {
  const c = cfgObj.value || {}
  const provs = (c.provider && typeof c.provider === 'object') ? c.provider : {}
  const keys = Object.keys(provs).filter(k => String(k || '').trim())
  const key = keys[0] || 'llm'
  const prov = (provs[key] && typeof provs[key] === 'object') ? provs[key] : {}
  const opts = prov.options || {}
  kiloProviderKey.value = key
  kiloProviderKeyOrig = key
  kiloApiKey.value = opts.apiKey || ''
  kiloNpm.value = prov.npm || '@ai-sdk/openai-compatible'
  kiloName.value = prov.name || 'LLM Router'
  const perm = (c.permission && typeof c.permission === 'object') ? c.permission : {}
  initKiloPerms(perm)
  const ms = (prov.models && typeof prov.models === 'object') ? prov.models : {}
  kiloModels.value = Object.keys(ms).map(id => {
    const e = (ms[id] && typeof ms[id] === 'object') ? ms[id] : {}
    const mm = (e.modalities && typeof e.modalities === 'object') ? e.modalities : {}
    const inp = Array.isArray(mm.input) ? mm.input : []
    const eo = (e.options && typeof e.options === 'object') ? e.options : {}
    return {
      id,
      name: e.name || '',
      reasoning: e.reasoning === true ? 'true' : e.reasoning === false ? 'false' : '',
      effort: String(eo.reasoningEffort || ''),
      vision: inp.includes('image') ? 'true' : '',
    }
  })
  kiloModel.value = String(c.model || '')
  kiloSmallModel.value = String(c.small_model || '')
  kiloSubagentModel.value = String(c.subagent_model || '')
}
// Kilo 权限写回：allow/ask/deny 直接写字符串（覆盖旧值）；空值且原是对象规则时保留；
// 空值且非对象删除该键；全空且原无 permission 时删除顶层键
export function kiloApplyPerms(c) {
  const hadPerm = !!(c.permission && typeof c.permission === 'object')
  if (!hadPerm) c.permission = {}
  const perm = c.permission
  for (const { k } of KILO_PERM_KEYS) {
    const st = kiloPerms.value[k]
    if (!st) continue
    if (st.mode) { perm[k] = st.mode; continue }
    if (st.obj) continue
    delete perm[k]
  }
  if (!hadPerm && !Object.keys(perm).length) delete c.permission
}
export function kiloProviderPrefix() {
  return String(kiloProviderKey.value || '').trim() || 'llm'
}
export function kiloQualifyModel(m) {
  return (m && !String(m).includes('/')) ? kiloProviderPrefix() + '/' + String(m) : String(m || '')
}
export function kiloApplyToObj() {
  const c = cfgObj.value
  if (!c || typeof c !== 'object') return
  if (!c.provider || typeof c.provider !== 'object') c.provider = {}
  const pk = kiloProviderPrefix()
  const prev = (c.provider[pk] && typeof c.provider[pk] === 'object') ? c.provider[pk] : {}
  const prevOpts = (prev.options && typeof prev.options === 'object') ? prev.options : {}
  const prevModels = (prev.models && typeof prev.models === 'object') ? prev.models : {}
  const models = {}
  for (const row of kiloModels.value) {
    const id = String(row.id || '').trim()
    if (!id) continue
    const old = (prevModels[id] && typeof prevModels[id] === 'object') ? prevModels[id] : {}
    const oldM = (old.modalities && typeof old.modalities === 'object') ? old.modalities : {}
    const oldInput = Array.isArray(oldM.input) ? oldM.input : []
    const oldOutput = Array.isArray(oldM.output) ? oldM.output : []
    const oldOpts = (old.options && typeof old.options === 'object') ? old.options : {}
    const entry = {}
    for (const k of Object.keys(old)) if (k !== 'name' && k !== 'reasoning' && k !== 'modalities' && k !== 'options') entry[k] = old[k]
    if (String(row.name || '').trim()) entry.name = String(row.name).trim()
    if (row.reasoning === 'true' || row.reasoning === 'false') entry.reasoning = row.reasoning === 'true'
    // 推理深度档位：写到模型条目的 options.reasoningEffort（与 OpenCode 同构，
    // @ai-sdk/openai-compatible 透传给上游）；空值移除该键但保留 options 其余键
    const opts = {}
    for (const k of Object.keys(oldOpts)) if (k !== 'reasoningEffort') opts[k] = oldOpts[k]
    if (String(row.effort || '').trim()) opts.reasoningEffort = String(row.effort).trim()
    if (Object.keys(opts).length) entry.options = opts
    // 视觉（modalities）：按模板的 { input: [], output: [] } 双通道结构处理。
    // 「视觉」开关只管输入通道 input（模型能否接受图片），output（图片生成）
    // 不受开关影响、原样保留——避免把非生图模型误标为可输出图片。视觉开启往
    // input 加 "image"，关闭移除；未设置（''）原样保留原 modalities（含 output
    // 与其它通道），不新增空壳。原文件无 modalities 且未设置视觉时不产生该键。
    let input = [...oldInput]
    const output = [...oldOutput]
    if (row.vision === 'true') {
      if (!input.includes('image')) input.push('image')
      entry.modalities = { ...oldM, input, output }
    } else if (row.vision === 'false') {
      input = input.filter(x => x !== 'image')
      entry.modalities = { ...oldM, input, output }
    } else if (old.modalities && typeof old.modalities === 'object') {
      entry.modalities = old.modalities
    }
    models[id] = entry
  }
  c.provider[pk] = {
    ...prev,
    npm: kiloNpm.value || '@ai-sdk/openai-compatible',
    name: kiloName.value || 'LLM Router',
    options: { ...prevOpts, baseURL: derivedBaseUrl(), apiKey: kiloApiKey.value || '' },
    models,
  }
  // 表单改变了 provider 键名时，移除旧键条目避免残留
  if (kiloProviderKeyOrig && kiloProviderKeyOrig !== pk) {
    delete c.provider[kiloProviderKeyOrig]
    kiloProviderKeyOrig = pk
  }
  const setModel = (key, v) => {
    const s = String(v || '').trim()
    if (s) c[key] = s
    else delete c[key]
  }
  setModel('model', kiloModel.value)
  setModel('small_model', kiloSmallModel.value)
  setModel('subagent_model', kiloSubagentModel.value)
  kiloApplyPerms(c)
}
// mcp 只读展示：Kilo 的 MCP 声明在顶层 mcp 键（对象形态），返回已配置服务器名
export function kiloMcpServerNames() {
  const m = cfgObj.value && typeof cfgObj.value === 'object' ? cfgObj.value.mcp : undefined
  if (!m || typeof m !== 'object' || Array.isArray(m)) return []
  return Object.keys(m).filter(k => {
    const e = m[k]
    return e !== false && (!e || typeof e !== 'object' || e.enabled !== false)
  })
}

export let kiloMuted = false
export function setKiloMuted(v) { kiloMuted = v }

// JSONC 源码 → cfgObj 回填（switchMode json→form 时调用；kind of syncToObjFromSource 的 JSONC 宽容版）
export function kiloFromSource() {
  try {
    const t = String(cfgContent.value || '{}')
    const parsed = parseJsonc(t)
    cfgObj.value = parsed && typeof parsed === 'object' ? parsed : {}
    return true
  } catch (e) { toast(t('sync.jsonError')); return false }
}
// JSONC 宽容解析：先剥离注释/尾逗号，失败再走 repairJsonText 尝试修复
export function parseJsonc(text) {
  const rep = repairJsonText(text)
  if (!rep) throw new Error('invalid jsonc')
  return JSON.parse(rep.text)
}

export function initKilo() {
  watch([kiloProviderKey, kiloApiKey, kiloNpm, kiloName, kiloModels, kiloPerms, kiloModel, kiloSmallModel, kiloSubagentModel], () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'kilo' || kiloMuted || jsonEcho) return
    try { kiloApplyToObj(); syncToJson() } catch (e) {}
  }, { deep: true })
}