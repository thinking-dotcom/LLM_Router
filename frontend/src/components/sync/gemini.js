// Gemini CLI（~/.gemini/，固定 /gai 接入）：settings.json 表单 + .env / GEMINI.md 源码
import { ref, watch } from 'vue'
import { cfgObj, cfgEdit, cfgTarget, genericMuted, jsonEcho, syncToJson } from './store'

export const geminiEnvContent = ref('') // ~/.gemini/.env 源码
export const geminiEnvExists = ref(false)
export const geminiEnvFile = ref('~/.gemini/.env')
// GEMINI.md（全局规则，Markdown 源码直编）
export const geminiAgentsContent = ref('')
export const geminiAgentsExists = ref(false)
export const geminiAgentsFile = ref('~/.gemini/GEMINI.md')

// Gemini API Key：真实存储位置是 ~/.gemini/.env 的 GEMINI_API_KEY（Gemini CLI 官方约定），
// 表单字段直接读写 .env 源码内容，保存时随主配置一并落盘
export const geminiApiKey = ref('')
export function geminiApiKeyFromEnv() {
  const m = String(geminiEnvContent.value || '').match(/^GEMINI_API_KEY\s*=\s*(.*)$/m)
  if (!m) return ''
  return m[1].trim().replace(/^"(.*)"$/, '$1').replace(/^'(.*)'$/, '$1')
}
export function geminiSetApiKey(v) {
  const line = 'GEMINI_API_KEY=' + String(v ?? '').trim()
  const lines = String(geminiEnvContent.value || '').split('\n')
  let done = false
  const out = lines.map(ln => {
    if (!done && /^GEMINI_API_KEY\s*=/.test(ln)) { done = true; return line }
    return ln
  })
  if (!done) {
    if (out.length && out[out.length - 1].trim() !== '') out.push('')
    out.push(line)
  }
  geminiEnvContent.value = out.join('\n')
}

export function geminiSettingsPreset() {
  return {
    model: { name: '' },
    ui: { theme: 'dark', hideWindowTitle: true },
    general: { vimMode: false, disableAutoUpdate: false, debugMode: false },
    privacy: { usageStatisticsEnabled: false },
    security: { auth: { selectedType: 'gemini-api-key' } },
    autoAccept: false,
  }
}
export const geminiForm = ref({
  modelName: '',
  thinking: '', effortOpenai: '', budgetAnthropic: '',
  temperature: '', topP: '', topK: '', maxOutputTokens: '',
  theme: 'dark', hideWindowTitle: true,
  vimMode: false, disableAutoUpdate: false, debugMode: false,
  usageStatisticsEnabled: '', // 权限三态：'' 不写 / 'true' / 'false'
  selectedAuthType: 'gemini-api-key',
  autoAccept: '', // 权限三态：'' 不写 / 'true' / 'false'
})
export function initGeminiForm() {
  const c = cfgObj.value || {}
  const obj = v => (v && typeof v === 'object' && !Array.isArray(v)) ? v : {}
  const gen = obj(c.general), ui = obj(c.ui), priv = obj(c.privacy)
  const mdl = obj(c.model), auth = obj(obj(c.security).auth)
  // 回填推理深度（三种家族参数各自独立）：
  // Gemini：thinkingBudget=0 → off；否则取 thinkingLevel；仅有预算时按阈值折算
  const gcc = obj(mdl.generateContentConfig), tc = obj(gcc.thinkingConfig)
  let thinking = ''
  if (tc.thinkingBudget === 0) thinking = 'off'
  else if (tc.thinkingLevel) thinking = String(tc.thinkingLevel).toLowerCase()
  else {
    const b = Number(tc.thinkingBudget)
    if (isFinite(b) && b > 0) thinking = b < 2048 ? 'low' : (b < 16384 ? 'medium' : 'high')
  }
  // 回填采样参数（留空 = 不显式携带，交由模型/路由器默认）
  const numOr = v => (v !== null && v !== undefined && v !== '' && isFinite(Number(v))) ? String(v) : ''
  geminiForm.value = {
    modelName: mdl.name || '',
    thinking,
    effortOpenai: typeof gcc.reasoningEffort === 'string' ? gcc.reasoningEffort : '',
    budgetAnthropic: Number(gcc.anthropicThinkingBudget) > 0 ? String(gcc.anthropicThinkingBudget) : '',
    temperature: numOr(gcc.temperature),
    topP: numOr(gcc.topP),
    topK: numOr(gcc.topK),
    maxOutputTokens: numOr(gcc.maxOutputTokens),
    theme: ui.theme || 'dark',
    hideWindowTitle: ui.hideWindowTitle !== false,
    vimMode: !!gen.vimMode,
    disableAutoUpdate: !!gen.disableAutoUpdate,
    debugMode: !!gen.debugMode,
    usageStatisticsEnabled: priv.usageStatisticsEnabled === true ? 'true' : priv.usageStatisticsEnabled === false ? 'false' : '',
    selectedAuthType: auth.selectedType || 'gemini-api-key',
    autoAccept: c.autoAccept === true ? 'true' : c.autoAccept === false ? 'false' : '',
  }
}
// 合并写回：只覆盖表单管控的字段，其余（mcpServers/context/tools 等）原样保留
export function geminiApplyToObj() {
  const c = cfgObj.value
  if (!c || typeof c !== 'object') return
  const f = geminiForm.value
  if (!c.model || typeof c.model !== 'object') c.model = {}
  if (String(f.modelName).trim()) c.model.name = String(f.modelName).trim()
  else if (c.model.name != null) delete c.model.name
  // 推理深度（三种家族参数并存，写入 model.generateContentConfig；
  // 路由器按实际请求的模型类型择一使用并剥离其余两种）：
  // - Gemini 家族：thinkingConfig（off 用 thinkingBudget=0，档位用 thinkingLevel）
  // - OpenAI 兼容家族：reasoningEffort
  // - Anthropic 家族：anthropicThinkingBudget（token 预算）
  if (f.thinking || f.effortOpenai || f.budgetAnthropic || f.temperature || f.topP || f.topK || f.maxOutputTokens) {
    if (!c.model.generateContentConfig || typeof c.model.generateContentConfig !== 'object') c.model.generateContentConfig = {}
  }
  const gcc = (c.model.generateContentConfig && typeof c.model.generateContentConfig === 'object') ? c.model.generateContentConfig : null
  if (gcc) {
    if (f.thinking) {
      gcc.thinkingConfig = f.thinking === 'off' ? { thinkingBudget: 0 } : { thinkingLevel: f.thinking.toUpperCase() }
    } else {
      delete gcc.thinkingConfig
    }
    if (f.effortOpenai) gcc.reasoningEffort = f.effortOpenai
    else delete gcc.reasoningEffort
    if (f.budgetAnthropic) gcc.anthropicThinkingBudget = Number(f.budgetAnthropic)
    else delete gcc.anthropicThinkingBudget
    // 采样参数：留空删除（交由默认），合法数值写入（Gemini/OpenAI/Anthropic 三代请求按需使用）
    const numOr = v => (v === '' || v == null || !isFinite(Number(v))) ? null : Number(v)
    const tv = numOr(f.temperature)
    if (tv != null) gcc.temperature = tv
    else delete gcc.temperature
    const pv = numOr(f.topP)
    if (pv != null) gcc.topP = pv
    else delete gcc.topP
    const kv = numOr(f.topK)
    if (kv != null) gcc.topK = kv
    else delete gcc.topK
    const mo = numOr(f.maxOutputTokens)
    if (mo != null) gcc.maxOutputTokens = mo
    else delete gcc.maxOutputTokens
    if (!Object.keys(gcc).length) delete c.model.generateContentConfig
  }
  if (!c.ui || typeof c.ui !== 'object') c.ui = {}
  if (String(f.theme).trim()) c.ui.theme = String(f.theme).trim()
  c.ui.hideWindowTitle = !!f.hideWindowTitle
  if (!c.general || typeof c.general !== 'object') c.general = {}
  c.general.vimMode = !!f.vimMode
  c.general.disableAutoUpdate = !!f.disableAutoUpdate
  c.general.debugMode = !!f.debugMode
  // 权限三态：true / false 显式写入；'' 不写（移除已有键）
  if (!c.privacy || typeof c.privacy !== 'object') c.privacy = {}
  if (f.usageStatisticsEnabled === 'true') c.privacy.usageStatisticsEnabled = true
  else if (f.usageStatisticsEnabled === 'false') c.privacy.usageStatisticsEnabled = false
  else if ('usageStatisticsEnabled' in c.privacy) delete c.privacy.usageStatisticsEnabled
  if (!c.security || typeof c.security !== 'object') c.security = {}
  if (!c.security.auth || typeof c.security.auth !== 'object') c.security.auth = {}
  if (String(f.selectedAuthType).trim()) c.security.auth.selectedType = String(f.selectedAuthType).trim()
  if (f.autoAccept === 'true') c.autoAccept = true
  else if (f.autoAccept === 'false') c.autoAccept = false
  else if ('autoAccept' in c) delete c.autoAccept
}

// watchers 在 init 中注册（组件 setup 上下文内）
export function initGemini() {
  watch(geminiApiKey, () => {
    if (!cfgEdit.value || cfgTarget.value?.id !== 'gemini') return
    if (geminiApiKeyFromEnv() === geminiApiKey.value) return // 同值不重写 .env
    geminiSetApiKey(geminiApiKey.value)
  })
// 表单 → 模版 JSON 实时联动：填写 Gemini 面板时，settings.json 源码文本框实时更新
	// genericMuted 与其它客户端一致：载入期间不触发表单→源码的重写
	watch(geminiForm, () => {
		if (!cfgEdit.value || cfgTarget.value?.id !== 'gemini' || genericMuted || jsonEcho) return
		try { geminiApplyToObj(); syncToJson() } catch (e) {}
	}, { deep: true })
}