<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { ui } from '../store'
import { t } from '../i18n'
import api from '../api'
import { copyText } from '../utils'

const models = ref([])
const loading = ref(false)
const error = ref(false)
const searchBuf = ref('')
const hitKey = ref(null)
let searchTimer = null
let hitTimer = null

const groups = computed(() => {
  const g = new Map()
  for (const m of models.value) {
    const key = m.slice(0, 3).toLowerCase()
    if (!g.has(key)) g.set(key, [])
    g.get(key).push(m)
  }
  return [...g.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([key, items]) => ({
    key,
    items,
    head: items[0].slice(0, 3).toUpperCase(),
  }))
})

// 请求序号守卫：过期响应丢弃，loading 只由最新请求控制
let fetchSeq = 0
async function fetchModels() {
  const p = ui.modelsDialog.provider
  if (!p) return
  const seq = ++fetchSeq
  loading.value = true
  error.value = false
  models.value = []
  try {
    const r = await api.FetchModelsList(p)
    if (seq !== fetchSeq) return
    models.value = r && r.ok && Array.isArray(r.models) ? r.models : []
  } catch (e) {
    if (seq !== fetchSeq) return
    error.value = true
  }
  if (seq === fetchSeq) loading.value = false
}

watch(() => ui.modelsDialog.open, (open) => {
  if (open) {
    searchBuf.value = ''
    hitKey.value = null
    fetchModels()
  }
})

function copyModel(m, e) {
  copyText(m, e.currentTarget)
}

// 键盘字母定位模型分组（不区分大小写）
function onKeydown(e) {
  if (!ui.modelsDialog.open) return
  if (e.key === 'Escape') { searchBuf.value = ''; return }
  if (e.key.length !== 1 || !/[a-zA-Z0-9]/.test(e.key)) return
  searchBuf.value += e.key.toLowerCase()
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { searchBuf.value = '' }, 900)
  const q = searchBuf.value.slice(0, 3)
  for (const g of document.querySelectorAll('.models-group')) {
    if (g.dataset.prefix && g.dataset.prefix.startsWith(q)) {
      hitKey.value = g.dataset.prefix
      if (hitTimer) clearTimeout(hitTimer)
      hitTimer = setTimeout(() => { hitKey.value = null }, 700)
      g.scrollIntoView({ block: 'start', behavior: 'smooth' })
      break
    }
  }
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
  clearTimeout(searchTimer)
  clearTimeout(hitTimer)
})
</script>

<template>
  <div class="dialog-overlay" id="models-dialog" :class="{ open: ui.modelsDialog.open }">
    <div class="dialog" style="display:flex;flex-direction:column">
      <div class="models-body">
        <span v-if="loading" style="color:var(--t3)">{{ t('prov.fetching') }}</span>
        <span v-else-if="error" style="color:var(--t3)">{{ t('prov.fetchModelsFail') }}</span>
        <template v-else-if="models.length">
          <div class="models-hint">{{ t('models.hint') }}</div>
          <div
            v-for="g in groups"
            :key="g.key"
            class="models-group"
            :data-prefix="g.key"
            :class="{ 'mg-hit': hitKey === g.key }"
          >
            <div class="models-group-head">{{ g.head }}</div>
            <div v-for="m in g.items" :key="m" class="model-item">
              <span class="model-name">{{ m }}</span>
              <button class="model-copy" :title="t('prov.copyModel')" @click="copyModel(m, $event)">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
              </button>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="models-empty">{{ t('prov.noModels') }}</div>
          <button class="btn btn-sm" style="margin:0 auto;display:block" @click="fetchModels">{{ t('prov.test') }}</button>
        </template>
      </div>
      <div class="dialog-foot">
        <span></span>
        <div class="right">
          <button class="btn btn-primary btn-sm" @click="ui.modelsDialog.open = false">{{ t('dlg.confirm') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
