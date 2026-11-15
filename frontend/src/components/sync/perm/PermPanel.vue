<script setup>
// 统一权限面板：模式胶囊 + 规则表（规则 / 处置 / 删除）+ 客户端特有附加项（extra 插槽）。
// 行数据由父组件以数组持有（默认 {text, mode}，可用 textKey/modeKey 适配其它字段名），
// 所有变更都以新数组 emit 回去，便于父组件用 computed 适配自己的存储结构。
import { ref } from 'vue'
import { t } from '../../../i18n'

const props = defineProps({
  modes: { type: Array, default: () => [] },   // [{ v, label }]
  mode: { type: String, default: '' },
  modeLabel: { type: String, default: '' },
  rows: { type: Array, default: () => [] },
  textKey: { type: String, default: 'text' },
  modeKey: { type: String, default: 'mode' },
  readonlyText: { type: Boolean, default: false },
  rowPlaceholder: { type: String, default: '' },
  quick: { type: Array, default: () => [] },   // [{ label, v }]
  showRules: { type: Boolean, default: true },
  rulesLabel: { type: String, default: '' },
  addLabel: { type: String, default: '' },
})
const emit = defineEmits(['update:mode', 'update:rows'])

const DECS = [
  { v: 'allow', label: () => t('sync.allow') },
  { v: 'ask', label: () => t('sync.ask') },
  { v: 'deny', label: () => t('sync.deny') },
]
const draft = ref('')

const val = (row, key) => String(row?.[key] ?? '')
function setMode(v) { emit('update:mode', props.mode === v ? '' : v) }
function setDecision(i, v) {
  const next = props.rows.map((r, idx) => idx === i ? { ...r, [props.modeKey]: v } : r)
  emit('update:rows', next)
}
function setText(i, v) {
  const next = props.rows.map((r, idx) => idx === i ? { ...r, [props.textKey]: v } : r)
  emit('update:rows', next)
}
function remove(i) { emit('update:rows', props.rows.filter((_, idx) => idx !== i)) }
function add() {
  const text = draft.value.trim()
  if (!text) return
  if (props.rows.some(r => val(r, props.textKey) === text)) { draft.value = ''; return }
  emit('update:rows', [...props.rows, { [props.textKey]: text, [props.modeKey]: 'ask' }])
  draft.value = ''
}
function pickQuick(v) { draft.value = v }
</script>

<template>
  <div class="perm-panel">
    <div v-if="modes.length" class="perm-sec">
      <div class="perm-sec-title">{{ modeLabel || t('sync.permMode') }}</div>
      <div class="perm-modes">
        <button v-for="m in modes" :key="m.v" type="button" class="perm-mode-btn" :class="{ on: mode === m.v }"
          @click="setMode(m.v)">{{ m.label }}</button>
      </div>
    </div>

    <div v-if="showRules" class="perm-sec">
      <div class="perm-sec-title">
        {{ rulesLabel || t('sync.permRules') }}
        <span v-if="rows.length" class="perm-sec-count">{{ rows.length }}</span>
      </div>
      <div class="perm-rules">
        <div v-for="(r, i) in rows" :key="i" class="perm-rule">
          <span v-if="readonlyText" class="perm-rule-key" :title="val(r, textKey)">{{ val(r, textKey) }}</span>
          <input v-else class="perm-rule-text" :value="val(r, textKey)" :placeholder="rowPlaceholder"
            @input="setText(i, $event.target.value)" />
          <div class="perm-rule-decs">
            <button v-for="d in DECS" :key="d.v" type="button" class="perm-dec" :class="[d.v, { on: val(r, modeKey) === d.v }]"
              @click="setDecision(i, d.v)">{{ d.label() }}</button>
          </div>
          <button type="button" class="perm-rule-x" :title="t('common.delete')" @click="remove(i)">×</button>
        </div>
        <div class="perm-rule-add">
          <input v-model="draft" :placeholder="rowPlaceholder" @keyup.enter="add" />
          <button type="button" class="perm-add-btn" :disabled="!draft.trim()" @click="add">{{ addLabel || t('sync.addRule') }}</button>
          <div v-if="quick.length" class="perm-quick">
            <button v-for="q in quick" :key="q.v" type="button" class="perm-quick-btn" @click="pickQuick(q.v)">{{ q.label }}</button>
          </div>
        </div>
      </div>
    </div>

    <div v-if="$slots.extra" class="perm-sec">
      <slot name="extra" />
    </div>
  </div>
</template>

<style scoped>
.perm-panel{display:flex;flex-direction:column;gap:12px}
.perm-sec{display:flex;flex-direction:column;gap:8px}
.perm-sec-title{font-size:10.5px;font-weight:600;color:var(--t2)}
.perm-sec-count{display:inline-flex;align-items:center;justify-content:center;min-width:16px;height:15px;padding:0 5px;margin-left:6px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-size:9.5px;font-weight:700}
.perm-modes{display:flex;flex-wrap:wrap;gap:6px}
.perm-mode-btn{padding:3px 12px;border:1px solid var(--border);border-radius:999px;background:transparent;color:var(--t2);font-size:11px;font-family:inherit;cursor:pointer;transition:background .12s,color .12s,border-color .12s}
.perm-mode-btn:hover{border-color:var(--border-hover);color:var(--text)}
.perm-mode-btn.on{border-color:var(--accent);background:var(--accent-soft);color:var(--accent);font-weight:600}
.perm-rules{display:flex;flex-direction:column;border:1px solid var(--border);border-radius:8px;overflow:visible}
.perm-rule{display:grid;grid-template-columns:minmax(0,1fr) auto auto;align-items:center;gap:10px;padding:6px 10px;border-bottom:1px solid var(--border)}
.perm-rule:last-of-type{border-bottom:none}
.perm-rule-key,.perm-rule-text{font-family:var(--mono);font-size:10.5px;color:var(--text);min-width:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.perm-rule-key{color:var(--t2)}
.perm-rule-text{width:100%;padding:2px 4px;border:1px solid transparent;border-radius:4px;background:transparent;font-family:var(--mono)}
.perm-rule-text:hover{border-color:var(--border)}
.perm-rule-text:focus{outline:none;border-color:var(--accent);background:var(--input-bg)}
.perm-rule-decs{display:flex;gap:2px;flex:none}
.perm-dec{padding:1px 8px;border:1px solid transparent;border-radius:999px;background:transparent;color:var(--t3);font-size:10px;font-family:inherit;cursor:pointer;transition:background .12s,color .12s}
.perm-dec:hover{color:var(--text);background:var(--s3)}
.perm-dec.allow.on{background:var(--green-soft,rgba(63,185,80,.15));color:var(--green);font-weight:600}
.perm-dec.ask.on{background:var(--amber-soft);color:var(--amber);font-weight:600}
.perm-dec.deny.on{background:var(--red-soft);color:var(--red);font-weight:600}
.perm-rule-x{flex:none;width:20px;height:20px;padding:0;border:0;border-radius:4px;background:transparent;color:var(--t3);font-size:13px;line-height:1;cursor:pointer}
.perm-rule-x:hover{background:var(--red-soft);color:var(--red)}
.perm-rule-add{display:flex;align-items:center;gap:8px;padding:6px 10px;flex-wrap:wrap}
.perm-rule-add>input{flex:1 1 220px;min-width:0;padding:3px 8px;border:1px solid var(--border);border-radius:6px;background:var(--input-bg);color:var(--text);font-size:10.5px;font-family:var(--mono)}
.perm-rule-add>input:focus{outline:none;border-color:var(--accent)}
.perm-add-btn{padding:2px 12px;border:1px solid var(--border);border-radius:6px;background:var(--s2);color:var(--t2);font-size:10.5px;font-family:inherit;cursor:pointer}
.perm-add-btn:hover:not(:disabled){border-color:var(--accent);color:var(--accent)}
.perm-add-btn:disabled{opacity:.45;cursor:default}
.perm-quick{display:flex;flex-wrap:wrap;gap:4px}
.perm-quick-btn{padding:1px 8px;border:1px solid var(--border);border-radius:999px;background:transparent;color:var(--t3);font-size:9.5px;font-family:var(--mono);cursor:pointer}
.perm-quick-btn:hover{border-color:var(--border-hover);color:var(--text)}
</style>
