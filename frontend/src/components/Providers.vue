<script setup>
import { ref, computed, reactive, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { state, ui, provStatus, provModels, refresh } from '../store'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'
import { copyText, placePopup } from '../utils'

const providers = computed(() => state.config?.providers || [])
const provErrors = reactive({}) // id -> 错误消息
const provCountLoading = reactive({}) // id -> 模型数加载中标记（防并发重入）

const menuEl = ref(null)

function addProvider() {
  ui.providerDialog = { open: true, provider: null }
}
function openProviderDialog(p) {
  ui.providerDialog = { open: true, provider: p }
}

// 状态点 class 与 title
function dotClass(p) {
  if (!p.enabled) return ''
  const st = provStatus[p.id]
  return st === 'ok' ? 'on' : st === 'fail' ? 'fail' : st === 'testing' ? 'testing' : ''
}
function dotTitle(p) {
  if (!p.enabled) return t('prov.disabled')
  const st = provStatus[p.id]
  if (st === 'ok') return t('prov.connectedTitle', { n: provModels[p.id]?.count ?? 0 })
  if (st === 'fail') return t('prov.unavailableTitle', { msg: provErrors[p.id] || '' })
  if (st === 'testing') return t('prov.testing')
  return t('prov.untested')
}

// 模型数：优先用 GetProviderModels（不发网络），无结果时对启用项再 FetchModels
function modelCountText(p) {
  const c = provModels[p.id]
  if (!c) return ''
  if (c.failed) return t('prov.fetchFail')
  if (c.count > 0) return t('prov.models', { n: c.count })
  return t('prov.noModels')
}

async function loadModelCount(p) {
  const id = p.id
  // 去重标记在首个 await 前置位，防止并发重入
  if (provModels[id] || provCountLoading[id]) return
  provCountLoading[id] = true
  // 归属校验：响应回来时该服务商已不存在则丢弃结果
  const owned = () => providers.value.some(x => x.id === id)
  try {
    const r = await api.GetProviderModels(p)
    if (!owned()) return
    const n = r && Array.isArray(r.models) ? r.models.length : 0
    if (n > 0) { provModels[id] = { count: n }; return }
    if (!p.enabled) { provModels[id] = { count: 0 }; return }
    const fr = await api.FetchModels(p)
    if (!owned()) return
    if (fr && fr.ok) provModels[id] = { count: fr.models ? fr.models.length : 0 }
    else provModels[id] = { failed: true }
  } catch (e) {
    if (owned()) provModels[id] = { failed: true }
  } finally {
    delete provCountLoading[id]
  }
}

// 通过拉取模型列表检测连通性
async function checkProvider(providerId) {
  if (provStatus[providerId] === 'testing') return
  provStatus[providerId] = 'testing'
  try {
    const provider = state.config?.providers?.find(p => p.id === providerId)
    if (!provider) throw new Error(t('prov.notFound'))
    const r = await api.FetchModels(provider)
    // 归属校验：响应回来时该服务商已被删除则丢弃结果
    if (!state.config?.providers?.some(p => p.id === providerId)) { delete provStatus[providerId]; return }
    provStatus[providerId] = r.ok ? 'ok' : 'fail'
    provModels[providerId] = { count: r.ok && r.models ? r.models.length : 0, failed: !r.ok }
    if (!r.ok) provErrors[providerId] = (r.error || '').slice(0, 50)
  } catch (e) {
    provStatus[providerId] = 'fail'
    provErrors[providerId] = (e.message || e).toString().slice(0, 50)
  }
}

// 自动检测所有启用的、尚未检测过的服务商
function autoCheckAll() {
  if (!api) return
  for (const p of providers.value) {
    if (!p.enabled) continue
    if (provStatus[p.id]) continue
    checkProvider(p.id)
  }
}

let autoCheckTimer = null
function scheduleAutoCheck() {
  clearTimeout(autoCheckTimer)
  autoCheckTimer = setTimeout(autoCheckAll, 500)
}

// 配置特征签名：enabled/baseUrl/type/models/aliases 变化时使卡片模型数/连通性缓存失效
const provSig = {} // id -> 签名
function provSignature(p) {
  const al = Object.entries(p.aliases || {}).sort((a, b) => a[0] < b[0] ? -1 : 1).map(([k, v]) => k + '=' + v).join(',')
  return `${p.id}|${p.enabled !== false}|${p.baseUrl || ''}|${p.type || ''}|${String((p.models || []).join(','))}|${al}`
}

// 服务商列表变化时加载模型数并安排自动检测
watch(providers, (list) => {
  for (const p of list) {
    const sig = provSignature(p)
    if (provSig[p.id] !== undefined && provSig[p.id] !== sig) {
      // 配置变了：旧模型数/失败标记/连通状态不再可信，清除后重新拉取
      delete provModels[p.id]
      delete provStatus[p.id]
    }
    provSig[p.id] = sig
  }
  list.forEach(p => loadModelCount(p))
  scheduleAutoCheck()
}, { immediate: true })

// 开关切换
async function toggleProvider(p, checked) {
  const cfg = JSON.parse(JSON.stringify(state.config || {}))
  const prov = cfg.providers.find(x => x.id === p.id)
  if (!prov) return
  prov.enabled = checked
  delete provStatus[p.id]
  delete provModels[p.id]
  delete provSig[p.id]
  try {
    const r = await api.SaveConfig(cfg)
    if (!r || !r.ok) toast(t('common.fail', { msg: r?.error || '' }))
    await refresh(true)
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}

// 卡片 ⋯ 菜单
function openMenu(p, event) {
  if (ui.menu.open && ui.menu.provider?.id === p.id) {
    ui.menu.open = false
    return
  }
  ui.menu.open = true
  ui.menu.provider = p
  ui.menu.rect = event.currentTarget.getBoundingClientRect()
}

watch(() => ui.menu.open, async (open) => {
  if (open) {
    await nextTick()
    if (menuEl.value && ui.menu.rect) placePopup(menuEl.value, ui.menu.rect)
  }
})

async function menuAction(action) {
  const p = ui.menu.provider
  ui.menu.open = false
  if (!p) return
  if (action === 'edit') openProviderDialog(p)
  else if (action === 'delete') ui.deleteDialog = { open: true, provider: p }
  else if (action === 'test') checkProvider(p.id)
  else if (action === 'viewModels') ui.modelsDialog = { open: true, provider: p }
  else if (action === 'ids') await copyModelIds(p)
}

async function copyModelIds(p) {
  try {
    const r = await api.FetchModelsList(p)
    const ids = ((r && r.models) || []).join(', ')
    if (!ids) toast(t('prov.noModelsToast'))
    else { copyText(ids, null, true); toast(t('prov.copiedModels', { n: r.models.length })) }
  } catch (e) { toast(t('prov.fetchModelsFail')) }
}

// 点击菜单外部关闭
function onDocClick(e) {
  if (!ui.menu.open) return
  if (menuEl.value && menuEl.value.contains(e.target)) return
  if (e.target.closest('.menu-btn')) return
  ui.menu.open = false
}

onMounted(() => document.addEventListener('click', onDocClick))
onUnmounted(() => {
  document.removeEventListener('click', onDocClick)
  clearTimeout(autoCheckTimer)
})

// 供父级面板标题栏调用（“添加服务商”按钮已上移到面板标题）
defineExpose({ addProvider })
</script>

<template>
  <div>
    <div
      v-if="!providers.length"
      style="text-align:center;color:var(--t3);padding:28px 0"
    >{{ t('prov.empty') }}<br><br>{{ t('prov.emptyTip') }}</div>

    <template v-else>
      <div class="prov-grid">
        <div v-for="p in providers" :key="p.id" class="prov-card">
          <div class="phead">
            <div style="display:flex;align-items:center;gap:6px">
              <span class="prov-dot" :class="dotClass(p)" :title="dotTitle(p)"></span>
              <span class="pname" :title="p.name">{{ p.name }}</span>
              <span class="ptype">{{ p.type }}</span>
            </div>
            <div style="display:flex;align-items:center;gap:6px">
              <label class="toggle">
                <input type="checkbox" :checked="p.enabled" @change="toggleProvider(p, $event.target.checked)">
                <span class="slider"></span>
              </label>
              <span class="menu-btn" :title="t('prov.more')" @click.stop="openMenu(p, $event)">⋯</span>
            </div>
          </div>
          <div class="pmeta">
            <span class="purl">{{ p.baseUrl || '' }}</span>
            <span class="pmcount">{{ modelCountText(p) }}</span>
          </div>
        </div>
      </div>
    </template>

    <!-- ⋯ 菜单 -->
    <div v-if="ui.menu.open" ref="menuEl" class="popup-menu" style="position:fixed;left:0;top:0">
      <div class="popup-item" @click="menuAction('test')">{{ t('prov.test') }}</div>
      <div class="popup-item" @click="menuAction('edit')">{{ t('prov.edit') }}</div>
      <div class="popup-item" @click="menuAction('viewModels')">{{ t('prov.viewModels') }}</div>
      <div class="popup-item" @click="menuAction('ids')">{{ t('prov.copyIds') }}</div>
      <div class="popup-item" @click="menuAction('delete')" style="color:var(--red)">{{ t('prov.delete') }}</div>
    </div>
  </div>
</template>
