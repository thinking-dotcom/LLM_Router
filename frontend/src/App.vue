<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ui, refresh, refreshStats, refreshActive } from './store'
import { t } from './i18n'
import { loadTargets as loadClientTargets } from './components/sync/store'
import Overview from './components/Overview.vue'
import Logs from './components/Logs.vue'
import Providers from './components/Providers.vue'
import Settings from './components/Settings.vue'
import Skills from './components/Skills.vue'
import Mcps from './components/Mcps.vue'
import McpMappingDialog from './components/McpMappingDialog.vue'
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
})
onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
  timers.forEach(clearInterval)
})
</script>

<template>
  <div id="app">
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
              <button class="btn btn-xs btn-ghost" :title="skillsRef?.skillsDir" @click="skillsRef?.openDir()">📁 {{ t('skills.dir') }}</button>
              <button class="btn btn-xs btn-ghost" :title="t('skills.refresh')" @click="skillsRef?.loadSkills()">↻ {{ t('skills.refresh') }}</button>
              <button class="btn btn-xs p-btn-add" @click="skillsRef?.openSkill()">+ {{ t('common.new') }}</button>
            </div>
          </div>
          <div class="panel-body"><Skills ref="skillsRef" /></div>
        </div>

        <!-- MCP -->
        <div v-show="activeRight === 'mcps'" class="right-content p-mcps">
          <div class="page-head">
            <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
              <button class="btn btn-xs" @click="ui.mcpMappingDialog.open = true">{{ t('mcp.mapping') }}</button>
              <button class="btn btn-xs p-btn-add" @click="mcpsRef?.openMcp()">+ {{ t('common.add') }}</button>
            </div>
          </div>
          <div class="panel-body"><Mcps ref="mcpsRef" /></div>
        </div>

        <!-- 客户端 -->
        <div v-show="activeRight === 'sync'" class="right-content p-sync">
        <div class="page-head"></div>
        <div class="panel-body">
          <Clients />
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
            <span class="sess-total" :title="sessionsRef?.state?.root || ''">{{ sessionsRef?.state?.summary || '' }}</span>
            <div style="margin-left:auto;display:flex;align-items:center;gap:6px">
              <button class="btn btn-xs btn-ghost" :title="t('sessions.refresh')" @click="sessionsRef?.refresh()">↻ {{ t('sessions.refresh') }}</button>
              <button
                class="btn btn-xs btn-ghost" :class="{ disabled: !sessionsRef?.state?.canOpenRoot }"
                :title="sessionsRef?.state?.root || t('sessions.noRoot')"
                @click="sessionsRef?.openRoot()"
              >📁 {{ t('sessions.openRoot') }}</button>
              <button class="btn btn-xs btn-danger" :class="{ disabled: !sessionsRef?.state?.sel }" @click="sessionsRef?.askDelete()">
                {{ t('sessions.deleteSelected') }}<template v-if="sessionsRef?.state?.sel"> ({{ sessionsRef?.state.sel }})</template>
              </button>
            </div>
          </div>
          <div class="panel-body"><Sessions ref="sessionsRef" /></div>
        </div>
      </section>
    </main>

    <ProviderDialog />
    <DeleteDialog />
    <ClearLogsDialog />
    <McpMappingDialog />
    <ModelsDialog />
    <Toast />
  </div>
</template>
