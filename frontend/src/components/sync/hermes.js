// Hermes（~/.hermes/config.yaml，YAML 结构化表单）
// 受管键：顶级 model: 块内的 provider / base_url / api_key / model / api /
// temperature / top_p / max_output_tokens / max_ctx_tokens / thinking / thinking_effort
// （base_url 自动指向路由器）；其余内容原样保留
import { ref, watch } from 'vue'
import { cfgContent, cfgEdit, cfgTarget, derivedBaseUrl } from './store'

export const hermesForm = ref({
  provider: 'openai-compatible', apiKey: '',
  model: '', api: 'openai',
  temperature: '', topP: '', maxOutputTokens: '', maxCtxTokens: '',
  thinking: '', thinkingEffort: '',
})
export let hermesMuted = false
export function setHermesMuted(v) { hermesMuted = v }
export const hermesModelOpen = ref(false) // 默认模型下拉
// YAML model 块受管键的固定书写顺序（书写时才输出非空键）
export const HERMES_MODEL_KEYS = ['provider', 'base_url', 'api_key', 'model', 'api', 'temperature', 'top_p', 'max_output_tokens', 'max_ctx_tokens', 'thinking', 'thinking_effort']
export function hermesInitFrom(text) {
  const f = {
    provider: 'openai-compatible', apiKey: '',
    model: '', api: 'openai',
    temperature: '', topP: '', maxOutputTokens: '', maxCtxTokens: '',
    thinking: '', thinkingEffort: '',
  }
  let inModel = false
  const unq = s => s.replace(/^"(.*)"$/, '$1').replace(/^'(.*)'$/, '$1')
  for (const ln of String(text || '').split(/\r?\n/)) {
    const t = ln.trim()
    if (/^model:\s*(#.*)?$/.test(t)) { inModel = true; continue }
    if (inModel) {
      if (/^\S/.test(ln)) inModel = false // 下一个顶级键（无缩进）：model 块结束
      else {
        const kv = t.match(/^(provider|api_key|model|api|temperature|top_p|max_output_tokens|max_ctx_tokens|thinking|thinking_effort)\s*:\s*(.*)$/)
        if (kv) {
          const val = unq(kv[2].trim())
          if (kv[1] === 'provider' && val) f.provider = val
          else if (kv[1] === 'api_key') f.apiKey = val
          else if (kv[1] === 'model') f.model = val
          else if (kv[1] === 'api') f.api = val || 'openai'
          else if (kv[1] === 'temperature') f.temperature = val
          else if (kv[1] === 'top_p') f.topP = val
          else if (kv[1] === 'max_output_tokens') f.maxOutputTokens = val
          else if (kv[1] === 'max_ctx_tokens') f.maxCtxTokens = val
          else if (kv[1] === 'thinking') f.thinking = val
          else if (kv[1] === 'thinking_effort') f.thinkingEffort = val
        }
      }
    }
  }
  hermesForm.value = f
}
export function hermesWriteYaml(text) {
  const f = hermesForm.value
  const quote = v => '"' + String(v ?? '').replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"'
  const flat = v => String(v ?? '').trim()
  const numPick = v => (/^-?\d+(\.\d+)?$/.test(String(v ?? '').trim()) ? String(v).trim() : '')
  // 组装 model 块各受管键值；无值的键不参与输出
  const vals = { provider: f.provider || 'openai-compatible', base_url: derivedBaseUrl(), api_key: quote(f.apiKey) }
  if (flat(f.model)) vals.model = quote(f.model)
  if (flat(f.api)) vals.api = quote(f.api)
  const tm = numPick(f.temperature)
  if (tm) vals.temperature = tm
  const tp = numPick(f.topP)
  if (tp) vals.top_p = tp
  const mo = numPick(f.maxOutputTokens)
  if (mo) vals.max_output_tokens = mo
  const mc = numPick(f.maxCtxTokens)
  if (mc) vals.max_ctx_tokens = mc
  if (f.thinking === 'true' || f.thinking === true) {
    vals.thinking = 'true'
    if (flat(f.thinkingEffort)) vals.thinking_effort = quote(f.thinkingEffort)
  } else if (f.thinking === 'false') vals.thinking = 'false'
  const lines = String(text || '').split('\n')
  // 第一遍：定位顶级 model: 块与既有受管键所在行（全量受管键，避免重复插入）
  let modelIdx = -1
  let inModel = false
  const keyLine = {}
  const managedRe = /^(provider|base_url|api_key|model|api|temperature|top_p|max_output_tokens|max_ctx_tokens|thinking|thinking_effort)\s*:/
  lines.forEach((ln, i) => {
    const t = ln.trim()
    if (modelIdx < 0) {
      if (/^model:\s*(#.*)?$/.test(t)) { modelIdx = i; inModel = true }
      return
    }
    if (inModel) {
      if (/^\S/.test(ln)) { inModel = false; return } // 下一个顶级键：model 块结束
      const kv = t.match(managedRe)
      if (kv && !(kv[1] in keyLine)) keyLine[kv[1]] = i
    }
  })
  if (modelIdx < 0) {
    // 无 model: 块：去掉尾部空行后追加受管块（仅输出有值的键）
    let s = lines.join('\n').replace(/\n*$/, '\n')
    const body = HERMES_MODEL_KEYS.filter(k => k === 'provider' || k === 'base_url' || vals[k] !== undefined).map(k => '  ' + k + ': ' + vals[k]).join('\n')
    return s + 'model:\n' + body + '\n'
  }
  // 第二遍：既有受管键原地替换值；缺失的键按固定顺序插到 model: 行后；其余一律不动
  const missing = HERMES_MODEL_KEYS.filter(k => !(k in keyLine))
  const out = []
  inModel = false
  for (const ln of lines) {
    const t = ln.trim()
    if (/^model:\s*(#.*)?$/.test(t)) {
      out.push(ln)
      inModel = true
      // 只插入「缺行且需要输出」的键，保持 model 块紧凑
      const toAdd = missing.filter(k => k === 'provider' || k === 'base_url' || vals[k] !== undefined)
      for (const k of toAdd) out.push('  ' + k + ': ' + vals[k])
      continue
    }
    if (inModel && /^\s/.test(ln)) {
      const m = t.match(managedRe)
      if (m) {
        // 受管键：有值 → 原地替换；无值 → 删除该键行
        if (vals[m[1]] !== undefined) out.push('  ' + m[1] + ': ' + vals[m[1]])
        continue
      }
      out.push(ln) // model 块内非受管键：保留
      continue
    }
    if (inModel && /^\S/.test(ln)) inModel = false
    out.push(ln)
  }
  let s = out.join('\n')
  if (!s.endsWith('\n')) s += '\n'
  return s
}
export function hermesYamlPreset() {
  return `# ~/.hermes/config.yaml
# Hermes Agent 参考模版：模型接入指向本路由器
# 完整字段参考 Hermes Agent 官方文档（hermes-agent.nousresearch.com）

model:
  provider: openai-compatible
  base_url: ` + derivedBaseUrl() + `
  api_key: "1"
`
}

// watchers 在 init 中注册（组件 setup 上下文内）
export function initHermes() {
  watch(hermesForm, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'hermes' || hermesMuted) return
    try { cfgContent.value = hermesWriteYaml(cfgContent.value) } catch (e) {}
  }, { deep: true })
}