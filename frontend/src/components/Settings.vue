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
const fallbackModels = ref([])   // 兜底模型链（按序多层兜底，元素为模型 ID）
const routerModels = ref([])     // 候选 = 路由返回的模型 ID 列表
const authToken = ref('')
const reasoningDepth = ref('high')
// 请求头注入规则（config.json headerRules）：每条 = 头名称 + 值 + 生效供应商（空 = 全部）
const headerRules = ref([])
function addHeaderRule() { headerRules.value.push({ name: '', value: '', provider: '' }) }
function removeHeaderRule(i) { headerRules.value.splice(i, 1) }
// 内置自动规则（路由器代码维护，不可编辑）：opencode 网关会话头，见 router/opencode.go。
// 仅作展示让注入配置自成一体；用户在下方添加同名规则可覆盖其值（用户规则后应用）。
const builtinHeaderRules = [
  { name: 'x-opencode-session', descKey: 'set.builtinOpencodeDesc' },
]
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
// 候选模型：已入选的不再出现在下拉里
const fbFilter = ref('')
const fbCandidates = computed(() => {
  const q = fbFilter.value.trim().toLowerCase()
  return routerModels.value.filter(m => !fallbackModels.value.includes(m) &&
    (!q || m.toLowerCase().includes(q)))
})
async function loadRouterModels() {
  let models = []
  try {
    if (api.GetModelsByFormat) {
      const r = await api.GetModelsByFormat('/oai')
      for (const m of (r?.models || [])) {
        if (m && !String(m).includes('*')) models.push(String(m))
      }
    }
  } catch (e) {}
  if (!models.length && state.config) {
    for (const p of (state.config.providers || [])) {
      if (p.enabled === false) continue
      for (const m of [...(p.models || []), ...Object.keys(p.aliases || {})]) {
        if (m && !String(m).includes('*')) models.push(String(m))
      }
    }
  }
  routerModels.value = [...new Set(models)].sort()
}
function pickFallback(m) {
  if (!m || fallbackModels.value.includes(m)) return
  fallbackModels.value = [...fallbackModels.value, m]
  ddOpen.value = false
}
function removeFallback(i) { fallbackModels.value = fallbackModels.value.filter((_, j) => j !== i) }

function initForm() {
  const cfg = state.config
  if (!cfg) return false
  port.value = cfg.port || 3000
  host.value = cfg.host || '127.0.0.1'
  defaultProvider.value = cfg.defaultProvider || ''
  fallbackModels.value = Array.isArray(cfg.fallbackModels) ? cfg.fallbackModels.slice() : []
  authToken.value = cfg.authToken || ''
  reasoningDepth.value = cfg.reasoningDepth || 'high'
  headerRules.value = Array.isArray(cfg.headerRules)
    ? cfg.headerRules.map(r => ({ name: r.name || '', value: r.value || '', provider: r.provider || '' }))
    : []
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
  loadRouterModels()
  document.addEventListener('click', onDocClick)
})
onUnmounted(() => document.removeEventListener('click', onDocClick))

// 兜底下拉
function toggleDefaultMenu() {
  if (ddOpen.value) { ddOpen.value = false; return }
  ddOpen.value = true
  fbFilter.value = ''
  nextTick(() => {
    if (ddEl.value && ddBtnEl.value) {
      ddEl.value.style.width = Math.max(ddBtnEl.value.offsetWidth, 220) + 'px'
      placePopup(ddEl.value, ddBtnEl.value.getBoundingClientRect())
      ddEl.value.querySelector('.fb-search')?.focus()
    }
  })
}
// 回车选中当前过滤结果的第一项
function fbSearchKey(e) {
  if (e.key === 'Enter') {
    e.preventDefault()
    if (fbCandidates.value.length) pickFallback(fbCandidates.value[0])
  }
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
  cfg2.fallbackModels = fallbackModels.value.slice()
  cfg2.authToken = authToken.value
  cfg2.reasoningDepth = reasoningDepth.value
  // 请求头注入：丢弃未填名称的行，落库前统一去空白（与后端 Normalize 一致）
  cfg2.headerRules = headerRules.value
    .filter(r => r.name.trim())
    .map(r => ({ name: r.name.trim(), value: r.value.trim(), provider: (r.provider || '').trim() }))
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
            <label>{{ t('set.fallback') }}<span class="hint">{{ t('set.fallbackHint') }}</span></label>
            <div class="fb-box">
              <span v-for="(m, i) in fallbackModels" :key="m" class="fb-chip" :title="t('set.fallbackLayer', { n: i + 1 })">
                <i class="fb-idx">{{ i + 1 }}</i><span class="fb-name">{{ m }}</span>
                <button type="button" class="fb-x" :title="t('common.delete')" @click="removeFallback(i)">×</button>
              </span>
              <button class="dd-btn fb-add" ref="ddBtnEl" type="button" @click="toggleDefaultMenu">+ {{ t('set.fallbackAdd') }}</button>
            </div>
          </div>
          <div class="field">
            <label>{{ t('set.reasoning') }}<span class="hint">{{ t('set.reasoningHint') }}</span></label>
            <div class="theme-options">
              <button v-for="opt in reasoningOptions" :key="opt" class="theme-opt" :class="{ active: reasoningDepth === opt }" type="button" @click="reasoningDepth = opt">{{ t('set.rz_' + opt) }}</button>
            </div>
          </div>
          <div class="field">
            <label>{{ t('set.headerRules') }}<span class="hint">{{ t('set.headerRulesHint') }}</span></label>
            <div class="hr-table">
              <div class="hr-tr hr-head">
                <span>{{ t('set.headerName') }}</span>
                <span>{{ t('set.headerValue') }}</span>
                <span>{{ t('set.headerProvider') }}</span>
                <span></span>
              </div>
              <div v-for="b in builtinHeaderRules" :key="b.name" class="hr-tr hr-builtin"
                :title="t('set.builtinTip')">
                <span class="hr-mono">{{ b.name }}</span>
                <span class="hr-desc">{{ t(b.descKey) }}</span>
                <span>{{ t('set.autoGateway') }}</span>
                <span class="hr-lock"><i class="fa-solid fa-lock"></i></span>
              </div>
              <div v-for="(r, i) in headerRules" :key="i" class="hr-tr">
                <input v-model="r.name" :placeholder="t('set.headerNamePh')" spellcheck="false" autocomplete="off">
                <input v-model="r.value" class="hr-mono" :placeholder="t('set.headerValuePh')" spellcheck="false" autocomplete="off">
                <select v-model="r.provider" :title="t('set.headerRulesHint')">
                  <option value="">{{ t('set.allProviders') }}</option>
                  <option v-for="p in providers" :key="p.id || p.name" :value="p.name">{{ p.name }}</option>
                </select>
                <button type="button" class="fb-x hr-del" :title="t('common.delete')" @click="removeHeaderRule(i)">×</button>
              </div>
            </div>
            <button class="dd-btn fb-add" style="margin-top:6px" type="button" @click="addHeaderRule">+ {{ t('set.headerRuleAdd') }}</button>
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
            <span v-if="updateReady" class="about-update" @click="openDownload" :title="t('set.download')">{{ appInfo.latestVersion }} <i class="fa-solid fa-arrow-down"></i></span>
          </div>
          <div v-if="appInfo.updateAvailable && !updateReady" class="about-new">{{ t('set.newVersion') }}: {{ appInfo.latestVersion }}</div>
        </div>
      </div>
    </div>

    <!-- 兜底模型追加下拉（候选 = 路由返回的模型 ID，已入选的隐藏）
         Teleport 到 body：深色主题 .panel 带 backdrop-filter 会成为 fixed 子元素的包含块，
         留在面板内下拉会按面板原点定位而偏移，脱离面板才与视口坐标一致 -->
    <Teleport to="body">
    <div v-if="ddOpen" ref="ddEl" class="popup-menu dd-popup" style="position:fixed;left:0;top:0">
      <input v-model="fbFilter" class="fb-search" type="text" spellcheck="false"
        :placeholder="t('set.fallbackSearch')" @keydown="fbSearchKey" />
      <div class="fb-list">
        <template v-if="fbCandidates.length">
          <div v-for="m in fbCandidates" :key="m" class="popup-item dd-opt" @click="pickFallback(m)">
            <span class="dd-opt-name fb-mono">{{ m }}</span>
          </div>
        </template>
        <div v-else class="popup-item" style="cursor:default;color:var(--t3)">{{ t('set.fallbackNoMore') }}</div>
      </div>
    </div>
    </Teleport>
  </div>
</template>

<style scoped>
/* 兜底模型链：有序胶囊（序号=兜底层级）+ 追加按钮 */
.fb-box{display:flex;flex-wrap:wrap;align-items:center;gap:6px;padding:6px 8px;border:1px solid var(--border);border-radius:var(--radius-sm);min-height:36px;background:var(--s2,var(--surface))}
.fb-chip{display:inline-flex;align-items:center;gap:5px;max-width:100%;padding:2px 4px 2px 2px;border:1px solid var(--accent-soft,#3a4a6a);border-radius:999px;background:var(--accent-soft);color:var(--text);font-size:11px}
.fb-idx{flex:none;width:16px;height:16px;border-radius:50%;background:var(--accent);color:#fff;font-size:9px;font-weight:700;display:flex;align-items:center;justify-content:center;font-style:normal}
.fb-name{font-family:var(--mono);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:200px}
.fb-x{flex:none;border:0;background:transparent;color:var(--t3);cursor:pointer;font-size:12px;line-height:1;padding:0 2px}
.fb-x:hover{color:var(--red)}
.fb-add{border:1px dashed var(--border-hover);background:transparent;color:var(--t2);padding:3px 10px;font-size:11px}
.fb-add:hover{color:var(--accent);border-color:var(--accent)}
.fb-mono{font-family:var(--mono)}
.fb-search{width:100%;box-sizing:border-box;margin-bottom:4px;padding:5px 8px;border:1px solid var(--border);border-radius:6px;background:var(--s2,var(--surface));color:var(--text);font-size:11.5px;font-family:var(--mono);outline:none}
.fb-search:focus{border-color:var(--accent)}
.fb-list{max-height:240px;overflow-y:auto;scrollbar-width:none}
.fb-list::-webkit-scrollbar{display:none}
/* 请求头注入：表格式排列（表头 + 内置行 + 用户规则行），四列同宽对齐：
   名称 34 / 值 34 / 生效供应商 26 / 操作 22px */
.hr-table{border:1px solid var(--border);border-radius:var(--radius-sm);overflow:hidden;background:var(--s2,var(--surface))}
.hr-tr{display:grid;grid-template-columns:minmax(0,34fr) minmax(0,34fr) minmax(0,26fr) 22px;gap:7px;align-items:center;padding:5px 8px;border-top:1px solid var(--border)}
.hr-tr:first-child{border-top:0}
.hr-head{padding:6px 8px;font-size:9px;font-weight:700;letter-spacing:.5px;color:var(--t3);text-transform:uppercase;background:var(--surface)}
.hr-tr input,.hr-tr select{width:100%;min-width:0}
.hr-tr>span{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.hr-mono{font-family:var(--mono)}
.hr-builtin{cursor:help;color:var(--t2);font-size:11px}
.hr-builtin .hr-desc{color:var(--t3);font-size:10px}
.hr-del{flex:none;font-size:13px;padding:0 2px}
.hr-lock{text-align:center;color:var(--t3);font-size:10px}
</style>
