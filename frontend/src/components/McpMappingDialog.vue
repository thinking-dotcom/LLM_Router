<script setup>
import { ref, watch } from 'vue'
import { ui } from '../store'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'

const targets = ref([])   // 全部目标客户端
const mcps = ref([])      // 全部 MCP 服务
const selTargets = ref(new Set())
const selServers = ref(new Set())

// 打开时加载数据并清空选择
watch(() => ui.mcpMappingDialog.open, async (open) => {
  if (!open) return
  selTargets.value = new Set()
  selServers.value = new Set()
  try {
    const [tg, mp] = await Promise.all([api.ListTargets(), api.GetMcps()])
    targets.value = Array.isArray(tg) ? tg : []
    mcps.value = (mp && Array.isArray(mp.servers)) ? mp.servers : []
  } catch (e) {
    targets.value = []; mcps.value = []
  }
})

function toggleTarget(id, enabled) {
  if (!enabled) return
  const next = new Set(selTargets.value)
  next.has(id) ? next.delete(id) : next.add(id)
  selTargets.value = next
}
function toggleServer(name) {
  const next = new Set(selServers.value)
  next.has(name) ? next.delete(name) : next.add(name)
  selServers.value = next
}

function close() { ui.mcpMappingDialog.open = false }

async function save() {
  if (!selTargets.value.size || !selServers.value.size) {
    toast(t('mcp.mappingPickBoth')); return
  }
  try {
    const res = await api.SyncMcpMapping([...selTargets.value], [...selServers.value])
    const list = Array.isArray(res) ? res : []
    const fails = list.filter(r => r.error)
    if (fails.length) {
      toast(t('mcp.mappingFail', { msg: fails.map(f => `${f.name}: ${f.error}`).join('; ') }))
    } else {
      toast(t('mcp.mappingSaved'))
    }
    close()
  } catch (e) {
    toast(t('common.fail', { msg: e?.message || '' }))
  }
}

// 客户端 logo（与 Clients.vue 同源：frontend/public/logos/）
const TARGET_LOGOS = {
  'claude-code': '/logos/claude.svg',
  'claude-desktop': '/logos/claude.svg',
  'gemini': '/logos/gemini.svg',
  'opencode': '/logos/opencode.svg',
  'grok-build': '/logos/grok.svg',
  'pi': '/logos/pi.svg',
  'codex': '/logos/codex.svg',
  'cline': '/logos/cline.svg',
  'hermes': '/logos/hermes.svg',
}
function targetLogo(id) { return TARGET_LOGOS[id] || '' }
function initials(name) { return (name || '?').trim().slice(0, 2).toUpperCase() }
</script>

<template>
  <div class="dialog-overlay" :class="{ open: ui.mcpMappingDialog.open }">
    <div class="dialog" style="width:min(560px, calc(100% - 24px))">
      <h3>{{ t('mcp.mappingTitle') }}
        <span style="margin-left:auto;display:flex;align-items:center;gap:6px">
          <button class="btn btn-green btn-sm" type="button" @click="save">{{ t('dlg.save') }}</button>
          <button class="btn btn-ghost btn-sm" type="button" @click="close">{{ t('mcp.mappingClose') }}</button>
        </span>
      </h3>
      <div class="map-split">
        <!-- 左：客户端图标 -->
        <div class="map-pane">
          <div v-if="!targets.length" class="map-empty">—</div>
          <div class="map-grid">
            <div
              v-for="tg in targets" :key="tg.id"
              class="map-client"
              :class="{ selected: selTargets.has(tg.id), disabled: !tg.mcpSupported || tg.placeholder }"
              :title="tg.mcpSupported ? tg.name : tg.name + '（' + t('mcp.mappingNotMcp') + '）'"
              @click="toggleTarget(tg.id, tg.mcpSupported && !tg.placeholder)"
            >
              <img v-if="targetLogo(tg.id)" class="map-avatar" :src="targetLogo(tg.id)" :alt="tg.name">
              <span v-else class="map-avatar map-avatar-fallback">{{ initials(tg.name) }}</span>
              <span class="map-client-name">{{ tg.name }}</span>
            </div>
          </div>
        </div>
        <!-- 右：MCP 名称列表 -->
        <div class="map-pane map-right">
          <div v-if="!mcps.length" class="map-empty">{{ t('mcp.empty') }}</div>
          <div
            v-for="s in mcps" :key="s.name"
            class="map-server"
            :class="{ selected: selServers.has(s.name) }"
            @click="toggleServer(s.name)"
          >{{ s.name }}</div>
        </div>
      </div>
    </div>
  </div>
</template>
