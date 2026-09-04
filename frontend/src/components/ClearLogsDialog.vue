<script setup>
import { ui, refreshLogs } from '../store'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'

function close() { ui.clearLogsDialog.open = false }

async function confirm() {
  close()
  if (!api.ClearLogs) return
  try {
    const r = await api.ClearLogs()
    if (r && r.ok === false) toast(t('common.fail', { msg: r.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
  refreshLogs()
}
</script>

<template>
  <div class="dialog-overlay" :class="{ open: ui.clearLogsDialog.open }">
    <div class="dialog" style="width:280px">
      <h3>{{ t('dlg.clearLogsTitle') }}</h3>
      <p class="delete-desc">{{ t('dlg.clearLogsDesc') }}</p>
      <div class="dialog-foot">
        <span></span>
        <div class="right">
          <button class="btn btn-ghost btn-sm" type="button" @click="close">{{ t('dlg.cancel') }}</button>
          <button class="btn btn-danger btn-sm" type="button" @click="confirm">{{ t('dlg.confirm') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
