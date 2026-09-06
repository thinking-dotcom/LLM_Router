<script setup>
import { ref, computed, watch, reactive, onMounted } from 'vue'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'
import { targetLogo } from './sync/shared'

// 会话管理：浏览 / 删除各 AI CLI 客户端落在本机磁盘上的历史会话记录。
// 主体为当前客户端的会话条目表，每条会话最左侧以该客户端 logo 标识归属；
// 客户端切换收进右下角浮动按钮（chatbot 图标 → 从右往左展开竖向客户端
// 图标栏），选中后收起。支持多选批量删除与终端恢复（终端图标收纳在
// 客户端 logo 上：点击 logo 在页面中心弹出 cmd / PowerShell 双宿主浮层）。

const clients = ref([])      // 客户端概览（后端注册表驱动）
const activeId = ref('')     // 当前选中的客户端
const items = ref([])        // 当前客户端的会话条目
const loading = ref(false)
const selected = ref(new Set()) // 选中的会话路径
const confirming = ref(false)   // 删除确认对话框
const navOpen = ref(false)      // 右下角浮动图标栏是否展开
const resumePop = ref(null)     // 终端恢复浮层：当前会话条目对象（null=收起）

const activeClient = computed(() => clients.value.find(c => c.id === activeId.value) || null)
const allSelected = computed(() => items.value.length > 0 && selected.value.size === items.value.length)
// 行首客户端图标：单客户端视图下所有条目均属当前客户端
const rowLogo = computed(() => (activeClient.value && targetLogo(activeClient.value.id)) || '')

// 对外状态：驱动页面顶部工具条（.page-head 位于本组件之外，由 App.vue 渲染）
const state = reactive({
  sel: 0,             // 已选会话数（删除按钮计数/禁用）
})
function syncUi() {
  state.sel = selected.value.size
}
watch([activeClient, items, selected], syncUi, { immediate: true })

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

// 客户端图标点击：已接入（managed）才进入会话条目；未接入置灰，点击提示原因。
// 选中后收起浮动图标栏。
function pickClient(c) {
  if (!c.managed) {
    toast(c.note || t('sessions.unmanaged'))
    return
  }
  navOpen.value = false
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

// 在指定宿主的终端中以该客户端的恢复命令拉起会话（claude --resume <id> 等）。
// shell: 'cmd'（默认）| 'powershell'。客户端不支持按会话恢复（Copilot/pi）时
// toast 说明原因并返回 false。
async function openTerminal(p, shell) {
  if (!activeClient.value?.resumeSupported) {
    toast(activeClient.value?.resumeNote || t('sessions.noResume'))
    return false
  }
  try {
    const r = await api.OpenSessionTerminal(activeId.value, p, shell || 'cmd')
    if (r && r.ok === false) { toast(t('common.fail', { msg: r.error || '' })); return false }
    return true
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })); return false }
}

// 点击行首客户端 logo：弹出行内浮层（页面中心）展示 cmd / PowerShell 两个
// 终端图标供选择；客户端不支持恢复时直接提示原因，不弹浮层。
function showResumePop(it) {
  if (!activeClient.value?.resumeSupported) {
    toast(activeClient.value?.resumeNote || t('sessions.noResume'))
    return
  }
  resumePop.value = it
}
// 浮层中点击某个终端图标（shell='cmd'|'powershell'）：执行恢复，成功即收起浮层。
async function doResumePop(shell) {
  const it = resumePop.value
  if (!it) return
  const ok = await openTerminal(it.path, shell)
  if (ok) resumePop.value = null
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

// ---- 暴露给 App.vue 顶部工具条（.page-head）的动作 ----
// 刷新：重载客户端概览并保持当前选中
function refresh() { return loadClients(activeId.value) }

onMounted(() => loadClients())
defineExpose({ loadClients, refresh, askDelete, state })
</script>

<template>
  <div class="sessions-wrap">
    <!-- 会话条目表（客户端切换在右下角浮动图标栏） -->
    <div class="recent-card sess-card">
      <div class="logs-scroll">
        <table v-if="items.length" class="logs-table sess-table">
          <colgroup>
            <col class="c-ck"><col class="c-logo"><col class="c-name"><col class="c-time"><col class="c-act">
          </colgroup>
          <thead>
            <tr>
              <th><input type="checkbox" :checked="allSelected" @change="toggleAll"></th>
              <th></th>
              <th>{{ t('sessions.colName') }}</th><th>{{ t('sessions.colTime') }}</th><th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="it in items" :key="it.path" :class="{ sel: selected.has(it.path) }">
              <td><input type="checkbox" :checked="selected.has(it.path)" @change="toggle(it.path)"></td>
              <td class="sr-logo">
                <img
                  v-if="rowLogo" class="sr-logo-img"
                  :src="rowLogo" :alt="activeClient?.name"
                  :title="activeClient?.resumeSupported ? t('sessions.openTerminal') : (activeClient?.resumeNote || t('sessions.noResume'))"
                  draggable="false" role="button" tabindex="0"
                  @click="showResumePop(it)" @keydown.enter.prevent="showResumePop(it)"
                />
              </td>
              <td class="sr-name" :title="it.path">{{ it.name }}</td>
              <td class="sr-time">{{ it.mtime }}</td>
              <td>
                <button class="btn btn-xs btn-ghost btn-ico" :title="t('sessions.openItem')" @click="openLocation(it.path)">📂</button>
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

    <!-- 右下角浮动：chatbot 图标 → 展开/收起客户端图标导航栏 -->
    <div class="sess-float">
      <transition name="sess-nav">
        <div v-if="navOpen" class="sess-nav">
          <div v-if="!clients.length" class="empty-tip" style="padding:6px 4px">{{ t('sessions.noClients') }}</div>
          <button
            v-for="c in clients" :key="c.id"
            class="sess-chip" :class="{ on: c.id === activeId, empty: c.managed && !c.rootExists, unmanaged: !c.managed }"
            :title="chipTip(c)"
            @click="pickClient(c)"
          >
            <img v-if="targetLogo(c.id)" class="sess-chip-logo" :src="targetLogo(c.id)" alt="" draggable="false" />
          </button>
        </div>
      </transition>
      <button
        class="sess-fab" :class="{ on: navOpen }"
        :title="t('sessions.chooseClient')"
        @click="navOpen = !navOpen"
      >
        <img src="/logos/chatbot.svg" alt="clients" draggable="false" />
      </button>
    </div>

    <!-- 终端恢复浮层（点击行首客户端 logo 弹出，页面水平垂直中心） -->
    <div class="dialog-overlay" :class="{ open: resumePop }" @click.self="resumePop=null">
      <div class="dialog resume-pop">
        <h3>{{ t('sessions.openTerminal') }}</h3>
        <p class="resume-sess" :title="resumePop?.path">{{ resumePop?.name }}</p>
        <div class="resume-actions">
          <button class="resume-act" :title="t('sessions.resumeInCmd')" @click="doResumePop('cmd')">
            <img class="resume-ico term-cmd" src="/logos/terminal-cmd-svgrepo-com.svg" alt="" draggable="false" />
            <span class="resume-lab">cmd</span>
          </button>
          <button class="resume-act" :title="t('sessions.resumeInPS')" @click="doResumePop('powershell')">
            <img class="resume-ico" src="/logos/shell-svgrepo-com.svg" alt="" draggable="false" />
            <span class="resume-lab">PowerShell</span>
          </button>
        </div>
        <div class="dialog-foot">
          <span></span>
          <div class="right">
            <button class="btn btn-ghost btn-sm" type="button" @click="resumePop=null">{{ t('dlg.cancel') }}</button>
          </div>
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
