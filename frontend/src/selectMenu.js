/*
 * 原生 <select> 弹层替换：拦截原生下拉弹层，改为自绘、跟随应用主题的弹层。
 *
 * 背景：展开后的选项列表由 WebView2/OS 直接渲染，页面 CSS 无法控制其配色
 * （浅色系统应用模式下永远白底）。全局拦截 mousedown 阻止原生弹层，
 * 用与 .popup-menu 同一设计语言的 fixed 定位面板替代：
 *  - 不改任何模板（全应用 ~130 个 select 统一生效）；
 *  - 选值通过 selectedIndex + 派发 input/change 实现，v-model 读到的仍是
 *    option._value（对象/数字值不丢）；
 *  - 键盘可用：Enter/Space/↑/↓ 打开，↑/↓/Home/End 移动，Enter/Space 确认，
 *    Esc/Tab 关闭；外点、滚动、缩放即关。
 */
(function () {
  var pop = null, anchor = null, opts = [], act = -1
  var list = null

  function usable(sel) {
    if (!sel || sel.tagName !== 'SELECT' || sel.disabled || sel.multiple || (sel.size > 1) || !sel.options.length) return false
    var r = sel.getBoundingClientRect() // 隐藏/未渲染的 select（宽高为 0）不接管
    return r.width > 0 && r.height > 0
  }

  function snapshot(sel) {
    opts = []
    for (var i = 0; i < sel.options.length; i++) {
      var o = sel.options[i]
      opts.push({ idx: i, text: o.text, disabled: o.disabled, on: o.selected })
    }
  }

  function buildPop() {
    var d = document.createElement('div')
    d.className = 'csel-pop'
    d.setAttribute('role', 'listbox')
    list = document.createElement('div')
    list.style.padding = '0'
    d.appendChild(list)
    opts.forEach(function (o, i) {
      var it = document.createElement('div')
      it.className = 'csel-opt' + (o.on ? ' on' : '') + (o.disabled ? ' dis' : '')
      it.setAttribute('role', 'option')
      it.setAttribute('aria-selected', o.on ? 'true' : 'false')
      var span = document.createElement('span')
      span.textContent = o.text
      it.appendChild(span)
      if (o.on) {
        var ck = document.createElement('i')
        ck.className = 'ri-check-line ck'
        it.appendChild(ck)
      }
      if (!o.disabled) {
        it.addEventListener('mouseenter', function () { setAct(i) })
        it.addEventListener('mousedown', function (e) { e.preventDefault() })
        it.addEventListener('click', function () { pick(i) })
      }
      list.appendChild(it)
    })
    return d
  }

  function setAct(i) {
    act = i
    var items = list.children
    for (var k = 0; k < items.length; k++) items[k].classList.toggle('act', k === i)
    if (items[i]) {
      var it = items[i]
      var t = it.offsetTop, b = t + it.offsetHeight
      if (t < list.parentNode.scrollTop) list.parentNode.scrollTop = t
      else if (b > list.parentNode.scrollTop + list.parentNode.clientHeight) list.parentNode.scrollTop = b - list.parentNode.clientHeight
    }
  }

  function place() {
    var r = anchor.getBoundingClientRect()
    pop.style.visibility = 'hidden'
    pop.style.left = '0px'
    pop.style.top = '0px'
    pop.style.minWidth = Math.max(r.width, 120) + 'px'
    pop.style.maxWidth = 'min(420px, 92vw)'
    var vw = window.innerWidth, vh = window.innerHeight
    var ph = Math.min(pop.offsetHeight, 280)
    var w = pop.offsetWidth
    var left = Math.min(Math.max(8, r.left), vw - w - 8)
    var top = r.bottom + 4
    if (top + ph > vh - 8 && r.top - ph - 4 >= 8) top = r.top - ph - 4
    top = Math.max(8, Math.min(top, vh - ph - 8))
    pop.style.left = Math.round(left) + 'px'
    pop.style.top = Math.round(top) + 'px'
    pop.style.maxHeight = ph + 'px'
    pop.style.visibility = ''
  }

  function open(sel) {
    close()
    anchor = sel
    snapshot(sel)
    if (!opts.some(function (o) { return o.on })) act = 0
    pop = buildPop()
    document.body.appendChild(pop)
    place()
    var on = opts.findIndex(function (o) { return o.on })
    setAct(on >= 0 ? on : 0)
    list.children[act] && list.children[act].scrollIntoView({ block: 'nearest' })
  }

  function pick(i) {
    var o = opts[i]
    if (!o || o.disabled) return
    if (anchor.selectedIndex !== o.idx) {
      anchor.selectedIndex = o.idx
      anchor.dispatchEvent(new Event('input', { bubbles: true }))
      anchor.dispatchEvent(new Event('change', { bubbles: true }))
    }
    close()
  }

  function close(refocus) {
    if (!pop) return
    pop.remove()
    pop = null
    list = null
    if (anchor) {
      var a = anchor
      anchor = null
      if (refocus) a.focus({ preventScroll: true })
    }
  }

  document.addEventListener('mousedown', function (e) {
    var t = e.target
    if (pop) {
      if (pop.contains(t)) return
      var again = t === anchor
      close()
      if (again) { e.preventDefault(); return } // 再点同一下拉 = 收起（toggle）
    }
    if (!usable(t)) return
    e.preventDefault() // 拦下原生弹层
    open(t)
  }, true)

  document.addEventListener('keydown', function (e) {
    if (pop) {
      var k = e.key
      if (k === 'Escape' || k === 'Tab') { close(true); e.preventDefault(); return }
      if (k === 'ArrowDown') { setAct(Math.min(act + 1, opts.length - 1)); e.preventDefault(); return }
      if (k === 'ArrowUp') { setAct(Math.max(act - 1, 0)); e.preventDefault(); return }
      if (k === 'Home') { setAct(0); e.preventDefault(); return }
      if (k === 'End') { setAct(opts.length - 1); e.preventDefault(); return }
      if (k === 'Enter' || k === ' ') { pick(act); e.preventDefault(); return }
      return
    }
    var t = e.target
    if (!usable(t)) return
    if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'NumpadEnter') {
      e.preventDefault()
      open(t)
    }
  }, true)

  // 弹层随文档滚动/缩放即收起（弹层自身滚动除外）
  document.addEventListener('scroll', function (e) {
    if (pop && e.target !== pop) close()
  }, true)
  window.addEventListener('resize', function () { if (pop) close() })
})()
