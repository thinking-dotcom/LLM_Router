// Kimi Code（~/.kimi-code/config.toml，TOML 结构化表单）
// 受管内容（对齐官方配置文件文档）：顶层 default_model / default_permission_mode、
// [providers.<key>]（type / base_url / api_key）、[models."<alias>"]（provider / model /
// max_context_size / display_name / default_effort）、[thinking]（enabled / effort / keep）
// 与 [[permission.rules]]（decision / scope / pattern / reason）段；其余段与块内
// 非受管键原样保留。AGENTS.md（全局 Kimi 专属 Agent 指令）源码直编。
import { ref, watch, nextTick } from 'vue'
import { tq } from '../shared'
import { cfgContent, cfgEdit, cfgTarget, derivedBaseUrl } from '../store'

export const KIMI_EFFORT_LEVELS = ['low', 'medium', 'high', 'xhigh', 'max']
// 常用权限规则快选项（Kimi pattern 语法：工具名 或 工具名(参数模式)，* 通配）
export const KIMI_PERM_RULES = ['Read', 'Grep', 'Glob', 'Edit', 'Bash', 'WebFetch', 'WebSearch', 'Bash(rm -rf*)', 'mcp__*__*']
export const kimiForm = ref({ defaultModel: '', permMode: '' })
export const kimiProviderRows = ref([]) // { key, type, baseUrl, apiKey, extra: [] }
export const kimiModelRows = ref([])    // { alias, provider, model, context, display, effort, extra: [] }
export const kimiThinking = ref({ enabled: '', effort: '', keep: '', extra: [] }) // [thinking] 段
// 统一权限面板：默认权限模式三档（点选已选项取消 = 不写入，沿用客户端默认）
export const KIMI_PERM_MODES = [
  { v: 'manual', label: 'sync.opt.manual' },
  { v: 'yolo', label: 'sync.opt.yolo' },
  { v: 'auto', label: 'sync.opt.auto' },
]
export const kimiPermRows = ref([])     // [[permission.rules]] 行 { decision, pattern, scope, reason, extra: [] }
export const kimiUnmanaged = ref([])    // 非受管内容块（段头 + 键行/注释）
export let kimiMuted = false
export function setKimiMuted(v) { kimiMuted = v }
export const kimiModelOpen = ref(false) // 模型下拉
export const kimiCustomPattern = ref('') // 自定义规则输入（权限面板底部 pcl-add 行）
export const kimiAgentsContent = ref('') // ~/.kimi-code/AGENTS.md 源码
export const kimiAgentsExists = ref(false)
export const kimiAgentsFile = ref('~/.kimi-code/AGENTS.md')

// toml 键：裸键直接写，否则双引号基本字符串转义
export function kimiTomlKey(k) {
  if (/^[A-Za-z0-9_-]+$/.test(String(k || ''))) return String(k)
  return '"' + String(k ?? '').replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"'
}
export function kimiUnquote(s) {
  const v = (s || '').trim()
  if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) return v.slice(1, -1)
  return v
}
// 段头键解析：去引号、取首段（裸键可带点分子表，取首段为服务名/模型别名）
export function kimiSectionKey(inner) {
  return kimiUnquote(inner.split('.')[0])
}
export function kimiTomlPreset() {
  return `# ~/.kimi-code/config.toml

default_model = "llm"

[providers."llm-router"]
type = "openai"
base_url = "` + derivedBaseUrl() + `"
api_key = ""

[models."llm"]
provider = "llm-router"
model = ""
max_context_size = 128000
`
}
export function kimiReadToml(text) {
  const pRows = []
  const mRows = []
  const unmanaged = []
  const permRows = []
  let think = { enabled: '', effort: '', keep: '', extra: [] }
  let curRow = null // 当前受管块（provider / model / 权限规则行）
  let sec = ''
  let curUn = null  // 当前非受管段
  for (const raw of String(text || '').split('\n')) {
    const line = raw.trim()
    const ah = line.match(/^\[\[([^\]]+)\]\]$/)
    const h = ah ? null : line.match(/^\[([^\]]+)\]$/)
    if (ah) {
      curRow = null
      curUn = null
      const inner = ah[1].trim()
      if (inner === 'permission.rules') {
        curRow = { decision: '', pattern: '', scope: '', reason: '', extra: [] }
        permRows.push(curRow)
        sec = 'permrule'
        continue
      }
      // 其它数组表段（[[hooks]] 等）：整段原样保留
      sec = ''
      curUn = { lines: [raw] }
      unmanaged.push(curUn)
      continue
    }
    if (h) {
      curRow = null
      curUn = null
      const inner = h[1].trim()
      if (inner.startsWith('providers.')) {
        const key = kimiSectionKey(inner.slice('providers.'.length))
        if (key) { curRow = { key, type: '', baseUrl: '', apiKey: '', extra: [] }; pRows.push(curRow); sec = 'provider' }
        continue
      }
      if (inner.startsWith('models.')) {
        const alias = kimiSectionKey(inner.slice('models.'.length))
        if (alias) { curRow = { alias, provider: '', model: '', context: '', display: '', effort: '', extra: [] }; mRows.push(curRow); sec = 'model' }
        continue
      }
      if (inner === 'thinking') {
        think = { enabled: '', effort: '', keep: '', extra: [] }
        curRow = think
        sec = 'thinking'
        continue
      }
      // 非受管段：从段头起整段原样保留
      sec = ''
      curUn = { lines: [raw] }
      unmanaged.push(curUn)
      continue
    }
    if (curUn) { curUn.lines.push(raw); continue }
    const kv = line.match(/^([A-Za-z0-9_.-]+)\s*=\s*(.+)$/)
    if (kv) {
      const key = kv[1], val = kv[2].trim()
      if (!curRow) {
        if (key === 'default_model') kimiForm.value.defaultModel = kimiUnquote(val)
        else if (key === 'default_permission_mode') kimiForm.value.permMode = kimiUnquote(val)
        else unmanaged.push({ lines: [raw] })
        continue
      }
      if (sec === 'provider') {
        if (key === 'type') curRow.type = kimiUnquote(val)
        else if (key === 'base_url') curRow.baseUrl = kimiUnquote(val)
        else if (key === 'api_key') curRow.apiKey = kimiUnquote(val)
        else curRow.extra.push(raw)
      } else if (sec === 'model') {
        if (key === 'provider') curRow.provider = kimiUnquote(val)
        else if (key === 'model') curRow.model = kimiUnquote(val)
        else if (key === 'max_context_size') curRow.context = val
        else if (key === 'display_name') curRow.display = kimiUnquote(val)
        else if (key === 'default_effort') curRow.effort = kimiUnquote(val)
        else curRow.extra.push(raw)
      } else if (sec === 'thinking') {
        if (key === 'enabled') curRow.enabled = kimiUnquote(val)
        else if (key === 'effort') curRow.effort = kimiUnquote(val)
        else if (key === 'keep') curRow.keep = kimiUnquote(val)
        else curRow.extra.push(raw)
      } else if (sec === 'permrule') {
        if (key === 'decision') curRow.decision = kimiUnquote(val)
        else if (key === 'pattern') curRow.pattern = kimiUnquote(val)
        else if (key === 'scope') curRow.scope = kimiUnquote(val)
        else if (key === 'reason') curRow.reason = kimiUnquote(val)
        else curRow.extra.push(raw)
      }
    } else if (line !== '') {
      // 注释/空白行：受管块内原样保留在该块，段外保留到非受管区
      if (curRow) curRow.extra.push(raw)
      else unmanaged.push({ lines: [raw] })
    }
  }
  // 行内部键 kbk（列表 :key 用）：仅存在于内存，序列化时不参与（见 kimiBuildToml）
  let kbk = 0
  for (const r of pRows) r.kbk = 'kbp' + (kbk++)
  for (const r of mRows) r.kbk = 'kbm' + (kbk++)
  kimiProviderRows.value = pRows
  kimiModelRows.value = mRows
  kimiThinking.value = think
  kimiPermRows.value = permRows
  kimiUnmanaged.value = unmanaged
}
// 布尔键（enabled）由下拉原样输出 true/false，不做引号包裹
// 行内部键 kbk 只服务列表渲染，写盘时必须忽略：受管键均显式列出、extra 为原始行，
// 因此 kbk 天然不会落盘，无需额外过滤。
export function kimiBuildToml() {
  const out = ['# ~/.kimi-code/config.toml']
  const dm = String(kimiForm.value.defaultModel || '').trim()
  if (dm) out.push('', 'default_model = ' + tq(dm))
  const pm = String(kimiForm.value.permMode || '').trim()
  if (pm) out.push('default_permission_mode = ' + tq(pm))
  for (const p of kimiProviderRows.value) {
    const key = String(p.key || '').trim()
    if (!key) continue
    out.push('', '[providers.' + kimiTomlKey(key) + ']')
    out.push('type = ' + tq(p.type || 'openai'))
    if (String(p.baseUrl || '').trim()) out.push('base_url = ' + tq(p.baseUrl))
    if (String(p.apiKey || '').trim()) out.push('api_key = ' + tq(p.apiKey))
    for (const ln of (p.extra || [])) out.push(ln)
  }
  for (const m of kimiModelRows.value) {
    const alias = String(m.alias || '').trim()
    if (!alias) continue
    out.push('', '[models.' + kimiTomlKey(alias) + ']')
    if (String(m.provider || '').trim()) out.push('provider = ' + tq(m.provider))
    if (String(m.model || '').trim()) out.push('model = ' + tq(m.model))
    if (String(m.context || '').trim()) out.push('max_context_size = ' + String(m.context).trim())
    if (String(m.effort || '').trim()) out.push('default_effort = ' + tq(m.effort))
    if (String(m.display || '').trim()) out.push('display_name = ' + tq(m.display))
    for (const ln of (m.extra || [])) out.push(ln)
  }
  // [thinking] 受管段：三键任一或块内保留行存在时才输出
  const tk = kimiThinking.value || {}
  if (tk.enabled || tk.effort || tk.keep || (tk.extra && tk.extra.length)) {
    out.push('', '[thinking]')
    if (tk.enabled) out.push('enabled = ' + tk.enabled)
    if (tk.effort) out.push('effort = ' + tq(tk.effort))
    if (tk.keep) out.push('keep = ' + tq(tk.keep))
    for (const ln of (tk.extra || [])) out.push(ln)
  }
  // [[permission.rules]] 数组表：pattern 必填，decision 默认不写（Kimi 按缺失处理）
  for (const r of kimiPermRows.value) {
    const pat = String(r.pattern || '').trim()
    if (!pat) continue
    out.push('', '[[permission.rules]]')
    if (String(r.decision || '').trim()) out.push('decision = ' + tq(r.decision))
    if (String(r.scope || '').trim()) out.push('scope = ' + tq(r.scope))
    out.push('pattern = ' + tq(pat))
    if (String(r.reason || '').trim()) out.push('reason = ' + tq(r.reason))
    for (const ln of (r.extra || [])) out.push(ln)
  }
  // 非受管内容原样保留在末尾（[loop_control] / [mcp] / [[hooks]] 等）
  for (const blk of kimiUnmanaged.value) {
    if (blk && Array.isArray(blk.lines) && blk.lines.length) out.push('', ...blk.lines)
  }
  return out.join('\n') + '\n'
}
// 行内部键 kbk 序号：新建行的稳定标识（列表 :key 用），序列化时被显式剔除
let kbkSeq = 1
export function kimiAddProvider() { kimiProviderRows.value.push({ key: '', type: 'openai', baseUrl: '', apiKey: '', extra: [], kbk: 'kbp' + (kbkSeq++) }) }
export function kimiAddModel() {
  const row = { alias: '', provider: '', model: '', context: '', display: '', effort: '', extra: [], kbk: 'kbm' + (kbkSeq++) }
  if (kimiProviderRows.value.length === 1) row.provider = kimiProviderRows.value[0].key
  kimiModelRows.value.push(row)
  return row
}
export function kimiDefaultCandidates() {
  return kimiModelRows.value.map(r => String(r.alias || '').trim()).filter(Boolean)
}
// 固定工具规则的当前处置（opencode 权限面板风格：键位常驻，点选三态）
export function kimiRuleDecision(pattern) {
  const row = kimiPermRows.value.find(r => r.pattern === String(pattern || ''))
  return row ? String(row.decision || '') : ''
}
// 点选固定规则的 decision 三态：设置/切换；清除时若该行无保留内容（scope/reason/extra）则整行移除
export function kimiToggleRule(pattern, mode) {
  const pat = String(pattern || '')
  if (!pat) return
  let row = kimiPermRows.value.find(r => r.pattern === pat)
  if (row && row.decision === mode) {
    if (!(row.extra || []).length && !String(row.scope || '').trim() && !String(row.reason || '').trim()) {
      kimiPermRows.value.splice(kimiPermRows.value.indexOf(row), 1)
    } else {
      row.decision = ''
    }
    return
  }
  if (!row) { row = { decision: '', pattern: pat, scope: '', reason: '', extra: [] }; kimiPermRows.value.push(row) }
  row.decision = mode
}
// 自定义（非预设）规则行：文件中原有规则保持可编辑/可删除
export function kimiCustomRows() {
  return kimiPermRows.value.filter(r => !KIMI_PERM_RULES.includes(String(r.pattern || '')))
}
// 自定义行 decision 三态：点选已激活项再次点击取消（回到未设置）
export function kimiPermDecision(row, mode) {
  if (!row) return
  row.decision = row.decision === mode ? '' : mode
}
// 底部输入添加自定义规则（回车或点「添加」）
export function kimiAddCustomRule() {
  const pat = String(kimiCustomPattern.value || '').trim()
  if (!pat || kimiPermRows.value.some(r => r.pattern === pat)) return
  kimiPermRows.value.push({ decision: '', pattern: pat, scope: '', reason: '', extra: [] })
  kimiCustomPattern.value = ''
}

// watchers 在 init 中注册（组件 setup 上下文内）
export function initKimi() {
  // 表单 → TOML 实时联动：填写面板时，config.toml 源码文本框实时更新
  watch([kimiForm, kimiProviderRows, kimiModelRows, kimiThinking, kimiPermRows], () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'kimi' || kimiMuted) return
    try { cfgContent.value = kimiBuildToml() } catch (e) {}
  }, { deep: true })
}