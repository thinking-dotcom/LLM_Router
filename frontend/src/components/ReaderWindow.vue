<script setup>
// 会话阅读窗口（--read 实例，每窗口一个会话）：只读渲染会话的对话内容，
// 用户/助手气泡区分；数据经 LoadReaderContent 一次性拉取（窗口关了即结束）。
import { ref, onMounted, nextTick } from 'vue'
import { t } from '../i18n'
import api from '../api'

const state = ref('loading') // loading | ready | empty | fail
const failMsg = ref('')
const title = ref('')
const msgs = ref([])
const bodyEl = ref(null)
const copied = ref(false) // 复制成功后的短暂反馈（图标变勾 + 绿色）
let copyTimer = null

onMounted(async () => {
  try {
    const r = await api.LoadReaderContent()
    if (r && r.ok) {
      title.value = r.title || ''
      msgs.value = r.messages || []
      state.value = msgs.value.length ? 'ready' : 'empty'
    } else {
      failMsg.value = (r && r.error) || ''
      state.value = 'fail'
    }
  } catch (e) {
    failMsg.value = e?.message || String(e)
    state.value = 'fail'
  }
  await nextTick()
  // 长会话：定位到最新消息（底部），阅读时向上回溯
  if (bodyEl.value) bodyEl.value.scrollTop = bodyEl.value.scrollHeight
})

function roleOf(m) {
  return m.role === 'user' ? 'user' : 'assistant'
}

// 复制用的纯文本：标题 + 每条消息的「[角色] + 正文」，用户/助手用本地化标签。
function buildPlainText() {
  const parts = []
  if (title.value) parts.push(title.value, '')
  for (const m of msgs.value) {
    const role = roleOf(m) === 'user' ? t('read.user') : t('read.assistant')
    parts.push('[' + role + ']', String(m.text || ''), '')
  }
  return parts.join('\n').trim()
}

// 右上角复制图标：整段会话写入剪贴板。优先 Clipboard API；老版 WebView 或
// 非安全上下文下回退到 textarea + execCommand，避免复制静默失败。
async function copyAll() {
  const text = buildPlainText()
  if (!text) return
  let ok = false
  try {
    await navigator.clipboard.writeText(text)
    ok = true
  } catch (e) {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.top = '-1000px'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    try { ok = document.execCommand('copy') } catch (e2) { ok = false }
    ta.remove()
  }
  if (!ok) return
  copied.value = true
  clearTimeout(copyTimer)
  copyTimer = setTimeout(() => { copied.value = false }, 1600)
}
</script>

<template>
  <div class="reader">
    <header class="rd-head">
      <span class="rd-title" :title="title">{{ title || t('read.title') }}</span>
      <span v-if="state === 'ready'" class="rd-count">{{ t('read.msgCount', { n: msgs.length }) }}</span>
      <!-- 右上角：一键复制整段会话内容 -->
      <button
        v-if="state === 'ready'"
        type="button"
        class="rd-copy" :class="{ ok: copied }"
        :title="copied ? t('read.copied') : t('read.copyAll')"
        :aria-label="copied ? t('read.copied') : t('read.copyAll')"
        @click="copyAll"
      >
        <svg v-if="!copied" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
          stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <rect x="9" y="9" width="12" height="12" rx="2" />
          <path d="M5 15V5a2 2 0 0 1 2-2h10" />
        </svg>
        <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
          stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <polyline points="20 6 9 17 4 12" />
        </svg>
      </button>
    </header>

    <div ref="bodyEl" class="rd-body">
      <div v-if="state === 'loading'" class="rd-tip">{{ t('read.loading') }}</div>

      <template v-else-if="state === 'ready'">
        <div v-for="(m, i) in msgs" :key="i" class="rd-msg" :class="roleOf(m)">
          <div class="rd-role">{{ t(roleOf(m) === 'user' ? 'read.user' : 'read.assistant') }}</div>
          <div class="rd-text">{{ m.text }}</div>
        </div>
      </template>

      <div v-else-if="state === 'empty'" class="rd-tip">{{ t('read.empty') }}</div>
      <div v-else class="rd-tip rd-fail">{{ t('read.fail') }}<br><span class="rd-err">{{ failMsg }}</span></div>
    </div>
  </div>
</template>

<style scoped>
.reader{position:fixed;inset:0;display:flex;flex-direction:column;background:#000000;color:#c9d1d9;font-size:13px}
.rd-head{flex:none;display:flex;align-items:center;gap:10px;padding:10px 14px;border-bottom:1px solid #242424;background:#0a0a0a}
.rd-title{flex:1;min-width:0;font-weight:600;color:#e6edf3;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.rd-count{flex:none;font-size:12px;color:#7d8590}
/* 右上角复制图标：默认低对比，悬停提亮，复制成功后短暂变绿打勾 */
.rd-copy{
  flex:none;display:inline-flex;align-items:center;justify-content:center;
  width:26px;height:26px;padding:0;cursor:pointer;
  border:1px solid #242424;border-radius:6px;background:#111111;color:#9aa4ad;
  transition:border-color .15s,color .15s,background .15s;
}
.rd-copy:hover{border-color:#58a6ff;color:#e6edf3;background:#1a1a1a}
.rd-copy:active{transform:scale(.96)}
.rd-copy.ok{border-color:#3fb950;color:#3fb950;background:rgba(63,185,80,.12)}
.rd-body{flex:1;overflow-y:auto;padding:16px 14px;box-sizing:border-box}
.rd-tip{margin-top:40px;text-align:center;color:#7d8590;line-height:1.8}
.rd-fail .rd-err{color:#f85149;font-size:12px}

.rd-msg{max-width:860px;margin:0 auto 14px;padding:10px 14px;border-radius:10px;border:1px solid #242424;background:#0a0a0a}
.rd-msg.user{background:#101a26;border-color:#243a55}
.rd-role{font-size:11px;font-weight:600;letter-spacing:.4px;color:#58a6ff;margin-bottom:6px}
.rd-msg.assistant .rd-role{color:#3fb950}
.rd-text{white-space:pre-wrap;word-break:break-word;line-height:1.65;color:#c9d1d9}
.rd-msg.user .rd-text{color:#e6edf3}

.rd-body::-webkit-scrollbar{width:8px}
.rd-body::-webkit-scrollbar-thumb{background:#242424;border-radius:4px}
</style>
