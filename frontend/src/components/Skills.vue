<script setup>
import { ref, onMounted } from 'vue'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'

// ---------- 技能 ----------
const skills = ref([])
const skillsDir = ref('')
const editing = ref(false)
const editName = ref('')
const editContent = ref('')
const isNew = ref(false)
async function loadSkills() {
  try { skills.value = (await api.GetSkillsList()) || [] } catch (e) { skills.value = [] }
  try { if (api.GetSkillsDir) skillsDir.value = (await api.GetSkillsDir()) || '' } catch (e) {}
}
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
  const r = await api.DeleteSkill(s.name)
  toast(r && r.ok ? t('skills.deleted') : t('common.fail', { msg: r?.error || '' }))
  if (r && r.ok) await loadSkills()
}
onMounted(loadSkills)
defineExpose({ openSkill, openDir, loadSkills, skillsDir })
</script>

<template>
  <div>
    <div v-if="!skills.length" class="empty-tip">{{ t('skills.empty') }}<br><span style="font-family:var(--mono);font-size:9px;color:var(--t3)">{{ skillsDir }}</span></div>
    <div v-for="s in skills" :key="s.name" class="skill-row">
      <div class="skill-main" @click="openSkill(s)">
        <div class="skill-name">{{ s.name }}</div>
        <div class="skill-desc">{{ s.desc || t('skills.noDesc') }}</div>
      </div>
      <button class="btn btn-danger" @click.stop="askDelete(s)">{{ t('common.delete') }}</button>
    </div>

    <div v-if="editing" class="dialog-overlay open" @click.self="editing=false">
      <div class="dialog" style="width:min(720px, calc(100% - 24px))">
        <h3>{{ isNew ? t('skills.newSkill') : editName || t('common.edit') }}</h3>
        <div class="field"><label>{{ t('skills.name') }}</label><input v-model="editName" :disabled="!isNew"></div>
        <div class="field"><label>{{ t('skills.content') }}</label><textarea v-model="editContent" rows="22" style="font-family:var(--mono);resize:vertical;min-height:46vh"></textarea></div>
        <div class="dialog-foot"><div></div><div class="right"><button class="btn btn-ghost btn-sm" type="button" @click="editing=false">{{ t('common.cancel') }}</button><button class="btn btn-green btn-sm" type="button" @click="saveSkill">{{ t('common.save') }}</button></div></div>
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
  </div>
</template>
