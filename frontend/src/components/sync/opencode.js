// OpenCode（opencode.json，固定 /oai 接入）：provider.llm 模型/鉴权 + permission 权限表单
// 模型行字段：id / name / reasoning（推理深度档位）/ temperature（采样温度）
import { ref, watch, nextTick } from 'vue'
import { cfgObj, cfgEdit, cfgTarget, derivedBaseUrl, jsonEcho, syncToJson } from './store'

export const ocAgentsContent = ref('')
export const ocAgentsExists = ref(false)
export const ocAgentsFile = ref('~/.config/opencode/AGENTS.md')

export const ocAccessMode = ref('/oai')
export const ocApiKey = ref('')
export const ocNpm = ref('@ai-sdk/openai-compatible')
export const ocName = ref('LLM')
export const ocModels = ref([]) // [{id,name,reasoning,temperature}]，reasoning 为推理深度档位，temperature 为采样温度
// 权限配置：覆盖 OpenCode 全部权限键（含全局兜底 *）。
// 每键点选三态：allow / ask / deny；点选已激活项再次点击取消（回到未设置）；
// 配置中已有的对象细粒度规则面板不支持编辑，保存时原样保留
export const OC_PERM_KEYS = [
  { k: '*', hintKey: 'sync.oc.itemFallback' },
  { k: 'read', hintKey: 'sync.oc.itemRead' },
  { k: 'edit', hintKey: 'sync.oc.itemEdit' },
  { k: 'glob', hintKey: 'sync.oc.itemGlob' },
  { k: 'grep', hintKey: 'sync.oc.itemGrep' },
  { k: 'bash', hintKey: 'sync.oc.itemBash' },
  { k: 'task', hintKey: 'sync.oc.itemTask' },
  { k: 'skill', hintKey: 'sync.oc.itemSkill' },
  { k: 'lsp', hintKey: 'sync.oc.itemLsp' },
  { k: 'question', hintKey: 'sync.oc.itemQuestion' },
  { k: 'webfetch', hintKey: 'sync.oc.itemWebfetch' },
  { k: 'websearch', hintKey: 'sync.oc.itemWebsearch' },
  { k: 'external_directory', hintKey: 'sync.oc.itemExternal' },
  { k: 'doom_loop', hintKey: 'sync.oc.itemDoom' },
]
// 点选模式：点选已激活的模式再次点击取消（回到未设置）
export const ocPerms = ref({}) // key -> { mode, obj }，obj 标记该键原值是对象规则（面板不可编辑，保存时保留）
export function ocPickMode(key, mode) {
  const st = ocPerms.value[key]
  if (!st) return
  st.mode = st.mode === mode ? '' : mode
}
export function initOcForm() {
  const c = cfgObj.value || {}
  const llm = (c.provider && typeof c.provider === 'object' && c.provider.llm && typeof c.provider.llm === 'object') ? c.provider.llm : {}
  const opts = llm.options || {}
  ocApiKey.value = opts.apiKey || ''
  ocNpm.value = llm.npm || '@ai-sdk/openai-compatible'
  ocName.value = llm.name || 'LLM'
  ocAccessMode.value = '/oai'
  const perm = (c.permission && typeof c.permission === 'object') ? c.permission : {}
  initOcPerms(perm)
  const ms = (llm.models && typeof llm.models === 'object') ? llm.models : {}
  ocModels.value = Object.keys(ms).map(id => ({
    id,
    name: (ms[id] && ms[id].name) || '',
    reasoning: (ms[id] && ms[id].options && typeof ms[id].options === 'object') ? (ms[id].options.reasoningEffort || '') : '',
    temperature: (ms[id] && ms[id].options && typeof ms[id].options === 'object') ? ((ms[id].options.temperature ?? '') === '' ? '' : String(ms[id].options.temperature)) : '',
  }))
}
// 权限键初始化：字符串值直接映射为点选模式；对象值（细粒度规则）面板不支持编辑，
// 标记 obj 以便保存时原样保留（不误删）；无值视为未设置
export function initOcPerms(perm) {
  const o = {}
  for (const { k } of OC_PERM_KEYS) {
    const v = perm ? perm[k] : undefined
    if (typeof v === 'string') o[k] = { mode: v, obj: false }
    else if (v && typeof v === 'object') o[k] = { mode: '', obj: true }
    else o[k] = { mode: '', obj: false }
  }
  ocPerms.value = o
}
// 合并写回：provider.llm 结构整体重建；permission 覆盖全部权限键，
// 其余顶层键原样保留
export function ocApplyToObj() {
  const c = cfgObj.value
  if (!c || typeof c !== 'object') return
  if (!c.provider || typeof c.provider !== 'object') c.provider = {}
  // 合并写回：provider.llm 只覆盖受管键（npm/name/options.{baseURL,apiKey}/models），
  // llm 下的其它未管键原样保留；模型条目同样按 id 合并，保留未管字段与其它 options
  const prevLlm = (c.provider.llm && typeof c.provider.llm === 'object') ? c.provider.llm : {}
  const prevOpts = (prevLlm.options && typeof prevLlm.options === 'object') ? prevLlm.options : {}
  const prevModels = (prevLlm.models && typeof prevLlm.models === 'object') ? prevLlm.models : {}
  const models = {}
  for (const row of ocModels.value) {
    const id = String(row.id || '').trim()
    if (!id) continue
    const old = (prevModels[id] && typeof prevModels[id] === 'object') ? prevModels[id] : {}
    const oldOpts = (old.options && typeof old.options === 'object') ? old.options : {}
    const entry = {}
    for (const k of Object.keys(old)) if (k !== 'name' && k !== 'options') entry[k] = old[k]
    if (String(row.name || '').trim()) entry.name = String(row.name).trim()
    const opts = {}
    for (const k of Object.keys(oldOpts)) if (k !== 'reasoningEffort' && k !== 'temperature') opts[k] = oldOpts[k]
    if (String(row.reasoning || '').trim()) opts.reasoningEffort = String(row.reasoning).trim()
    if (String(row.temperature ?? '').trim() && isFinite(Number(row.temperature))) opts.temperature = Number(row.temperature)
    if (Object.keys(opts).length) entry.options = opts
    models[id] = entry
  }
  c.provider.llm = {
    ...prevLlm,
    npm: ocNpm.value || '@ai-sdk/openai-compatible',
    name: ocName.value || 'LLM',
    options: { ...prevOpts, baseURL: derivedBaseUrl(ocAccessMode.value), apiKey: ocApiKey.value || '' },
    models,
  }
  // 权限键：allow/ask/deny 直接写字符串（覆盖旧值，含对象）；
  // 空值且原值是对象（细粒度规则）时原样保留，避免误删用户已有配置；
  // 空值且非对象时删除该键；全空且原先无 permission 对象时不残留空壳
  const hadPerm = !!(c.permission && typeof c.permission === 'object')
  if (!hadPerm) c.permission = {}
  const perm = c.permission
  for (const { k } of OC_PERM_KEYS) {
    const st = ocPerms.value[k]
    if (!st) continue
    if (st.mode) { perm[k] = st.mode; continue }
    if (st.obj) continue // 对象规则：保留原值
    delete perm[k]
  }
  if (!hadPerm && !Object.keys(perm).length) delete c.permission
}

// 表单 → 模板 JSON 实时联动（watchers 在 init 中注册）
// ocMuted：模板载入期间静默——否则 initOcForm 的赋值会触发 watcher，
// 用 ocApplyToObj 重建 provider.llm 导致源码框键序与磁盘文件不一致。
export let ocMuted = false
export function setOcMuted(v) { ocMuted = v }
export function initOpencode() {
  watch([ocApiKey, ocNpm, ocName, ocModels, ocPerms], () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'opencode' || ocMuted || jsonEcho) return
    try { ocApplyToObj(); syncToJson() } catch (e) {}
  }, { deep: true })
}