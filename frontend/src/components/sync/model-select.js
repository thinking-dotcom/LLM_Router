// 公共模型下拉：可搜索 + 首字母分组 + 字母键定位。
// pi 目标时并入 pi 实时拉取列表（piBaseModels），引用 pi 模块导出。
import { ref } from 'vue'
import { cfgTarget, modelOptions } from './store'
import { piBaseModels, piM, piSettings } from './pi'

export const modelSelOpen = ref(-1)
export const cfgModelOpen = ref(false)
export function modelGroupsByLetter(search) {
  const s = String(search || '').toLowerCase().trim()
  const pool = cfgTarget.value?.id === 'pi'
    ? [...new Set([...piBaseModels.value, ...modelOptions.value])]
    : modelOptions.value
  const list = pool.filter(m => !s || String(m).toLowerCase().includes(s)).slice().sort()
  const byLetter = {}
  for (const m of list) {
    const L = (String(m).charAt(0) || '#').toUpperCase()
    ;(byLetter[L] = byLetter[L] || []).push(m)
  }
  return Object.keys(byLetter).sort().map(l => ({ letter: l, items: byLetter[l] }))
}
export function pickModel(i, m) {
  const row = piM.value.models[i]
  if (row) row.id = m
  modelSelOpen.value = -1
}
export function pickDefaultModel(m) {
  const cur = piSettings.value.defaultModel || ''
  const idx = cur.indexOf('/')
  piSettings.value.defaultModel = idx >= 0 ? cur.slice(0, idx + 1) + m : m
  cfgModelOpen.value = false
}