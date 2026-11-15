<script setup>
import { ref, computed, watch } from 'vue'
import { ui, state, refresh } from '../store'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'
import { delay } from '../utils'

const PF_BASE_HINT = {
  'openai': 'https://api.deepseek.com',
  'anthropic': 'https://api.anthropic.com',
  'gemini-generateContent': 'https://generativelanguage.googleapis.com',
  'gemini-openai': 'https://generativelanguage.googleapis.com',
  'gemini-interactions': 'https://generativelanguage.googleapis.com/v1beta/interactions',
  'response': 'https://api.openai.com',
}
const PF_PATH_HINT = {
  'openai': '/v1/chat/completions',
  'gemini-openai': '/v1beta/openai/chat/completions',
  'response': '/v1/responses',
  'anthropic': '/v1/messages',
  'gemini-generateContent': '/v1beta/models/{model}:generateContent',
  'gemini-interactions': '/v1beta/interactions',
}

const name = ref('')
const type = ref('openai')
const baseUrl = ref('')
const apiKey = ref('')
const aliases = ref('')
const error = ref('')
const keyRevealed = ref(false)
// API 格式自定义下拉（原生 select 的弹出列表无法自定义样式）
// Gemini 上游三种格式统一以 gemini- 前缀命名：gemini-generateContent（原生
// legacy 协议）、gemini-interactions（原生 GA 新协议）、gemini-openai（OpenAI
// 兼容层）。后端只存 5 种 ProviderType，generateContent 与 interactions 统一
// 存为 type=gemini，按 baseUrl 是否含 /interactions 自动分流
// （format.isGeminiInteractions）；两个展示名保存时都归一为 gemini。
const API_FORMATS = ['openai', 'gemini-openai', 'response', 'anthropic', 'gemini-generateContent', 'gemini-interactions']
const afmtOpen = ref(false)

// 展示选项 → 后端存储类型（两种 Gemini 原生协议都存为 gemini）
const TYPE_STORAGE = {
  'gemini-generateContent': 'gemini',
  'gemini-interactions': 'gemini',
}
function storageType(t) { return TYPE_STORAGE[t] || t }

// 已存 provider → 展示选项：type=gemini 时按 base 是否指向 /interactions 区分两种原生协议
function displayType(p) {
  const t = p?.type || 'openai'
  if (t === 'gemini') {
    return /\/interactions/i.test(p?.baseUrl || '') ? 'gemini-interactions' : 'gemini-generateContent'
  }
  return t
}

const isEdit = computed(() => !!ui.providerDialog.provider)
const title = computed(() => isEdit.value ? t('prov.editTitle') : t('prov.addTitle'))
const baseHint = computed(() => PF_BASE_HINT[type.value] || 'https://...')
const pathHint = computed(() => PF_PATH_HINT[type.value] || '')

// 打开时初始化表单
watch(() => ui.providerDialog.open, (open) => {
  if (!open) return
  const p = ui.providerDialog.provider
  name.value = p?.name || ''
  type.value = displayType(p)
  baseUrl.value = p?.baseUrl || ''
  apiKey.value = p?.apiKey || ''
  aliases.value = p?.aliases ? Object.entries(p.aliases).map(([k, v]) => k + ' = ' + v).join('\n') : ''
  error.value = ''
  keyRevealed.value = false
  afmtOpen.value = false
})

function close() { ui.providerDialog.open = false }

function reset() {
  name.value = ''
  type.value = 'openai'
  baseUrl.value = ''
  apiKey.value = ''
  aliases.value = ''
  error.value = ''
}

async function save() {
  const nm = name.value.trim()
  let base = baseUrl.value.trim()
  if (!nm) { error.value = t('prov.enterName'); return }
  if (!/^https?:\/\//.test(base)) { error.value = t('prov.baseUrlErr'); return }

  // 选了 Interactions 但 base 未带 /interactions 时自动补全路径，
  // 否则后端 isGeminiInteractions 判定不出，请求会静默走错协议（generateContent）。
  if (type.value === 'gemini-interactions' && !/\/interactions/i.test(base)) {
    base = base.replace(/\/+$/, '') + '/v1beta/interactions'
  }

  const aliasObj = {}
  const aliasText = aliases.value.trim()
  if (aliasText) {
    for (const line of aliasText.split('\n')) {
      const sep = line.includes('=') ? '=' : line.includes('→') ? '→' : line.includes('->') ? '->' : null
      if (!sep) continue
      const idx = line.indexOf(sep)
      const k = line.slice(0, idx).trim()
      const v = line.slice(idx + sep.length).trim()
      if (k && v) aliasObj[k] = v
    }
  }

  // 配置尚未加载完成时先拉取，避免用空对象覆盖清空 providers/API 密钥
  if (!state.config) await refresh(true)
  const cfg = JSON.parse(JSON.stringify(state.config || {}))
  const editingId = isEdit.value ? ui.providerDialog.provider.id : null
  const provider = {
    id: editingId || 'p-' + Math.random().toString(16).slice(2, 8),
    name: nm, type: storageType(type.value), baseUrl: base, apiKey: apiKey.value,
    models: [], aliases: aliasObj, enabled: true,
  }
  const existing = cfg.providers.find(x => x.id === provider.id)
  if (existing) {
    provider.models = existing.models || []
    provider.enabled = existing.enabled !== false
  }
  const idx = cfg.providers.findIndex(x => x.id === provider.id)
  if (idx >= 0) cfg.providers[idx] = provider
  else cfg.providers.push(provider)

  let r
  try { r = await api.SaveConfig(cfg) } catch (e) { error.value = (e?.message || e).toString(); return }
  if (r.ok) {
    ui.providerDialog.open = false
    await refresh(true)
    if (r.restartNeeded) toast(t('set.savedRestart'))
  } else {
    error.value = r.error || t('dlg.saveFail')
  }
}
</script>

<template>
  <div class="dialog-overlay" id="prov-dialog" :class="{ open: ui.providerDialog.open }">
    <div class="dialog">
      <h3>{{ title }}</h3>
      <div class="field"><label>{{ t('dlg.name') }}</label><input v-model="name" :placeholder="t('dlg.typePh')"></div>
      <div class="field">
        <label>{{ t('dlg.apiFormat') }} <span class="hint">{{ t('dlg.apiFormatHint') }}</span></label>
        <div class="afmt-sel">
          <input :value="type" readonly @focus="afmtOpen=true" @click.stop="afmtOpen=true" @blur="delay(()=>{ afmtOpen=false },120)" />
          <svg class="afmt-chev" viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>
          <div v-show="afmtOpen" class="afmt-drop">
            <div class="afmt-label">{{ t('dlg.groupUpstream') }}</div>
            <button v-for="o in API_FORMATS" :key="o" type="button" :class="{ on: type === o }" @mousedown.prevent="type=o; afmtOpen=false">{{ o }}</button>
          </div>
        </div>
        <div class="field-hint">{{ pathHint ? t('prov.path', { path: pathHint }) : '' }}</div>
      </div>
      <div class="field"><label>{{ t('dlg.baseUrl') }}</label><input v-model="baseUrl" :placeholder="baseHint"></div>
      <div class="field">
        <label>{{ t('dlg.apiKey') }} <span class="hint">{{ t('dlg.apiKeyHint') }}</span></label>
        <div class="field-key" :class="{ revealed: keyRevealed }">
          <input v-model="apiKey" :type="keyRevealed ? 'text' : 'password'" placeholder="sk-...">
          <button class="eye-btn" type="button" @click="keyRevealed = !keyRevealed">
            <svg class="eye-open" viewBox="0 0 24 24" width="15" height="15"><path fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" d="M1.5 12S5 5.5 12 5.5 22.5 12 22.5 12 19 18.5 12 18.5 1.5 12 1.5 12Z"/><circle cx="12" cy="12" r="3.2" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>
            <svg class="eye-closed" viewBox="0 0 24 24" width="15" height="15"><path fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" d="M3 3l18 18M10.6 10.6A3.2 3.2 0 0 0 12 15.2a3.2 3.2 0 0 0 3.2-3.2M9.4 5.9A9.7 9.7 0 0 1 12 5.5c7 0 10.5 6.5 10.5 6.5a18 18 0 0 1-3.3 4.2M6.1 6.1C3.7 7.6 1.5 12 1.5 12S5 18.5 12 18.5c1.2 0 2.3-.2 3.3-.5"/></svg>
          </button>
        </div>
      </div>
      <div class="field"><label>{{ t('dlg.aliases') }} <span class="hint">{{ t('dlg.aliasesHint') }}</span></label><textarea v-model="aliases" rows="3" placeholder="my-model = real-model"></textarea></div>
      <div class="dialog-foot">
        <button class="btn btn-danger btn-sm" type="button" @click="reset">{{ t('dlg.reset') }}</button>
        <div class="right">
          <span class="error-msg">{{ error }}</span>
          <button class="btn btn-ghost btn-sm" type="button" @click="close">{{ t('dlg.cancel') }}</button>
          <button class="btn btn-primary btn-sm" type="button" @click="save">{{ t('dlg.save') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* API 格式自定义下拉：原生 select 弹出列表无法自定义样式，改为绝对定位面板
（.dialog 带 backdrop-filter，禁止用 fixed 定位弹层） */
.afmt-sel{position:relative}
.afmt-sel > input{padding-right:28px;cursor:pointer}
.afmt-chev{position:absolute;right:9px;top:50%;transform:translateY(-50%);color:var(--t3);pointer-events:none}
.afmt-drop{position:absolute;z-index:50;top:calc(100% + 4px);left:0;right:0;max-height:180px;overflow:auto;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius-sm);box-shadow:0 8px 24px rgba(0,0,0,.35);padding:3px 0;scrollbar-width:thin;scrollbar-color:var(--border-hover) transparent}
.afmt-label{padding:2px 10px 4px;font-size:9px;font-weight:700;letter-spacing:.04em;color:var(--t3);text-transform:uppercase}
.afmt-drop > button{display:block;width:100%;text-align:left;padding:5px 10px;background:transparent;border:0;color:var(--text);font-size:12px;cursor:pointer;border-radius:4px;margin:0 0 0 0}
.afmt-drop > button:hover{background:var(--accent-soft);color:var(--accent)}
.afmt-drop > button.on{color:var(--accent);font-weight:600}
.afmt-drop > button.on::after{content:'✓';float:right;font-weight:700}
</style>
