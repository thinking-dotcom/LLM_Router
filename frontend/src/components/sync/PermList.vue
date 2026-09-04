<script setup>
import { ref } from 'vue'
import { t } from '../../i18n'
import { permModeNext } from './shared'

// 通用「权限规则胶囊列表」：每条规则一行胶囊（状态着色圆点 + 可编辑规则文本 + 状态词），
// 点按胶囊（文本输入框与删除按钮除外）在 允许→询问→拒绝 间循环；底部输入框添加新规则。
// 各客户端的权限面板（allow/ask/deny 规则）统一改用此组件。
// 说明：自由文本规则以「存在即已配置」为模型，无「未设」档位——循环到尾回到「允许」，
// 删除用 ×（写盘时规则文本置空同样可移除）。预设/固定键列表的「未设」档由调用面板呈现。
const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  placeholder: { type: String, default: '' },
  noAdd: { type: Boolean, default: false }, // 隐藏底部「添加新规则」行（仅查看/调整已有规则）
})
const emit = defineEmits(['update:modelValue'])

const input = ref('')

const capCls = m => (m === 'allow' || m === 'ask' || m === 'deny') ? m : 'def'
const capWord = m => m === 'allow' ? t('sync.allow') : m === 'ask' ? t('sync.ask') : m === 'deny' ? t('sync.deny') : t('sync.opt.default')
// 点按胶囊循环：allow → ask → deny → allow（自由文本行无「未设」档，删除走 ×）
function cycle(row) {
  emit('update:modelValue', props.modelValue.map(r => {
    if (r !== row) return r
    return { ...r, mode: r.mode === 'deny' ? 'allow' : permModeNext(r.mode) }
  }))
}
// 整行胶囊点按即循环，但文本输入框与删除按钮的事件不冒泡触发
function rowClick(row, e) {
  const t = e && e.target
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.closest('.perm-cap-x'))) return
  cycle(row)
}
function remove(row) {
  emit('update:modelValue', props.modelValue.filter(r => r !== row))
}
function add() {
  const text = String(input.value || '').trim()
  if (!text) return
  emit('update:modelValue', [...props.modelValue, { text, mode: 'allow' }])
  input.value = ''
}
// 文本编辑同样用新数组上抛：对 computed 绑定方写回配置对象才生效
function onTextInput(row, e) {
  emit('update:modelValue', props.modelValue.map(r => r === row ? { ...r, text: e.target.value } : r))
}
</script>

<template>
  <div class="perm-cap-list">
    <div v-for="(row, i) in modelValue" :key="i" class="perm-cap-row" :class="capCls(row.mode)"
      :aria-label="String(row.text || '') + ' · ' + capWord(row.mode)" :title="t('sync.permCapClick')" @click="rowClick(row, $event)">
      <span class="perm-cap-dot"></span>
      <input class="perm-cap-edit" :value="row.text" :placeholder="placeholder || 'Bash(npm test)'" @wheel.stop @input="onTextInput(row, $event)" />
      <span class="perm-cap-state" :class="capCls(row.mode)">{{ capWord(row.mode) }}</span>
      <button type="button" class="perm-cap-x" :title="t('sync.delete')" @click.stop="remove(row)">×</button>
    </div>
    <div v-if="!noAdd" class="perm-cap-row perm-cap-add">
      <input class="perm-cap-edit" v-model="input" :placeholder="placeholder || 'Bash(npm test)'" @keyup.enter="add" @wheel.stop />
      <button type="button" class="btn btn-xs btn-ghost" @click="add">{{ t('common.add') }}</button>
    </div>
  </div>
</template>
