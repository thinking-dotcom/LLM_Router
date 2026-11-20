// 即时文字提示：鼠标滑入即显示，替代浏览器原生 title 的延迟弹出。
// 对页面内所有带 title 的元素生效（工具栏图标、卡片图标等）。
let tipEl = null
let current = null
let savedTitle = ''

function ensureTip() {
  if (tipEl) return tipEl
  tipEl = document.createElement('div')
  tipEl.className = 'ui-tip'
  document.body.appendChild(tipEl)
  return tipEl
}

function titled(el) {
  while (el && el !== document.body) {
    if (el.getAttribute && el.getAttribute('title')) return el
    el = el.parentElement
  }
  return null
}

function place(el) {
  const r = el.getBoundingClientRect()
  const t = ensureTip()
  const w = t.offsetWidth
  const h = t.offsetHeight
  let x = r.left + r.width / 2 - w / 2
  let y = r.top - h - 6
  if (y < 4) y = r.bottom + 6
  t.style.left = Math.max(4, Math.min(x, window.innerWidth - w - 4)) + 'px'
  t.style.top = y + 'px'
}

function hide() {
  if (current && savedTitle) current.setAttribute('title', savedTitle)
  current = null
  savedTitle = ''
  if (tipEl) tipEl.style.display = 'none'
}

document.addEventListener('mouseover', (e) => {
  if (e.buttons) return
  const el = titled(e.target)
  if (!el || el === current) return
  hide()
  current = el
  savedTitle = el.getAttribute('title') || ''
  el.removeAttribute('title')
  // 简洁提示：优先用简短的 aria-label，避免把冗长的 title 说明整段弹出
  const label = el.getAttribute('aria-label') || savedTitle
  const html = el.getAttribute('data-tip-html')
  if (!label && !html) return
  const t = ensureTip()
  if (html) {
    t.innerHTML = html
  } else {
    t.textContent = label
  }
  t.style.display = 'block'
  place(el)
})

document.addEventListener('mouseout', (e) => {
  if (!current) return
  if (titled(e.relatedTarget) === current) return
  hide()
})

window.addEventListener('blur', hide)
window.addEventListener('scroll', hide, true)
