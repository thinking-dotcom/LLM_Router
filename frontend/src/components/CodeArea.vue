<script setup>
// CodeArea：带行号栏的源码编辑框（可复用）。
// 行号随内容行数实时生成；滚动时行号栏与文本区 scrollTop 同步。
// 两栏使用相同字体/字号/行高/上内边距，保证逐行对齐；文本区不换行，长行横向滚动。
// 内容为 JSON 时提供「格式化」按钮：合法则原地 2 空格美化，非法提示错误行列。
import { ref, computed } from 'vue'
import { toast } from '../toast'
import { t } from '../i18n'
import { jsonErrorPos } from '../utils'

const props = defineProps({ modelValue: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue'])

const lnEl = ref(null)
const text = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v),
})
const lineNumbers = computed(() => {
  const n = (props.modelValue || '').split('\n').length
  let s = ''
  for (let i = 1; i <= n; i++) s += i + '\n'
  return s
})
function onScroll(e) {
  if (lnEl.value && e && e.target) lnEl.value.scrollTop = e.target.scrollTop
}
// 启发式：以 { 或 [ 开头才视为 JSON，避免在 TOML/YAML/Markdown 源码上显示无关按钮
const looksJson = computed(() => /^\s*[[{]/.test(props.modelValue || ''))
function format() {
  try {
    text.value = JSON.stringify(JSON.parse(props.modelValue || ''), null, 2) + '\n'
  } catch (e) {
    const p = jsonErrorPos(props.modelValue)
    toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError'))
  }
}
</script>

<template>
  <div class="code-area">
    <div ref="lnEl" class="ca-ln">{{ lineNumbers }}</div>
    <textarea v-model="text" class="ca-ta" spellcheck="false" @scroll="onScroll"></textarea>
    <button v-if="looksJson" type="button" class="ca-format" :title="t('common.format')" @click="format">{{ t('common.format') }}</button>
  </div>
</template>

<style scoped>
.code-area{height:min(472px,68vh);flex:none;display:flex;min-height:0;border:1px solid var(--bd,#333);border-radius:8px;background:var(--bg2,#141414);overflow:hidden;position:relative}
.ca-ln{width:40px;flex-shrink:0;text-align:right;padding:8px 8px 8px 0;font-family:Consolas,monospace;font-size:12px;line-height:1.5;color:var(--t3,#888);background:rgba(128,128,128,.08);overflow:hidden;user-select:none;white-space:pre}
.ca-ta{flex:1;resize:none;font-family:Consolas,monospace;font-size:12px;line-height:1.5;padding:8px;border:none;outline:none;background:transparent;color:inherit;white-space:pre;overflow:auto;scrollbar-width:none;-ms-overflow-style:none}
/* 文本区隐藏滚动条但保留滚动（长行横向滚动/纵向滚动均可用滚轮、触控板与键盘） */
.ca-ta::-webkit-scrollbar{width:0;height:0;display:none}
.ca-format{position:absolute;top:6px;right:10px;z-index:2;padding:1px 10px;font-size:9.5px;line-height:1.6;font-family:inherit;border:1px solid var(--border,#444);border-radius:999px;background:var(--surface,#1d1d1d);color:var(--t2,#aaa);cursor:pointer;opacity:.75;transition:var(--transition,.15s)}
.ca-format:hover{opacity:1;border-color:var(--accent,#34d399);color:var(--accent,#34d399)}
</style>
