<script setup>
import { ref, computed, watch, onUnmounted } from 'vue'
import { state } from '../store'
import { t } from '../i18n'
import { fmtNum } from '../utils'

// 推理值含义：用简化表格展示（data-tip-html 由即时提示模块渲染）
const reasoningTipHtml = computed(() => {
  const rows = t('logs.reasoningTip') || []
  const body = (Array.isArray(rows) ? rows : []).map(([k, v]) => `<tr><td>${k}</td><td>${v}</td></tr>`).join('')
  return `<table class="ui-tip-table"><tbody>${body}</tbody></table>`
})
const flashKeys = ref(new Set())
let prevLogKeys = new Set()
// 闪烁清理定时器句柄（组件卸载时清理）
let flashTimer = null

// 内容稳定 key：轮询会替换成全新日志对象，不能依赖对象身份，
// 同内容第 N 次出现时追加序号消歧（对同一数组两次调用结果一致）
function contentKey(e) {
  return `${e.time || ''}|${e.model || e.path || ''}|${e.status || 0}|${e.ms || 0}`
}
function logKeys(arr) {
  const counts = new Map()
  return arr.map(e => {
    const base = contentKey(e)
    const n = (counts.get(base) || 0) + 1
    counts.set(base, n)
    return n === 1 ? base : `${base}#${n}`
  })
}

const logs = computed(() => {
  const arr = state.logs || []
  const keys = logKeys(arr)
  return arr.map((e, i) => {
    const tm = e.time || ''
    const mm = tm.slice(11, 19)
    const status = Number(e.status) || 0
    const cls = status >= 400 ? 'err' : (status >= 200 && status < 300 ? 'ok' : '')
    const name = e.model || e.path || '-'
    const ms = Number(e.ms) || 0
    // 耗时分档配色：>=5s 红、>=2s 黄、其余常规
    const msCls = ms >= 5000 ? 'slow' : ms >= 2000 ? 'warn' : ''
    const u = e.usage || {}
    const prompt = Number(u.prompt_tokens) || 0
    const completion = Number(u.completion_tokens) || 0
    // 推理列两行：上行为客户端原始值（reasoning），带 "*" 后缀 = 客户端未携带、
    // 由路由器按全局推理深度档位注入（前端把后缀拆出来单独染色）；下行为按推理
    // 映射表转换后真正发往上游的字段值（reasoningOut）
    const rzRaw = String(e.reasoning || '')
    const rzInj = rzRaw.endsWith('*')
    const rzVal = rzInj ? rzRaw.slice(0, -1) : rzRaw
    // 原始推理也只展示值（后端可能给「字段=值」）；注入标记 * 照常保留
    const rzText = rzVal.includes('=') ? rzVal.slice(rzVal.lastIndexOf('=') + 1) : rzVal
    // 上游推理只展示值：后端给的是「字段=值」文本，这里截取最后一个 = 之后的值
    const rzOutRaw = String(e.reasoningOut || '')
    const rzOut = rzOutRaw.includes('=') ? rzOutRaw.slice(rzOutRaw.lastIndexOf('=') + 1) : rzOutRaw
    // 秒为单位：<10s 保留一位小数（整数省小数点），>=10s 取整，避免长文本撑破固定列宽
    const s = ms / 1000
    const msText = ms > 0 ? (s >= 10 ? String(Math.round(s)) : s.toFixed(1).replace(/\.0$/, '')) + 's' : '—'
    const key = keys[i]
    return { mm, status, cls, name, provider: e.provider || '', rzText, rzInj, rzOut, msText, msCls, prompt, completion, hasTok: prompt > 0 || completion > 0, key }
  })
})

// 检测新日志条目，标记闪烁（首次加载仅建立基线，不闪烁；只有之后新增的记录才有背景色）
let baselineReady = false
let pendingClear = new Set() // 自上次定时器起所有待清除的闪烁键（跨批次合并）
watch(() => state.logs, (newLogs) => {
  if (!newLogs || !newLogs.length) {
    prevLogKeys = new Set()
    baselineReady = false
    return
  }
  const curKeys = new Set(logKeys(newLogs))
  if (!baselineReady) {
    prevLogKeys = curKeys
    baselineReady = true
    return
  }
  const added = []
  for (const k of curKeys) {
    if (!prevLogKeys.has(k)) added.push(k)
  }
  prevLogKeys = curKeys
  if (added.length === 0) return
  const next = new Set(flashKeys.value)
  for (const k of added) {
    next.add(k)
    pendingClear.add(k)
  }
  flashKeys.value = next
  // 新一轮日志到达时只重置定时器，不清空 pendingClear：
  // 否则旧批的键永远不会被清除，行高亮残留到永远
  clearTimeout(flashTimer)
  flashTimer = setTimeout(() => {
    const clearSet = pendingClear
    pendingClear = new Set()
    flashKeys.value = new Set([...flashKeys.value].filter(k => !clearSet.has(k)))
  }, 2000)
}, { immediate: true })

onUnmounted(() => clearTimeout(flashTimer))
</script>

<template>
  <div class="recent-card logs-card">
    <div class="card-head">{{ t('logs.title') }}</div>
    <div class="logs-scroll">
      <table v-if="logs.length" class="logs-table">
        <colgroup>
          <col class="c-time"><col class="c-model"><col class="c-prov"><col class="c-rz"><col class="c-rzo"><col class="c-in"><col class="c-out"><col class="c-ms"><col class="c-st">
        </colgroup>
        <thead>
          <tr>
            <th>{{ t('logs.colTime') }}</th><th>{{ t('logs.colModel') }}</th><th>{{ t('logs.colProvider') }}</th><th>{{ t('logs.colReasoning') }}</th><th>{{ t('logs.colReasoningOut') }}</th><th>{{ t('logs.colIn') }}</th><th>{{ t('logs.colOut') }}</th><th>{{ t('logs.colMs') }}</th><th>{{ t('logs.colStatus') }}</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="r in logs" :key="r.key">
            <tr
              class="log-row"
              :class="{
                'row-flash': flashKeys.has(r.key),
                'row-flash-ok': flashKeys.has(r.key) && r.cls === 'ok',
                'row-flash-err': flashKeys.has(r.key) && r.cls === 'err',
              }"
            >
              <td class="lr-time">{{ r.mm }}</td>
              <td class="lr-model" :title="r.name">{{ r.name }}</td>
              <td class="lr-prov" :title="r.provider">{{ r.provider || '—' }}</td>
              <td class="lr-rz" :class="{ 'rz-inj': r.rzInj, 'rz-none': !r.rzText }" :title="t('logs.reasoningTipTitle')" :data-tip-html="reasoningTipHtml">
                <template v-if="r.rzText">{{ r.rzText }}<i v-if="r.rzInj">*</i></template>
                <template v-else>—</template>
              </td>
              <td class="lr-rzo" :class="{ 'rz-none': !r.rzOut }" :title="r.rzOut || t('logs.rzNone')">{{ r.rzOut || t('logs.rzNone') }}</td>
              <td class="lr-tok" :class="{ 'tok-empty': !r.hasTok }">
                <template v-if="r.hasTok">
                  <span class="tok-in" :title="t('logs.inTip')">↑{{ fmtNum(r.prompt) }}</span>
                </template>
                <template v-else>—</template>
              </td>
              <td class="lr-tok" :class="{ 'tok-empty': !r.hasTok }">
                <template v-if="r.hasTok">
                  <span class="tok-out" :title="t('logs.outTip')">↓{{ fmtNum(r.completion) }}</span>
                </template>
                <template v-else>—</template>
              </td>
              <td class="lr-ms" :class="r.msCls">{{ r.msText }}</td>
              <td class="lr-status"><span class="st-pill" :class="r.cls">{{ r.status }}</span></td>
            </tr>
          </template>
        </tbody>
      </table>
      <div v-else class="recent-empty">{{ t('ov.recentEmpty') }}</div>
    </div>
  </div>
</template>
