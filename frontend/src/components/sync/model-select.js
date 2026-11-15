// 公共模型下拉：可搜索 + 首字母分组 + 字母键定位。
// pi 目标时并入 pi 实时拉取列表（piBaseModels）；
// codebuddy 目标时并入 models.json 端点模型行 id（cbModelIds）——CLI 可选模型即该清单；
// deepcode 目标时并入 CLI 内置模型 ID（DEEPCODE_BUILTIN_MODELS）——CLI 不限制模型名，
// 内置 ID 只是它的默认菜单，路由器模型池同样可选。
import { ref } from 'vue'
import { cfgTarget, modelOptions } from './store'
import { piBaseModels, piM, piSettings } from './clients/pi'
import { cbModelsObj } from './clients/codebuddy'
import { DEEPCODE_BUILTIN_MODELS } from './clients/deepcode'

export const modelSelOpen = ref(-1)
export const cfgModelOpen = ref(false)
// codebuddy：models.json models[] 各行 id（面板 ① 卡维护的端点模型）
export function cbModelIds() {
  const rows = cbModelsObj.value?.models
  if (!Array.isArray(rows)) return []
  const ids = []
  for (const r of rows) {
    const id = String((r && r.id) || '').trim()
    if (id) ids.push(id)
  }
  return ids
}
export function modelGroupsByLetter(search) {
  const s = String(search || '').toLowerCase().trim()
  const tid = cfgTarget.value?.id
  let pool
  if (tid === 'pi') pool = [...new Set([...piBaseModels.value, ...modelOptions.value])]
  else if (tid === 'codebuddy') pool = [...new Set([...cbModelIds(), ...modelOptions.value])]
  else if (tid === 'deepcode') pool = [...new Set([...DEEPCODE_BUILTIN_MODELS, ...modelOptions.value])]
  else pool = modelOptions.value
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