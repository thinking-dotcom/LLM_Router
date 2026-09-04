<script setup>
import { ref, reactive, watch, onMounted } from 'vue'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'

// ---------- MCP ----------
const mcps = ref([])
const mcpBase = ref('')
const mcpForm = reactive({ idx: -1, name: '', desc: '', type: 'sse', argsText: '[]', json: '' })
const mcpEdit = ref(false)
// 编辑开始时记录原名称，改名保存后用于清理旧配置文件
let mcpOriginalName = ''
async function loadMcps() {
  try { const r = (await api.GetMcps()) || {}; mcpBase.value = r.baseUrl || ''; mcps.value = r.servers || [] } catch (e) { mcps.value = [] }
}
// 静默持久化到 data/mcp（baseUrl 保留原值），返回是否成功
async function persist() {
  try {
    const r = await api.SaveMcps({ baseUrl: mcpBase.value, servers: mcps.value })
    if (!r || !r.ok) { toast(t('common.fail', { msg: r?.error || '' })); return false }
    return true
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })); return false }
}
function openMcp(s = null) {
  if (s) {
    mcpOriginalName = s.name
    mcpForm.idx = mcps.value.indexOf(s); mcpForm.name = s.name; mcpForm.desc = s.desc || ''
    // 内容 = 配置文件 JSON（name/desc/args 由表单管理，不重复显示）
    const { name: _n, desc: _d, args: _a, ...rest } = s
    mcpForm.json = JSON.stringify(rest, null, 2)
    mcpForm.argsText = JSON.stringify(s.args || [], null, 2)
    mcpForm.type = s.type || 'sse'
  }
  else {
    mcpOriginalName = ''
    mcpForm.idx = -1; mcpForm.name = ''; mcpForm.desc = ''
    mcpForm.json = '{\n  "url": ""\n}'
    mcpForm.argsText = '[]'
    mcpForm.type = 'sse'
  }
  mcpEdit.value = true
}
// 类型切换时自动同步“内容”里的 type 字段
watch(() => mcpForm.type, (v) => {
  try {
    const o = JSON.parse(mcpForm.json || '{}')
    if (o.type !== v) { o.type = v; mcpForm.json = JSON.stringify(o, null, 2) }
  } catch (e) {}
})
// 解析参数配置文本框（JSON 数组），失败返回 null
function parseArgs() {
  const txt = (mcpForm.argsText || '').trim()
  if (!txt) return []
  try {
    const v = JSON.parse(txt)
    if (!Array.isArray(v)) throw new Error('not array')
    return v.map(String)
  } catch (e) { toast(t('common.fail', { msg: 'args' })); return null }
}
// 从“内容”JSON 提取字段（解析失败返回 null）
function parseJson() {
  try { return JSON.parse(mcpForm.json) } catch (e) { toast(t('common.fail', { msg: e?.message || 'JSON' })); return null }
}
async function saveMcp() {
  if (!mcpForm.name.trim()) { toast(t('mcp.nameRequired')); return }
  // 校验“内容”JSON 与参数配置
  const obj = parseJson()
  if (obj === null) return
  const args = parseArgs()
  if (args === null) return
  const type = obj.type || mcpForm.type || 'sse'
  if (type !== 'stdio' && (!obj.url || !String(obj.url).trim())) { toast(t('mcp.urlRequired')); return }
  // 组装完整文件内容：名称/描述/参数 + “内容”原文字段，写入对应 JSON 文件
  const doc = { name: mcpForm.name.trim(), desc: mcpForm.desc.trim(), ...obj, type, args }
  if (doc.args.length === 0) delete doc.args
  let r
  try { r = await api.SaveMcpFile(mcpForm.name.trim(), JSON.stringify(doc, null, 2)) } catch (e) { toast(t('common.fail', { msg: e?.message || '' })); return }
  if (!r || !r.ok) { toast(t('common.fail', { msg: r?.error || '' })); return }
  // 改名后旧配置文件仍残留在磁盘，走全量持久化清理（SaveMcps 会删除列表外文件）
  if (mcpOriginalName && mcpOriginalName !== doc.name) {
    const oldIdx = mcps.value.findIndex(x => x.name === mcpOriginalName)
    if (oldIdx >= 0) mcps.value.splice(oldIdx, 1)
    mcps.value.push(doc)
    const okPersist = await persist()
    if (!okPersist) {
      // 新名文件已落盘成功，只是旧文件清理失败：明确提示并关闭弹窗，
      // 避免弹窗滞留导致再次保存时对新名重复执行改名清理逻辑
      toast(t('common.fail', { msg: '已保存新配置文件，但旧文件清理失败，可手动删除旧名 .json 文件' }))
      mcpEdit.value = false
      await loadMcps()
      return
    }
  }
  mcpEdit.value = false
  await loadMcps()
}
// 删除确认（应用内对话框，替代原生 confirm）
const deleting = ref(null)
function askDelete(s) { deleting.value = s }
async function doDelete() {
  const s = deleting.value
  deleting.value = null
  if (!s) return
  const idx = mcps.value.indexOf(s)
  if (idx < 0) {
    // 目标已被列表刷新替换（如持久化失败后重载）：不做删除，避免 splice(-1,1) 误删末项
    await loadMcps()
    return
  }
  mcps.value.splice(idx, 1)
  // 持久化失败时从后端恢复真实状态，避免 UI 与后端背离
  if (!(await persist())) await loadMcps()
}
onMounted(loadMcps)
defineExpose({ openMcp })

// 列表行稳定唯一 key：名称不保证全局唯一，用对象自增 id
const rowIds = new WeakMap()
let rowSeq = 0
function rowKey(s) {
  let id = rowIds.get(s)
  if (id === undefined) { id = ++rowSeq; rowIds.set(s, id) }
  return id
}
</script>

<template>
  <div>
    <div v-if="!mcps.length" class="empty-tip">{{ t('mcp.empty') }}</div>
    <div v-for="s in mcps" :key="rowKey(s)" class="skill-row">
      <div class="skill-main" @click="openMcp(s)">
        <div class="skill-name">{{ s.name }} <span class="mcp-type">{{ s.type }}</span></div>
        <div class="skill-desc">{{ s.desc || s.command || s.url || '' }}</div>
      </div>
      <button class="btn btn-danger" @click.stop="askDelete(s)">{{ t('common.delete') }}</button>
    </div>

    <div v-if="mcpEdit" class="dialog-overlay open" @click.self="mcpEdit=false">
      <div class="dialog" style="width:min(560px, calc(100% - 24px))">
        <h3>{{ mcpForm.idx >= 0 ? mcpForm.name || t('common.edit') : t('mcp.add') }}
          <span style="margin-left:auto;display:flex;align-items:center;gap:6px">
            <button class="btn btn-ghost btn-sm" type="button" @click="mcpEdit=false">{{ t('common.cancel') }}</button>
            <button class="btn btn-green btn-sm" type="button" @click="saveMcp">{{ t('common.save') }}</button>
          </span>
        </h3>
        <div class="field-row"><div class="field"><label>{{ t('mcp.name') }}</label><input v-model="mcpForm.name"></div><div class="field"><label>{{ t('mcp.type') }}</label><select v-model="mcpForm.type"><option value="sse">sse</option><option value="stdio">stdio</option><option value="http">http</option></select></div></div>
        <div class="field"><label>{{ t('mcp.desc') }}</label><input v-model="mcpForm.desc" :placeholder="t('mcp.descPlaceholder')"></div>
        <div class="field"><label>{{ t('mcp.argsCfg') }}</label><textarea v-model="mcpForm.argsText" rows="4" style="font-family:var(--mono);resize:vertical" :placeholder="'[&quot;--port&quot;, &quot;3000&quot;]'"></textarea></div>
        <div class="field"><label>{{ t('mcp.content') }}</label><textarea v-model="mcpForm.json" rows="10" style="font-family:var(--mono);resize:vertical;min-height:120px"></textarea></div>
      </div>
    </div>

    <!-- 删除确认（应用内样式） -->
    <div class="dialog-overlay" :class="{ open: !!deleting }" @click.self="deleting=null">
      <div class="dialog" style="width:min(420px, calc(100% - 24px))">
        <h3>{{ t('dlg.deleteTitle') }}</h3>
        <p class="delete-desc">{{ t('mcp.confirmDelete', { name: deleting?.name || '' }) }}</p>
        <div class="dialog-foot">
          <span></span>
          <div class="right">
            <button class="btn btn-ghost btn-sm" type="button" @click="deleting=null">{{ t('dlg.cancel') }}</button>
            <button class="btn btn-danger btn-sm" type="button" @click="doDelete">{{ t('dlg.confirmDeleteBtn') }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
