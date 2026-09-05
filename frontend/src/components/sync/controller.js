import { setCcMuted, setCcModelsReady, setCcModelsSeeded } from './cc'
import { setClineModelsMuted, setClineConnMuted, setClineGlobalMuted, setClineCapsMuted } from './cline'
import { setOcMuted } from './opencode'
import { setHermesMuted } from './hermes'
import { setCodexMuted } from './codex'
import { setKimiMuted } from './kimi'
import { setGrokMuted } from './grok'
import { setGenericMuted, setJsonEcho } from './store'
// Clients.vue 中枢：currentCfgFile/currentCfgExists、switchMode、通用表单↔源码联动、
// buildFormText / saveTargetConfig / openTargetConfig，以及跨客户端的统一模型下拉。
// 依赖全部客户端模块（单向：controller → clients → store/shared，无反向依赖）。
import { ref, computed, watch, nextTick } from 'vue'
import { toast } from '../../toast'
import { t } from '../../i18n'
import api from '../../api'
import { jsonErrorPos, repairJsonText, pruneIllegalTemplateKeys } from '../../utils'
import {
  cfgEdit, cfgInfoOpen, cfgTarget, cfgLoading, cfgSaving, cfgMode, cfgFormat, cfgFile, cfgExists,
  cfgContent, cfgObj, ensureClientPaths, loadModelOptions, modelOptions, syncToJson, genericMuted,
  jsonEcho, GENERIC_EXCLUDED_IDS, routerHintForTarget,
} from './store'
import { claudeEnvModels, normalizeClaudeEnvModels, initClaudeExtra, syncClaudeEnvModelsToObj, syncClaudeExtraToObj, claudeBuildFormText, loadClaudeMarkdown, saveClaudeMarkdown } from './claude'
import { initPiForm, piBuildModelsText, piSettingsBuildText, piSettingsContent, piSettingsExists, piSettingsFile, syncSettingsFromSource, piMcpContent, piMcpExists, piMcpFile, piAgentsContent, piAgentsExists, piAgentsFile, piSettingsPreset, initPiSettingsForm, fetchPiBaseModels } from './pi'
import { grokBuildToml, grokReadToml, grokMuted, grokAgentsContent, grokAgentsExists, grokAgentsFile, grokTomlPreset, grokBlocks, grokOrder, grokUnmanaged, grokUiExtras, grokForm, grokEmptyForm, fetchGrokBaseModels } from './grok'
import { codexInitFrom, codexWriteToml, codexMuted, codexAgentsContent, codexAgentsExists, codexAgentsFile, codexTomlPreset, codexForm, CODEX_RESERVED_PROVIDERS } from './codex'
import { kimiBuildToml, kimiReadToml, kimiMuted, kimiAgentsContent, kimiAgentsExists, kimiAgentsFile, kimiTomlPreset, kimiForm, kimiProviderRows, kimiModelRows, kimiThinking, kimiPermRows, kimiUnmanaged } from './kimi'
import { geminiApplyToObj, initGeminiForm, geminiSettingsPreset, geminiEnvContent, geminiEnvExists, geminiEnvFile, geminiAgentsContent, geminiAgentsExists, geminiAgentsFile, geminiApiKey, geminiApiKeyFromEnv, geminiForm } from './gemini'
import { ocApplyToObj, initOcForm, ocMuted, ocAgentsContent, ocAgentsExists, ocAgentsFile, ocModels } from './opencode'
import { clineModelsContent, clineModelsExists, clineModelsFile, clineProvidersContent, clineProvidersExists, clineProvidersFile, clineAgentsContent, clineAgentsExists, clineAgentsFile, clineGlobalStateContent, clineGlobalStateExists, clineGlobalStateFile, initClineModelsForm, initClineConnForm, initClineGlobalForm, clineModelsMuted, clineConnMuted, clineGlobalMuted, applyClineModelsForm, clineAutoFillModels } from './cline'
import { ccApplyToObj, initCcForm, ccSeedModelsFromList, ccLoadNoLogin, ccSettingsContent, ccSettingsExists, ccSettingsFile, ccProvidersContent, ccProvidersExists, ccProvidersFile, ccApplyToSettings, ccApplyToProviders, ccNoLogin, ccForm, ccMuted, ccModelsReady, ccModelsSeeded, ccNoLoginMsg, ccModelOpen, ccEffortOpen, ccEffortPick } from './cc'
import { writeCopilotEnv, saveCopilotPerm, loadCopilotEnv, loadCopilotPerm, copilotPermFile, copilotPermExists } from './copilot'
import { hermesInitFrom, hermesWriteYaml, hermesMuted, hermesYamlPreset } from './hermes'
import { kiloApplyToObj, kiloFromSource, initKiloForm, kiloModels, kiloModel, kiloSmallModel, kiloSubagentModel, kiloApiKey, kiloProviderKey, kiloProviderPrefix, kiloQualifyModel, kiloAgentsContent, kiloAgentsExists, kiloAgentsFile, parseJsonc, setKiloMuted } from './kilo'
import { agyApplyToObj, initAgyForm, agySettingsPreset, agyMcpContent, agyMcpExists, agyMcpFile, agyForm, writeAgyEnv, loadAgyEnv, loadAgyAliasPanel, applyAgyAliases } from './agy'

// ============ 当前编辑文件 / 存在态（各客户端专属页签） ============
export function currentCfgFile() {
  if (cfgTarget.value?.id === 'pi') {
    if (cfgMode.value === 'globjson') return piSettingsFile.value
    if (cfgMode.value === 'pimcp') return piMcpFile.value
    if (cfgMode.value === 'agentsmd') return piAgentsFile.value
  }
  if (cfgTarget.value?.id === 'grok-build' && cfgMode.value === 'grokagents') return grokAgentsFile.value
  if (cfgTarget.value?.id === 'codex' && cfgMode.value === 'codexagents') return codexAgentsFile.value
  if (cfgTarget.value?.id === 'kimi' && cfgMode.value === 'kimiagents') return kimiAgentsFile.value
  if (cfgTarget.value?.id === 'cline') {
    if (cfgMode.value === 'clinemodels') return clineModelsFile.value
    if (cfgMode.value === 'clineproviders') return clineProvidersFile.value
    if (cfgMode.value === 'clineagents') return clineAgentsFile.value
    if (cfgMode.value === 'clineglobalstate') return clineGlobalStateFile.value
  }
  if (cfgTarget.value?.id === 'commandcode') {
    if (cfgMode.value === 'ccsettings') return ccSettingsFile.value
    if (cfgMode.value === 'ccproviders') return ccProvidersFile.value
  }
  if (cfgTarget.value?.id === 'gemini') {
    if (cfgMode.value === 'geminienv') return geminiEnvFile.value
    if (cfgMode.value === 'geminimd') return geminiAgentsFile.value
  }
  if (cfgTarget.value?.id === 'copilot' && cfgMode.value === 'copilotperm') return copilotPermFile.value
  if (cfgTarget.value?.id === 'agy' && cfgMode.value === 'agymcp') return agyMcpFile.value
  return cfgFile.value
}
export function currentCfgExists() {
  if (cfgTarget.value?.id === 'pi') {
    if (cfgMode.value === 'globjson') return piSettingsExists.value
    if (cfgMode.value === 'pimcp') return piMcpExists.value
    if (cfgMode.value === 'agentsmd') return piAgentsExists.value
  }
  if (cfgTarget.value?.id === 'grok-build' && cfgMode.value === 'grokagents') return grokAgentsExists.value
  if (cfgTarget.value?.id === 'codex' && cfgMode.value === 'codexagents') return codexAgentsExists.value
  if (cfgTarget.value?.id === 'kimi' && cfgMode.value === 'kimiagents') return kimiAgentsExists.value
  if (cfgTarget.value?.id === 'cline') {
    if (cfgMode.value === 'clinemodels') return clineModelsExists.value
    if (cfgMode.value === 'clineproviders') return clineProvidersExists.value
    if (cfgMode.value === 'clineagents') return clineAgentsExists.value
    if (cfgMode.value === 'clineglobalstate') return clineGlobalStateExists.value
  }
  if (cfgTarget.value?.id === 'commandcode') {
    if (cfgMode.value === 'ccsettings') return ccSettingsExists.value
    if (cfgMode.value === 'ccproviders') return ccProvidersExists.value
  }
  if (cfgTarget.value?.id === 'gemini') {
    if (cfgMode.value === 'geminienv') return geminiEnvExists.value
    if (cfgMode.value === 'geminimd') return geminiAgentsExists.value
  }
  if (cfgTarget.value?.id === 'copilot' && cfgMode.value === 'copilotperm') return copilotPermExists.value
  if (cfgTarget.value?.id === 'agy' && cfgMode.value === 'agymcp') return agyMcpExists.value
  return cfgExists.value
}

// JSON 源码页签标题：统一显示实际文件名（取配置文件路径末段，pi/gemini/grok/kimi 固定知名文件）
export const tabJsonLabel = computed(() => {
  const id = cfgTarget.value?.id
  if (id === 'pi') return 'models.json'
  if (id === 'gemini') return 'settings.json'
  if (id === 'grok-build') return 'config.toml'
  if (id === 'kimi') return 'config.toml'
  if (id === 'kilo') return 'kilo.jsonc'
  const base = String(cfgFile.value || '').split(/[\\/]/).pop()
  return base || t('sync.tabJson')
})

export function switchMode(m) {
  // 进入 JSON 源码页签时，把当前表单内容序列化为源码
  if (m === 'json' && cfgMode.value === 'form') {
    if (cfgFormat.value === 'toml') {
      if (cfgTarget.value?.id === 'grok-build') cfgContent.value = grokBuildToml()
      else if (cfgTarget.value?.id === 'codex') cfgContent.value = codexWriteToml(cfgContent.value)
      else if (cfgTarget.value?.id === 'kimi') cfgContent.value = kimiBuildToml()
    }
    else if (cfgTarget.value?.id === 'hermes') cfgContent.value = hermesWriteYaml(cfgContent.value)
    else if (cfgTarget.value?.id === 'pi') cfgContent.value = piBuildModelsText()
    else if (cfgTarget.value?.id === 'opencode') { ocApplyToObj(); syncToJson() }
    else if (cfgTarget.value?.id === 'gemini') { geminiApplyToObj(); syncToJson() }
    else if (cfgTarget.value?.id === 'commandcode') { ccApplyToObj(); syncToJson() }
    else if (cfgTarget.value?.id === 'kilo') { kiloApplyToObj(); syncToJson() }
    else if (cfgTarget.value?.id === 'agy') { agyApplyToObj(); syncToJson() }
    else syncToJson()
  }
  if (m === 'form' && cfgMode.value === 'json') {
    // TOML/YAML 原文流不做 JSON 回填，由下方各客户端专属回读处理
    if (cfgFormat.value !== 'toml' && cfgFormat.value !== 'yaml') {
      // Kilo 为 JSONC（可含注释/尾逗号）：走 JSONC 宽容解析回填
      if (cfgTarget.value?.id === 'kilo') { if (!kiloFromSource()) return }
      else if (!syncToObjFromSource()) return
    }
    // initXxxForm 整体替换表单会触发回写 watcher；统一静默包裹，
    // 避免刚从 JSON 解析出的源码被表单重建（丢失未管字段）无提示覆写
    setGenericMuted(true)
    if (cfgTarget.value?.id === 'pi') initPiForm()
    if (cfgTarget.value?.id === 'opencode') initOcForm()
    if (cfgTarget.value?.id === 'gemini') initGeminiForm()
    if (cfgTarget.value?.id === 'commandcode') initCcForm()
    if (cfgTarget.value?.id === 'kilo') { setKiloMuted(true); initKiloForm(); nextTick(() => { setKiloMuted(false) }) }
    if (cfgTarget.value?.id === 'grok-build') { setGrokMuted(true); grokReadToml(cfgContent.value); nextTick(() => { setGrokMuted(false) }) }
    if (cfgTarget.value?.id === 'codex') { setCodexMuted(true); codexInitFrom(cfgContent.value); nextTick(() => { setCodexMuted(false) }) }
    if (cfgTarget.value?.id === 'hermes') { setHermesMuted(true); hermesInitFrom(cfgContent.value); nextTick(() => { setHermesMuted(false) }) }
    if (cfgTarget.value?.id === 'kimi') { setKimiMuted(true); kimiReadToml(cfgContent.value); nextTick(() => { setKimiMuted(false) }) }
    if (cfgTarget.value?.id === 'agy') initAgyForm()
    nextTick(() => { setGenericMuted(false) })
  }
  // 离开全局设置源码页签时回填表单；进入时序列化源码
  if (cfgMode.value === 'globjson' && m !== 'globjson') {
    if (!syncSettingsFromSource()) return
  }
  if (m === 'globjson') {
    piSettingsContent.value = piSettingsBuildText()
  }
  cfgMode.value = m
}
// switchMode 内的 JSON→表单回填：与 syncToObj 同语义（避免与 store.syncToObj 命名冲突）
function syncToObjFromSource() {
  try {
    const v = JSON.parse(cfgContent.value || '{}')
    cfgObj.value = v && typeof v === 'object' ? v : {}
    return true
  } catch (e) { toast(t('sync.jsonError')); return false }
}

// ============ 统一模型下拉（pi-sel 样式）：claude / opencode / gemini 复用 ============
export const claudeModelOpen = ref(-1)
export const ocModelOpen = ref(-1)
export const geminiModelOpen = ref(false)
export function pickClaudeModel(i, m) { if (claudeEnvModels.value[i]) claudeEnvModels.value[i].modelId = m; claudeModelOpen.value = -1 }
export function pickOcModel(i, m) { if (ocModels.value[i]) ocModels.value[i].id = m; ocModelOpen.value = -1 }
export function pickGeminiModel(m) { geminiForm.value.modelName = m; geminiModelOpen.value = false }

// ============ 构建 / 保存 / 打开 ============
export function buildFormText() {
  const id = cfgTarget.value.id
  if (id === 'claude-code') {
    return claudeBuildFormText()
  }
  if (id === 'pi') return piBuildModelsText()
  if (id === 'gemini') { geminiApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'opencode') { ocApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'commandcode') { ccApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'kilo') { kiloApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'agy') { agyApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'grok-build') return grokBuildToml()
  if (id === 'codex') return codexWriteToml(cfgContent.value)
  if (id === 'hermes') return hermesWriteYaml(cfgContent.value)
  if (id === 'kimi') return kimiBuildToml()
  return JSON.stringify(cfgObj.value, null, 2)
}

export async function saveTargetConfig() {
  if (!cfgTarget.value) return
  // codex：提供商标识不得使用 Codex 内置保留 ID（会被忽略导致接入失效）
  if (cfgTarget.value.id === 'codex') {
    const pk = (codexForm.value.providerKey || '').trim() || 'llmrouter'
    if (CODEX_RESERVED_PROVIDERS.includes(pk)) { toast(t('sync.codexReserved', { pk })); return }
  }
  // Gemini .env 源码模式：只保存 ~/.gemini/.env，不动主配置
  if (cfgTarget.value.id === 'gemini' && cfgMode.value === 'geminienv') {
    cfgSaving.value = true
    try {
      const g = await api.SaveGeminiDotEnv(geminiEnvContent.value)
      if (g && g.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: g?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Copilot CLI 环境变量 BYOK 模式：不落盘配置文件，直接写入用户环境变量
  if (cfgTarget.value.id === 'copilot' && cfgMode.value === 'copilotenv') {
    if (await writeCopilotEnv()) cfgEdit.value = false
    return
  }
  // Copilot CLI 工具审批权限：保存 ~/.copilot/permissions-config.json（源码直编）
  if (cfgTarget.value.id === 'copilot' && cfgMode.value === 'copilotperm') {
    cfgSaving.value = true
    let ok = false
    try { ok = await saveCopilotPerm() } finally { cfgSaving.value = false }
    if (ok) cfgEdit.value = false
    return
  }
  // Antigravity CLI mcp_config.json 源码模式：只保存 ~/.gemini/config/mcp_config.json，不动主配置
  if (cfgTarget.value.id === 'agy' && cfgMode.value === 'agymcp') {
    try { if (agyMcpContent.value.trim()) JSON.parse(agyMcpContent.value) } catch (e) { const p = jsonErrorPos(agyMcpContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    try {
      const am = await api.SaveAgyMcp(agyMcpContent.value)
      if (am && am.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: am?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Gemini GEMINI.md 源码模式：只保存 ~/.gemini/GEMINI.md，不动主配置
  if (cfgTarget.value.id === 'gemini' && cfgMode.value === 'geminimd') {
    cfgSaving.value = true
    try {
      const ga = await api.SaveGeminiAgents(geminiAgentsContent.value)
      if (ga && ga.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: ga?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // OpenCode AGENTS.md 源码模式：只保存 AGENTS.md，不动主配置
  if (cfgTarget.value.id === 'opencode' && cfgMode.value === 'openagents') {
    cfgSaving.value = true
    try {
      const oa = await api.SaveOpencodeAgents(ocAgentsContent.value)
      if (oa && oa.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: oa?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Kilo AGENTS.md 源码模式：只保存 AGENTS.md，不动主配置
  if (cfgTarget.value.id === 'kilo' && cfgMode.value === 'kiloagents') {
    cfgSaving.value = true
    try {
      const ka = await api.SaveKiloAgents(kiloAgentsContent.value)
      if (ka && ka.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: ka?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Codex AGENTS.md 源码模式：只保存 ~/.codex/AGENTS.md，不动主配置
  if (cfgTarget.value.id === 'codex' && cfgMode.value === 'codexagents') {
    cfgSaving.value = true
    try {
      const r = await api.SaveCodexAgents(codexAgentsContent.value)
      if (r && r.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: r?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Cline models.json 源码模式：只保存 ~/.cline/data/settings/models.json，不动主配置
  if (cfgTarget.value.id === 'cline' && cfgMode.value === 'clinemodels') {
    try { if (clineModelsContent.value.trim()) JSON.parse(clineModelsContent.value) } catch (e) { const p = jsonErrorPos(clineModelsContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    try {
      const r = await api.SaveClineModels(clineModelsContent.value)
      if (r && r.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: r?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Cline providers.json 源码模式：只保存 ~/.cline/data/settings/providers.json，不动主配置
  if (cfgTarget.value.id === 'cline' && cfgMode.value === 'clineproviders') {
    try { if (clineProvidersContent.value.trim()) JSON.parse(clineProvidersContent.value) } catch (e) { const p = jsonErrorPos(clineProvidersContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    try {
      const r = await api.SaveClineProviders(clineProvidersContent.value)
      if (r && r.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: r?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Cline AGENTS.md 源码模式：只保存 ~/.agents/AGENTS.md，不动主配置
  if (cfgTarget.value.id === 'cline' && cfgMode.value === 'clineagents') {
    cfgSaving.value = true
    try {
      const r = await api.SaveClineAgents(clineAgentsContent.value)
      if (r && r.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: r?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // JSON 源码自动格式化：合法则原地美化（页签显示与落盘内容同步），非法返回 false 由调用方提示
  function autoFormatJson(refv) {
    if (!String(refv.value || '').trim()) return true
    try { refv.value = JSON.stringify(JSON.parse(refv.value), null, 2) + '\n'; return true }
    catch (e) { return false }
  }
  // Cline globalState.json 源码模式：只保存 ~/.cline/data/globalState.json（真实文件直写），不动主配置
  if (cfgTarget.value.id === 'cline' && cfgMode.value === 'clineglobalstate') {
    if (clineGlobalStateContent.value.trim() && !autoFormatJson(clineGlobalStateContent)) { const p = jsonErrorPos(clineGlobalStateContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    try {
      const r = await api.SaveClineGlobalState(clineGlobalStateContent.value)
      if (r && r.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: r?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Command Code settings.json 源码模式：只保存 ~/.commandcode/settings.json，不动主配置
  if (cfgTarget.value.id === 'commandcode' && cfgMode.value === 'ccsettings') {
    if (ccSettingsContent.value.trim() && !autoFormatJson(ccSettingsContent)) { const p = jsonErrorPos(ccSettingsContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    try {
      const r = await api.SaveCommandcodeSettings(ccSettingsContent.value)
      if (r && r.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: r?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Command Code providers.json 源码模式：只保存 ~/.commandcode/providers.json，不动主配置
  if (cfgTarget.value.id === 'commandcode' && cfgMode.value === 'ccproviders') {
    if (ccProvidersContent.value.trim() && !autoFormatJson(ccProvidersContent)) { const p = jsonErrorPos(ccProvidersContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    try {
      const r = await api.SaveCommandcodeProviders(ccProvidersContent.value)
      if (r && r.ok) { toast(t('sync.configSaved')); cfgEdit.value = false }
      else toast(t('common.fail', { msg: r?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Claude Code CLAUDE.md 源码模式：只保存 ~/.claude/CLAUDE.md，不动主配置
  if (cfgTarget.value.id === 'claude-code' && cfgMode.value === 'claudemd') {
    cfgSaving.value = true
    try {
      const cmErr = await saveClaudeMarkdown()
      if (cmErr) toast(t('common.fail', { msg: cmErr }))
      else toast(t('sync.configSaved')); cfgEdit.value = false
    } finally { cfgSaving.value = false }
    return
  }
  let text = ''
  if (cfgMode.value === 'form') {
    try { text = buildFormText() } catch (e) { toast(t('sync.jsonError')); return }
  } else {
    text = cfgContent.value
    // TOML/YAML/JSONC 源码不做前端 JSON 校验（hermes 由后端 SaveTargetConfig 做 YAML 校验；
    // kilo 的 jsonc 允许注释/尾逗号，由后端 jsoncToJSON 宽容校验）
    if (cfgFormat.value !== 'toml' && cfgFormat.value !== 'yaml' && cfgFormat.value !== 'jsonc') {
      if (text.trim() && !autoFormatJson(cfgContent)) { const p = jsonErrorPos(text); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
      text = cfgContent.value
    }
  }
  // pi 全局设置源码模式：校验 + 自动格式化后再保存
  if (cfgTarget.value.id === 'pi' && cfgMode.value === 'globjson') {
    if (piSettingsContent.value.trim() && !autoFormatJson(piSettingsContent)) { const p = jsonErrorPos(piSettingsContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
  }
  // agy 合并：模型映射随「保存」一并落盘——先解析映射文本并回写路由器服务商 aliases，
  // 失败（目标模型解析不到已启用服务商等）即中止整次保存，避免无效映射仍写入 settings.json
  if (cfgTarget.value.id === 'agy') {
    cfgSaving.value = true
    const ae = await applyAgyAliases()
    cfgSaving.value = false
    if (ae) { toast(t('common.fail', { msg: ae })); return }
  }
  cfgSaving.value = true
  try {
    const r = await api.SaveTargetConfig(cfgTarget.value.id, text)
    let saveErr = ''
    // pi 合并：全局设置、AGENTS.md 与 models.json 一同保存
    if (cfgTarget.value.id === 'pi') {
      const setText = cfgMode.value === 'globjson' ? piSettingsContent.value : piSettingsBuildText()
      const s = await api.SavePiSettings(setText)
      if (!(s && s.ok)) saveErr = s?.error || t('sync.saveFailPiSettings')
      const mc = await api.SavePiMcp(piMcpContent.value)
      if (mc && !mc.ok && !saveErr) saveErr = mc?.error || t('sync.saveFailMcp')
      const ag = await api.SavePiAgents(piAgentsContent.value)
      if (ag && !ag.ok && !saveErr) saveErr = ag?.error || t('sync.saveFailAgents')
    }
    // grok 合并：AGENTS.md 与 config.toml 一同保存
    if (cfgTarget.value.id === 'grok-build') {
      const ga = await api.SaveGrokAgents(grokAgentsContent.value)
      if (ga && !ga.ok && !saveErr) saveErr = ga?.error || t('sync.saveFailAgents')
    }
    // kimi 合并：AGENTS.md（全局 Kimi 专属 Agent 指令）与 config.toml 一同保存
    if (cfgTarget.value.id === 'kimi') {
      const ka = await api.SaveKimiAgents(kimiAgentsContent.value)
      if (ka && !ka.ok && !saveErr) saveErr = ka?.error || t('sync.saveFailAgents')
    }
    // gemini 合并：.env（含 GEMINI_API_KEY）与 settings.json 一同保存
    if (cfgTarget.value.id === 'gemini') {
      const ge = await api.SaveGeminiDotEnv(geminiEnvContent.value)
      if (ge && !ge.ok && !saveErr) saveErr = ge?.error || t('sync.saveFailDotEnv')
    }
    // cline 合并：models.json（模型清单）、providers.json（连接凭据）与 globalState.json（全局默认）随主配置一同保存。
    // 先校验三份源码均为合法 JSON：表单联动经 srcObj 解析，非法 JSON 会被静默当作 {}，
    // 若不在此拦截会把损坏内容重建为最小结构落盘（与源码页签保存前的校验保持一致）
    if (cfgTarget.value.id === 'cline') {
      for (const [nm, rv] of [['models.json', clineModelsContent], ['providers.json', clineProvidersContent], ['globalState.json', clineGlobalStateContent]]) {
        const t = String(rv.value || '').trim()
        if (!t) continue
        try { JSON.parse(t) } catch (e) {
          const p = jsonErrorPos(rv.value)
          toast(p ? `${nm} ${t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg })}` : `${nm} ${t('sync.jsonError')}`)
          return
        }
      }
      try { applyClineModelsForm() } catch (e) {}
      if (!saveErr && clineModelsContent.value.trim()) {
        const cm = await api.SaveClineModels(clineModelsContent.value)
        if (!(cm && cm.ok)) saveErr = cm?.error || t('sync.saveFailModels')
      }
      if (!saveErr && clineProvidersContent.value.trim()) {
        const cp = await api.SaveClineProviders(clineProvidersContent.value)
        if (!(cp && cp.ok)) saveErr = cp?.error || t('sync.saveFailProviders')
      }
      if (!saveErr && clineGlobalStateContent.value.trim()) {
        const gs = await api.SaveClineGlobalState(clineGlobalStateContent.value)
        if (!(gs && gs.ok)) saveErr = gs?.error || t('sync.saveFailGlobal')
      }
    }
    // commandcode 合并：providers.json（BYOK 模型端点）与 settings.json（权限、推理深度）随主配置一同保存
    if (cfgTarget.value.id === 'commandcode') {
      // 模型清单以点选/手写为准，保存时不做自动全量合并（避免把用户点选取消的模型加回）
      try { ccApplyToSettings(); ccApplyToProviders() } catch (e) {}
      if (!saveErr && ccProvidersContent.value.trim()) {
        const cp = await api.SaveCommandcodeProviders(ccProvidersContent.value)
        if (!(cp && cp.ok)) saveErr = cp?.error || t('sync.saveFailProviders')
      }
      if (!saveErr && ccSettingsContent.value.trim()) {
        const cs = await api.SaveCommandcodeSettings(ccSettingsContent.value)
        if (!(cs && cs.ok)) saveErr = cs?.error || t('sync.saveFailSettings')
      }
      // 自动免登录：provider 指向本地服务商（非 command-code）时，保存即写入免登录凭据
      //（环境变量 setx + auth.json，当前终端立即生效），无需手动勾选；
      // 填 command-code（官方云端订阅）时保持用户手动选择，不强行干预
      if (!saveErr) {
        const prov = String(ccForm.value.provider || '').trim() || String(ccForm.value.endpointId || '').trim() || 'llmrouter'
        if (prov !== 'command-code') {
          ccNoLogin.value = true
          const nl = await api.SetCommandcodeNoLogin(true)
          if (nl) saveErr = t('sync.noLoginWriteFail', { msg: nl })
        }
      }
    }
    // codex 合并：env_key 指定的用户环境变量自动映射为路由 Token（取自设置页 authToken；
    // 变量已存在且值相同则幂等跳过；路由未设 Token 时写空值，仅保证变量存在让 Codex 通过检查）
    if (cfgTarget.value.id === 'codex') {
      // codexInitFrom 会整体重置表单并触发回写 watcher；临时静默避免保存后源码被无提示重写
      setCodexMuted(true)
      const cf = codexInitFrom(text)
      nextTick(() => { setCodexMuted(false) })
      var ek = cf.apiKey ? '' : cf.envKey // api_key 明文已写入配置文件时无需环境变量
      if (ek) {
        const c = await api.GetConfig()
        const se = await api.SetUserEnv(ek, String((c && c.authToken) || ''))
        if (!(se && se.ok) && !saveErr) saveErr = se?.error || t('sync.envWriteFail')
      }
    }
    // agy 合并：modelProvider 为 gemini 时自动写接入环境变量（agy 官方契约：
    // 端点读 GOOGLE_GEMINI_BASE_URL、API Key 读 GEMINI_API_KEY，settings.json 的
    // baseUrl/apiKey 字段不被 CLI 读取，仅靠这两个变量让 CLI 连到本路由器）
    if (cfgTarget.value.id === 'agy') {
      const mp = String((cfgObj.value && cfgObj.value.modelProvider) || '').trim().toLowerCase()
      if (mp === 'gemini') {
        const aew = await writeAgyEnv()
        if (aew && !saveErr) saveErr = aew
      }
    }
    if (r && r.ok && !saveErr) { toast(t('sync.configSaved')); cfgEdit.value = false }
    else toast(t('common.fail', { msg: (r && !r.ok ? r.error : saveErr) || '?' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
}

export async function openTargetConfig(tg) {
  if (!tg || tg.placeholder || cfgLoading.value) return
  cfgEdit.value = true
  cfgLoading.value = true
  ensureClientPaths()
  setGenericMuted(true) // 模板载入期间静默通用双向联动，避免赋值触发回写打乱源码键序
  setGrokMuted(true) // 同上：静默 grokForm 联动，避免载入时重写 config.toml 丢失注释与非受管内容
  setKimiMuted(true) // 同上：静默 kimiForm 联动，避免载入/重置时重写 config.toml
  setKiloMuted(true) // 同上：静默 kiloForm 联动，避免载入时重写 kilo.jsonc
  cfgTarget.value = tg
  cfgInfoOpen.value = false
  cfgMode.value = 'form'
  cfgFormat.value = tg.configFormat || 'json'
  cfgFile.value = tg.configFile || ''
  cfgExists.value = true
  cfgContent.value = ''
  cfgObj.value = {}
  grokBlocks.value = {}
  grokUnmanaged.value = []
  grokUiExtras.value = []
  grokOrder.value = []
  grokUnmanaged.value = []
  grokForm.value = grokEmptyForm()
  kimiForm.value = { defaultModel: '', permMode: '' }
  kimiProviderRows.value = []
  kimiModelRows.value = []
  kimiThinking.value = { enabled: '', effort: '', keep: '', extra: [] }
  kimiPermRows.value = []
  kimiUnmanaged.value = []
  kiloModels.value = []
  kiloModel.value = ''
  kiloSmallModel.value = ''
  kiloSubagentModel.value = ''
  kiloApiKey.value = ''
  setCcModelsReady(false)
  setCcModelsSeeded(false)
  loadModelOptions()
  try {
    const r = await api.GetTargetConfig(tg.id)
    if (r && r.ok) {
      cfgFile.value = r.file || cfgFile.value
      cfgExists.value = r.exists !== false
      // hasContent 按内容判定（而非 exists）：后端在真实文件缺失时返回首次初始值
      // 模板内容（exists=false），编辑器仍应展示该初始值并可保存部署到真实文件
      let hasContent = !!r.content
      // 第 2 步：校验与修复（仅 JSON）。可修复 → 直接回写真实配置文件；修不动 → 保留原文走错误提示
      if (hasContent && (tg.configFormat || 'json') === 'json') {
        const rep = repairJsonText(r.content)
        if (!rep) {
          toast(t('sync.brokenJsonToast', { name: tg.name }))
        } else {
          let text = rep.text
          const noteList = [...rep.notes]
          try {
            const v = JSON.parse(text)
            if (pruneIllegalTemplateKeys(v, tg.id)) { text = JSON.stringify(v, null, 2) + '\n'; noteList.push(t('sync.pruneInvalid')) }
          } catch (e2) {}
          if (text !== r.content) {
            r.content = text
            hasContent = true
            try { await api.SaveTargetConfig(tg.id, text); toast(t('sync.autoRepairToast', { name: tg.name, list: noteList.join('、') })) } catch (e3) {}
          }
        }
      }
      if (tg.id === 'grok-build') {
        // TOML：原文编辑 + 结构化表单；无文件时使用预设模板
        cfgContent.value = hasContent ? (r.content || '') : grokTomlPreset()
        grokForm.value = grokReadToml(cfgContent.value)
        // AGENTS.md（~/.grok/AGENTS.md）并入同一编辑页（Markdown 源码直编）
        try {
          const ar = await api.GetGrokAgents()
          const hasA = !!(ar && ar.ok && ar.exists)
          grokAgentsExists.value = hasA
          grokAgentsContent.value = hasA ? (ar.content || '') : ''
        } catch (e) { grokAgentsExists.value = false; grokAgentsContent.value = '' }
        fetchGrokBaseModels()
      } else if (tg.id === 'kimi') {
        // TOML：原文编辑 + 结构化表单；无文件时使用预设模板
        cfgContent.value = hasContent ? (r.content || '') : kimiTomlPreset()
        setKimiMuted(true)
        kimiReadToml(cfgContent.value)
        nextTick(() => { setKimiMuted(false) })
        // AGENTS.md（~/.kimi-code/AGENTS.md）并入同一编辑页（Markdown 源码直编）
        try {
          const ar = await api.GetKimiAgents()
          const hasA = !!(ar && ar.ok && ar.exists)
          kimiAgentsExists.value = hasA
          kimiAgentsContent.value = hasA ? (ar.content || '') : ''
        } catch (e) { kimiAgentsExists.value = false; kimiAgentsContent.value = '' }
      } else if (tg.id === 'pi') {
        if (hasContent) {
          try { const v = JSON.parse(r.content); cfgObj.value = v && typeof v === 'object' ? v : {} } catch (e) { cfgObj.value = { providers: {} } }
        } else {
          cfgObj.value = { providers: { LLM: { api: 'openai-completions', baseUrl: 'http://127.0.0.1:3000', apiKey: '', models: [{ id: '', name: '', contextWindow: 128000, input: ['text'], reasoning: false, supportsTools: true, supportsVision: false }] } } }
        }
        initPiForm()
        syncToJson()
        fetchPiBaseModels()
        // 全局设置（~/.pi/agent/settings.json）并入同一编辑页
        try {
          const sr = await api.GetPiSettings()
          const hasSet = !!(sr && sr.ok && sr.content) // 真实文件缺失时后端返回首次初始值，同样可用于编辑
          piSettingsExists.value = !!(sr && sr.ok && sr.exists)
          if (sr && sr.file) piSettingsFile.value = sr.file
          let sc = {}
          if (hasSet) { try { sc = JSON.parse(sr.content) || {} } catch (e) { sc = {} } }
          else sc = piSettingsPreset()
          initPiSettingsForm(sc)
          piSettingsContent.value = hasSet ? sr.content : JSON.stringify(sc, null, 2)
        } catch (e) { initPiSettingsForm(piSettingsPreset()); piSettingsContent.value = JSON.stringify(piSettingsPreset(), null, 2) }
        // AGENTS.md（~/.pi/agent/AGENTS.md）并入同一编辑页（Markdown 源码直编）
        try {
          const ar = await api.GetPiAgents()
          const hasA = !!(ar && ar.ok && ar.content) // 首次初始值模板内容同样展示
          piAgentsExists.value = !!(ar && ar.ok && ar.exists)
          piAgentsContent.value = hasA ? (ar.content || '') : ''
        } catch (e) { piAgentsExists.value = false; piAgentsContent.value = '' }
        // mcp.json（~/.pi/agent/mcp.json）并入同一编辑页（JSON 源码直编）
        try {
          const mr = await api.GetPiMcp()
          const hasM = !!(mr && mr.ok && mr.content) // 首次初始值模板内容同样展示
          piMcpExists.value = !!(mr && mr.ok && mr.exists)
          if (mr && mr.file) piMcpFile.value = mr.file
          piMcpContent.value = hasM ? mr.content : JSON.stringify({ mcpServers: {} }, null, 2)
        } catch (e) { piMcpExists.value = false; piMcpContent.value = JSON.stringify({ mcpServers: {} }, null, 2) }
      } else if (tg.id === 'gemini') {
        if (hasContent) {
          try { const v = JSON.parse(r.content); cfgObj.value = v && typeof v === 'object' ? v : {} } catch (e) { cfgObj.value = {} }
        } else {
          cfgObj.value = { ...geminiSettingsPreset() }
        }
        initGeminiForm()
        syncToJson()
        // 同时加载 ~/.gemini/.env 源码页签
        try {
          const ge = await api.GetGeminiDotEnv()
          geminiEnvExists.value = !!(ge && ge.ok && ge.exists)
          geminiEnvContent.value = (ge && ge.ok) ? (ge.content || '') : ''
        } catch (e) { geminiEnvExists.value = false; geminiEnvContent.value = '' }
        // 同时加载 GEMINI.md（~/.gemini/GEMINI.md）源码页签（Markdown 源码直编）
        try {
          const ga = await api.GetGeminiAgents()
          const hasGA = !!(ga && ga.ok && ga.content) // 首次初始值模板内容同样展示
          geminiAgentsExists.value = !!(ga && ga.ok && ga.exists)
          geminiAgentsContent.value = hasGA ? (ga.content || '') : ''
        } catch (e) { geminiAgentsExists.value = false; geminiAgentsContent.value = '' }
        geminiApiKey.value = geminiApiKeyFromEnv()
      } else if (tg.id === 'opencode') {
        // 直接读写真实 opencode.json（无模板中转）；真实文件缺失时后端返回首次初始值模板内容
        const ocRaw = (r.content || '').trim()
        if (ocRaw) {
          try { const v = JSON.parse(ocRaw); cfgObj.value = v && typeof v === 'object' ? v : {} } catch (e) { cfgObj.value = {} }
        } else {
          cfgObj.value = {}
        }
        setOcMuted(true)
        initOcForm()
        syncToJson()
        nextTick(() => { setOcMuted(false) })
        // 同时加载 AGENTS.md（~/.config/opencode/AGENTS.md）源码页签（Markdown 源码直编）
        try {
          const ar = await api.GetOpencodeAgents()
          const hasA = !!(ar && ar.ok && ar.content) // 首次初始值模板内容同样展示
          ocAgentsExists.value = !!(ar && ar.ok && ar.exists)
          ocAgentsContent.value = hasA ? (ar.content || '') : ''
        } catch (e) { ocAgentsExists.value = false; ocAgentsContent.value = '' }
      } else if (tg.id === 'cline') {
        // 同时加载 models.json / providers.json / AGENTS.md（均直接读写真实文件）源码页签；
        // 主配置（真实 cline_mcp_settings.json）解析进 cfgObj 供 JSON 页签与保存
        if ((r.content || '').trim()) {
          try { const v = JSON.parse(r.content); cfgObj.value = v && typeof v === 'object' ? v : {} } catch (e) { cfgObj.value = {} }
        } else {
          cfgObj.value = {}
        }
        syncToJson()
        try {
          const mr = await api.GetClineModels()
          clineModelsExists.value = !!(mr && mr.ok && mr.exists)
          clineModelsContent.value = (mr && mr.ok) ? (mr.content || '') : ''
        } catch (e) { clineModelsExists.value = false; clineModelsContent.value = '' }
        setClineModelsMuted(true)
        setClineCapsMuted(true)
        initClineModelsForm()
        nextTick(() => { setClineCapsMuted(false); setClineModelsMuted(false) })
        // 模型清单按提供商自动并入全部可用模型，无需手动点选添加
        clineAutoFillModels()
        try {
          const pr = await api.GetClineProviders()
          clineProvidersExists.value = !!(pr && pr.ok && pr.exists)
          clineProvidersContent.value = (pr && pr.ok) ? (pr.content || '') : ''
        } catch (e) { clineProvidersExists.value = false; clineProvidersContent.value = '' }
        setClineConnMuted(true)
        initClineConnForm()
        nextTick(() => { setClineConnMuted(false) })
        try {
          const ar = await api.GetClineAgents()
          clineAgentsExists.value = !!(ar && ar.ok && ar.exists)
          clineAgentsContent.value = (ar && ar.ok) ? (ar.content || '') : ''
        } catch (e) { clineAgentsExists.value = false; clineAgentsContent.value = '' }
        try {
          const gs = await api.GetClineGlobalState()
          clineGlobalStateExists.value = !!(gs && gs.ok && gs.exists)
          if (gs && gs.ok && gs.file) clineGlobalStateFile.value = gs.file
          clineGlobalStateContent.value = (gs && gs.ok && gs.exists && gs.content) ? gs.content : ''
        } catch (e) { clineGlobalStateExists.value = false; clineGlobalStateContent.value = '' }
        setClineGlobalMuted(true)
        initClineGlobalForm()
        nextTick(() => { setClineGlobalMuted(false) })
      } else if (tg.id === 'commandcode') {
        // JSON：直接读写真实文件，解析进 cfgObj 驱动结构化表单；无内容时给官方字段骨架
        if ((r.content || '').trim()) {
          try { const v = JSON.parse(r.content); cfgObj.value = v && typeof v === 'object' ? v : {} } catch (e) { cfgObj.value = {} }
        } else {
          cfgObj.value = { provider: 'command-code', model: '', theme: 'auto', compactMode: 'default', reasoningEffort: {} }
        }
        // settings.json（权限、推理深度）与 providers.json（BYOK 模型端点）并入同一编辑页
        try {
          const sr = await api.GetCommandcodeSettings()
          ccSettingsExists.value = !!(sr && sr.ok && sr.exists)
          if (sr && sr.file) ccSettingsFile.value = sr.file
          ccSettingsContent.value = (sr && sr.ok && sr.content) ? sr.content : JSON.stringify({ model: '', effort: '', permissions: { defaultMode: 'default', allow: [], ask: [], deny: [], additionalDirectories: [] } }, null, 2)
        } catch (e) {
          ccSettingsExists.value = false
          ccSettingsContent.value = JSON.stringify({ model: '', effort: '', permissions: { defaultMode: 'default', allow: [], ask: [], deny: [], additionalDirectories: [] } }, null, 2)
        }
        try {
          const pr = await api.GetCommandcodeProviders()
          ccProvidersExists.value = !!(pr && pr.ok && pr.exists)
          if (pr && pr.file) ccProvidersFile.value = pr.file
          ccProvidersContent.value = (pr && pr.ok && pr.content) ? pr.content : JSON.stringify({ provider: { llmrouter: { name: 'LLMRouter', api: 'openai-completions', baseURL: routerHintForTarget(), apiKey: '$COMMAND_CODE_API_KEY', models: {} } } }, null, 2)
        } catch (e) {
          ccProvidersExists.value = false
          ccProvidersContent.value = JSON.stringify({ provider: { llmrouter: { name: 'LLMRouter', api: 'openai-completions', baseURL: routerHintForTarget(), apiKey: '$COMMAND_CODE_API_KEY', models: {} } } }, null, 2)
        }
        setCcModelsReady(true)
        setCcModelsSeeded(false)
        setCcMuted(true)
        initCcForm()
        // 模型清单异步拉取可能未就绪：seed 前等待一次，避免空列表被标记已种入
        if (!modelOptions.value.length) { try { await loadModelOptions() } catch (e) {} }
        ccSeedModelsFromList()
        ccLoadNoLogin()
        syncToJson()
        nextTick(() => { setCcMuted(false) })
      } else if (tg.id === 'hermes') {
        // YAML：原文编辑 + 结构化表单；无内容时使用预设模板
        cfgContent.value = hasContent ? (r.content || '') : hermesYamlPreset()
        setHermesMuted(true)
        hermesInitFrom(cfgContent.value)
        nextTick(() => { setHermesMuted(false) })
      } else if (tg.id === 'agy') {
        // JSON：直接读写真实 settings.json，解析进 cfgObj 供表单与源码页签使用
        if (hasContent) {
          try { const v = JSON.parse(r.content); cfgObj.value = v && typeof v === 'object' ? v : {} } catch (e) { cfgObj.value = {} }
        } else {
          cfgObj.value = { ...agySettingsPreset() }
        }
        initAgyForm()
        syncToJson()
        // 同时加载接入环境变量（GOOGLE_GEMINI_BASE_URL / GEMINI_API_KEY，BYOK 接入源）
        try { await loadAgyEnv() } catch (e) {}
        // 同时加载模型映射面板（负载 agy 内置 ID → 上游模型的服务商 aliases）
        try { await loadAgyAliasPanel() } catch (e) {}
        // 同时加载全局 mcp_config.json（~/.gemini/config/mcp_config.json）源码页签
        try {
          const mr = await api.GetAgyMcp()
          const hasM = !!(mr && mr.ok && mr.content) // 初始内容（空对象）同样展示
          agyMcpExists.value = !!(mr && mr.ok && mr.exists)
          if (mr && mr.file) agyMcpFile.value = mr.file
          agyMcpContent.value = hasM ? mr.content : JSON.stringify({ mcpServers: {} }, null, 2)
        } catch (e) { agyMcpExists.value = false; agyMcpContent.value = JSON.stringify({ mcpServers: {} }, null, 2) }
      } else if (tg.id === 'kilo') {
        // JSONC：真实文件缺失时后端返回首次初始值模板内容（同样可编辑部署）。
        // 表单与源码直接读写真实 kilo.jsonc：含注释/尾逗号走 parseJsonc 宽容解析
        // 进 cfgObj 供表单与 JSON 页签使用；cfgContent 保留原文，用户改表单后
        // 由 kilo watcher 重写（保留未管字段），避免打开即丢失 JSONC 注释。
        if (hasContent) {
          try {
            const v = parseJsonc(r.content)
            cfgObj.value = v && typeof v === 'object' ? v : {}
          } catch (e) { cfgObj.value = {} }
        } else {
          cfgObj.value = {}
        }
        cfgContent.value = hasContent ? (r.content || '') : ''
        setKiloMuted(true)
        initKiloForm()
        nextTick(() => { setKiloMuted(false) })
        // 同时加载 AGENTS.md（~/.config/kilo/AGENTS.md）源码页签（Markdown 源码直编）
        try {
          const ar = await api.GetKiloAgents()
          const hasA = !!(ar && ar.ok && ar.content) // 首次初始值模板内容同样展示
          kiloAgentsExists.value = !!(ar && ar.ok && ar.exists)
          kiloAgentsContent.value = hasA ? (ar.content || '') : ''
          kiloAgentsFile.value = (ar && ar.ok) ? (ar.file || '') : ''
        } catch (e) { kiloAgentsExists.value = false; kiloAgentsContent.value = '' }
      } else {
        if (hasContent) {
          try { const v = JSON.parse(r.content); cfgObj.value = v && typeof v === 'object' ? v : {} } catch (e) { cfgObj.value = {} }
        } else {
          if (tg.id === 'claude-code') cfgObj.value = { model: '', permissions: { defaultMode: 'default' }, env: {} }
          else cfgObj.value = { model: '', accessMode: tg.apiFormat || '/oai', permissions: { defaultMode: 'default' } }
        }
        if (tg.id === 'claude-code') {
          // 重置环境变量模型行再重建，避免上次打开的其它目标的旧行残留并被保存
          claudeEnvModels.value = []
          normalizeClaudeEnvModels()
          initClaudeExtra()
          loadClaudeMarkdown()
        }
        syncToJson()
      }
      if (tg.id === 'codex') {
        // TOML：原文编辑 + 结构化表单；无内容时使用预设模板；同时加载 AGENTS.md 页签
        cfgContent.value = hasContent ? (r.content || '') : codexTomlPreset()
        setCodexMuted(true)
        codexInitFrom(cfgContent.value)
        nextTick(() => { setCodexMuted(false) })
        // 同时加载 AGENTS.md（模版 agent/Json_modles/codex/AGENTS.md）源码页签
        try {
          const ar = await api.GetCodexAgents()
          const hasA = !!(ar && ar.ok && ar.exists)
          codexAgentsExists.value = hasA
          codexAgentsContent.value = hasA ? (ar.content || '') : ''
        } catch (e) { codexAgentsExists.value = false; codexAgentsContent.value = '' }
      }
    } else {
      toast(t('common.fail', { msg: r?.error || '?' }))
      cfgEdit.value = false
    }
  } catch (e) {
    toast(t('common.fail', { msg: e?.message || '?' }))
    cfgEdit.value = false
  } finally {
    cfgLoading.value = false
    nextTick(() => { setGenericMuted(false); setGrokMuted(false); setKimiMuted(false); setKiloMuted(false) })
    // Copilot CLI：无主配置表单，打开即进入环境变量 BYOK 面板并回显当前值
    if (tg && tg.id === 'copilot') { cfgMode.value = 'copilotenv'; loadCopilotEnv(); loadCopilotPerm() }
  }
}

// 通用 表单↔源码 双向联动（在组件 setup 上下文内注册）
export function initSyncController() {
  // 正向：表单改动（直接写 cfgObj）→ JSON 源码页签实时序列化
  watch(cfgObj, () => {
    if (!cfgEdit.value || genericMuted || jsonEcho) return
    if (GENERIC_EXCLUDED_IDS.includes(cfgTarget.value?.id)) return
    if ((cfgFormat.value || 'json') !== 'json') return
    try { syncToJson() } catch (e) {}
  }, { deep: true })
  // 反向：JSON 源码页签输入 → 实时解析回填 cfgObj 与表单；语法非法时静默跳过，不打断输入
  watch(cfgContent, () => {
    if (!cfgEdit.value || genericMuted || jsonEcho) return
    if (cfgMode.value !== 'json') return
    if ((cfgFormat.value || 'json') !== 'json') return
    let v
    try { v = JSON.parse(cfgContent.value || '{}') } catch (e) { return }
    if (!v || typeof v !== 'object') return
    setJsonEcho(true)
    cfgObj.value = v
    const id = cfgTarget.value?.id
    if (id === 'claude-code') { claudeEnvModels.value = []; normalizeClaudeEnvModels(); initClaudeExtra() }
    else if (id === 'pi') initPiForm()
    else if (id === 'gemini') initGeminiForm()
    else if (id === 'opencode') initOcForm()
    else if (id === 'commandcode') initCcForm()
    else if (id === 'agy') initAgyForm()
    nextTick(() => { setJsonEcho(false) })
  })
}