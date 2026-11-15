<script setup>
import { ref, reactive, onMounted } from 'vue'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'
import { delay, copyText } from '../utils'
import { targetLogo } from './sync/shared'

// ---------- MCP ----------
const mcps = ref([])
const mcpBase = ref('')
// 表单即标准 MCP 服务参数：stdio → command/args/env；sse/http → url/headers；
// 其余自定义键（transport 等）收进「其它字段」，保存时平铺回配置文件
const mcpForm = reactive({ idx: -1, name: '', desc: '', type: 'stdio', command: '', argsText: '[]', envText: '', url: '', headersText: '', extraText: '' })
const mcpEdit = ref(false)
// 类型自定义下拉（原生 select 的弹出列表无法自定义样式）
const MCP_TYPES = ['stdio', 'sse', 'http']
const mtypOpen = ref(false)
// 编辑开始时记录原名称，改名保存后用于清理旧配置文件
let mcpOriginalName = ''
// 已注入分布 {服务名: [客户端id]}（列名旁显示客户端小图标）与客户端名表
const injected = ref({})
const clientNames = ref({})
async function loadMcps() {
  try { const r = (await api.GetMcps()) || {}; mcpBase.value = r.baseUrl || ''; mcps.value = r.servers || [] } catch (e) { mcps.value = [] }
  try { injected.value = (await api.GetMcpInjectedTargets()) || {} } catch (e) { injected.value = {} }
}
function clientName(id) { return clientNames.value[id] || id }
const selectedName = ref('')
function selectMcp(name) { selectedName.value = name }
// 复制该 MCP 的标准 mcpServers JSON（含 name 外壳，可直接粘贴进客户端配置文件）
function copyMcpJson(s, ev) {
  const entry = { type: s.type }
  if (s.type === 'stdio') {
    entry.command = s.command || ''
    if (Array.isArray(s.args) && s.args.length) entry.args = s.args
    if (s.env && Object.keys(s.env).length) entry.env = s.env
  } else {
    entry.url = s.url || ''
    if (s.headers && Object.keys(s.headers).length) entry.headers = s.headers
  }
  const skip = new Set(['name', 'desc', 'type', 'command', 'args', 'url', 'env', 'headers'])
  for (const [k, v] of Object.entries(s)) if (!skip.has(k)) entry[k] = v
  copyText(JSON.stringify({ mcpServers: { [s.name]: entry } }, null, 2), ev?.currentTarget)
}
// 一键注入：全部 MCP 服务 → 全部支持注入的客户端（替换式），不再弹选择窗口
async function injectAll() {
  try {
    const [tg, mp] = await Promise.all([api.ListTargets(), api.GetMcps()])
    const ids = (Array.isArray(tg) ? tg : []).filter(x => x.mcpSupported && !x.placeholder).map(x => x.id)
    const names = ((mp && mp.servers) || []).map(s2 => s2.name)
    if (!ids.length || !names.length) { toast(t('mcp.mappingPickBoth')); return }
    const res = await api.SyncMcpMapping(ids, names)
    const list = Array.isArray(res) ? res : []
    const fails = list.filter(r => r.error)
    if (fails.length) toast(t('mcp.mappingFail', { msg: fails.map(f => f.name + ': ' + f.error).join('; ') }))
    else toast(t('mcp.mappingSaved'))
    await loadMcps()
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}
// 点击已注入客户端图标：在系统文件管理器中打开该客户端 MCP 配置文件位置
// 一键踢出：清空所有客户端 MCP 配置文件里的全部服务（整体替换法，其它内容保留）
async function ejectAll() {
  try {
    const r = await api.EjectAllMcp()
    if (r && r.ok) {
      toast(r.error ? t('mcp.ejectPartial', { n: r.count, msg: r.error }) : t('mcp.ejected', { n: r.count }))
      await loadMcps()
    } else {
      toast(t('common.fail', { msg: (r && r.error) || '' }))
    }
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}
// 踢出：把当前选中的 MCP 从该客户端配置文件中剔除（整体替换法，其余服务保留）
async function kickMcp(cid) {
  try {
    const r = await api.RemoveMcpFromClient(cid, selectedName.value)
    if (r && r.ok) { toast(t('mcp.kicked', { name: clientName(cid) })); await loadMcps() }
    else toast(t('common.fail', { msg: (r && r.error) || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}
async function openMcpLocation(id) {
  try {
    const r = await api.OpenClientMcpLocation(id)
    if (r && !r.ok) toast(t('common.fail', { msg: r.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
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
    mcpForm.type = s.type || 'stdio'
    mcpForm.command = s.command || ''
    mcpForm.argsText = JSON.stringify(s.args || [], null, 2)
    mcpForm.url = s.url || ''
    const rest = { ...s }
    for (const k of ['name', 'desc', 'type', 'command', 'args', 'url']) delete rest[k]
    mcpForm.envText = rest.env && typeof rest.env === 'object' && !Array.isArray(rest.env) ? JSON.stringify(rest.env, null, 2) : ''
    delete rest.env
    mcpForm.headersText = rest.headers && typeof rest.headers === 'object' && !Array.isArray(rest.headers) ? JSON.stringify(rest.headers, null, 2) : ''
    delete rest.headers
    mcpForm.extraText = Object.keys(rest).length ? JSON.stringify(rest, null, 2) : ''
  }
  else {
    mcpOriginalName = ''
    mcpForm.idx = -1; mcpForm.name = ''; mcpForm.desc = ''; mcpForm.type = 'stdio'
    mcpForm.command = ''; mcpForm.argsText = '[]'; mcpForm.envText = ''
    mcpForm.url = ''; mcpForm.headersText = ''; mcpForm.extraText = ''
  }
  mcpEdit.value = true
}
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
// 解析对象字段文本框（env / headers / 其它），空串视作 {}，失败返回 null
function parseObj(txt, label) {
  const s = (txt || '').trim()
  if (!s) return {}
  try {
    const v = JSON.parse(s)
    if (!v || typeof v !== 'object' || Array.isArray(v)) throw new Error('not object')
    return v
  } catch (e) { toast(t('common.fail', { msg: label })); return null }
}
async function saveMcp() {
  if (!mcpForm.name.trim()) { toast(t('mcp.nameRequired')); return }
  const args = parseArgs()
  if (args === null) return
  const env = parseObj(mcpForm.envText, 'env')
  if (env === null) return
  const headers = parseObj(mcpForm.headersText, 'headers')
  if (headers === null) return
  const extra = parseObj(mcpForm.extraText, t('mcp.extraCfg'))
  if (extra === null) return
  const type = mcpForm.type
  if (type === 'stdio' && !mcpForm.command.trim()) { toast(t('mcp.commandRequired')); return }
  if (type !== 'stdio' && !mcpForm.url.trim()) { toast(t('mcp.urlRequired')); return }
  // 组装标准 MCP 服务参数并写入对应 JSON 文件
  const doc = { name: mcpForm.name.trim(), desc: mcpForm.desc.trim(), type }
  if (type === 'stdio') {
    doc.command = mcpForm.command.trim()
    if (args.length) doc.args = args
    if (Object.keys(env).length) doc.env = env
  } else {
    doc.url = mcpForm.url.trim()
    if (Object.keys(headers).length) doc.headers = headers
  }
  for (const [k, v] of Object.entries(extra)) doc[k] = v
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
// ---- MCP 配置目录 ----
const mcpDir = ref('')
onMounted(async () => {
  try { if (api.GetMcpDir) mcpDir.value = (await api.GetMcpDir()) || '' } catch (e) {}
})
async function openDir() {
  // 在系统文件管理器中打开 MCP 配置目录（目录不存在时后端先创建）
  try {
    const r = await api.OpenMcpDir()
    if (r && r.ok === false) toast(t('common.fail', { msg: r.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}
onMounted(async () => {
  loadMcps()
  try {
    const tg = await api.ListTargets()
    const m = {}
    for (const x of (Array.isArray(tg) ? tg : [])) m[x.id] = x.name
    clientNames.value = m
  } catch (e) {}
})
defineExpose({ openMcp, injectAll, ejectAll, openDir, mcpDir })

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
  <div class="mcps-split">
    <!-- 左：MCP 列表（点行选中，右侧联动显示其注入分布） -->
    <div class="mcps-left">
      <div v-if="!mcps.length" class="empty-tip">{{ t('mcp.empty') }}</div>
      <div v-for="s in mcps" :key="rowKey(s)" class="skill-row" :class="{ sel: selectedName === s.name }">
        <div class="skill-main" @click="selectMcp(s.name)">
          <div class="skill-name">{{ s.name }} <span class="mcp-type">{{ s.type }}</span></div>
          <div class="skill-desc">{{ s.desc || s.command || s.url || '' }}</div>
        </div>
        <button class="btn" @click.stop="copyMcpJson(s, $event)">{{ t('common.copy') }}</button>
        <button class="btn" @click.stop="openMcp(s)">{{ t('common.edit') }}</button>
        <button class="btn btn-danger" @click.stop="askDelete(s)">{{ t('common.delete') }}</button>
      </div>
    </div>
    <!-- 右：选中 MCP 被注入的客户端（点卡片打开其 MCP 配置文件位置） -->
    <div class="mcps-right">
      <div class="mcp-right-head">{{ t('mcp.rightTitle') }}</div>
      <template v-if="selectedName">
        <div v-if="(injected[selectedName] || []).length" class="mcp-right-grid">
          <div v-for="cid in injected[selectedName]" :key="cid" class="mcp-right-card"
            :title="t('mcp.injectedTo', { name: clientName(cid) })" @click="openMcpLocation(cid)">
            <img v-if="targetLogo(cid)" :src="targetLogo(cid)" alt="">
            <span class="mcp-right-cli">{{ clientName(cid) }}</span>
            <button type="button" class="mcp-kick" :title="t('mcp.kickTip')"
              @click.stop="kickMcp(cid)">{{ t('mcp.kick') }}</button>
          </div>
        </div>
        <div v-else class="mcp-right-empty">{{ t('mcp.rightEmpty') }}</div>
      </template>
      <div v-else class="mcp-right-empty">{{ t('mcp.rightNone') }}</div>
    </div>
  </div>

    <!-- Teleport 到 body：深色主题 .panel 带 backdrop-filter，会成为 fixed 子元素的包含块，
         留在面板内时遮罩只覆盖面板、弹窗按面板居中；脱离面板才覆盖整个窗口。
         表单弹窗不随点击遮罩关闭——误点外侧会丢掉整套已填参数，关闭只走「取消/保存」 -->
    <Teleport to="body">
    <div v-if="mcpEdit" class="dialog-overlay open">
      <div class="dialog" style="width:min(560px, calc(100% - 24px))">
        <h3>{{ mcpForm.idx >= 0 ? mcpForm.name || t('common.edit') : t('mcp.add') }}
          <span style="margin-left:auto;display:flex;align-items:center;gap:6px">
            <button class="btn btn-ghost btn-sm" type="button" @click="mcpEdit=false">{{ t('common.cancel') }}</button>
            <button class="btn btn-green btn-sm" type="button" @click="saveMcp">{{ t('common.save') }}</button>
          </span>
        </h3>
        <div class="field-row"><div class="field"><label>{{ t('mcp.name') }}</label><input v-model="mcpForm.name"></div><div class="field"><label>{{ t('mcp.type') }}</label><div class="mtyp-sel"><input :value="mcpForm.type" readonly @focus="mtypOpen=true" @click.stop="mtypOpen=true" @blur="delay(()=>{ mtypOpen=false },120)" /><svg class="mtyp-chev" viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg><div v-show="mtypOpen" class="mtyp-drop"><button v-for="o in MCP_TYPES" :key="o" type="button" :class="{ on: mcpForm.type === o }" @mousedown.prevent="mcpForm.type=o; mtypOpen=false">{{ o }}</button></div></div></div></div>
        <div class="field"><label>{{ t('mcp.desc') }}</label><input v-model="mcpForm.desc" :placeholder="t('mcp.descPlaceholder')"></div>
        <!-- 标准 MCP 参数按类型分组：stdio → command/args/env；sse/http → url/headers -->
        <template v-if="mcpForm.type === 'stdio'">
          <div class="field"><label>{{ t('mcp.command') }}</label><input v-model="mcpForm.command" :placeholder="t('mcp.commandPh')" style="font-family:var(--mono)"></div>
          <div class="field-row">
            <div class="field"><label>{{ t('mcp.argsCfg') }}</label><textarea v-model="mcpForm.argsText" rows="3" style="font-family:var(--mono);resize:vertical" :placeholder="'[&quot;-y&quot;, &quot;mcp-server&quot;]'"></textarea></div>
            <div class="field"><label>{{ t('mcp.envCfg') }}</label><textarea v-model="mcpForm.envText" rows="3" style="font-family:var(--mono);resize:vertical" :placeholder="'{&quot;API_KEY&quot;: &quot;…&quot;}'"></textarea></div>
          </div>
        </template>
        <template v-else>
          <div class="field"><label>{{ t('mcp.url') }}</label><input v-model="mcpForm.url" placeholder="https://…" style="font-family:var(--mono)"></div>
          <div class="field"><label>{{ t('mcp.headersCfg') }}</label><textarea v-model="mcpForm.headersText" rows="3" style="font-family:var(--mono);resize:vertical" :placeholder="'{&quot;Authorization&quot;: &quot;Bearer …&quot;}'"></textarea></div>
        </template>
        <div class="field"><label>{{ t('mcp.extraCfg') }}</label><textarea v-model="mcpForm.extraText" rows="2" style="font-family:var(--mono);resize:vertical" :placeholder="t('mcp.extraPh')"></textarea></div>
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
    </Teleport>
</template>

<style scoped>
/* 左右分栏：左 MCP 列表 / 右 选中项的注入客户端分布 */
.mcps-split{display:flex;align-items:stretch;min-height:100%}
.mcps-left{flex:1;min-width:0;display:flex;flex-direction:column;gap:6px;border-right:1px solid var(--border);padding-right:12px}
.skill-row.sel{border-color:var(--accent)}
.mcps-right{flex:1;min-width:0;padding-left:12px;align-self:stretch}
.mcp-right-head{font-size:10px;color:var(--t3);letter-spacing:.04em;margin-bottom:6px}
.mcp-right-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(130px,1fr));gap:6px}
.mcp-right-card{position:relative;display:flex;align-items:center;gap:6px;padding:6px 8px;border:1px solid var(--border);border-radius:6px;background:transparent;color:var(--text);font-size:11.5px;cursor:pointer}
.mcp-right-card:hover{border-color:var(--accent);color:var(--accent)}
.mcp-right-card img{width:16px;height:16px;object-fit:contain;flex:none}
.mcp-right-cli{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
/* 踢出按钮：滑入卡片才出现（右侧浮层），点击从该客户端剔除当前 MCP */
.mcp-kick{display:none;position:absolute;right:4px;top:50%;transform:translateY(-50%);padding:1px 7px;border:1px solid var(--red,var(--red));border-radius:999px;background:var(--surface);color:var(--red,#e06c75);font-size:9.5px;font-weight:700;cursor:pointer;font-family:inherit;line-height:1.5}
.mcp-right-card:hover .mcp-kick{display:inline-block}
.mcp-kick:hover{background:var(--red-soft,rgba(200,60,60,.15))}
.mcp-right-card:hover .mcp-right-cli{max-width:calc(100% - 46px)}
.mcp-right-cli{max-width:100%;transition:max-width .12s}
.mcp-right-empty{color:var(--t3);font-size:11px;line-height:1.6}
/* MCP 类型自定义下拉：原生 select 弹出列表无法自定义样式，改为绝对定位面板 */
.mtyp-sel{position:relative}
.mtyp-sel > input{padding-right:28px;cursor:pointer}
.mtyp-chev{position:absolute;right:9px;top:50%;transform:translateY(-50%);color:var(--t3);pointer-events:none}
.mtyp-drop{position:absolute;z-index:50;top:calc(100% + 4px);left:0;right:0;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius-sm);box-shadow:0 8px 24px rgba(0,0,0,.35);padding:3px 0}
.mtyp-drop > button{display:block;width:100%;text-align:left;padding:5px 10px;background:transparent;border:0;color:var(--text);font-size:12px;cursor:pointer}
.mtyp-drop > button:hover{background:var(--accent-soft);color:var(--accent)}
.mtyp-drop > button.on{color:var(--accent);font-weight:600}
.mtyp-drop > button.on::after{content:'✓';float:right;font-weight:700}
</style>
