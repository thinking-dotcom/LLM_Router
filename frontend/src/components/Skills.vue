<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
import { targetLogo } from './sync/shared'

// ---------- 技能 ----------
const skills = ref([])
const skillsDir = ref('')
// 选中态与注入分布（右侧面板，与 MCP 服务页同款交互）
const selectedName = ref('')
const agentSkills = ref([])  // 各客户端技能目录扫描结果（GetAgentSkills）
const clientIds = ref({})    // 客户端名 → id（取卡片 logo）
async function loadSkills() {
  try { skills.value = (await api.GetSkillsList()) || [] } catch (e) { skills.value = [] }
  try { if (api.GetSkillsDir) skillsDir.value = (await api.GetSkillsDir()) || '' } catch (e) {}
  loadAgentSkills()
}
async function loadAgentSkills() {
  try { agentSkills.value = (await api.GetAgentSkills()) || [] } catch (e) { agentSkills.value = [] }
  try {
    const tg = await api.ListTargets()
    const m = {}
    for (const x of (Array.isArray(tg) ? tg : [])) m[x.name] = x.id
    clientIds.value = m
  } catch (e) {}
}
function selectSkill(s) { selectedName.value = s.name }
// 选中技能已复制进哪些客户端的技能目录
const injectedClients = computed(() => {
  if (!selectedName.value) return []
  return agentSkills.value.filter(g => (g.skills || []).some(x => x.name === selectedName.value))
})
function clientIdOf(g) { return clientIds.value[g.name] || '' }
// 打开该客户端技能目录中选中技能的位置（文件管理器选中）
function openClientSkillDir(g) {
  const base = (g.path || '').replace(/[\\/]+$/, '')
  const sep = base.includes('\\') || /^[a-zA-Z]:/.test(base) ? '\\' : '/'
  const p = base + sep + selectedName.value
  api.OpenFileLocation(p)
    .then(r => { if (r && r.ok === false) toast(t('common.fail', { msg: r.error || '' })) })
    .catch(e => toast(t('common.fail', { msg: e?.message || '' })))
}
const editing = ref(false)
const editName = ref('')
const editContent = ref('')
const isNew = ref(false)
async function openSkill(s = null) {
  if (s) {
    isNew.value = false; editName.value = s.name
    try { editContent.value = (await api.GetSkillContent(s.name)) || '' }
    catch (e) { toast(t('common.fail', { msg: e?.message || '' })); return }
  }
  else { isNew.value = true; editName.value = ''; editContent.value = '' }
  editing.value = true
}
// 在系统文件管理器中打开技能目录
async function openDir() {
  try {
    const r = await api.OpenSkillsDir()
    if (r && r.ok === false) toast(t('common.fail', { msg: r.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}
async function saveSkill() {
  if (!editName.value.trim()) { toast(t('skills.nameRequired')); return }
  let r
  try { r = await api.SaveSkill(editName.value.trim(), editContent.value) } catch (e) { toast(t('common.fail', { msg: e?.message || '' })); return }
  toast(r && r.ok ? t('skills.saved') : t('common.fail', { msg: r?.error || '' }))
  if (r && r.ok) { editing.value = false; await loadSkills() }
}
// 删除确认（应用内对话框，替代原生 confirm）
const deleting = ref(null)
function askDelete(s) { deleting.value = s }
async function doDelete() {
  const s = deleting.value
  deleting.value = null
  if (!s) return
  try {
    const r = await api.DeleteSkill(s.name)
    toast(r && r.ok ? t('skills.deleted') : t('common.fail', { msg: r?.error || '' }))
    if (r && r.ok) {
      if (selectedName.value === s.name) selectedName.value = ''
      await loadSkills()
    }
  } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) }
}
// ---- 一键注入 / 一键踢出 ----
// 注入：把 data/skills 全部技能整体复制进各支持客户端的技能目录（后端按注入
// 清单登记副本）；踢出：按清单从客户端技能目录移除副本，客户端自有文件不动。
const syncing = ref(false)
const syncProgress = ref({ done: 0, total: 0, name: '', active: false })
function onSyncProgress(done, total, name) {
  syncProgress.value = { done, total, name, active: true }
}
onBeforeUnmount(() => EventsOff('skills:sync-progress'))
// 一键删除：确认后删除全部技能（同样本程序 data/skills 下的副本，可重新新建）
const deleteAllConfirmOpen = ref(false)
const deletingAll = ref(false)
function askDeleteAll() { if (skills.value.length) deleteAllConfirmOpen.value = true }
async function doDeleteAll() {
  deleteAllConfirmOpen.value = false
  if (deletingAll.value) return
  deletingAll.value = true
  let ok = 0, fail = 0
  try {
    for (const s of skills.value.slice()) {
      try {
        const r = await api.DeleteSkill(s.name)
        if (r && r.ok) ok++; else fail++
      } catch (e) { fail++ }
    }
    toast(t('skills.deleteAllDone', { ok, fail }))
    if (selectedName.value) selectedName.value = ''
    await loadSkills()
  } finally { deletingAll.value = false }
}
async function bulkSync(inject) {
  if (syncing.value) return
  syncing.value = true
  syncProgress.value = { done: 0, total: 0, name: '', active: true }
  try {
    const r = await api.SyncAllSkills(inject)
    const rows = (r && r.results) || []
    const okN = rows.filter(x => x.ok).length
    const failN = rows.length - okN
    const fail = failN ? t('skills.someFail', { n: failN }) : ''
    toast(inject ? t('skills.injectOk', { n: okN }) + fail : t('skills.ejectOk', { n: okN }) + fail)
    if (rows.length) syncResults.value = { inject, rows }
    loadAgentSkills() // 右侧注入分布随注入/踢出刷新
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) } finally {
    syncProgress.value = { ...syncProgress.value, active: false }
    syncing.value = false
  }
}
function injectAll() { bulkSync(true) }
// 踢出会删客户端目录里的文件，先弹确认
const ejectConfirmOpen = ref(false)
function ejectAll() { ejectConfirmOpen.value = true }
function ejectAllDo() { ejectConfirmOpen.value = false; bulkSync(false) }
// 结果弹窗：逐客户端展示技能目录与成败（syncResults 为 null 时关闭）
const syncResults = ref(null)
onMounted(() => {
  loadSkills()
  EventsOn('skills:sync-progress', onSyncProgress)
})
defineExpose({ openSkill, openDir, loadSkills, skillsDir, injectAll, ejectAll, askDeleteAll, deletingAll })
</script>

<template>
  <div>
  <div v-if="syncProgress.active && syncProgress.total" class="sync-progress">
    <div class="sync-progress-bar"><i :style="{ width: Math.round(syncProgress.done / syncProgress.total * 100) + '%' }"></i></div>
    <span class="sync-progress-text">{{ syncProgress.done }}/{{ syncProgress.total }} · {{ syncProgress.name }}</span>
  </div>
  <div class="mcps-split">
    <!-- 左：技能列表（点行选中，右侧联动显示其注入分布） -->
    <div class="mcps-left">
      <div v-if="!skills.length" class="empty-tip">{{ t('skills.empty') }}<br><span style="font-family:var(--mono);font-size:9px;color:var(--t3)">{{ skillsDir }}</span></div>
      <div v-for="s in skills" :key="s.name" class="skill-row" :class="{ sel: selectedName === s.name }">
        <!-- 行本体只做选中：编辑只能通过「编辑」按钮进入（避免误触整行弹窗） -->
        <div class="skill-main" @click="selectSkill(s)">
          <div class="skill-name">{{ s.name }}</div>
          <div class="skill-desc">{{ s.desc || t('skills.noDesc') }}</div>
        </div>
        <div style="flex:none;display:flex;gap:6px">
          <button class="btn" type="button" :title="t('skills.editTip')" @click.stop="openSkill(s)">{{ t('common.edit') }}</button>
          <button class="btn btn-danger" type="button" @click.stop="askDelete(s)">{{ t('common.delete') }}</button>
        </div>
      </div>
    </div>
    <!-- 右：选中技能已注入的客户端（点卡片在文件管理器中打开该客户端里的技能目录） -->
    <div class="mcps-right">
      <div class="mcp-right-head">{{ selectedName ? t('skills.rightTitleOf', { name: selectedName }) : t('skills.rightTitle') }}</div>
      <template v-if="selectedName">
        <div v-if="injectedClients.length" class="mcp-right-grid">
          <div v-for="g in injectedClients" :key="g.path" class="mcp-right-card"
            :title="t('skills.injectedTo', { name: g.name })" @click="openClientSkillDir(g)">
            <img v-if="targetLogo(clientIdOf(g))" :src="targetLogo(clientIdOf(g))" alt="">
            <span class="mcp-right-cli">{{ g.name }}</span>
          </div>
        </div>
        <div v-else class="mcp-right-empty">{{ t('skills.rightEmpty') }}</div>
      </template>
      <div v-else class="mcp-right-empty">{{ t('skills.rightNone') }}</div>
    </div>
  </div>

    <!-- Teleport 到 body：深色主题 .panel 带 backdrop-filter，会成为 fixed 子元素的包含块，
         留在面板内时遮罩只覆盖面板、弹窗按面板居中；脱离面板才覆盖整个窗口 -->
    <Teleport to="body">
    <div v-if="editing" class="dialog-overlay open" @click.self="editing=false">
      <div class="dialog" style="width:min(720px, calc(100% - 24px))">
        <h3>{{ isNew ? t('skills.newSkill') : editName || t('common.edit') }}</h3>
        <div class="field"><label>{{ t('skills.name') }}</label><input v-model="editName" :disabled="!isNew"></div>
        <div class="field"><label>{{ t('skills.content') }}</label><textarea v-model="editContent" rows="22" style="font-family:var(--mono);resize:vertical;min-height:46vh"></textarea></div>
        <div class="dialog-foot"><div></div><div class="right"><button class="btn btn-ghost btn-sm" type="button" @click="editing=false">{{ t('common.cancel') }}</button><button class="btn btn-green btn-sm" type="button" @click="saveSkill">{{ t('common.save') }}</button></div></div>
      </div>
    </div>

    <!-- 一键踢出确认（会删除各客户端技能目录中本程序注入的副本） -->
    <div v-if="ejectConfirmOpen" class="dialog-overlay open" @click.self="ejectConfirmOpen=false">
      <div class="dialog" style="width:min(440px, calc(100% - 24px))">
        <h3>{{ t('skills.ejectAll') }}</h3>
        <p class="delete-desc">{{ t('skills.ejectConfirm') }}</p>
        <div class="dialog-foot">
          <span></span>
          <div class="right">
            <button class="btn btn-ghost btn-sm" type="button" @click="ejectConfirmOpen=false">{{ t('dlg.cancel') }}</button>
            <button class="btn btn-danger btn-sm" type="button" @click="ejectAllDo">{{ t('dlg.confirm') }}</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 一键注入/踢出结果：逐客户端展示技能目录与成败 -->
    <div v-if="syncResults" class="dialog-overlay open" @click.self="syncResults=null">
      <div class="dialog" style="width:min(640px, calc(100% - 24px))">
        <h3>{{ syncResults.inject ? t('skills.injectResult') : t('skills.ejectResult') }}</h3>
        <div class="skill-sync-list">
          <div v-for="r in syncResults.rows" :key="r.id + r.dir" class="skill-sync-row">
            <span class="skill-sync-name">{{ r.name }}</span>
            <span class="skill-sync-dir" :title="r.dir">{{ r.dir }}</span>
            <span class="skill-sync-st" :class="r.ok ? 'ok' : 'bad'">{{ r.ok ? (syncResults.inject ? t('skills.injectedN', { n: r.count }) : t('skills.ejected')) : t('common.fail', { msg: r.error || '?' }) }}</span>
          </div>
        </div>
        <div class="dialog-foot"><span></span><div class="right"><button class="btn btn-sm" type="button" @click="syncResults=null">{{ t('dlg.confirm') }}</button></div></div>
      </div>
    </div>

    <!-- 一键删除全部技能确认 -->
    <div v-if="deleteAllConfirmOpen" class="dialog-overlay open" @click.self="deleteAllConfirmOpen=false">
      <div class="dialog" style="width:min(420px, calc(100% - 24px))">
        <h3>{{ t('skills.deleteAll') }}</h3>
        <p class="delete-desc">{{ t('skills.deleteAllConfirm') }}</p>
        <div class="dialog-foot">
          <span></span>
          <div class="right">
            <button class="btn btn-ghost btn-sm" type="button" @click="deleteAllConfirmOpen=false">{{ t('dlg.cancel') }}</button>
            <button class="btn btn-danger btn-sm" type="button" @click="doDeleteAll">{{ t('dlg.confirmDeleteBtn') }}</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 删除确认（应用内样式） -->
    <div class="dialog-overlay" :class="{ open: !!deleting }" @click.self="deleting=null">
      <div class="dialog" style="width:min(420px, calc(100% - 24px))">
        <h3>{{ t('dlg.deleteTitle') }}</h3>
        <p class="delete-desc">{{ t('skills.confirmDelete', { name: deleting?.name || '' }) }}</p>
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
  </div>
</template>

<style scoped>
/* 一键注入进度条 */
.sync-progress{display:flex;align-items:center;gap:8px;padding:6px 8px;margin-bottom:8px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--s2)}
.sync-progress-bar{flex:1;height:5px;border-radius:99px;background:rgba(127,140,160,.25);overflow:hidden}
.sync-progress-bar i{display:block;height:100%;border-radius:99px;background:var(--accent);transition:width .2s}
.sync-progress-text{flex:none;font-size:10px;color:var(--t2);font-variant-numeric:tabular-nums}
/* 左右分栏（与 MCP 服务页同款）：左技能列表 / 右选中技能的注入客户端分布 */
.mcps-split{display:flex;align-items:stretch;min-height:100%}
.mcps-left{flex:1;min-width:0;display:flex;flex-direction:column;gap:6px;border-right:1px solid var(--border);padding-right:12px}
.skill-row.sel{border-color:var(--accent)}
.skill-row{cursor:pointer}
.mcps-right{flex:1;min-width:0;padding-left:12px;align-self:stretch}
.mcp-right-head{font-size:10px;color:var(--t3);letter-spacing:.04em;margin-bottom:6px}
.mcp-right-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(130px,1fr));gap:6px}
.mcp-right-card{position:relative;display:flex;align-items:center;gap:6px;padding:6px 8px;border:1px solid var(--border);border-radius:6px;background:transparent;color:var(--text);font-size:11.5px;cursor:pointer}
.mcp-right-card:hover{border-color:var(--accent);color:var(--accent)}
.mcp-right-card img{width:16px;height:16px;object-fit:contain;flex:none}
.mcp-right-cli{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.mcp-right-empty{color:var(--t3);font-size:11px;line-height:1.6}
/* 一键注入/踢出结果列表：客户端名 + 技能目录 + 成败，目录过长省略并悬停看全文 */
.skill-sync-list{max-height:46vh;overflow:auto;margin:10px 0 4px;display:flex;flex-direction:column;gap:6px}
.skill-sync-row{display:flex;align-items:center;gap:8px;font-size:11px}
.skill-sync-name{flex:none;width:110px;font-weight:700;color:var(--text);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.skill-sync-dir{flex:1;min-width:0;font-family:var(--mono);font-size:9.5px;color:var(--t3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;direction:rtl;text-align:left}
.skill-sync-st{flex:none;max-width:180px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.skill-sync-st.ok{color:var(--green);font-weight:700}
.skill-sync-st.bad{color:var(--red);font-weight:700}
</style>
