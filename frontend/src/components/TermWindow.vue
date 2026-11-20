<script setup>
// 内嵌终端窗口（--term 实例，每窗口一个会话）：xterm.js 渲染 winpty 会话输出，
// 键入经 TermWrite 回传。输出/退出事件都带会话号 sid——「重新启动」换新号，
// 旧会话的尾部事件直接丢弃，避免旧退出事件误盖新会话状态。
import { ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
import { t } from '../i18n'
import api from '../api'

const state = ref('starting') // starting | live | exit | fail
const failMsg = ref('')
const exitCode = ref(0)
const title = ref('')
const wrapEl = ref(null)
const termEl = ref(null)

let term = null
let fit = null
let ro = null
let mySid = 0 // TermStart 成功后由后端事件对齐；重启换号
let resizeTimer = null
let fitRetryTimer = null
let lastResize = { cols: 0, rows: 0 } // 去重：winpty 对同尺寸 set_size 也会触发整屏重发（TUI 重画叠字）

// base64 ↔ 字节（事件通道按 base64 走，绕开控制字符/编码转义问题）
const enc = new TextEncoder()
function toB64(s) {
  const b = enc.encode(s)
  let bin = ''
  for (let i = 0; i < b.length; i++) bin += String.fromCharCode(b[i])
  return btoa(bin)
}

function currentSize() {
  if (!term) return [0, 0]
  return [term.cols, term.rows]
}

async function start() {
  state.value = 'starting'
  failMsg.value = ''
  if (term) term.reset()
  // 等字体就绪再测量，避免首测尺寸与字体加载后不一致引发额外 resize（每次
  // resize 都会令 winpty 整屏重发、TUI 重画一遍造成内容叠层）
  try { await document.fonts.ready } catch (e) {}
  await nextTick()
  tryFit()
  const [cols, rows] = currentSize()
  try {
    const r = await api.TermStart(cols, rows)
    if (r && r.ok) {
      // 事件过滤会话号必须与后端对齐，否则输出会被整体丢弃（白屏）
      mySid = typeof r.sid === 'number' ? r.sid : mySid
      lastResize = { cols, rows }
      title.value = r.title || title.value
      state.value = 'live'
    } else {
      failMsg.value = (r && r.error) || ''
      state.value = 'fail'
    }
  } catch (e) {
    failMsg.value = e?.message || String(e)
    state.value = 'fail'
  }
}

// 尺寸同步：容器变化 → fit 折算行列 → 通知会话 resize
function tryFit() {
  if (!term || !fit || !wrapEl.value) return
  try { fit.fit() } catch (e) { /* 容器尺寸为 0 时 fit 抛错，忽略等下次 */ }
}

function scheduleFit() {
  clearTimeout(resizeTimer)
  resizeTimer = setTimeout(() => {
    tryFit()
    sendResize()
  }, 120)
}

// 尺寸真正变化才通知会话（同尺寸的 winpty_set_size 也会整屏重发）
function sendResize() {
  if (state.value !== 'live' || !term) return
  const [cols, rows] = currentSize()
  if (cols > 0 && rows > 0 && (cols !== lastResize.cols || rows !== lastResize.rows)) {
    lastResize = { cols, rows }
    api.TermResize(cols, rows)
  }
}

// 复制/粘贴：Ctrl+Shift+C / Ctrl+Shift+V 与右键菜单习惯一致；
// WebView2 支持navigator.clipboard（wails 本源为安全上下文），失败则静默。
function keyHandler(ev) {
  const ctrlShift = ev.ctrlKey && ev.shiftKey && !ev.altKey && !ev.metaKey
  if (ev.type === 'keydown' && ctrlShift && (ev.code === 'KeyC' || ev.code === 'KeyV')) {
    ev.preventDefault()
    const sel = term && term.getSelection()
    if (ev.code === 'KeyC') {
      if (sel) navigator.clipboard?.writeText(sel).catch(() => {})
      return false
    }
    navigator.clipboard?.readText().then((txt) => {
      if (txt && term) term.paste(txt)
    }).catch(() => {})
    return false
  }
  return undefined
}

onMounted(async () => {
  term = new Terminal({
    fontSize: 14,
    fontFamily: '"Cascadia Mono", Consolas, "Courier New", monospace',
    cursorBlink: true,
    scrollback: 5000,
    // winpty 兼容：其重发/擦除序列与规范 VT 有出入（行擦除语义弱），开启后
    // 由 xterm 按 winpty 规则解释，否则 resize 重画内容会叠层
    windowsMode: true,
    theme: {
      background: '#000000',
      foreground: '#c9d1d9',
      cursor: '#58a6ff',
      selectionBackground: '#264f78',
    },
  })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.attachCustomKeyEventHandler(keyHandler)
  term.onData((d) => {
    if (state.value === 'live') api.TermWrite(toB64(d))
  })
  term.open(termEl.value)
  term.focus()

  // 事件先订阅再启动，避免早期输出丢失
  EventsOn('term:out', (sid, data) => {
    if (sid !== mySid || !term) return
    term.write(Uint8Array.from(atob(data), (c) => c.charCodeAt(0)))
  })
  EventsOn('term:exit', (sid, code) => {
    if (sid !== mySid || state.value === 'fail') return
    exitCode.value = code
    state.value = 'exit'
  })

  ro = new ResizeObserver(scheduleFit)
  ro.observe(wrapEl.value)
  // 字体加载可能晚于首次 fit，稍后校正一次
  fitRetryTimer = setTimeout(() => { tryFit(); scheduleFit() }, 400)

  await start()
})

onBeforeUnmount(() => {
  EventsOff('term:out', 'term:exit')
  clearTimeout(resizeTimer)
  clearTimeout(fitRetryTimer)
  if (ro) ro.disconnect()
  api.TermStop()
  if (term) term.dispose()
  term = null
})

function restart() { start() }
function closeWin() { window.close() }
</script>

<template>
  <div class="termwin">
    <header class="tw-head">
      <span class="tw-dot" :class="state"></span>
      <span class="tw-title" :title="title">{{ title || t('term.starting') }}</span>
      <span class="tw-status" v-if="state === 'live'">{{ t('term.live') }}</span>
      <span class="tw-status warn" v-else-if="state === 'exit'">
        {{ t('term.exit') }} · {{ t('term.exitCode', { n: exitCode }) }}
      </span>
      <button v-if="state === 'exit' || state === 'fail'" type="button" class="tw-btn" @click="restart">
        {{ t('term.restart') }}
      </button>
      <button v-if="state === 'exit' || state === 'fail'" type="button" class="tw-btn" @click="closeWin">
        {{ t('term.close') }}
      </button>
    </header>

    <div ref="wrapEl" class="tw-wrap">
      <div ref="termEl" class="tw-term"></div>
      <div v-if="state === 'fail'" class="tw-mask">
        <div class="tw-mask-box">
          <div class="tw-mask-title">{{ t('term.fail') }}</div>
          <div class="tw-mask-err">{{ failMsg }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.termwin{position:fixed;inset:0;display:flex;flex-direction:column;background:#000000;color:#c9d1d9;font-size:13px}
.tw-head{flex:none;display:flex;align-items:center;gap:8px;padding:6px 12px;border-bottom:1px solid #242424;background:#0a0a0a}
.tw-dot{flex:none;width:8px;height:8px;border-radius:50%;background:#7d8590}
.tw-dot.live{background:#3fb950}
.tw-dot.exit,.tw-dot.fail{background:#f85149}
.tw-dot.starting{background:#d29922}
.tw-title{min-width:0;flex:1;font-weight:600;color:#e6edf3;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.tw-status{flex:none;font-size:12px;color:#7d8590}
.tw-status.warn{color:#f0883e}
.tw-btn{
  flex:none;padding:3px 12px;cursor:pointer;font-size:12px;
  border:1px solid #242424;border-radius:6px;background:#181818;color:#c9d1d9;
  transition:border-color .15s,color .15s;
}
.tw-btn:hover{border-color:#58a6ff;color:#e6edf3}
.tw-wrap{flex:1;min-height:0;padding:4px 6px;box-sizing:border-box;position:relative}
.tw-term{width:100%;height:100%}
.tw-mask{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;background:rgba(0,0,0,.75)}
.tw-mask-box{max-width:80%;padding:18px 24px;border:1px solid #242424;border-radius:10px;background:#0a0a0a;text-align:center}
.tw-mask-title{font-weight:600;color:#f85149;margin-bottom:8px}
.tw-mask-err{color:#7d8590;line-height:1.6;word-break:break-word}
</style>
