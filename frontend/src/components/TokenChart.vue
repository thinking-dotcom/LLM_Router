<script setup>
import { ref, computed } from 'vue'
import { state } from '../store'
import { t } from '../i18n'
import {
  fmtNum, fmtPct, chartColor, computeModelSegments,
} from '../utils'

const chartData = computed(() => {
  const { segments, total } = computeModelSegments(state.stats, state.activeModels)
  const rows = segments.map((s, i) => ({
    name: s.name,
    value: s.value,
    prompt: s.prompt || 0,
    completion: s.completion || 0,
    count: s.count || 0,
    pct: total > 0 ? s.value / total * 100 : 0,
    color: chartColor(i),
    active: state.activeModels.includes(s.name),
  }))
  return { rows, hasData: segments.length > 0 }
})

// 饼图扇形路径（SVG 弧线，从 12 点方向顺时针）
const CX = 100, CY = 100, R = 88
function polar(ang) {
  return [CX + R * Math.cos(ang), CY + R * Math.sin(ang)]
}
function slicePath(i) {
  const rows = chartData.value.rows
  // 占比达到整圆时弧线起终点重合会导致不渲染，与单模型占满一样走整圆画法
  if (rows.length === 1 || rows[i].pct >= 99.999) {
    return `M ${CX - R} ${CY} A ${R} ${R} 0 1 1 ${CX + R} ${CY} A ${R} ${R} 0 1 1 ${CX - R} ${CY} Z`
  }
  let start = -Math.PI / 2
  for (let j = 0; j < i; j++) start += rows[j].pct / 100 * Math.PI * 2
  const end = start + rows[i].pct / 100 * Math.PI * 2
  const [x1, y1] = polar(start)
  const [x2, y2] = polar(end)
  const large = end - start > Math.PI ? 1 : 0
  return `M ${CX} ${CY} L ${x1.toFixed(2)} ${y1.toFixed(2)} A ${R} ${R} 0 ${large} 1 ${x2.toFixed(2)} ${y2.toFixed(2)} Z`
}

const hoverModel = ref(null)
const tipPos = ref({ x: 0, y: 0 })
const tipRow = computed(() => chartData.value.rows.find(r => r.name === hoverModel.value) || null)

function onSliceEnter(row) { hoverModel.value = row.name }
function onSliceLeave() { hoverModel.value = null }
function onPieMove(e) {
  const rect = e.currentTarget.getBoundingClientRect()
  tipPos.value = { x: e.clientX - rect.left, y: e.clientY - rect.top }
}
</script>

<template>
  <div class="chart-card-new pie-page">
    <div class="card-head">{{ t('chart.title') }}</div>
    <div v-if="!state.stats" class="empty-chart">…</div>
    <div v-else-if="!chartData.hasData" class="empty-chart" v-html="t('chart.empty')"></div>
    <div v-else class="pie-split">
      <!-- 左：饼图（悬停扇形浮现 模型名 + 占比） -->
      <div class="pie-left" @mousemove="onPieMove" @mouseleave="onSliceLeave">
        <svg viewBox="0 0 200 200">
          <path
            v-for="(row, i) in chartData.rows"
            :key="row.name"
            class="pie-slice"
            :class="{ hot: hoverModel === row.name, dim: hoverModel && hoverModel !== row.name, 'slice-active': row.active }"
            :d="slicePath(i)"
            :fill="row.color"
            @mouseenter="onSliceEnter(row)"
          />
        </svg>
        <div v-if="tipRow" class="pie-tip" :style="{ left: tipPos.x + 'px', top: tipPos.y + 'px' }">
          <span class="pt-swatch" :style="{ background: tipRow.color }"></span>
          <span class="pt-name">{{ tipRow.name }}</span>
          <span class="pt-pct">{{ fmtPct(tipRow.pct) }}</span>
        </div>
      </div>
      <!-- 右：模型用量表格（与饼图联动高亮） -->
      <div class="pie-right">
        <table class="pie-table">
          <thead>
            <tr><th class="th-name">{{ t('chart.colModel') }}</th><th class="th-num" :title="t('chart.countTip')">{{ t('chart.colCount') }}</th><th class="th-num">{{ t('chart.colOut') }}</th><th class="th-num">{{ t('chart.colIn') }}</th></tr>
          </thead>
          <tbody>
            <tr
              v-for="row in chartData.rows"
              :key="row.name"
              :class="{ 'u-active': row.active, dim: hoverModel && hoverModel !== row.name, hot: hoverModel === row.name }"
              @mouseenter="hoverModel = row.name"
              @mouseleave="hoverModel = null"
            >
              <td class="td-name" :title="row.name">
                <span class="u-swatch" :style="{ background: row.color }"></span>{{ row.name }}
              </td>
              <td class="td-num">{{ row.count > 0 ? '×' + row.count : '—' }}</td>
              <td class="td-num tok-out">{{ fmtNum(row.completion) }}</td>
              <td class="td-num tok-in">{{ fmtNum(row.prompt) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
