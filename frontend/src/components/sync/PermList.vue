<script setup>
import { ref } from 'vue'
import { t } from '../../i18n'
import { permModeNext } from './shared'

// 通用「权限规则胶囊列表」：每条规则一行胶囊（可编辑规则文本 + 状态控件）。
// 两种状态控件由 pick 属性切换：
//   pick=false（默认）：行首 agy 式单选圆圈（恒选中、按档位着语义色）+ 尾部状态词，
//     点按整行或圆圈在 允许→询问→拒绝 间循环（输入框与删除按钮除外）。
//   pick=true：行内并排三枚 radio「允许 / 询问 / 拒绝」直接点选（agy 工具权限同款），
//     不再整行循环，也不显示尾部状态词。
// 两种模式都只有 允许/询问/拒绝 三档，没有「未设」；新增规则默认允许。
// 底部输入框用于添加新规则；每行 radio 用独立 name，保证各行互不影响。
// 各客户端的权限面板（allow/ask/deny 规则）统一改用此组件。
// 说明：自由文本规则以「存在即已配置」为模型，无「未设」档位——循环到尾回到「允许」，
// 删除用 ×（写盘时规则文本置空同样可移除）。预设/固定键列表的「未设」档由调用面板呈现。
const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  placeholder: { type: String, default: '' },
  noAdd: { type: Boolean, default: false }, // 隐藏底部「添加新规则」行（仅查看/调整已有规则）
  pick: { type: Boolean, default: false },  // 三档直接点选（允许/询问/拒绝 并排单选，agy 式），取代整行循环
})
const emit = defineEmits(['update:modelValue'])

const input = ref('')

// 命令能否执行只有这三档：允许 / 询问 / 拒绝（无「未设」，新增规则默认允许）
const PICK_MODES = ['allow', 'ask', 'deny']
const capCls = m => (m === 'allow' || m === 'ask' || m === 'deny') ? m : 'def'
const capWord = m => m === 'allow' ? t('sync.allow') : m === 'ask' ? t('sync.ask') : m === 'deny' ? t('sync.deny') : t('sync.opt.default')
// 三档点选：直接把该规则设为指定档（不经过循环）
function setMode(row, mode) {
  emit('update:modelValue', props.modelValue.map(r => r === row ? { ...r, mode } : r))
}
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
// 列表行键：规则无稳定 id，用「规则文本 + 同文本出现序号」代替数组下标，
// 避免删除中间行后 vue 复用 DOM 导致 radio 选中态与文本串行。
function capRowKey(row, i) {
  const text = String(row && row.text || '')
  let n = 0
  for (let j = 0; j < i; j++) if (String((props.modelValue[j] || {}).text || '') === text) n++
  return text + '\u0000' + n
}
</script>

<template>
  <div class="perm-cap-list">
    <!-- 行键用内容（text + 出现序号）而非数组下标：删除/重排后 DOM 不会复用错位 -->
    <div v-for="(row, i) in modelValue" :key="capRowKey(row, i)" class="perm-cap-row" :class="capCls(row.mode)"
      :aria-label="String(row.text || '') + ' · ' + capWord(row.mode)" :title="pick ? '' : t('sync.permCapClick')" @click="pick ? null : rowClick(row, $event)">
      <template v-if="pick">
        <input class="perm-cap-edit" :value="row.text" :placeholder="placeholder || 'Bash(npm test)'" @wheel.stop @input="onTextInput(row, $event)" />
        <label v-for="m in PICK_MODES" :key="m" class="perm-cap-pick" :class="m" :title="capWord(m)">
          <input type="radio" :name="'perm-cap-pick-' + i" :value="m" :checked="row.mode === m" @change="setMode(row, m)" />
          <span>{{ capWord(m) }}</span>
        </label>
      </template>
      <template v-else>
        <input type="radio" class="perm-cap-radio" :name="'perm-cap-row-' + i" :checked="true" :aria-label="capWord(row.mode)" @click.stop="cycle(row)" />
        <input class="perm-cap-edit" :value="row.text" :placeholder="placeholder || 'Bash(npm test)'" @wheel.stop @input="onTextInput(row, $event)" />
        <span class="perm-cap-state" :class="capCls(row.mode)">{{ capWord(row.mode) }}</span>
      </template>
      <button type="button" class="perm-cap-x" :title="t('sync.delete')" @click.stop="remove(row)"><i class="ri-close-line"></i></button>
    </div>
    <div v-if="!noAdd" class="perm-cap-row perm-cap-add">
      <input class="perm-cap-edit" v-model="input" :placeholder="placeholder || 'Bash(npm test)'" @keyup.enter="add" @wheel.stop />
      <button type="button" class="btn btn-xs btn-ghost" @click="add">{{ t('common.add') }}</button>
    </div>
  </div>
</template>
