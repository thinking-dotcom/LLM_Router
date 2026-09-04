<script setup>
import { ref } from 'vue'
import { t } from '../../i18n'
import { permRowToggleMode } from './shared'

// 通用「权限规则点选列表」：每条规则一行（文本 + 允许/询问/拒绝三态点选 + 删除），
// 底部输入框添加新规则。各客户端的权限面板（allow/ask/deny 规则）统一改用此组件。
const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  placeholder: { type: String, default: '' },
  noAdd: { type: Boolean, default: false }, // 隐藏底部「添加新规则」行（仅查看/调整已有规则）
})
const emit = defineEmits(['update:modelValue'])

const input = ref('')
const addMode = ref('allow') // 新规则的默认类别（可选：允许/询问/拒绝）

function pick(row, mode) {
  // 用全新数组/对象上抛，让 computed(v-model) 的 setter 生效：
  // 直接改行对象对 get 侧新建的瞬时对象无效，三态点选会毫无反应
  emit('update:modelValue', props.modelValue.map(r => {
    if (r !== row) return r
    const copy = { ...r }
    permRowToggleMode(copy, mode)
    return copy
  }))
}
function add() {
  const text = String(input.value || '').trim()
  if (!text) return
  emit('update:modelValue', [...props.modelValue, { text, mode: addMode.value }])
  input.value = ''
}
function pickAdd(mode) { addMode.value = addMode.value === mode ? '' : mode }
// 文本编辑同样用新数组上抛：对 computed 绑定方写回配置对象才生效
function onTextInput(row, e) {
  emit('update:modelValue', props.modelValue.map(r => r === row ? { ...r, text: e.target.value } : r))
}
</script>

<template>
  <div class="perm-list">
    <div v-for="(row, i) in modelValue" :key="i" class="perm-row-wrap" :class="{ on: row.mode }">
      <div class="pcl-head">
        <input class="pcl-text" :value="row.text" :placeholder="placeholder || 'Bash(npm test)'" @wheel.stop @input="onTextInput(row, $event)" />
        <div class="pcl-opts" role="radiogroup" :aria-label="row.text">
          <button type="button" class="pcl-opt allow" role="radio" :class="{ on: row.mode === 'allow' }" :aria-checked="row.mode === 'allow'" @click="pick(row, 'allow')">{{ t('sync.allow') }}</button>
          <button type="button" class="pcl-opt ask" role="radio" :class="{ on: row.mode === 'ask' }" :aria-checked="row.mode === 'ask'" @click="pick(row, 'ask')">{{ t('sync.ask') }}</button>
          <button type="button" class="pcl-opt deny" role="radio" :class="{ on: row.mode === 'deny' }" :aria-checked="row.mode === 'deny'" @click="pick(row, 'deny')">{{ t('sync.deny') }}</button>
        </div>
      </div>
    </div>
    <div v-if="!noAdd" class="pcl-add">
      <input class="pcl-input" v-model="input" :placeholder="placeholder || 'Bash(npm test)'" @keyup.enter="add" @wheel.stop />
      <div class="pcl-opts">
        <button type="button" class="pcl-opt allow" :class="{ on: addMode === 'allow' }" @click="pickAdd('allow')">{{ t('sync.allow') }}</button>
        <button type="button" class="pcl-opt ask" :class="{ on: addMode === 'ask' }" @click="pickAdd('ask')">{{ t('sync.ask') }}</button>
        <button type="button" class="pcl-opt deny" :class="{ on: addMode === 'deny' }" @click="pickAdd('deny')">{{ t('sync.deny') }}</button>
      </div>
      <button type="button" class="btn btn-xs btn-ghost" @click="add">{{ t('common.add') }}</button>
    </div>
  </div>
</template>
