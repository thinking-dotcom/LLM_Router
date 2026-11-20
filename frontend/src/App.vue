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
import {
  Cog6ToothIcon, TrashIcon, PlusCircleIcon, FolderOpenIcon, ArrowPathIcon,
  ArrowRightEndOnRectangleIcon, ArrowLeftEndOnRectangleIcon, BeakerIcon,
  CloudArrowUpIcon, XMarkIcon,
} from '@heroicons/vue/24/outline'

// 六块布局：概览大块 + 其余五块，title 由导航项标签提供
const panels = [
  { cls: 'p-overview', mode: null,            title: () => t('nav.overview') },
  { cls: 'p-logs',     mode: null,            title: () => t('nav.logs') },
  { cls: 'p-providers',mode: null,            title: () => t('nav.providers') },
  { cls: 'p-skills',   mode: 'skills',        title: () => t('nav.skills') },
  { cls: 'p-mcps',     mode: 'mcps',          title: () => t('nav.mcps') },
  { cls: 'p-sync',     mode: 'sync',          title: () => t('nav.sync') },
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
  ui.settingsPanel.open = false
}

let timers = []
onMounted(async () => {
  document.addEventListener('keydown', onKeydown)
  await refresh()
  await loadClientTargets()
  refreshStats()
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
          <button class="btn btn-xs btn-ico ovcog" :title="t('nav.settings')" :aria-label="t('nav.settings')" @click="ui.settingsPanel.open = true"><Cog6ToothIcon class="ico" /></button>
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
              <button class="btn btn-xs btn-ico log-tool-btn" :title="t('logs.clear')" :aria-label="t('logs.clear')" @click="ui.clearLogsDialog.open = true"><TrashIcon class="ico" /></button>
            </div>
          </div>
          <div class="panel-body"><Logs /></div>
        </div>

        <!-- 服务商 -->
        <div v-show="activeRight === 'providers'" class="right-content p-providers">
          <div class="page-head">
            <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
              <button class="btn btn-xs btn-ico p-title-btn p-btn-add prov-tool-btn" :title="t('prov.add')" :aria-label="t('prov.add')" @click="addProvider"><PlusCircleIcon class="ico" /></button>
            </div>
          </div>
          <div class="panel-body"><Providers ref="providersRef" /></div>
        </div>

        <!-- 技能 -->
        <div v-show="activeRight === 'skills'" class="right-content p-skills">
          <div class="page-head">
            <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
              <!-- 技能工具条：统一图标按钮，语义靠 title/aria-label 兜底 -->
              <button class="btn btn-xs btn-ico skill-tool-btn s-dir" :title="t('skills.dir')" :aria-label="t('skills.dir')" @click="skillsRef?.openDir()"><FolderOpenIcon class="ico" /></button>
              <button class="btn btn-xs btn-ico skill-tool-btn s-refresh" :title="t('skills.refresh')" :aria-label="t('skills.refresh')" @click="skillsRef?.loadSkills()"><ArrowPathIcon class="ico" /></button>
              <button class="btn btn-xs btn-ico skill-tool-btn s-inject" :disabled="skillsRef?.syncing" :title="t('skills.injectAllTip')" :aria-label="t('skills.injectAll')" @click="skillsRef?.injectAll()"><ArrowRightEndOnRectangleIcon class="ico" /></button>
              <button class="btn btn-xs btn-ico skill-tool-btn s-eject" :disabled="skillsRef?.syncing" :title="t('skills.ejectAllTip')" :aria-label="t('skills.ejectAll')" @click="skillsRef?.ejectAll()"><ArrowLeftEndOnRectangleIcon class="ico" /></button>
              <button class="btn btn-xs btn-ico skill-tool-btn s-delete" :disabled="skillsRef?.deletingAll" :title="t('skills.deleteAll')" :aria-label="t('skills.deleteAll')" @click="skillsRef?.askDeleteAll()"><TrashIcon class="ico" /></button>
              <button class="btn btn-xs btn-ico skill-tool-btn s-add" :title="t('skills.newSkill')" :aria-label="t('skills.newSkill')" @click="skillsRef?.openSkill()"><PlusCircleIcon class="ico" /></button>
            </div>
          </div>
          <div class="panel-body"><Skills ref="skillsRef" /></div>
        </div>

        <!-- MCP -->
        <div v-show="activeRight === 'mcps'" class="right-content p-mcps">
          <div class="page-head">
            <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
              <!-- MCP 工具条：统一图标按钮，语义靠 title/aria-label 兜底 -->
              <button class="btn btn-xs btn-ico mcp-tool-btn t-dir" :title="t('mcp.dir')" :aria-label="t('mcp.dir')" @click="mcpsRef?.openDir()"><FolderOpenIcon class="ico" /></button>
              <button class="btn btn-xs btn-ico mcp-tool-btn t-refresh" :title="t('mcp.refresh')" :aria-label="t('mcp.refresh')" @click="mcpsRef?.loadMcps()"><ArrowPathIcon class="ico" /></button>
              <button class="btn btn-xs btn-ico mcp-tool-btn t-test" :title="t('mcp.testAll')" :aria-label="t('mcp.testAll')" :disabled="!!mcpsRef?.testingAll" @click="mcpsRef?.testAll()"><BeakerIcon class="ico" :class="{ spin: !!mcpsRef?.testingAll }" /></button>
              <button class="btn btn-xs btn-ico mcp-tool-btn t-eject" :title="t('mcp.ejectAllTip')" :aria-label="t('mcp.ejectAll')" @click="mcpsRef?.ejectAll()"><ArrowLeftEndOnRectangleIcon class="ico" /></button>
              <button class="btn btn-xs btn-ico mcp-tool-btn t-inject" :title="t('mcp.mappingAllTip')" :aria-label="t('mcp.mapping')" @click="mcpsRef?.injectAll()"><ArrowRightEndOnRectangleIcon class="ico" /></button>
              <button class="btn btn-xs btn-ico mcp-tool-btn t-add" :title="t('mcp.add')" :aria-label="t('mcp.add')" @click="mcpsRef?.openMcp()"><PlusCircleIcon class="ico" /></button>
            </div>
          </div>
          <div class="panel-body"><Mcps ref="mcpsRef" /></div>
        </div>

        <!-- 客户端 -->
        <div v-show="activeRight === 'sync'" class="right-content p-sync">
        <div class="page-head">
          <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
            <button class="btn btn-xs btn-ico sync-update-btn" :class="{ 'p-btn-add': clientsOutdated > 0, busy: clientsUpdatingAll }"
              :disabled="!clientsOutdated || clientsUpdatingAll" :title="t('sync.cli.updateAllTip')"
              @click="clientsRef?.cliUpdateAll()">
              <CloudArrowUpIcon class="ico" :class="{ spin: clientsUpdatingAll }" />
              <span v-if="clientsOutdated && !clientsUpdatingAll" class="sync-update-count">{{ clientsOutdated }}</span>
            </button>
          </div>
        </div>
        <div class="panel-body">
          <Clients ref="clientsRef" />
        </div>
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
              <button class="btn btn-xs btn-ghost btn-ico sess-tool-btn ss-dir" :title="t('sessions.openDirTip')" :aria-label="t('sessions.openDir')" @click="sessionsRef?.openClientDir()"><FolderOpenIcon class="ico" /></button>
              <button class="btn btn-xs btn-ghost btn-ico sess-tool-btn ss-clear" :title="t('sessions.clearAllTip')" :aria-label="t('sessions.clearAll')" @click="sessionsRef?.askClearAll()"><TrashIcon class="ico" /></button>
              <button class="btn btn-xs btn-ghost btn-ico sess-tool-btn ss-refresh" :title="t('sessions.refresh')" :aria-label="t('sessions.refresh')" @click="sessionsRef?.refresh()"><ArrowPathIcon class="ico" /></button>
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
    <!-- 路由设置面板：概览标题齿轮图标打开 -->
    <div class="dialog-overlay settings-panel-ov" :class="{ open: ui.settingsPanel.open }" @click.self="ui.settingsPanel.open = false">
      <div class="dialog sp-dialog">
        <h3>{{ t('nav.settings') }}<button class="btn btn-xs btn-ico sp-close" :aria-label="t('nav.settings')" @click="ui.settingsPanel.open = false"><XMarkIcon class="ico" /></button></h3>
        <div class="sp-body"><Settings /></div>
      </div>
    </div>
    <Toast />
  </div>
</template>
