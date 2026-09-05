// Claude Code / Desktop（固定 /aai 接入）：settings.json（/ ~/.claude.json）表单编辑
import { ref, watch } from 'vue'
import api from '../../api'
import { getPath, setPath, permRowsFromLists, permRowsToLists } from './shared'
import { cfgObj, cfgEdit, cfgTarget, genericMuted, jsonEcho, routerHintForTarget, syncToJson } from './store'

export const claudeAccessMode = ref('/aai')
// 模型映射行的固定环境变量槽位（Claude Code 官方读取的 env 键），新行按序预填、不可手改
export const CLAUDE_ENV_KEY_SLOTS = ['ANTHROPIC_MODEL', 'ANTHROPIC_DEFAULT_SONNET_MODEL', 'ANTHROPIC_DEFAULT_OPUS_MODEL', 'ANTHROPIC_DEFAULT_HAIKU_MODEL', 'ANTHROPIC_SMALL_FAST_MODEL']
export function newClaudeEnvModelRow() {
  const used = new Set(claudeEnvModels.value.map(r => String(r.envKey || '').trim()).filter(Boolean))
  let key = ''
  for (const slot of CLAUDE_ENV_KEY_SLOTS) {
    if (!used.has(slot)) { key = slot; break }
  }
  if (!key) {
    let i = 2
    while (used.has('ANTHROPIC_MODEL_' + i)) i++
    key = 'ANTHROPIC_MODEL_' + i
  }
  return { envKey: key, modelId: '', contextWindow: '' }
}
export const claudeEnvModels = ref([])
// 表格上次从配置构建时的模型 ID 集合：删除行时用于判断 modelContext 中
// 哪些键是表格管理、可安全清理的（不含用户手写、表格从未管理的键）
let claudeManagedModels = new Set()
export function normalizeClaudeEnvModels() {
  if (!Array.isArray(claudeEnvModels.value)) claudeEnvModels.value = []
  if (claudeEnvModels.value.length === 0) {
    const env = getPath(cfgObj.value, 'env') || {}
    const mc = getPath(cfgObj.value, 'modelContext') || {}
    const rows = []
    for (const [k, v] of Object.entries(env)) {
      if (!/^ANTHROPIC_.*MODEL$/.test(k)) continue
      const mid = String(v)
      rows.push({ envKey: k, modelId: mid, contextWindow: String(mc?.[mid]?.contextWindow ?? '') })
    }
    claudeEnvModels.value = rows
    claudeManagedModels = new Set(rows.map(r => String(r.modelId || '').trim()).filter(Boolean))
  }
}
export function syncClaudeEnvModelsToObj() {
  const env = { ...(getPath(cfgObj.value, 'env') || {}) }
  // 先移除面板管理的旧 env 键，再按当前行重写，保证删除行真正生效
  for (const k of Object.keys(env)) {
    if (/^ANTHROPIC_.*MODEL$/.test(k)) delete env[k]
  }
  const activeModels = new Set()
  const curCtx = getPath(cfgObj.value, 'modelContext')
  const ctx = (curCtx && typeof curCtx === 'object' && !Array.isArray(curCtx)) ? { ...curCtx } : {}
  for (const row of claudeEnvModels.value) {
    const k = String(row.envKey || '').trim()
    if (!k) continue
    env[k] = String(row.modelId || '').trim()
    const mid = String(row.modelId || '').trim()
    if (!mid) continue
    activeModels.add(mid)
    if (String(row.contextWindow).trim()) ctx[mid] = { contextWindow: Number(row.contextWindow) }
  }
  // 表格曾管理、现已不再被任何行引用的模型上下文一并清理（删除行不残留）
  for (const id of claudeManagedModels) {
    if (!activeModels.has(id)) delete ctx[id]
  }
  claudeManagedModels = new Set([...claudeManagedModels, ...activeModels])
  setPath(cfgObj.value, 'env', env)
  setPath(cfgObj.value, 'modelContext', ctx)
}

// ---- Claude：默认模型（settings.json 顶层 model）与权限规则列表 ----
// permissions 官方结构：{ defaultMode, allow:[], deny:[], ask:[], additionalDirectories:[] }
// 规则语法：Bash(npm run test) / Read(/path) / Edit(...) / WebFetch(domain) 等；
// 表单以「每行一条规则」的文本区编辑 allow/deny/ask 数组。
export const claudeDefaultModel = ref('')
export const claudePermRules = ref({ addDirs: '' })
export const claudePermRows = ref([]) // [{ text, mode }] 权限规则点选列表（allow/ask/deny 统一点选）
// modelContext 映射的文本视图（"模型id:上下文窗口" 每行一个），与 cfgObj.modelContext 双向转换
export function initClaudeExtra() {
  const c = cfgObj.value || {}
  claudeDefaultModel.value = String(c.model || '')
  const perms = (c.permissions && typeof c.permissions === 'object' && !Array.isArray(c.permissions)) ? c.permissions : {}
  claudePermRules.value = {
    // 当前官方为 permissions.additionalDirectories；兼容旧版顶层 additionalDirectories
    addDirs: Array.isArray(perms.additionalDirectories)
      ? perms.additionalDirectories.join('\n')
      : (Array.isArray(c.additionalDirectories) ? c.additionalDirectories.join('\n') : ''),
  }
  claudePermRows.value = permRowsFromLists(perms)
  initClaudeExtraFields()
}
export function syncClaudeExtraToObj() {
  const c = cfgObj.value
  if (!c || typeof c !== 'object') return
  if (String(claudeDefaultModel.value || '').trim()) c.model = String(claudeDefaultModel.value).trim()
  else if (c.model) delete c.model
  const prevPerm = (c.permissions && typeof c.permissions === 'object' && !Array.isArray(c.permissions)) ? c.permissions : {}
  // 保留已有 defaultMode 等旁路键，仅重建三条规则数组（来自点选列表按类别分组）
  const perms = { ...prevPerm }
  const listVal = s => String(s || '').split('\n').map(x => x.trim()).filter(Boolean)
  const lists = permRowsToLists(claudePermRows.value)
  for (const k of ['allow', 'deny', 'ask']) {
    if (lists[k].length) perms[k] = lists[k]
    else delete perms[k]
  }
  const ad = listVal(claudePermRules.value.addDirs)
  if (ad.length) perms.additionalDirectories = ad
  else delete perms.additionalDirectories
  c.permissions = perms
  syncClaudeExtraFieldsToObj()
}

// ---- Claude：settings.json 模版其余字段（除 model/permissions/env/mcpServers 等已管理键） ----
// type: text 文本 | number 数字 | bool 三态('' 默认/true/false) | select 枚举 | list 多行文本→字符串数组
// 按参考模版 agent/Json_modles/claude/settings.json 重新分组：
// template 组就是模版顶层键（除 model/permissions/env/mcpServers），其余按用途归类；
// 渲染时每组分一张卡片：bool/select/number 等短字段并排，list 独占一行。
export const CLAUDE_EXTRA_GROUPS = [
  { id: 'template', fields: [
    { key: 'theme', type: 'select', options: ['dark', 'light', 'dark-daltonized', 'light-daltonized', 'dark-ansi', 'light-ansi'] },
    { key: 'editorMode', type: 'select', options: ['normal', 'emacs', 'vim'] },
    { key: 'autoCompactEnabled', type: 'bool' },
    { key: 'maxContextTokens', type: 'number' },
    { key: 'cleanupPeriodDays', type: 'number' },
    { key: 'alwaysThinkingEnabled', type: 'bool' },
    { key: 'includeGitInstructions', type: 'bool' },
  ]},
  { id: 'run', fields: [
    { key: 'agent', type: 'text' },
    { key: 'effortLevel', type: 'select', options: ['low', 'medium', 'high', 'xhigh'] },
    { key: 'language', type: 'text' },
  ]},
  { id: 'ctx', fields: [
    { key: 'autoCompactWindow', type: 'number' },
    { key: 'autoContinueAtUsageLimit', type: 'bool' },
  ]},
  { id: 'govern', fields: [
    { key: 'disableAllHooks', type: 'bool' },
    { key: 'allowedMcpServers', type: 'list' },
    { key: 'deniedMcpServers', type: 'list' },
    { key: 'allowManagedMcpServersOnly', type: 'bool' },
    { key: 'allowAllClaudeAiMcpsLoad', type: 'bool' },
  ]},
  { id: 'misc', fields: [
    { key: 'preferredNotifChannel', type: 'select', options: ['auto', 'iterm2', 'iterm2_with_bell', 'terminal_bell', 'notifications_disabled'] },
    { key: 'autoUpdatesChannel', type: 'select', options: ['stable', 'rc', 'latest', 'disabled'] },
    { key: 'skipWebFetchPreflight', type: 'bool' },
    { key: 'includeCoAuthoredBy', type: 'bool' },
  ]},
]
// 拍平：init / sync 逻辑遍历全部字段，不区分分组
export const CLAUDE_EXTRA_FIELDS = CLAUDE_EXTRA_GROUPS.flatMap(g => g.fields)
// ---- Claude Code 全局记忆/指令文件 CLAUDE.md（~/.claude/CLAUDE.md，源码页签直读直写） ----
export const claudeMdContent = ref('')
export const claudeMdExists = ref(false)
export const claudeMdFile = ref('~/.claude/CLAUDE.md')
export async function loadClaudeMarkdown() {
  try {
    const r = await api.GetClaudeMarkdown()
    claudeMdExists.value = !!(r && r.ok && r.exists)
    if (r && r.file) claudeMdFile.value = r.file
    claudeMdContent.value = (r && r.ok && r.exists && r.content) ? r.content : ''
  } catch (e) { claudeMdExists.value = false; claudeMdContent.value = '' }
}
export async function saveClaudeMarkdown() {
  try {
    const r = await api.SaveClaudeMarkdown(claudeMdContent.value)
    return (r && r.ok) ? '' : (r?.error || 'CLAUDE.md 保存失败')
  } catch (e) { return e?.message || 'CLAUDE.md 保存失败' }
}
// 表单值：文本/数字为字符串，bool 为三态字符串，list 为多行字符串（与 cfgObj 双向转换）
export const claudeExtra = ref({})
const removeClaudePath = (obj, path) => {
  const parts = path.split('.')
  let cur = obj
  for (let i = 0; i < parts.length - 1; i++) {
    if (!cur || typeof cur !== 'object') return
    cur = cur[parts[i]]
  }
  if (cur && typeof cur === 'object') delete cur[parts[parts.length - 1]]
}
const initClaudeExtraFields = () => {
  const c = cfgObj.value || {}
  for (const f of CLAUDE_EXTRA_FIELDS) {
    let v = getPath(c, f.key)
    if (Array.isArray(v)) v = v.join('\n')
    else if (f.type === 'bool') v = (v === true || v === 'true') ? 'true' : (v === false || v === 'false') ? 'false' : ''
    else v = (v == null) ? '' : String(v)
    claudeExtra.value[f.key] = v
  }
}
const syncClaudeExtraFieldsToObj = () => {
  const c = cfgObj.value
  if (!c || typeof c !== 'object') return
  for (const f of CLAUDE_EXTRA_FIELDS) {
    const raw = claudeExtra.value[f.key]
    const empty = raw == null || String(raw).trim() === ''
    if (f.type === 'bool') {
      if (raw === 'true') setPath(c, f.key, true)
      else if (raw === 'false') setPath(c, f.key, false)
      else removeClaudePath(c, f.key)
    } else if (f.type === 'number') {
      if (empty) removeClaudePath(c, f.key)
      else {
        const n = Number(raw)
        if (Number.isFinite(n)) setPath(c, f.key, n)
      }
    } else if (f.type === 'list') {
      const arr = String(raw || '').split('\n').map(x => x.trim()).filter(Boolean)
      if (arr.length) setPath(c, f.key, arr)
      else removeClaudePath(c, f.key)
    } else {
      if (empty) removeClaudePath(c, f.key)
      else setPath(c, f.key, String(raw).trim())
    }
  }
  // attribution 三个子键全空时移除空对象，避免残留
  if (c.attribution && typeof c.attribution === 'object' && !Object.keys(c.attribution).length) delete c.attribution
}

export function initClaude() {
  // 模型列表行编辑（含上下文长度列）→ 实时写回 cfgObj.modelContext 并刷新 JSON 源码页签
  watch(claudeEnvModels, () => {
    if (!cfgEdit.value || genericMuted || jsonEcho) return
    const id = cfgTarget.value?.id
    if (id !== 'claude-code') return
    try { syncClaudeEnvModelsToObj(); syncToJson() } catch (e) {}
  }, { deep: true })
  // 默认模型 / 权限规则 / 模版高级字段 → 实时写回 cfgObj
  watch([claudeDefaultModel, claudePermRules, claudePermRows, claudeExtra], () => {
    if (!cfgEdit.value || genericMuted || jsonEcho) return
    const id = cfgTarget.value?.id
    if (id !== 'claude-code') return
    try { syncClaudeExtraToObj(); syncToJson() } catch (e) {}
  }, { deep: true })
}
export function claudeBuildFormText() {
  syncClaudeEnvModelsToObj()
  syncClaudeExtraToObj()
  setPath(cfgObj.value, 'env.ANTHROPIC_BASE_URL', routerHintForTarget())
  return JSON.stringify(cfgObj.value, null, 2)
}