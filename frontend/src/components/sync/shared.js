// Clients.vue 拆分出的共享工具与常量（无状态，无 Vue 副作用）
import { t } from '../../i18n'

// ============ 常量 ============
export const ACCESS_OPTIONS = [
  { value: '/oai', label: () => t('sync.optOpenaiCompat') },
  { value: '/aai', label: () => 'Anthropic' },
  { value: '/gai', label: () => 'Gemini' },
]
// 权限模式点选卡片：name 为主文案（中文为简短说明，英文为模式名），desc 为英文模式下的副文案
export const PERM_OPTIONS = [
  { value: 'default', name: () => t('sync.permDefaultName'), desc: () => t('sync.permDefaultDesc') },
  { value: 'acceptEdits', name: () => t('sync.permAcceptEditsName'), desc: () => t('sync.permAcceptEditsDesc') },
  { value: 'bypassPermissions', name: () => t('sync.permBypassName'), desc: () => t('sync.permBypassDesc') },
  { value: 'plan', name: () => t('sync.permPlanName'), desc: () => t('sync.permPlanDesc') },
]

// ============ 通用工具 ============
export function getPath(obj, path) {
  let cur = obj
  for (const p of path.split('.')) {
    if (cur == null || typeof cur !== 'object') return undefined
    cur = cur[p]
  }
  return cur
}
export function setPath(obj, path, value) {
  const parts = path.split('.')
  let cur = obj
  for (let i = 0; i < parts.length - 1; i++) {
    if (!cur[parts[i]] || typeof cur[parts[i]] !== 'object') cur[parts[i]] = {}
    cur = cur[parts[i]]
  }
  cur[parts[parts.length - 1]] = value
}
export function deepMerge(base, patch) {
  const out = Array.isArray(base) ? [...base] : { ...(base && typeof base === 'object' ? base : {}) }
  if (!patch || typeof patch !== 'object') return out
  for (const [k, v] of Object.entries(patch)) {
    if (v === undefined) continue
    if (v && typeof v === 'object' && !Array.isArray(v) && out[k] && typeof out[k] === 'object' && !Array.isArray(out[k])) out[k] = deepMerge(out[k], v)
    else out[k] = v
  }
  return out
}
export function srcObj(content) {
  try { const v = JSON.parse(content || '{}'); return v && typeof v === 'object' ? v : {} } catch (e) { return {} }
}
// 源码合法性检查：空内容视为合法（待初始化）；非空但解析失败返回 false。
// 表单联动 watcher 在源码非法时应停止回写，避免把损坏内容静默重建为最小结构
export function srcObjValid(content) {
  const t = String(content || '').trim()
  if (!t) return true
  try { JSON.parse(t); return true } catch (e) { return false }
}
// 权限文本框滚轮边界抑制：文本框内滚动到达顶部/底部时阻止默认滚动与冒泡，
// 避免带动外层页面一起滚动（即使文本框自身已无内容可滚）
export function taWheelStop(e) {
  const el = e.currentTarget
  const d = e.deltaY
  const atTop = el.scrollTop <= 0 && d < 0
  const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 1 && d > 0
  if (atTop || atBottom) { e.preventDefault(); e.stopPropagation() }
}
// 权限规则快选文本域（按行去重切换）：与 CommandCode 面板同一套胶囊交互
export function permToggleRule(list, rule) {
  const arr = String(list || '').trim() ? String(list).split(/\r?\n/).map(s => s.trim()).filter(Boolean) : []
  const i = arr.indexOf(rule)
  if (i >= 0) arr.splice(i, 1)
  else arr.push(rule)
  return arr.join('\n')
}
export function permRuleOn(list, rule) {
  return String(list || '').split(/\r?\n/).some(s => s.trim() === rule)
}

// ---------- 权限规则点选列表（统一 UI，各客户端共用）----------
// 把 allow/ask/deny 三个数组合并成 [{ text, mode }] 的行列表。
export function permRowsFromLists(lists) {
  const rows = []
  for (const mode of ['allow', 'ask', 'deny']) {
    const arr = lists && Array.isArray(lists[mode]) ? lists[mode] : []
    for (const it of arr) {
      const text = String(it).trim()
      if (text) rows.push({ text, mode })
    }
  }
  return rows
}
// 反向：按 mode 分组拆回 { allow, ask, deny } 数组（每个非空文本一行）。
export function permRowsToLists(rows) {
  const out = { allow: [], ask: [], deny: [] }
  for (const r of rows || []) {
    const text = String(r.text || '').trim()
    if (!text) continue
    const mode = (r.mode === 'ask' || r.mode === 'deny') ? r.mode : 'allow'
    out[mode].push(text)
  }
  return out
}
// 点选切换：点选已激活的模式再次点击取消（回到未设置）。
export function permRowToggleMode(row, mode) {
  if (!row) return
  row.mode = row.mode === mode ? '' : mode
}

// ============ 客户端 logo（frontend/public/logos/）============
export const TARGET_LOGOS = {
  'claude-code': '/logos/claude.svg',
  'claude-desktop': '/logos/claude.svg',
  'gemini': '/logos/gemini.svg',
  'opencode': '/logos/opencode.svg',
  'grok-build': '/logos/grok.svg',
  'pi': '/logos/pi.svg',
  'codex': '/logos/codex.svg',
  'cline': '/logos/cline.svg',
  'hermes': '/logos/hermes.svg',
  'commandcode': '/logos/commandcode.svg',
  'copilot': '/logos/copilot.svg',
  'kimi': '/logos/kimi.svg',
  'kilo': '/logos/kilo.svg',
  'agy': '/logos/agy.svg',
}
export function targetLogo(id) { return TARGET_LOGOS[id] || '' }

// ============ TOML 引号工具 ============
export const tq = v => '"' + String(v ?? '').replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"'
export function unquoteToml(s) {
  const v = (s || '').trim()
  if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) return v.slice(1, -1)
  return v
}