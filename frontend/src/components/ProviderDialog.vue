<script setup>
import { ref, computed, watch } from 'vue'
import { ui, state, refresh } from '../store'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'

const PF_BASE_HINT = {
  'openai': 'https://api.deepseek.com',
  'anthropic': 'https://api.anthropic.com',
  'gemini': 'https://generativelanguage.googleapis.com',
  'gemini-openai': 'https://generativelanguage.googleapis.com',
  'response': 'https://api.openai.com',
}
const PF_PATH_HINT = {
  'openai': '/v1/chat/completions',
  'gemini-openai': '/v1beta/openai/chat/completions',
  'response': '/v1/responses',
  'anthropic': '/v1/messages',
  'gemini': '/v1beta/models/{model}:generateContent',
}

const name = ref('')
const type = ref('openai')
const baseUrl = ref('')
const apiKey = ref('')
const aliases = ref('')
const error = ref('')
const keyRevealed = ref(false)

const isEdit = computed(() => !!ui.providerDialog.provider)
const title = computed(() => isEdit.value ? t('prov.editTitle') : t('prov.addTitle'))
const baseHint = computed(() => PF_BASE_HINT[type.value] || 'https://...')
const pathHint = computed(() => PF_PATH_HINT[type.value] || '')

// 打开时初始化表单
watch(() => ui.providerDialog.open, (open) => {
  if (!open) return
  const p = ui.providerDialog.provider
  name.value = p?.name || ''
  type.value = p?.type || 'openai'
  baseUrl.value = p?.baseUrl || ''
  apiKey.value = p?.apiKey || ''
  aliases.value = p?.aliases ? Object.entries(p.aliases).map(([k, v]) => k + ' = ' + v).join('\n') : ''
  error.value = ''
  keyRevealed.value = false
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
  const base = baseUrl.value.trim()
  if (!nm) { error.value = t('prov.enterName'); return }
  if (!/^https?:\/\//.test(base)) { error.value = t('prov.baseUrlErr'); return }

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
    name: nm, type: type.value, baseUrl: base, apiKey: apiKey.value,
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
        <select v-model="type">
          <optgroup :label="t('dlg.groupUpstream')">
            <option value="openai">openai</option>
            <option value="gemini-openai">gemini-openai</option>
            <option value="response">response</option>
            <option value="anthropic">anthropic</option>
            <option value="gemini">gemini</option>
          </optgroup>
        </select>
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
