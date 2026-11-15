<script setup>
// CodeArea：带行号栏 + 语法高亮的源码编辑框（可复用）。
// 结构：行号栏 | 高亮层（<pre>，只读） + textarea（文字透明，覆盖其上，负责编辑/光标/选择）
// 三层用同一套字体/字号/行高/内边距，滚动由 textarea 驱动并同步给行号栏与高亮层，逐行严丝合缝。
// 高亮按内容启发式选语言：JSON / TOML / Markdown，都不像时按纯文本（只做 HTML 转义）。
import { ref, computed } from 'vue'
import { toast } from '../toast'
import { t } from '../i18n'
import { jsonErrorPos } from '../utils'

const props = defineProps({ modelValue: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue'])

const lnEl = ref(null)
const hlEl = ref(null)
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
// 文本区滚动时把行号栏与高亮层一起带走（横向也要同步，长行才能对齐）
function onScroll(e) {
  const t = e && e.target
  if (!t) return
  if (lnEl.value) lnEl.value.scrollTop = t.scrollTop
  if (hlEl.value) { hlEl.value.scrollTop = t.scrollTop; hlEl.value.scrollLeft = t.scrollLeft }
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

// ---- 语法高亮（不引第三方库，按内容形态选语言）----
const esc = s => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
const sp = (cls, txt) => '<span class="t-' + cls + '">' + esc(txt) + '</span>'

const hlMode = computed(() => {
  const s = props.modelValue || ''
  if (looksJson.value) return 'json'
  if (/^\s*[A-Za-z0-9_.\-"']+\s*=/m.test(s) || /^\s*\[\[?[^\]]*\]\]?\s*$/m.test(s)) return 'toml'
  if (/^\s*#{1,6}\s/m.test(s) || /```/.test(s)) return 'md'
  return 'text'
})

// JSON：字符串（后跟冒号的是键）、数字、true/false/null、注释、标点
function hlJson(src) {
  const re = /"(?:\\.|[^"\\])*"(?=\s*:)|"(?:\\.|[^"\\])*"|\b(?:true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|\/\/[^\n]*|\/\*[\s\S]*?\*\/|[{}\[\],:]/g
  let out = '', last = 0, m
  while ((m = re.exec(src))) {
    const tk = m[0]
    out += esc(src.slice(last, m.index))
    let cls = 'punct'
    if (tk[0] === '"') cls = /^\s*:/.test(src.slice(m.index + tk.length)) ? 'key' : 'str'
    else if (tk === 'true' || tk === 'false' || tk === 'null') cls = 'lit'
    else if (tk[0] === '-' || (tk >= '0' && tk <= '9')) cls = 'num'
    else if (tk[0] === '/') cls = 'com'
    out += sp(cls, tk)
    last = m.index + tk.length
  }
  return out + esc(src.slice(last))
}

// TOML：整行注释、[表头]、key = value（值再分字符串/数字/布尔，行尾 # 注释）
function hlTomlVal(v) {
  const ci = v.indexOf(' #')
  let tail = ''
  if (ci >= 0) { tail = sp('com', v.slice(ci)); v = v.slice(0, ci) }
  const tv = v.trim()
  let body
  if (/^(?:"""|''')/.test(tv)) body = sp('str', v)
  else if (/^["']/.test(tv)) body = sp('str', v)
  else if (/^(?:true|false)$/.test(tv)) body = sp('lit', v)
  else if (/^-?\d/.test(tv)) body = sp('num', v)
  else if (tv === '') body = ''
  else body = sp('punct', v) // 数组 / 日期 / 多行值
  return body + tail
}
function hlToml(src) {
  return src.split('\n').map(line => {
    if (/^\s*#/.test(line)) return sp('com', line)
    const th = /^(\s*)(\[\[?[^\]]*\]\]?)(\s*)$/.exec(line)
    if (th) return th[1] + sp('key', th[2]) + th[3]
    const kv = /^(\s*)([A-Za-z0-9_.\-"'\[\]]+)(\s*=\s*)(.*)$/.exec(line)
    if (kv) return kv[1] + sp('key', kv[2]) + sp('punct', kv[3]) + hlTomlVal(kv[4])
    return esc(line)
  }).join('\n')
}

// Markdown：标题、代码栅栏、引用、列表标记 + 行内 code/加粗/链接
function hlMdInline(s) {
  return esc(s)
    .replace(/(`[^`]*`)/g, '<span class="t-str">$1</span>')
    .replace(/(\*\*[^*]+\*\*)/g, '<span class="t-lit">$1</span>')
    .replace(/(\[[^\]]*\]\([^)]*\))/g, '<span class="t-num">$1</span>')
}
function hlMd(src) {
  return src.split('\n').map(line => {
    if (/^\s*#{1,6}\s/.test(line)) return sp('head', line)
    if (/^\s*(?:```|~~~)/.test(line)) return sp('com', line)
    if (/^\s*>/.test(line)) return sp('com', line)
    const li = /^(\s*)([-*+]|\d+\.)(\s+)(.*)$/.exec(line)
    if (li) return li[1] + sp('key', li[2]) + li[3] + hlMdInline(li[4])
    return hlMdInline(line)
  }).join('\n')
}

const hlHtml = computed(() => {
  const s = props.modelValue || ''
  const body = hlMode.value === 'json' ? hlJson(s)
    : hlMode.value === 'toml' ? hlToml(s)
      : hlMode.value === 'md' ? hlMd(s)
        : esc(s)
  // 末尾补一个换行：textarea 光标停在最后一行时，高亮层的高度与之保持一致
  return body + '\n'
})
</script>

<template>
  <div class="code-area">
    <div ref="lnEl" class="ca-ln">{{ lineNumbers }}</div>
    <div class="ca-wrap">
      <pre ref="hlEl" class="ca-hl" aria-hidden="true" v-html="hlHtml"></pre>
      <textarea v-model="text" class="ca-ta" spellcheck="false" @scroll="onScroll"></textarea>
    </div>
    <button v-if="looksJson" type="button" class="ca-format" :title="t('common.format')" @click="format">{{ t('common.format') }}</button>
  </div>
</template>

<style scoped>
/* 编辑器配色：深色对齐 GitHub Dark（与应用深色主题同源），浅色走 GitHub Light。
   配色是"编辑器自己的主题"，不跟随应用主色，避免主色与语法色互相干扰 */
.code-area{
  --ed-font:"Cascadia Mono","JetBrains Mono",Consolas,"SF Mono",Menlo,monospace;
  --ed-bg:#0d1117; --ed-gutter:#010409; --ed-line:#21262d;
  --ed-txt:#e6edf3; --ed-caret:#e6edf3; --ed-ln:#6e7681;
  --ed-key:#79c0ff; --ed-str:#a5d6ff; --ed-num:#79c0ff; --ed-lit:#ff7b72;
  --ed-punct:#8b949e; --ed-com:#8b949e; --ed-head:#7ee787; --ed-sel:rgba(56,139,253,.32);
  /* 高度交给父级（.cfg-src 是 flex 列）：一路撑到底部，不再用固定高度留下空白；
     min-height 只在窗口极矮时兜底（此时外层 .cfg-body 自身可滚动） */
  flex:1;min-height:200px;display:flex;
  border:1px solid var(--ed-line);border-radius:8px;background:var(--ed-bg);overflow:hidden;position:relative
}
:root[data-theme="light"] .code-area{
  --ed-bg:#ffffff; --ed-gutter:#f6f8fa; --ed-line:#d0d7de;
  --ed-txt:#1f2328; --ed-caret:#1f2328; --ed-ln:#8c959f;
  --ed-key:#0550ae; --ed-str:#0a3069; --ed-num:#0550ae; --ed-lit:#cf222e;
  --ed-punct:#57606a; --ed-com:#6e7781; --ed-head:#1a7f37; --ed-sel:rgba(84,174,255,.28)
}
/* 行号栏：与正文同字体/行高/上内边距，逐行对齐；右侧一条细分隔线 */
.ca-ln{width:42px;flex-shrink:0;text-align:right;padding:8px 8px 8px 0;font-family:var(--ed-font);font-size:12px;line-height:1.5;color:var(--ed-ln);background:var(--ed-gutter);border-right:1px solid var(--ed-line);overflow:hidden;user-select:none;white-space:pre}
.ca-wrap{position:relative;flex:1;min-width:0}
/* 高亮层与文本区必须完全重合：字体、字号、行高、内边距、换行规则、制表位都取同一组值 */
.ca-hl,.ca-ta{margin:0;font-family:var(--ed-font);font-size:12px;line-height:1.5;padding:8px 10px;white-space:pre;tab-size:2;overflow-wrap:normal;word-break:normal}
.ca-hl{position:absolute;inset:0;overflow:hidden;pointer-events:none;color:var(--ed-txt)}
.ca-ta{position:absolute;inset:0;resize:none;border:none;outline:none;background:transparent;color:transparent;caret-color:var(--ed-caret);overflow:auto;scrollbar-width:none;-ms-overflow-style:none}
/* 文字透明后选区仍要看得见：只给背景色，文字保持透明显出下层高亮 */
.ca-ta::selection{background:var(--ed-sel)}
/* 文本区隐藏滚动条但保留滚动（长行横向滚动/纵向滚动均可用滚轮、触控板与键盘） */
.ca-ta::-webkit-scrollbar{width:0;height:0;display:none}
/* 语法着色 */
.ca-hl :deep(.t-key){color:var(--ed-key)}
.ca-hl :deep(.t-str){color:var(--ed-str)}
.ca-hl :deep(.t-num){color:var(--ed-num)}
.ca-hl :deep(.t-lit){color:var(--ed-lit)}
.ca-hl :deep(.t-punct){color:var(--ed-punct)}
.ca-hl :deep(.t-com){color:var(--ed-com);font-style:italic}
.ca-hl :deep(.t-head){color:var(--ed-head);font-weight:700}
/* 格式化：浮在编辑器右上角，平时低调、hover 才亮 */
.ca-format{position:absolute;top:8px;right:12px;z-index:3;padding:1px 10px;font-size:9.5px;line-height:1.7;font-family:inherit;border:1px solid var(--ed-line);border-radius:6px;background:rgba(110,118,129,.16);color:var(--ed-txt);cursor:pointer;opacity:.8;transition:color .15s,border-color .15s,background-color .15s,opacity .15s}
.ca-format:hover{opacity:1;color:var(--ed-key);border-color:var(--ed-key);background:rgba(110,118,129,.26)}
</style>
