<script setup>
import { computed } from 'vue'
import { ui, state, refresh } from '../store'
import { t } from '../i18n'
import { toast } from '../toast'
import { esc } from '../utils'
import api from '../api'

const name = computed(() => ui.deleteDialog.provider?.name || '')
const descHtml = computed(() => t('dlg.deleteDesc', { name: esc(name.value) }))

function close() { ui.deleteDialog.open = false }

async function confirm() {
  const p = ui.deleteDialog.provider
  close()
  if (!p) return
  // 配置尚未加载完成时先拉取，避免用空对象覆盖清空其余 providers
  if (!state.config) await refresh(true)
  const cfg = JSON.parse(JSON.stringify(state.config || {}))
  cfg.providers = (cfg.providers || []).filter(x => x.id !== p.id)
  try {
    const r = await api.SaveConfig(cfg)
    if (!r || !r.ok) { toast(t('common.fail', { msg: r?.error || '' })); return }
    await refresh(true)
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}
</script>

<template>
  <div class="dialog-overlay" :class="{ open: ui.deleteDialog.open }">
    <div class="dialog" style="width:280px">
      <h3>{{ t('dlg.deleteTitle') }}</h3>
      <p class="delete-desc" v-html="descHtml"></p>
      <div class="dialog-foot">
        <span></span>
        <div class="right">
          <button class="btn btn-ghost btn-sm" type="button" @click="close">{{ t('dlg.cancel') }}</button>
          <button class="btn btn-danger btn-sm" type="button" @click="confirm">{{ t('dlg.confirmDeleteBtn') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
