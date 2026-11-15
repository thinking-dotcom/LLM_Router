import { toast } from './toast'
import { t } from './i18n'

// HTML 转义（模型名来自客户端请求、外部可控，拼进模板字符串前必须转义）
export function esc(s) {
  return (s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

// 进位边界：本档四舍五入满 1000 时升到上一档单位，避免出现「1000.0k」这类越界值
export const fmtNum = n => n >= 999.95e6 ? (n / 1e9).toFixed(2) + 'B' : n >= 999.95e3 ? (n / 1e6).toFixed(1) + 'M' : n >= 1e3 ? (n / 1e3).toFixed(1) + 'k' : String(n)

// 百分比展示：0 显示 0.0%（此前会误入 <0.1% 分支）；大于 0 且 <0.1% 显示 <0.1%，其余保留 1 位小数
export const fmtPct = pct => pct <= 0 ? '0.0%' : pct < 0.1 ? '<0.1%' : pct.toFixed(1) + '%'

// 黄金角取色：任意数量模型都不会颜色重复。
// 起点取主色蓝（~215°，与界面单一主色同族）而非 0°（红色＝界面里的错误语义，
// 且首个模型常是最主要的一条）；饱和度压到 46% 与中性面色同档，避免图表比界面
// 更抢眼；奇偶索引交替明暗，相邻扇区在色相之外多一层亮度区分。
export const chartColor = i => {
  const hue = Math.round(215 + i * 137.508) % 360
  return `hsl(${hue}, 46%, ${i % 2 ? 52 : 60}%)`
}

// 模板内延迟执行。模板表达式里的裸 setTimeout 会被编译成 `_ctx.setTimeout`——
// render proxy 上并无该属性（它只回退到 setup 绑定与 app.config.globalProperties，
// 而 setTimeout 两者都不在），运行时会抛 "is not a function"，延迟关闭弹层静默失效。
// 同理裸写 window.setTimeout 也会变成 `_ctx.window`。故模板里一律走此 helper。
export const delay = (fn, ms = 120) => setTimeout(fn, ms)

// 本地日期键，须与后端 ComputeStats 的 localDateKey 完全一致
export function todayKey() {
  const d = new Date()
  return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0')
}

// 客户端接入信息：统一 /router 地址，协议由路由器按请求路径自动识别
export function endpointInfo(baseURL, fmt) {
  const base = String(baseURL || '').replace(/\/+$/, '') + '/router'
  // paths：{ p: 完整路径, tag: 区分用 i18n 键（单路径为空串） }
  if (fmt === 'anthropic') return { sdk: base, paths: [{ p: base + '/v1/messages', tag: '' }] }
  // OpenAI 兼容两代接口：Chat Completions + Responses API
  if (fmt === 'openai') return { sdk: base, paths: [{ p: base + '/chat/completions', tag: 'ep.openaiChat' }, { p: base + '/v1/responses', tag: 'ep.openaiResponses' }] }
  // Gemini 两代协议并列展示：legacy generateContent + 新协议 Interactions（2026 GA）
  if (fmt === 'gemini') return { sdk: base, paths: [{ p: base + '/v1beta/models/{model}:generateContent', tag: 'ep.geminiLegacy' }, { p: base + '/v1beta/interactions', tag: 'ep.geminiInter' }] }
  return { sdk: base, paths: [{ p: base + '/chat/completions', tag: 'ep.openaiChat' }] }
}

// 复制文本；silent=true 时由调用方自己提示
export async function copyText(text, btn, silent = false) {
  try {
    await navigator.clipboard.writeText(text)
    if (!silent) toast(t('common.copied'))
    flashCopied(btn)
  } catch (e) { toast(t('common.copyFail')) }
}

// 复制成功后的短暂反馈：仅变绿高亮，不替换文字（避免换字导致按钮宽高变化/页面抖动），
// 具体的“已复制”提示由 copyText 里的 toast 提供。
function flashCopied(btn) {
  if (!btn || btn.dataset.flashing) return
  btn.dataset.flashing = '1'
  if (!btn.classList.contains('ok')) btn.classList.add('ok')
  setTimeout(() => {
    if (btn.classList.contains('ok')) btn.classList.remove('ok')
    delete btn.dataset.flashing
  }, 1400)
}

// 把绝对定位弹层摆到锚点下方；下方放不下则翻到上方，并在两个方向上按弹层实际尺寸夹紧
export function placePopup(menu, rect, gap = 4) {
  const mw = menu.offsetWidth
  const mh = menu.offsetHeight
  let top = rect.bottom + gap
  if (top + mh > window.innerHeight - 6) top = rect.top - mh - gap
  let left = rect.left
  if (left + mw > window.innerWidth - 6) left = window.innerWidth - 6 - mw
  if (left < 6) left = 6
  menu.style.left = left + 'px'
  menu.style.top = Math.max(6, top) + 'px'
}

// squarified treemap：在 w×h 的虚拟空间内按占比铺排矩形，返回 0-100 的百分比坐标
export function squarifyTiles(items, w, h) {
  const sum = items.reduce((s, it) => s + it.value, 0)
  if (sum <= 0) return []
  const scale = (w * h) / sum
  const areas = items.map(it => it.value * scale).sort((a, b) => b - a)

  const rects = []
  let rx = 0, ry = 0, rw = w, rh = h
  let i = 0
  while (i < areas.length && rw > 0.5 && rh > 0.5) {
    const longSide = Math.max(rw, rh)
    const horizontal = rw >= rh

    let row = []
    let rowArea = 0
    let worst = Infinity
    let j = i
    while (j < areas.length) {
      const candidate = row.concat(areas[j])
      const ca = rowArea + areas[j]
      const thickness = ca / longSide
      let wst = 0
      for (const a of candidate) {
        const s1 = a / thickness, s2 = thickness
        const asp = Math.max(s1, s2) / Math.min(s1, s2)
        if (asp > wst) wst = asp
      }
      if (wst < worst) { worst = wst; row = candidate; rowArea = ca; j++ }
      else break
    }

    const thickness = rowArea / longSide
    let offset = 0
    for (let k = 0; k < row.length; k++) {
      const len = row[k] / thickness
      if (horizontal) rects.push({ x: rx + offset, y: ry, w: len, h: thickness })
      else rects.push({ x: rx, y: ry + offset, w: thickness, h: len })
      offset += len
    }
    if (horizontal) { ry += thickness; rh -= thickness }
    else { rx += thickness; rw -= thickness }
    i = j
  }
  // 极端情况下（如条目总面积恰好铺满空间）可能提前退出导致 rects 不足，
  // 为剩余条目补齐占位矩形，避免调用方访问到 undefined 崩溃。
  while (rects.length < items.length) {
    rects.push({ x: 0, y: 0, w: 0, h: 0 })
  }
  return rects.map(r => ({ x: r.x / w * 100, y: r.y / h * 100, w: r.w / w * 100, h: r.h / h * 100 }))
}

// 汇总今日各模型用量（降序），并入在途活跃模型（0 token 占位）；
// 每段附带 prompt（输入）/ completion（输出）/ count（请求数）明细
export function computeModelSegments(stats, activeModels) {
  const today = todayKey()
  const modelTokens = {}
  for (const [model, data] of Object.entries(stats?.perModel || {})) {
    const dd = data.days?.[today]
    if (!dd || dd.count <= 0) continue
    modelTokens[model] = {
      value: (dd.prompt || 0) + (dd.completion || 0),
      prompt: dd.prompt || 0,
      completion: dd.completion || 0,
      count: dd.count || 0,
    }
  }
  for (const m of activeModels || []) {
    if (m && !(m in modelTokens)) modelTokens[m] = { value: 0, prompt: 0, completion: 0, count: 0 }
  }
  const sorted = Object.entries(modelTokens).sort((a, b) => b[1].value - a[1].value)
  const segments = sorted.map(([name, o]) => ({ name, value: o.value, prompt: o.prompt, completion: o.completion, count: o.count }))
  const total = segments.reduce((s, x) => s + x.value, 0) || 1
  return { segments, total }
}

// JSON 语法错误定位：返回 {line, col, msg}（1 起始），语法合法返回 null。
// 自带轻量扫描器而非依赖引擎报错——不同浏览器 SyntaxError 文案不一，且新版
// V8 常常不含位置；扫描器保证「第 X 行第 Y 列」稳定可用。
export function jsonErrorPos(text) {
  const t = String(text == null ? '' : text)
  let i = 0, line = 1, col = 1
  const disp = c => (c === '\n' ? '换行' : c === '\t' ? '制表符' : `"${c}"`)
  const err = msg => ({ line, col, msg })
  const adv = k => { while (k-- && i < t.length) { if (t[i] === '\n') { line++; col = 1 } else col++; i++ } }
  const skipWs = () => { while (i < t.length) { const c = t[i]; if (c === ' ' || c === '\t' || c === '\n' || c === '\r') adv(1); else break } }
  const lit = w => { if (t.startsWith(w, i)) { adv(w.length); return true } return false }
  function scanStr() { // 调用点已停在 '"'
    const startLine = line, startCol = col
    adv(1)
    while (i < t.length) {
      const c = t[i]
      if (c === '\\') { adv(2); continue }
      if (c === '"') { adv(1); return true }
      if (c === '\n' || c === '\r' || c.charCodeAt(0) < 0x20) { line = startLine; col = startCol; return false }
      adv(1)
    }
    line = startLine; col = startCol
    return false // 未闭合
  }
  const numRe = /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/
  const stack = [] // {obj:true/false, empty:true}
  let mode = 'v'   // v=期待值 k=期待键名或'}' a=值结束，期待逗号/闭括号/结尾
  for (;;) {
    skipWs()
    if (mode === 'v') {
      if (i >= t.length) {
        const top = stack[stack.length - 1]
        if (top) return err(top.obj ? '输入已结束：对象缺少 "}"' : '输入已结束：数组缺少 "]"')
        if (!t.trim()) return null
        return err('输入意外结束')
      }
      const top = stack[stack.length - 1]
      const c = t[i]
      if (c === '{') { adv(1); stack.push({ obj: true, empty: true }); mode = 'k' }
      else if (c === '[') { adv(1); stack.push({ obj: false, empty: true }); mode = 'v' }
      else if (!top || top.obj || !top.empty || c !== ']') {
        if (c === '"') { const ok = scanStr(); if (!ok) return err('字符串未闭合或含未转义的换行/控制字符'); mode = 'a' }
        else if (c === '-' || (c >= '0' && c <= '9')) { const m = numRe.exec(t.slice(i)); if (!m) return err(`非法数字 ${disp(c)}…`); adv(m[0].length); mode = 'a' }
        else if (lit('true') || lit('false') || lit('null')) mode = 'a'
        else return err(`不应出现的字符 ${disp(c)}`)
        if (stack.length) stack[stack.length - 1].empty = false
      } else { adv(1); stack.pop(); mode = 'a' } // 空数组直接闭合
    } else if (mode === 'k') {
      const top = stack[stack.length - 1]
      if (i >= t.length) return err('输入已结束：对象缺少 "}"')
      const c = t[i]
      if (c === '"') {
        if (!scanStr()) return err('键名字符串未闭合')
        skipWs()
        if (t[i] !== ':') return err(`缺少冒号 ":"${i < t.length ? `，此处是 ${disp(t[i])}` : ''}`)
        adv(1); mode = 'v'
      } else if (c === '}' && top.empty) { adv(1); stack.pop(); mode = 'a' }
      else return err(`对象键名应为双引号字符串，不应出现 ${disp(c)}`)
    } else { // a
      if (!stack.length) {
        if (i < t.length) return err('多余的内容：JSON 应在此结束')
        return null
      }
      if (i >= t.length) return err(stack[stack.length - 1].obj ? '输入已结束：对象缺少 "}"' : '输入已结束：数组缺少 "]"')
      const top = stack[stack.length - 1]
      const c = t[i]
      const close = top.obj ? '}' : ']'
      if (c === ',') {
        adv(1)
        top.empty = false
        skipWs()
        const nc = t[i]
        if (nc === close) return err(`多余的逗号","（${top.obj ? '对象' : '数组'}最后一项后不能有逗号）`)
        mode = top.obj ? 'k' : 'v'
      } else if (c === close) { adv(1); stack.pop(); mode = 'a' }
      else return err(`此处应为 "," 或 "${close}"，却是 ${disp(c)}`)
    }
  }
}

// ---- 模板 JSON 载入期修复（业务流程第 2 步：校验与修复） ----

// 字符串感知地剥离 // 与 /* */ 注释；块注释内的换行保留以维持行号稳定
function stripJsonComments(t) {
  let out = '', i = 0, n = t.length, inStr = false
  while (i < n) {
    const c = t[i]
    if (inStr) {
      out += c
      if (c === '\\') { out += t[i + 1] || ''; i += 2; continue }
      if (c === '"') inStr = false
      i++; continue
    }
    if (c === '"') { inStr = true; out += c; i++; continue }
    if (c === '/' && t[i + 1] === '/') { while (i < n && t[i] !== '\n') i++; continue }
    if (c === '/' && t[i + 1] === '*') {
      i += 2
      while (i < n && !(t[i] === '*' && t[i + 1] === '/')) { out += t[i] === '\n' ? '\n' : ' '; i++ }
      i += 2; out += ' '; continue
    }
    out += c; i++
  }
  return out
}

// 字符串感知地删除 } 或 ] 前的尾逗号（连同其后的空白）
function stripTrailingCommas(t) {
  let out = '', i = 0, n = t.length, inStr = false
  while (i < n) {
    const c = t[i]
    if (inStr) {
      out += c
      if (c === '\\') { out += t[i + 1] || ''; i += 2; continue }
      if (c === '"') inStr = false
      i++; continue
    }
    if (c === '"') { inStr = true; out += c; i++; continue }
    if (c === ',') {
      let j = i + 1
      while (j < n && (t[j] === ' ' || t[j] === '\t' || t[j] === '\n' || t[j] === '\r')) j++
      if (t[j] === '}' || t[j] === ']') { i++; continue } // 丢弃逗号，空白保留在后续输出中
    }
    out += c; i++
  }
  return out
}

// 已知非法顶层键（不在对应客户端官方 schema 中），载入时删减；tools 内非法开关一并清除
const ILLEGAL_TEMPLATE_KEYS = {
  opencode: { top: ['context', 'maxTokens', 'mcpServers', 'systemPrompt', 'temperature', 'ui', 'providerName', 'accessMode', 'permissions'], tools: ['websearch'] },
}

// 就地删减非法键；有删减返回 true
export function pruneIllegalTemplateKeys(obj, targetId) {
  const rule = ILLEGAL_TEMPLATE_KEYS[targetId]
  if (!obj || typeof obj !== 'object' || !rule) return false
  let hit = false
  for (const k of rule.top) if (k in obj) { delete obj[k]; hit = true }
  if (rule.tools && obj.tools && typeof obj.tools === 'object') {
    for (const k of rule.tools) if (k in obj.tools) { delete obj.tools[k]; hit = true }
  }
  return hit
}

// 尝试修复 JSON 文本：返回 {text, notes[], changed}；修不动（仍非法）返回 null。
export function repairJsonText(raw) {
  let t = String(raw == null ? '' : raw)
  const orig = t
  const notes = []
  if (t.charCodeAt(0) === 0xfeff) { t = t.slice(1); notes.push('去除 BOM') }
  const noCmt = stripJsonComments(t)
  if (noCmt !== t) { t = noCmt; notes.push('移除注释') }
  const noTc = stripTrailingCommas(t)
  if (noTc !== t) { t = noTc; notes.push('移除多余逗号') }
  try { JSON.parse(t) } catch (e) { return null }
  return { text: t, notes, changed: t !== orig }
}
