<script setup>
import { ref, reactive, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { state, ui, refresh, applyTheme } from '../store'
import { t, i18nState, setLanguage } from '../i18n'
import { toast } from '../toast'
import api from '../api'
import { placePopup } from '../utils'

const providers = computed(() => state.config?.providers || [])

// 本地表单（初始化一次，15s 轮询更新 state.config 不会覆盖用户输入）
const port = ref(3000)
const host = ref('127.0.0.1')
const defaultProvider = ref('')
const authToken = ref('')
const reasoningDepth = ref('high')
const autoStart = ref(false)
const tray = ref(false)
const tokenRevealed = ref(false)

// 推理深度五档（config.json reasoningDepth）
const reasoningOptions = ['none', 'low', 'medium', 'high', 'max']

// 分组折叠（跨保存/切语言保留）
const openGroups = reactive({ system: false, appearance: false, about: false })

// 关于信息（程序名 / 当前版本 / 更新信息）
const appInfo = ref({ name: '', version: '', latestVersion: '', downloadURL: '', updateAvailable: false })

// 兜底下拉
const ddOpen = ref(false)
const ddEl = ref(null)
const ddBtnEl = ref(null)

const defLabelText = computed(() => {
  const defLabel = providers.value.find(p => p.id === defaultProvider.value)
  return defLabel
    ? (defLabel.enabled === false ? `${defLabel.name}（${t('prov.disabled')}）` : defLabel.name)
    : t('set.none')
})

function initForm() {
  const cfg = state.config
  if (!cfg) return false
  port.value = cfg.port || 3000
  host.value = cfg.host || '127.0.0.1'
  defaultProvider.value = cfg.defaultProvider || ''
  authToken.value = cfg.authToken || ''
  reasoningDepth.value = cfg.reasoningDepth || 'high'
  return true
}

// config 就绪后初始化一次表单（之后轮询不再覆盖）
const formInitialized = ref(false)
watch(() => state.config, (c) => {
  if (c && !formInitialized.value) {
    initForm()
    formInitialized.value = true
  }
}, { immediate: true })

onMounted(() => {
  if (api.GetAutoStartEnabled) {
    api.GetAutoStartEnabled().then(v => { autoStart.value = !!v }).catch(() => {})
  }
  if (api.GetTrayEnabled) {
    api.GetTrayEnabled().then(v => { tray.value = !!v }).catch(() => {})
  }
  if (api.GetAppInfo) {
    api.GetAppInfo().then(v => { if (v) appInfo.value = { ...appInfo.value, ...v } }).catch(() => {})
  }
  document.addEventListener('click', onDocClick)
})
onUnmounted(() => document.removeEventListener('click', onDocClick))

// 兜底下拉
function toggleDefaultMenu() {
  if (ddOpen.value) { ddOpen.value = false; return }
  ddOpen.value = true
  nextTick(() => {
    if (ddEl.value && ddBtnEl.value) {
      ddEl.value.style.width = Math.max(ddBtnEl.value.offsetWidth, 160) + 'px'
      placePopup(ddEl.value, ddBtnEl.value.getBoundingClientRect())
    }
  })
}
function selectDefault(p) {
  if (p.enabled === false) {
    toast(t('set.defaultDisabledToast'))
    return
  }
  defaultProvider.value = p.id
  ddOpen.value = false
}
function onDocClick(e) {
  if (!ddOpen.value) return
  if (ddEl.value && ddEl.value.contains(e.target)) return
  if (ddBtnEl.value && ddBtnEl.value.contains(e.target)) return
  ddOpen.value = false
}

// 主题 / 语言
function setTheme(theme) {
  applyTheme(theme)
  toast(theme === 'light' ? t('set.setThemeLight') : t('set.setThemeDark'))
}
function chooseLang(lang) {
  if (lang === i18nState.lang) return
  setLanguage(lang)
}

// 点击下载入口：跳转到目的下载地址（为空时不做任何事）
async function openDownload() {
  const url = (appInfo.value.downloadURL || '').trim()
  if (!url) return
  try {
    const r = await api.OpenURL(url)
    if (!r || r.ok === false) window.runtime?.BrowserOpenURL(url)
  } catch (e) {
    window.runtime?.BrowserOpenURL(url)
  }
}

// 是否有更新可下载（latestVersion 非空且给了下载地址）
const updateReady = computed(() => !!appInfo.value.latestVersion && !!appInfo.value.downloadURL)

// 开机自启 / 托盘常驻：切换即时生效，失败回滚开关状态
async function toggleAutoStart(checked) {
  const prev = !checked
  try {
    const r = await api.SetAutoStartEnabled(checked)
    if (r && r.ok === false) { autoStart.value = prev; toast(t('set.autoStartFail', { msg: r.error || '?' })) }
    else toast(checked ? t('set.autoStartOn') : t('set.autoStartOff'))
  } catch (e) { autoStart.value = prev; toast(t('set.autoStartFail', { msg: e?.message || '?' })) }
}
async function toggleTray(checked) {
  const prev = !checked
  try {
    const r = await api.SetTrayEnabled(checked)
    if (r && r.ok === false) { tray.value = prev; toast(t('set.trayFail', { msg: r.error || '?' })) }
    else toast(checked ? t('set.trayOn') : t('set.trayOff'))
  } catch (e) { tray.value = prev; toast(t('set.trayFail', { msg: e?.message || '?' })) }
}

async function save() {
  const portRaw = String(port.value).trim()
  const portNum = parseInt(portRaw, 10)
  if (!portRaw || isNaN(portNum) || portNum < 1 || portNum > 65535) {
    toast(t('set.portErr'))
    return
  }
  // 配置尚未加载完成时先拉取，避免用空对象覆盖清空 providers/API 密钥
  if (!state.config) await refresh(true)
  const cfg2 = JSON.parse(JSON.stringify(state.config || {}))
  cfg2.port = portNum
  cfg2.host = host.value.trim() || '127.0.0.1'
  cfg2.defaultProvider = defaultProvider.value
  cfg2.authToken = authToken.value
  cfg2.reasoningDepth = reasoningDepth.value
  cfg2.autoStart = autoStart.value
  cfg2.tray = tray.value

  let r
  try { r = await api.SaveConfig(cfg2) } catch (e) { toast(t('set.saveFail', { msg: e?.message || '?' })); return }
  toast(r.ok ? (r.restartNeeded ? t('set.savedRestart') : t('set.saved')) : t('set.saveFail', { msg: r.error }))
  if (r.ok && r.restartNeeded && state.running) {
    try {
      const rr = await api.RestartServer()
      if (rr && rr.ok) toast(t('set.restarted'))
      else toast(t('set.restartFail', { msg: rr && rr.error ? rr.error : '?' }))
    } catch (e) { toast(t('set.restartFail', { msg: e?.message || '?' })) }
  }
  if (r.ok) openGroups.system = false
  await refresh(true)
}
</script>

<template>
  <div>
    <!-- 系统设置 -->
    <div class="set-group" :class="{ open: openGroups.system }">
      <button class="set-group-head" type="button" @click="openGroups.system = !openGroups.system">
        <span>{{ t('set.system') }}</span>
        <svg class="chev" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>
      </button>
      <div class="set-group-body">
        <div class="set-group-inner">
          <div class="field-row">
            <div class="field"><label>{{ t('set.port') }}</label><input v-model="port" type="number" min="1" max="65535"></div>
            <div class="field"><label>{{ t('set.host') }}</label><input v-model="host"></div>
          </div>
          <div class="field">
            <label>{{ t('set.default') }}<span class="hint">{{ t('set.defaultHint') }}</span></label>
            <button class="dd-btn" ref="ddBtnEl" type="button" @click="toggleDefaultMenu">
              <span class="dd-label">{{ defLabelText }}</span>
              <svg class="dd-chev" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>
            </button>
          </div>
          <div class="field">
            <label>{{ t('set.reasoning') }}<span class="hint">{{ t('set.reasoningHint') }}</span></label>
            <div class="theme-options">
              <button v-for="opt in reasoningOptions" :key="opt" class="theme-opt" :class="{ active: reasoningDepth === opt }" type="button" @click="reasoningDepth = opt">{{ t('set.rz_' + opt) }}</button>
            </div>
          </div>
          <div class="field">
            <label>{{ t('set.token') }}<span class="hint">{{ t('set.tokenHint') }}</span></label>
            <div class="field-key" :class="{ revealed: tokenRevealed }">
              <input v-model="authToken" :type="tokenRevealed ? 'text' : 'password'" autocomplete="off">
              <button class="eye-btn" type="button" @click="tokenRevealed = !tokenRevealed">
                <svg class="eye-open" viewBox="0 0 24 24" width="15" height="15"><path fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" d="M1.5 12S5 5.5 12 5.5 22.5 12 22.5 12 19 18.5 12 18.5 1.5 12 1.5 12Z"/><circle cx="12" cy="12" r="3.2" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>
                <svg class="eye-closed" viewBox="0 0 24 24" width="15" height="15"><path fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" d="M3 3l18 18M10.6 10.6A3.2 3.2 0 0 0 12 15.2a3.2 3.2 0 0 0 3.2-3.2M9.4 5.9A9.7 9.7 0 0 1 12 5.5c7 0 10.5 6.5 10.5 6.5a18 18 0 0 1-3.3 4.2M6.1 6.1C3.7 7.6 1.5 12 1.5 12S5 18.5 12 18.5c1.2 0 2.3-.2 3.3-.5"/></svg>
              </button>
            </div>
          </div>
          <div class="field-row toggle-row">
            <div class="field"><label>{{ t('set.autoStart') }}<span class="hint">{{ t('set.autoStartHint') }}</span></label></div>
            <label class="toggle"><input type="checkbox" v-model="autoStart" @change="toggleAutoStart(autoStart)"><span class="slider"></span></label>
          </div>
          <div class="field-row toggle-row">
            <div class="field"><label>{{ t('set.tray') }}<span class="hint">{{ t('set.trayHint') }}</span></label></div>
            <label class="toggle"><input type="checkbox" v-model="tray" @change="toggleTray(tray)"><span class="slider"></span></label>
          </div>
          <button class="btn btn-green" style="width:100%;margin-top:2px" @click="save">{{ t('set.save') }}</button>
        </div>
      </div>
    </div>

    <!-- 界面设置 -->
    <div class="set-group" :class="{ open: openGroups.appearance }">
      <button class="set-group-head" type="button" @click="openGroups.appearance = !openGroups.appearance">
        <span>{{ t('set.appearance') }}</span>
        <svg class="chev" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>
      </button>
      <div class="set-group-body">
        <div class="set-group-inner">
          <div class="field">
            <label>{{ t('set.theme') }}<span class="hint">{{ t('set.themeHint') }}</span></label>
            <div class="theme-options">
              <button class="theme-opt" :class="{ active: ui.theme === 'dark' }" type="button" @click="setTheme('dark')">{{ t('set.dark') }}</button>
              <button class="theme-opt" :class="{ active: ui.theme === 'light' }" type="button" @click="setTheme('light')">{{ t('set.light') }}</button>
            </div>
          </div>
          <div class="field">
            <label>{{ t('set.lang') }}</label>
            <div class="theme-options">
              <button class="theme-opt t-lang" :class="{ active: i18nState.lang === 'zh' }" type="button" @click="chooseLang('zh')">{{ t('set.zh') }}</button>
              <button class="theme-opt t-lang" :class="{ active: i18nState.lang === 'en' }" type="button" @click="chooseLang('en')">{{ t('set.en') }}</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 关于 -->
    <div class="set-group" :class="{ open: openGroups.about }">
      <button class="set-group-head" type="button" @click="openGroups.about = !openGroups.about">
        <span>{{ t('set.about') }}</span>
        <svg class="chev" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>
      </button>
      <div class="set-group-body">
        <div class="set-group-inner">
          <div class="about-desc">{{ t('set.aboutDesc') }}</div>
          <div class="about-row">
            <span class="about-label">{{ t('set.currentVersion') }}</span>
            <span class="about-version">{{ appInfo.version || '—' }}</span>
            <span v-if="updateReady" class="about-update" @click="openDownload" :title="t('set.download')">{{ appInfo.latestVersion }} ↓</span>
          </div>
          <div v-if="appInfo.updateAvailable && !updateReady" class="about-new">{{ t('set.newVersion') }}: {{ appInfo.latestVersion }}</div>
        </div>
      </div>
    </div>

    <!-- 兜底服务商下拉 -->
    <div v-if="ddOpen" ref="ddEl" class="popup-menu dd-popup" style="position:fixed;left:0;top:0">
      <template v-if="providers.length">
        <div
          v-for="p in providers"
          :key="p.id"
          class="popup-item dd-opt"
          :class="{ sel: p.id === defaultProvider && p.enabled !== false }"
          :data-disabled="p.enabled === false ? '1' : undefined"
          @click="selectDefault(p)"
        >
          <span class="dd-opt-name">{{ p.name }}</span>
          <span class="dd-opt-right">
            <span v-if="p.enabled === false" class="dd-opt-disabled">{{ t('prov.disabled') }}</span>
            <span v-if="p.id === defaultProvider && p.enabled !== false" class="dd-opt-check">✓</span>
          </span>
        </div>
      </template>
      <div v-else class="popup-item" style="cursor:default;color:var(--t3)">{{ t('set.none') }}</div>
    </div>
  </div>
</template>
