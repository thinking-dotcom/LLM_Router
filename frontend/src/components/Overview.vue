<script setup>
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import { state, refresh } from '../store'
import { t, i18nState } from '../i18n'
import { toast } from '../toast'
import api from '../api'
import {
  fmtNum, todayKey, endpointInfo, copyText,
} from '../utils'

// 左栏宽度：按内容最大宽度自适应一次后固定（不再随 token 数字增长反复变宽）
const wrapEl = ref(null)
let paneFixed = false
function fixPaneWidth() {
  if (paneFixed || !wrapEl.value) return
  const pane = wrapEl.value.closest('.left-pane')
  if (!pane) return
  // 临时以 max-content 测量内容理想宽度，再还原并写死到左栏
  const el = wrapEl.value
  const prev = el.style.width
  el.style.width = 'max-content'
  const w = Math.ceil(el.getBoundingClientRect().width)
  el.style.width = prev
  if (w > 0) {
    // 加上 panel-body 左右内边距(22px)与边框(2px)，并留出更宽松的余量让概览更舒展；
    // CSS max-width 上限仍然生效
    pane.style.width = Math.max(400, w + 72) + 'px'
    paneFixed = true
  }
}
watch(() => !!state.stats, (ok) => {
  if (!ok) return
  nextTick(() => setTimeout(fixPaneWidth, 60))
}, { immediate: true })
function remeasure() {
  paneFixed = false
  const pane = wrapEl.value?.closest('.left-pane')
  if (pane) pane.style.width = ''
  nextTick(() => setTimeout(fixPaneWidth, 60))
}
// 切换语言后标签长度变化：清除固定宽度并按新文案重新测量
watch(() => i18nState.lang, remeasure)
// 字体异步加载完成后度量会变化，需重测一次
if (document.fonts?.ready) document.fonts.ready.then(() => { if (!paneFixed) return; remeasure() })
onMounted(() => setTimeout(fixPaneWidth, 300))

const EP_FORMATS = [
  { key: 'openai', label: 'ep.fmtOpenai' },
  { key: 'anthropic', label: 'Anthropic' },
  { key: 'gemini', label: 'Google' },
]
const epFmt = ref('openai')

const port = computed(() => state.config?.port || 3000)
const host = computed(() => state.config?.host || '127.0.0.1')
const baseURL = computed(() => state.localBase || `http://${host.value}:${port.value}`)
const running = computed(() => state.running)
const serverError = computed(() => state.serverError || '')

// 今日统计原始值（用于 title 提示）
const todayRaw = computed(() => {
  const stats = state.stats
  let todayTokens = 0, todayCount = 0, modelCount = 0
  const today = todayKey()
  const todayDay = stats?.days?.[today]
  if (todayDay) {
    todayCount = todayDay.count || 0
    todayTokens = (todayDay.prompt || 0) + (todayDay.completion || 0)
  }
  for (const [, data] of Object.entries(stats?.perModel || {})) {
    const dd = data.days?.[today]
    if (dd && dd.count > 0) modelCount++
  }
  return [todayCount, todayTokens, modelCount]
})

// 今日 token 拆分：所有被请求模型的 总输出（completion）/ 总输入（prompt）
const todaySplit = computed(() => {
  const today = todayKey()
  let cin = 0, cout = 0
  for (const [, data] of Object.entries(state.stats?.perModel || {})) {
    const dd = data.days?.[today]
    if (!dd || dd.count <= 0) continue
    cout += dd.completion || 0
    cin += dd.prompt || 0
  }
  return { cin, cout }
})

const todayValues = computed(() => {
  if (!state.stats) return ['…', '…', '…']
  return todayRaw.value.map(fmtNum)
})

const endpoint = computed(() => endpointInfo(baseURL.value, epFmt.value))

// 启动 / 停止
const starting = ref(false)
const stopping = ref(false)
async function startServer() {
  if (starting.value) return
  starting.value = true
  try {
    const r = await api.StartServer()
    if (r && r.ok === false) toast(t('server.startFail', { msg: r.error || '?' }))
  } catch (e) {
    toast(t('server.startFail', { msg: e?.message || '?' }))
    console.error(e)
  }
  starting.value = false
  await refresh(true)
}
async function stopServer() {
  if (stopping.value) return
  stopping.value = true
  try { await api.StopServer() } catch (e) { console.error(e) }
  toast(t('server.stoppedToast'))
  stopping.value = false
  await refresh(true)
}

// 复制接入地址
function copyEndpoint(text, e) {
  copyText(text, e.currentTarget)
}

// 打开管理后台
async function openAdmin(e) {
  const b = e.currentTarget
  if (b.classList.contains('disabled')) return
  try {
    const r = await api.OpenURL(baseURL.value)
    if (!r || r.ok === false) window.runtime?.BrowserOpenURL(baseURL.value)
  } catch (err) {
    window.runtime?.BrowserOpenURL(baseURL.value)
  }
}
</script>

<template>
  <div ref="wrapEl" class="overview-wrap">
    <!-- 状态条 -->
    <div class="overview-status" :class="running ? 'on' : 'off'">
      <div class="os-ring">
        <svg v-if="running" viewBox="0 0 38 38" fill="none" aria-hidden="true">
          <circle cx="19" cy="19" r="16" stroke="currentColor" stroke-width="2"/>
          <path d="M12 19.5l4.6 4.6L26 14.6" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
        <svg v-else viewBox="0 0 38 38" fill="none" aria-hidden="true">
          <circle cx="19" cy="19" r="16" stroke="currentColor" stroke-width="2"/>
          <path d="M14 14l10 10M24 14L14 24" stroke="currentColor" stroke-width="2.6" stroke-linecap="round"/>
        </svg>
      </div>
      <div class="os-info">
        <div class="os-label">{{ t('server.status') }}</div>
        <div class="os-value">{{ running ? t('server.running') : t('server.stopped') }}</div>
        <div v-if="serverError" class="os-error" :title="serverError">{{ serverError }}</div>
      </div>
      <button
        v-if="running"
        class="btn btn-xs os-btn os-stop"
        :disabled="stopping"
        :title="t('server.stopTip')"
        @click="stopServer"
      >{{ stopping ? t('server.stopping') : t('server.stop') }}</button>
      <button
        v-else
        class="btn btn-xs os-btn os-start"
        :disabled="starting"
        :title="t('server.startTip')"
        @click="startServer"
      >{{ starting ? t('server.starting') : t('server.start') }}</button>
    </div>

    <!-- 统计条：今日请求 / 总输出 / 总输入 / 在线模型 合并一行 -->
    <div class="stats-bar">
      <div class="sb-item" :title="t('stat.todayReqTip', { n: todayRaw[0] })">
        <span class="sb-value">{{ todayValues[0] }}</span>
        <span class="sb-label">{{ t('stat.todayReq') }}</span>
      </div>
      <div class="sb-item" :title="t('stat.totalOutTip')">
        <span class="sb-value sv-out">{{ fmtNum(todaySplit.cout) }}</span>
        <span class="sb-label">{{ t('stat.totalOut') }}</span>
      </div>
      <div class="sb-item" :title="t('stat.totalInTip')">
        <span class="sb-value sv-in">{{ fmtNum(todaySplit.cin) }}</span>
        <span class="sb-label">{{ t('stat.totalIn') }}</span>
      </div>
      <div class="sb-item" :title="t('stat.inUseTip')">
        <span class="sb-value">{{ todayValues[2] }}</span>
        <span class="sb-label">{{ t('stat.inUse') }}</span>
      </div>
    </div>

    <!-- 接入地址 -->
    <div class="endpoint-card">
      <div class="ep-head">
        <button
          class="btn btn-xs btn-ghost open-btn"
          :class="{ disabled: !running }"
          :title="t('ep.adminTip')"
          style="color:var(--accent)"
          @click="openAdmin"
        >{{ t('ep.admin') }}</button>
      </div>
      <div class="ep-body">
        <div class="ep-row">
          <div class="ep-label">{{ t('ep.sdkLabel') }}</div>
          <div class="ep-url"><code>{{ endpoint.sdk }}</code>
            <button class="btn btn-xs btn-ghost copy-btn" :title="t('ep.copySdkTip')" @click="copyEndpoint(endpoint.sdk, $event)">{{ t('ep.copy') }}</button></div>
        </div>
      </div>
      <!-- 协议切换：点击切换（epFmt 只决定展示哪套端点路径，无副作用）；
           不做滑入切换，避免光标扫过时误换 -->
      <div class="ep-tabs">
        <button
          v-for="f in EP_FORMATS"
          :key="f.key"
          class="ep-tab"
          :class="{ active: epFmt === f.key }"
          @click="epFmt = f.key"
        >{{ t(f.label) }}</button>
      </div>
      <div class="ep-body">
        <div v-for="p in endpoint.paths" :key="p.p" class="ep-row">
          <div class="ep-label">{{ t('ep.pathLabel') }}<template v-if="p.tag"> · {{ t(p.tag) }}</template></div>
          <div class="ep-url"><code>{{ p.p }}</code>
            <button class="btn btn-xs btn-ghost copy-btn" :title="t('ep.copyPathTip')" @click="copyEndpoint(p.p, $event)">{{ t('ep.copy') }}</button></div>
        </div>
      </div>
    </div>
  </div>
</template>
