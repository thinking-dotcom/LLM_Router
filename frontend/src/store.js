import { reactive, watch } from 'vue'
import api from './api'

export const ACTIVE_WINDOW_SECONDS = 2

function loadTheme() {
  try { return localStorage.getItem('theme') || 'dark' } catch (e) { return 'dark' }
}

// 界面字体大小档位（整体等比缩放，避免控件尺寸不变导致文字溢出）
export const FONT_SCALES = [0.9, 1, 1.1, 1.2]
function loadFontScale() {
  try {
    const v = Number(localStorage.getItem('fontScale'))
    return FONT_SCALES.includes(v) ? v : 1
  } catch (e) { return 1 }
}

// 全局共享状态
export const state = reactive({
  config: null,        // 配置对象（含 providers）
  running: false,      // 服务运行状态
  serverError: '',     // 启动错误
  localBase: '',       // 本地访问地址（后端算好，正确处理 0.0.0.0 等通配地址）
  stats: null,         // GetStats 统计结果
  activeModels: [],    // 活跃模型（光波动画）
  logs: [],            // 最近请求日志
})

// UI 状态（tab、主题、弹窗开关）
export const ui = reactive({
  theme: loadTheme(),
  fontScale: loadFontScale(),
  tab: 'overview',
  providerDialog: { open: false, provider: null },
  deleteDialog: { open: false, provider: null },
  modelsDialog: { open: false, provider: null },
  clearLogsDialog: { open: false }, // 清空日志确认框
  settingsPanel: { open: false }, // 路由设置面板（概览标题齿轮图标打开）
  menu: { open: false, provider: null, rect: null }, // 服务商卡片 ⋯ 菜单
})

// 服务商状态（跨轮询保留，不随 config 刷新重置）
export const provStatus = reactive({}) // id -> 'ok' | 'fail' | 'testing'
export const provModels = reactive({}) // id -> { count, models } | { failed: true }（count/models=服务器返回的模型列表原样）

// 主题：切换时写入 <html data-theme>
export function initTheme() {
  watch(
    () => ui.theme,
    v => { document.documentElement.dataset.theme = v === 'light' ? 'light' : 'dark' },
    { immediate: true }
  )
  // 字体大小：CSS zoom 对根元素整体缩放，文字与控件同步放大，布局不破
  watch(
    () => ui.fontScale,
    v => { document.documentElement.style.zoom = String(v) },
    { immediate: true }
  )
}

export function applyTheme(theme) {
  ui.theme = theme === 'light' ? 'light' : 'dark'
  try { localStorage.setItem('theme', ui.theme) } catch (e) { }
}

export function applyFontScale(v) {
  if (!FONT_SCALES.includes(v) || ui.fontScale === v) return
  ui.fontScale = v
  try { localStorage.setItem('fontScale', String(v)) } catch (e) { }
}

// 拉取运行状态 + 配置（15s 轮询与主动刷新共用）
// 进行中时复用同一个 Promise，避免并发重复请求与旧快照乱序覆盖新状态；
// force=true（保存后主动刷新）时跳过复用，确保拿到最新配置而非在途旧快照
let refreshInflight = null
let refreshSeq = 0
export async function refresh(force) {
  if (refreshInflight && !force) return refreshInflight
  const seq = ++refreshSeq
  const task = (async () => {
    if (!api.GetStatus || !api.GetConfig) return
    try {
      const [s, c] = await Promise.all([api.GetStatus(), api.GetConfig()])
      // force 刷新会另起请求；旧响应晚到时不得覆盖较新状态
      if (seq !== refreshSeq) return
      state.running = s.running
      state.config = c
      state.serverError = s.error || ''
      state.localBase = s.localBase || ''
    } catch (e) { console.error(e) }
    refreshLogs()
  })()
  refreshInflight = task
  try { await task } finally { if (refreshInflight === task) refreshInflight = null }
}

// 最近请求日志（最新在前）；请求序号守卫：过期响应不覆盖较新的日志状态
let logsSeq = 0
export async function refreshLogs() {
  if (!api.GetLogs) return
  const seq = ++logsSeq
  try {
    const logs = await api.GetLogs()
    if (seq !== logsSeq) return
    if (Array.isArray(logs)) state.logs = logs
  } catch (e) { /* ignore */ }
}

// 用量统计（2s 轮询）；活跃模型由 refreshActive（1s）负责，避免双路重复请求
// 与 refreshLogs 同样带序号守卫：过期响应不覆盖较新的统计结果
let statsSeq = 0
export async function refreshStats() {
  if (!api.GetStats) return
  const seq = ++statsSeq
  try {
    const stats = await api.GetStats()
    if (seq !== statsSeq) return
    state.stats = stats
  } catch (e) { /* ignore */ }
}

// 活跃模型（1s 高频轮询，仅用于光波动画）
let activeSeq = 0
export async function refreshActive() {
  if (!api.GetActiveModels) return
  const seq = ++activeSeq
  try {
    const r = await api.GetActiveModels(ACTIVE_WINDOW_SECONDS)
    if (seq !== activeSeq) return
    state.activeModels = Array.isArray(r?.models) ? r.models : []
  } catch (e) { /* ignore */ }
}
