<script setup>
import { ref, computed, onMounted } from 'vue'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'
import { targetLogo } from './sync/shared'

// 会话管理：浏览 / 删除各 AI CLI 客户端落在本机磁盘上的历史会话记录。
// 左上为客户端选择条（含会话数与体积），主体为该客户端的会话条目表，
// 支持多选批量删除与在系统文件管理器中打开所在位置。

const clients = ref([])      // 客户端概览（后端注册表驱动）
const activeId = ref('')     // 当前选中的客户端
const items = ref([])        // 当前客户端的会话条目
const loading = ref(false)
const selected = ref(new Set()) // 选中的会话路径
const confirming = ref(false)   // 删除确认对话框

const activeClient = computed(() => clients.value.find(c => c.id === activeId.value) || null)
const allSelected = computed(() => items.value.length > 0 && selected.value.size === items.value.length)

function fmtBytes(n) {
  if (!n) return '0 B'
  if (n >= 1073741824) return (n / 1073741824).toFixed(1) + ' GB'
  if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB'
  if (n >= 1024) return (n / 1024).toFixed(1) + ' KB'
  return n + ' B'
}

async function loadClients(pick) {
  try { clients.value = (await api.GetSessionClients()) || [] } catch (e) { clients.value = [] }
  if (!clients.value.length) { activeId.value = ''; items.value = []; return }
  const ids = clients.value.map(c => c.id)
  let id = pick && ids.includes(pick) ? pick : ''
  if (!id) {
    // 默认选中第一个有会话的已接入客户端；都没有则选第一个已接入客户端
    const withCount = clients.value.find(c => c.managed && c.count > 0)
    const anyManaged = clients.value.find(c => c.managed)
    id = withCount ? withCount.id : (anyManaged ? anyManaged.id : ids[0])
  }
  await selectClient(id)
}

async function selectClient(id) {
  if (loading.value) return
  activeId.value = id
  selected.value = new Set()
  items.value = []
  loading.value = true
  try {
    const r = await api.GetSessionItems(id)
    if (r && r.ok) items.value = r.items || []
    else toast(t('common.fail', { msg: r?.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
  loading.value = false
}

function toggleAll() {
  selected.value = allSelected.value ? new Set() : new Set(items.value.map(i => i.path))
}
function toggle(path) {
  const next = new Set(selected.value)
  next.has(path) ? next.delete(path) : next.add(path)
  selected.value = next
}

// 客户端胶囊点击：已接入（managed）才进入会话条目；未接入置灰，点击提示原因。
function pickClient(c) {
  if (!c.managed) {
    toast(c.note || t('sessions.unmanaged'))
    return
  }
  selectClient(c.id)
}

// 客户端胶囊悬停提示：名称 + 会话统计（总大小）或缺失根目录说明。
function chipTip(c) {
  if (!c.managed) return [c.name, c.note || t('sessions.unmanaged')].join('\n')
  const lines = [c.name]
  if (c.rootExists) {
    lines.push(t('sessions.total', { n: c.count || 0, size: fmtBytes(c.totalSize) }))
  } else {
    lines.push(t('sessions.noRoot'))
    if (c.root) lines.push(c.root)
  }
  return lines.join('\n')
}

// 在系统文件管理器中打开（目录型会话选中目录本身）
async function openLocation(p) {
  try {
    const r = await api.OpenFileLocation(p)
    if (r && r.ok === false) toast(t('common.fail', { msg: r.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}

// 在终端中以该客户端的恢复命令拉起会话（claude --resume <id> 等）。
// 客户端不支持按会话恢复（Copilot/pi）时置灰并 toast 说明原因。
async function openTerminal(p) {
  if (!activeClient.value?.resumeSupported) {
    toast(activeClient.value?.resumeNote || t('sessions.noResume'))
    return
  }
  try {
    const r = await api.OpenSessionTerminal(activeId.value, p)
    if (r && r.ok === false) toast(t('common.fail', { msg: r.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}

function askDelete() { if (selected.value.size) confirming.value = true }
async function doDelete() {
  const ids = [...selected.value]
  confirming.value = false
  if (!ids.length || !activeId.value) return
  try {
    const r = await api.DeleteSessionItems(activeId.value, ids)
    if (r && r.ok) {
      toast(t('sessions.deleted', { n: ids.length }))
      await loadClients(activeId.value) // 删除后刷新概览与条目
    } else toast(t('common.fail', { msg: r?.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}

onMounted(() => loadClients())
defineExpose({ loadClients })
</script>

<template>
  <div class="sessions-wrap">
    <!-- 客户端选择条：纯图标，悬停显示会话统计；未接入会话管理的客户端置灰 -->
    <div class="sess-clients">
      <button
        v-for="c in clients" :key="c.id"
        class="sess-chip" :class="{ on: c.id === activeId, empty: c.managed && !c.rootExists, unmanaged: !c.managed }"
        :title="chipTip(c)"
        @click="pickClient(c)"
      >
        <img v-if="targetLogo(c.id)" class="sess-chip-logo" :src="targetLogo(c.id)" alt="" draggable="false" />
      </button>
      <div v-if="!clients.length" class="empty-tip">{{ t('sessions.noClients') }}</div>
    </div>

    <!-- 工具条 -->
    <div class="sess-bar">
      <span class="sess-total" :title="activeClient?.root || ''">
        {{ activeClient ? t('sessions.total', { n: items.length, size: fmtBytes(activeClient.totalSize) }) : '' }}
      </span>
      <div class="sess-actions">
        <button class="btn btn-xs btn-ghost" :title="t('sessions.refresh')" @click="loadClients(activeId)">↻ {{ t('sessions.refresh') }}</button>
        <button
          class="btn btn-xs btn-ghost" :class="{ disabled: !activeClient?.rootExists }"
          :title="activeClient?.root || t('sessions.noRoot')"
          @click="openLocation(activeClient?.root)"
        >📁 {{ t('sessions.openRoot') }}</button>
        <button class="btn btn-xs btn-danger" :class="{ disabled: !selected.size }" @click="askDelete">
          {{ t('sessions.deleteSelected') }}<template v-if="selected.size"> ({{ selected.size }})</template>
        </button>
      </div>
    </div>

    <!-- 会话条目表 -->
    <div class="recent-card sess-card">
      <div class="logs-scroll">
        <table v-if="items.length" class="logs-table sess-table">
          <colgroup>
            <col class="c-ck"><col class="c-name"><col class="c-time"><col class="c-act">
          </colgroup>
          <thead>
            <tr>
              <th><input type="checkbox" :checked="allSelected" @change="toggleAll"></th>
              <th>{{ t('sessions.colName') }}</th><th>{{ t('sessions.colTime') }}</th><th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="it in items" :key="it.path" :class="{ sel: selected.has(it.path) }">
              <td><input type="checkbox" :checked="selected.has(it.path)" @change="toggle(it.path)"></td>
              <td class="sr-name" :title="it.path">{{ it.name }}</td>
              <td class="sr-time">{{ it.mtime }}</td>
              <td>
                <button class="btn btn-xs btn-ghost btn-ico" :title="t('sessions.openItem')" @click="openLocation(it.path)">📂</button>
                <button
                  class="btn btn-xs btn-ghost btn-ico sess-term" :class="{ 'no-resume': !activeClient?.resumeSupported }"
                  :title="activeClient?.resumeSupported ? t('sessions.openTerminal') : (activeClient?.resumeNote || t('sessions.noResume'))"
                  @click="openTerminal(it.path)"
                >❯_</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty-tip">
          <template v-if="loading">{{ t('sessions.loading') }}</template>
          <template v-else-if="activeClient && !activeClient.rootExists">{{ t('sessions.noRoot') }}<br><span class="sr-path">{{ activeClient.root }}</span></template>
          <template v-else-if="activeClient">{{ t('sessions.empty') }}</template>
        </div>
      </div>
    </div>

    <!-- 删除确认（应用内对话框，删除不可恢复） -->
    <div class="dialog-overlay" :class="{ open: confirming }" @click.self="confirming=false">
      <div class="dialog" style="width:min(420px, calc(100% - 24px))">
        <h3>{{ t('dlg.deleteTitle') }}</h3>
        <p class="delete-desc">{{ t('sessions.confirmDelete', { n: selected.size }) }}</p>
        <div class="dialog-foot">
          <span></span>
          <div class="right">
            <button class="btn btn-ghost btn-sm" type="button" @click="confirming=false">{{ t('dlg.cancel') }}</button>
            <button class="btn btn-danger btn-sm" type="button" @click="doDelete">{{ t('dlg.confirmDeleteBtn') }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
