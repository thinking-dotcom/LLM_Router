<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ui, refresh, refreshStats, refreshActive, refreshLogs } from './store'
import { t } from './i18n'
import { loadTargets as loadClientTargets } from './components/sync/store'
import Overview from './components/Overview.vue'
import Logs from './components/Logs.vue'
import Providers from './components/Providers.vue'
import Settings from './components/Settings.vue'
import Skills from './components/Skills.vue'
import Mcps from './components/Mcps.vue'
import Clients from './components/Clients.vue'
import TokenChart from './components/TokenChart.vue'
import Sessions from './components/Sessions.vue'
import ProviderDialog from './components/ProviderDialog.vue'
import DeleteDialog from './components/DeleteDialog.vue'
import ClearLogsDialog from './components/ClearLogsDialog.vue'
import ModelsDialog from './components/ModelsDialog.vue'
import Toast from './components/Toast.vue'

// 七块布局：概览大块 + 其余六块，title 由导航项标签提供
const panels = [
  { cls: 'p-overview', mode: null,            title: () => t('nav.overview') },
  { cls: 'p-logs',     mode: null,            title: () => t('nav.logs') },
  { cls: 'p-providers',mode: null,            title: () => t('nav.providers') },
  { cls: 'p-skills',   mode: 'skills',        title: () => t('nav.skills') },
  { cls: 'p-mcps',     mode: 'mcps',          title: () => t('nav.mcps') },
  { cls: 'p-sync',     mode: 'sync',          title: () => t('nav.sync') },
  { cls: 'p-settings', mode: null,            title: () => t('nav.settings') },
]

// 子组件 ref：面板标题栏的“添加/新建”按钮调用各自组件暴露的方法
const providersRef = ref(null)
const skillsRef = ref(null)
const mcpsRef = ref(null)
const sessionsRef = ref(null)
const clientsRef = ref(null)
// 客户端页顶部工具条「一键更新」：待更新数量与是否正在批量更新（均由 Clients.vue 暴露）
const clientsOutdated = computed(() => clientsRef.value?.cliOutdatedList?.length || 0)
const clientsUpdatingAll = computed(() => !!clientsRef.value?.cliUpdatingAll)

// 右区顶部导航：按当前语言标签文字长度升序（文字多的排后面，等长保持原顺序）
const rightTabs = [
  { mode: 'logs',      key: 'nav.logs' },
  { mode: 'providers', key: 'nav.providers' },
  { mode: 'skills',    key: 'nav.skills' },
  { mode: 'mcps',      key: 'nav.mcps' },
  { mode: 'sync',      key: 'nav.sync' },
  { mode: 'settings',  key: 'nav.settings' },
  { mode: 'status',    key: 'nav.chart' },
  { mode: 'sessions',  key: 'nav.sessions' },
]
const sortedTabs = computed(() =>
  [...rightTabs].sort((a, b) => t(a.key).length - t(b.key).length)
)
const activeRight = ref('logs')


function addProvider() { providersRef.value?.addProvider() }

// 清空请求日志：打开确认对话框（确认逻辑在 ClearLogsDialog 内）

function onKeydown(e) {
  if (e.key !== 'Escape') return
  ui.providerDialog.open = false
  ui.deleteDialog.open = false
  ui.modelsDialog.open = false
  ui.menu.open = false
}

let timers = []
// 启动后按内容自然高度调整窗口高度（只执行一次）：
// 临时把 #app 设为 max-content 测量内容理想高度，再还原并设置窗口高度
async function fitWindowHeight() {
  const rt = window.runtime
  if (!rt || !rt.WindowSetSize || !rt.WindowGetSize) return
  await new Promise(r => setTimeout(r, 500)) // 等首屏数据与字体就绪
  const appEl = document.getElementById('app')
  if (!appEl) return
  const prev = appEl.style.height
  appEl.style.height = 'max-content'
  const h = Math.ceil(appEl.getBoundingClientRect().height)
  appEl.style.height = prev
  if (h < 200) return
  try {
    const size = await rt.WindowGetSize()
    const nh = Math.max(420, Math.min(h, 1100)) // 下限防过矮，上限防超出屏幕
    if (Math.abs(nh - size.h) > 4) await rt.WindowSetSize(size.w, nh)
  } catch (e) {}
}
onMounted(async () => {
  document.addEventListener('keydown', onKeydown)
  await refresh()
  await loadClientTargets()
  refreshStats()
  fitWindowHeight()
  timers.push(setInterval(refresh, 15000))
  timers.push(setInterval(refreshStats, 2000))
  timers.push(setInterval(refreshActive, 1000))
  // 请求日志独立 2s 轮询（原先只随 15s 的 refresh 刷新，导致日志显示明显滞后）
  timers.push(setInterval(refreshLogs, 2000))
})
onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
  timers.forEach(clearInterval)
})
</script>

<template>
  <!-- 挂载点 index.html 已有 div#app（#app 的定位/尺寸样式都挂在它上），
       这里不能再叫 #app，否则出现双层 #app 嵌套、窗口测高也被内层 100vh 钳死 -->
  <div class="app-root">
    <main id="board">
      <!-- 左区：概览（以概览宽度为左右分界，固定左侧） -->
      <section class="panel left-pane">
        <div class="panel-title">
          <span class="p-title-dot"></span><span class="p-title-name">{{ t('nav.overview') }}</span>
        </div>
        <div class="panel-body"><Overview /></div>
      </section>

      <!-- 右区：顶部导航栏 + 对应页面 -->
      <section class="panel right-pane">
        <div class="right-nav">
          <button v-for="tb in sortedTabs" :key="tb.mode" class="right-tab" :class="{ on: activeRight === tb.mode }" @click="activeRight = tb.mode">{{ t(tb.key) }}</button>
        </div>

        <!-- 日志 -->
        <div v-show="activeRight === 'logs'" class="right-content p-logs">
          <div class="page-head">
            <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
              <button class="btn btn-xs" @click="ui.clearLogsDialog.open = true">{{ t('logs.clear') }}</button>
            </div>
          </div>
          <div class="panel-body"><Logs /></div>
        </div>

        <!-- 服务商 -->
        <div v-show="activeRight === 'providers'" class="right-content p-providers">
          <div class="page-head">
            <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
              <button class="btn btn-xs p-title-btn p-btn-add" @click="addProvider">+ {{ t('common.add') }}</button>
            </div>
          </div>
          <div class="panel-body"><Providers ref="providersRef" /></div>
        </div>

        <!-- 技能 -->
        <div v-show="activeRight === 'skills'" class="right-content p-skills">
          <div class="page-head">
            <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
              <!-- 工具条只留文字按钮（图标已移除，语义靠 title/aria-label 兜底） -->
              <button class="btn btn-xs" :title="skillsRef?.skillsDir" :aria-label="t('skills.dir')" @click="skillsRef?.openDir()">{{ t('skills.dir') }}</button>
              <button class="btn btn-xs" :title="t('skills.refresh')" :aria-label="t('skills.refresh')" @click="skillsRef?.loadSkills()">{{ t('skills.refresh') }}</button>
              <button class="btn btn-xs" :disabled="skillsRef?.syncing" :title="t('skills.injectAllTip')" :aria-label="t('skills.injectAll')" @click="skillsRef?.injectAll()">{{ t('skills.injectAll') }}</button>
              <button class="btn btn-xs" :disabled="skillsRef?.syncing" :title="t('skills.ejectAllTip')" :aria-label="t('skills.ejectAll')" @click="skillsRef?.ejectAll()">{{ t('skills.ejectAll') }}</button>
              <button class="btn btn-xs p-btn-add" @click="skillsRef?.openSkill()">{{ t('common.new') }}</button>
            </div>
          </div>
          <div class="panel-body"><Skills ref="skillsRef" /></div>
        </div>

        <!-- MCP -->
        <div v-show="activeRight === 'mcps'" class="right-content p-mcps">
          <div class="page-head">
            <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
              <!-- 工具条只留文字按钮（图标已移除，语义靠 title/aria-label 兜底） -->
              <button class="btn btn-xs" :title="mcpsRef?.mcpDir" :aria-label="t('mcp.dir')" @click="mcpsRef?.openDir()">{{ t('mcp.dir') }}</button>
              <button class="btn btn-xs" :title="t('mcp.ejectAllTip')" @click="mcpsRef?.ejectAll()">{{ t('mcp.ejectAll') }}</button>
              <button class="btn btn-xs" :title="t('mcp.mappingAllTip')" @click="mcpsRef?.injectAll()">{{ t('mcp.mapping') }}</button>
              <button class="btn btn-xs p-btn-add" @click="mcpsRef?.openMcp()">{{ t('common.add') }}</button>
            </div>
          </div>
          <div class="panel-body"><Mcps ref="mcpsRef" /></div>
        </div>

        <!-- 客户端 -->
        <div v-show="activeRight === 'sync'" class="right-content p-sync">
        <div class="page-head">
          <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
            <button class="btn btn-xs" :class="{ 'p-btn-add': clientsOutdated > 0 }"
              :disabled="!clientsOutdated || clientsUpdatingAll" :title="t('sync.cli.updateAllTip')"
              @click="clientsRef?.cliUpdateAll()">
              <i class="fa-solid fa-arrow-up-from-bracket"></i>
              {{ clientsUpdatingAll ? t('sync.cli.updatingAll') : t('sync.cli.updateAll') }}
              <template v-if="clientsOutdated && !clientsUpdatingAll">({{ clientsOutdated }})</template>
            </button>
          </div>
        </div>
        <div class="panel-body">
          <Clients ref="clientsRef" />
        </div>
        </div>

        <!-- 设置 -->
        <div v-show="activeRight === 'settings'" class="right-content p-settings">
          <div class="page-head"></div>
          <div class="panel-body"><Settings /></div>
        </div>

        <!-- Token 用量 -->
        <div v-show="activeRight === 'status'" class="right-content p-status">
          <div class="page-head"></div>
          <div class="panel-body board-status"><TokenChart /></div>
        </div>

        <!-- 会话管理 -->
        <div v-show="activeRight === 'sessions'" class="right-content p-sessions">
          <div class="page-head">
            <!-- 工具条只留图标（语义放 title/aria-label）：打开会话目录、一键清空、刷新
                 （单条删除在每行行尾垃圾桶图标，见 Sessions.vue 的 .sr-del） -->
            <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
              <button class="btn btn-xs btn-ghost btn-ico" :title="t('sessions.openDirTip')" :aria-label="t('sessions.openDir')" @click="sessionsRef?.openClientDir()"><i class="fa-solid fa-folder-open"></i></button>
              <button class="btn btn-xs btn-ghost btn-ico" :title="t('sessions.clearAllTip')" :aria-label="t('sessions.clearAll')" @click="sessionsRef?.askClearAll()"><i class="fa-solid fa-trash-can"></i></button>
              <button class="btn btn-xs btn-ghost btn-ico" :title="t('sessions.refresh')" :aria-label="t('sessions.refresh')" @click="sessionsRef?.refresh()"><i class="fa-solid fa-rotate-right"></i></button>
            </div>
          </div>
          <div class="panel-body"><Sessions ref="sessionsRef" /></div>
        </div>
      </section>
    </main>

    <ProviderDialog />
    <DeleteDialog />
    <ClearLogsDialog />
    <ModelsDialog />
    <Toast />
  </div>
</template>
