import { setCcMuted, setCcModelsReady, setCcModelsSeeded } from './clients/cc'
import { setOcMuted } from './clients/opencode'
import { setCodexMuted } from './clients/codex'
import { setKimiMuted } from './clients/kimi'
import { setGrokMuted } from './clients/grok'
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
  cfgEdit, cfgTarget, cfgLoading, cfgSaving, cfgMode, cfgFormat, cfgFile, cfgExists,
  cfgContent, cfgObj, ensureClientPaths, loadModelOptions, modelOptions, syncToJson, genericMuted,
  jsonEcho, GENERIC_EXCLUDED_IDS, routerHintForTarget,
} from './store'
import { claudeEnvModels, normalizeClaudeEnvModels, initClaudeExtra, syncClaudeEnvModelsToObj, syncClaudeExtraToObj, claudeBuildFormText, loadClaudeMarkdown, saveClaudeMarkdown } from './clients/claude'
import { initPiForm, piBuildModelsText, piSettingsBuildText, piSettingsContent, piSettingsExists, piSettingsFile, syncSettingsFromSource, piMcpContent, piMcpExists, piMcpFile, piAgentsContent, piAgentsExists, piAgentsFile, piSettingsPreset, initPiSettingsForm, fetchPiBaseModels } from './clients/pi'
import { grokBuildToml, grokReadToml, grokMuted, grokAgentsContent, grokAgentsExists, grokAgentsFile, grokTomlPreset, grokBlocks, grokOrder, grokUnmanaged, grokUiExtras, grokForm, grokEmptyForm, fetchGrokBaseModels } from './clients/grok'
import { codexInitFrom, codexWriteToml, codexMuted, codexAgentsContent, codexAgentsExists, codexAgentsFile, codexTomlPreset, codexForm, CODEX_RESERVED_PROVIDERS, loadCodexMappings, applyCodexMappings } from './clients/codex'
import { kimiBuildToml, kimiReadToml, kimiMuted, kimiAgentsContent, kimiAgentsExists, kimiAgentsFile, kimiTomlPreset, kimiForm, kimiProviderRows, kimiModelRows, kimiThinking, kimiPermRows, kimiUnmanaged } from './clients/kimi'
import { ocApplyToObj, initOcForm, ocMuted, ocAgentsContent, ocAgentsExists, ocAgentsFile, ocModels } from './clients/opencode'
import { ccApplyToObj, initCcForm, ccSeedModelsFromList, ccLoadNoLogin, ccSettingsContent, ccSettingsExists, ccSettingsFile, ccProvidersContent, ccProvidersExists, ccProvidersFile, ccApplyToSettings, ccApplyToProviders, ccNoLogin, ccNoLoginTouched, ccForm, ccMuted, ccModelsReady, ccModelsSeeded, ccNoLoginMsg, ccModelOpen, ccEffortOpen, ccEffortPick } from './clients/cc'
import { writeCopilotEnv, saveCopilotPerm, loadCopilotEnv, loadCopilotPerm, copilotPermFile, copilotPermExists, copilotPermContent } from './clients/copilot'
import { mimoApplyToObj, mimoFromSource, initMimoForm, mimoModel, mimoSmallModel, mimoApiKey, mimoProviderKey, mimoProviderPrefix, mimoQualifyModel, setMimoMuted } from './clients/mimocode'
import { parseJsonc } from './shared'
import { agyApplyToObj, initAgyForm, agySettingsPreset, agyMcpContent, agyMcpExists, agyMcpFile, agyForm, writeAgyEnv, loadAgyEnv, loadAgyAliasPanel, applyAgyAliases } from './clients/agy'
import {
  codebuddyPreset, initCodebuddyForm, codebuddyApplyToObj, cbModelsObj, cbModelsFromText, cbModelsBuildText,
  cbModelsContent, cbModelsExists, cbModelsFile, cbMcpContent, cbMcpExists, cbMcpFile,
} from './clients/codebuddy'
import { initDeepcodeForm, deepcodeApplyToObj, setDeepcodeMuted, loadDeepcodeAliasPanel, applyDeepcodeAliases } from './clients/deepcode'

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
  if (cfgTarget.value?.id === 'commandcode') {
    if (cfgMode.value === 'ccsettings') return ccSettingsFile.value
    if (cfgMode.value === 'ccproviders') return ccProvidersFile.value
  }
  if (cfgTarget.value?.id === 'copilot' && cfgMode.value === 'copilotperm') return copilotPermFile.value
  if (cfgTarget.value?.id === 'agy' && cfgMode.value === 'agymcp') return agyMcpFile.value
  if (cfgTarget.value?.id === 'codebuddy') {
    if (cfgMode.value === 'cbmodels') return cbModelsFile.value
    if (cfgMode.value === 'cbmcp') return cbMcpFile.value
  }
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
  if (cfgTarget.value?.id === 'commandcode') {
    if (cfgMode.value === 'ccsettings') return ccSettingsExists.value
    if (cfgMode.value === 'ccproviders') return ccProvidersExists.value
  }
  if (cfgTarget.value?.id === 'copilot' && cfgMode.value === 'copilotperm') return copilotPermExists.value
  if (cfgTarget.value?.id === 'agy' && cfgMode.value === 'agymcp') return agyMcpExists.value
  if (cfgTarget.value?.id === 'codebuddy') {
    if (cfgMode.value === 'cbmodels') return cbModelsExists.value
    if (cfgMode.value === 'cbmcp') return cbMcpExists.value
  }
  return cfgExists.value
}

// JSON 源码页签标题：统一显示实际文件名（取配置文件路径末段，pi/grok/kimi 固定知名文件）
export const tabJsonLabel = computed(() => {
  const id = cfgTarget.value?.id
  if (id === 'pi') return 'models.json'
  if (id === 'grok-build') return 'config.toml'
  if (id === 'kimi') return 'config.toml'
  if (id === 'mimocode') return 'mimocode.jsonc'
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
    else if (cfgTarget.value?.id === 'pi') cfgContent.value = piBuildModelsText()
    else if (cfgTarget.value?.id === 'opencode') { ocApplyToObj(); syncToJson() }
    else if (cfgTarget.value?.id === 'commandcode') { ccApplyToObj(); syncToJson() }
    else if (cfgTarget.value?.id === 'mimocode') { mimoApplyToObj(); syncToJson() }
    else if (cfgTarget.value?.id === 'agy') { agyApplyToObj(); syncToJson() }
    else if (cfgTarget.value?.id === 'codebuddy') { codebuddyApplyToObj(); syncToJson() }
    else if (cfgTarget.value?.id === 'deepcode') { deepcodeApplyToObj(); syncToJson() }
    else syncToJson()
  }
  if (m === 'form' && cfgMode.value === 'json') {
    // TOML/YAML 原文流不做 JSON 回填，由下方各客户端专属回读处理
    if (cfgFormat.value !== 'toml' && cfgFormat.value !== 'yaml') {
      // MimoCode 为 JSONC（可含注释/尾逗号）：走 JSONC 宽容解析回填
      if (cfgTarget.value?.id === 'mimocode') { if (!mimoFromSource()) return }
      else if (!syncToObjFromSource()) return
    }
    // initXxxForm 整体替换表单会触发回写 watcher；统一静默包裹，
    // 避免刚从 JSON 解析出的源码被表单重建（丢失未管字段）无提示覆写。
    // try/finally 保证即使某个 init 抛异常也必复位——否则 genericMuted 卡死，
    // 表单改动不再同步进 cfgObj，保存会静默写回旧内容（成功提示但文件没变）
    setGenericMuted(true)
    try {
      if (cfgTarget.value?.id === 'pi') initPiForm()
      // opencode 的 watcher 只看 ocMuted（不看 genericMuted）：initOcForm 整体替换表单会
      // 立刻触发 ocApplyToObj 重建 provider.llm，导致源码框键序与磁盘文件不一致
      if (cfgTarget.value?.id === 'opencode') { setOcMuted(true); initOcForm(); nextTick(() => { setOcMuted(false) }) }
      if (cfgTarget.value?.id === 'commandcode') initCcForm()
      if (cfgTarget.value?.id === 'mimocode') { setMimoMuted(true); initMimoForm(); nextTick(() => { setMimoMuted(false) }) }
      if (cfgTarget.value?.id === 'grok-build') { setGrokMuted(true); grokReadToml(cfgContent.value); nextTick(() => { setGrokMuted(false) }) }
      if (cfgTarget.value?.id === 'codex') { setCodexMuted(true); codexInitFrom(cfgContent.value); nextTick(() => { setCodexMuted(false) }) }
      if (cfgTarget.value?.id === 'kimi') { setKimiMuted(true); kimiReadToml(cfgContent.value); nextTick(() => { setKimiMuted(false) }) }
      if (cfgTarget.value?.id === 'agy') initAgyForm()
      if (cfgTarget.value?.id === 'codebuddy') initCodebuddyForm()
      // 同上：deepcode 的 watcher 也只认 deepcodeMuted
      if (cfgTarget.value?.id === 'deepcode') { setDeepcodeMuted(true); initDeepcodeForm(); nextTick(() => { setDeepcodeMuted(false) }) }
    } finally {
      nextTick(() => { setGenericMuted(false) })
    }
  }
  // 离开全局设置源码页签时回填表单；进入时序列化源码
  if (cfgMode.value === 'globjson' && m !== 'globjson') {
    if (!syncSettingsFromSource()) return
  }
  if (m === 'globjson') {
    piSettingsContent.value = piSettingsBuildText()
  }
  // codebuddy models.json 源码页签：进入时由端点行重建源码；离开时解析回 cbModelsObj。
  // 源码非法时阻止离开，避免表单行数据被损坏内容覆盖（保存路径另有 JSON 校验拦截）
  if (cfgMode.value === 'cbmodels' && m !== 'cbmodels') {
    if (!cbModelsFromText(cbModelsContent.value)) { toast(t('sync.jsonError')); return }
  }
  if (m === 'cbmodels' && cfgMode.value !== 'cbmodels') {
    cbModelsContent.value = cbModelsBuildText()
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

// ============ 统一模型下拉（pi-sel 样式）：claude / opencode 复用 ============
export const claudeModelOpen = ref(-1)
export const ocModelOpen = ref(-1)
export function pickClaudeModel(i, m) { if (claudeEnvModels.value[i]) claudeEnvModels.value[i].modelId = m; claudeModelOpen.value = -1 }
export function pickOcModel(i, m) { if (ocModels.value[i]) ocModels.value[i].id = m; ocModelOpen.value = -1 }

// ============ 构建 / 保存 / 打开 ============
export function buildFormText() {
  const id = cfgTarget.value.id
  if (id === 'claude-code') {
    return claudeBuildFormText()
  }
  if (id === 'pi') return piBuildModelsText()
  if (id === 'opencode') { ocApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'commandcode') { ccApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'mimocode') { mimoApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'agy') { agyApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'codebuddy') { codebuddyApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'deepcode') { deepcodeApplyToObj(); return JSON.stringify(cfgObj.value, null, 2) }
  if (id === 'grok-build') return grokBuildToml()
  if (id === 'codex') return codexWriteToml(cfgContent.value)
  if (id === 'kimi') return kimiBuildToml()
  return JSON.stringify(cfgObj.value, null, 2)
}

export async function saveTargetConfig(stayOpen = false) {
  if (!cfgTarget.value) return
  // codex：提供商标识不得使用 Codex 内置保留 ID（会被忽略导致接入失效）
  if (cfgTarget.value.id === 'codex') {
    const pk = (codexForm.value.providerKey || '').trim() || 'llmrouter'
    if (CODEX_RESERVED_PROVIDERS.includes(pk)) { toast(t('sync.codexReserved', { pk })); return }
  }
  // Copilot CLI「配置选项」BYOK 模式：写入用户环境变量 + settings.json；权限批准面板同步落盘 permissions-config.json
  if (cfgTarget.value.id === 'copilot' && cfgMode.value === 'copilotenv') {
    if (await writeCopilotEnv()) {
      // 权限配置胶囊条可能改写过源码：非空时校验 JSON 后一并保存（失败则保持窗口打开）
      const pc = copilotPermContent.value
      if (pc.trim()) {
        try { JSON.parse(pc) } catch (e) { const p = jsonErrorPos(pc); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
        if (!await saveCopilotPerm(true)) return
      }
      if (!stayOpen) cfgEdit.value = false
    }
    return
  }
  // Copilot CLI 工具审批权限：保存 ~/.copilot/permissions-config.json（源码直编，JSON 校验后落盘）
  if (cfgTarget.value.id === 'copilot' && cfgMode.value === 'copilotperm') {
    const pc = copilotPermContent.value
    try { if (pc.trim()) JSON.parse(pc) } catch (e) { const p = jsonErrorPos(pc); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    let ok = false
    try { ok = await saveCopilotPerm() } finally { cfgSaving.value = false }
    if (ok) if (!stayOpen) cfgEdit.value = false
    return
  }
  // Antigravity CLI mcp_config.json 源码模式：只保存 ~/.gemini/config/mcp_config.json，不动主配置
  if (cfgTarget.value.id === 'agy' && cfgMode.value === 'agymcp') {
    try { if (agyMcpContent.value.trim()) JSON.parse(agyMcpContent.value) } catch (e) { const p = jsonErrorPos(agyMcpContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    try {
      const am = await api.SaveAgyMcp(agyMcpContent.value)
      if (am && am.ok) { toast(t('sync.configSaved')); if (!stayOpen) cfgEdit.value = false }
      else toast(t('common.fail', { msg: am?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // CodeBuddy models.json 源码模式：只保存 ~/.codebuddy/models.json，不动主配置
  if (cfgTarget.value.id === 'codebuddy' && cfgMode.value === 'cbmodels') {
    try { if (cbModelsContent.value.trim()) JSON.parse(cbModelsContent.value) } catch (e) { const p = jsonErrorPos(cbModelsContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    try {
      const bm = await api.SaveCodebuddyModels(cbModelsContent.value)
      if (bm && bm.ok) { toast(t('sync.configSaved')); if (!stayOpen) cfgEdit.value = false }
      else toast(t('common.fail', { msg: bm?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // CodeBuddy mcp.json 源码模式：只保存 ~/.codebuddy/mcp.json，不动主配置
  if (cfgTarget.value.id === 'codebuddy' && cfgMode.value === 'cbmcp') {
    try { if (cbMcpContent.value.trim()) JSON.parse(cbMcpContent.value) } catch (e) { const p = jsonErrorPos(cbMcpContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    try {
      const cm = await api.SaveCodebuddyMcp(cbMcpContent.value)
      if (cm && cm.ok) { toast(t('sync.configSaved')); if (!stayOpen) cfgEdit.value = false }
      else toast(t('common.fail', { msg: cm?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // OpenCode AGENTS.md 源码模式：只保存 AGENTS.md，不动主配置
  if (cfgTarget.value.id === 'opencode' && cfgMode.value === 'openagents') {
    cfgSaving.value = true
    try {
      const oa = await api.SaveOpencodeAgents(ocAgentsContent.value)
      if (oa && oa.ok) { toast(t('sync.configSaved')); if (!stayOpen) cfgEdit.value = false }
      else toast(t('common.fail', { msg: oa?.error || '?' }))
    } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { cfgSaving.value = false }
    return
  }
  // Codex AGENTS.md 源码模式：只保存 ~/.codex/AGENTS.md，不动主配置
  if (cfgTarget.value.id === 'codex' && cfgMode.value === 'codexagents') {
    cfgSaving.value = true
    try {
      const r = await api.SaveCodexAgents(codexAgentsContent.value)
      if (r && r.ok) { toast(t('sync.configSaved')); if (!stayOpen) cfgEdit.value = false }
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
  // Command Code settings.json 源码模式：只保存 ~/.commandcode/settings.json，不动主配置
  if (cfgTarget.value.id === 'commandcode' && cfgMode.value === 'ccsettings') {
    if (ccSettingsContent.value.trim() && !autoFormatJson(ccSettingsContent)) { const p = jsonErrorPos(ccSettingsContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
    cfgSaving.value = true
    try {
      const r = await api.SaveCommandcodeSettings(ccSettingsContent.value)
      if (r && r.ok) { toast(t('sync.configSaved')); if (!stayOpen) cfgEdit.value = false }
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
      if (r && r.ok) { toast(t('sync.configSaved')); if (!stayOpen) cfgEdit.value = false }
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
      else { toast(t('sync.configSaved')); if (!stayOpen) cfgEdit.value = false }
    } finally { cfgSaving.value = false }
    return
  }
  let text = ''
  if (cfgMode.value === 'form') {
    try { text = buildFormText() } catch (e) { toast(t('sync.jsonError')); return }
  } else {
    text = cfgContent.value
    // TOML/JSONC 源码不做前端 JSON 校验（mimocode 的 jsonc 允许注释/尾逗号，
    // 由后端 jsoncToJSON 宽容校验）
    if (cfgFormat.value !== 'toml' && cfgFormat.value !== 'yaml' && cfgFormat.value !== 'jsonc') {
      if (text.trim() && !autoFormatJson(cfgContent)) { const p = jsonErrorPos(text); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
      text = cfgContent.value
    }
  }
  // pi：切到「全局设置 / mcp.json / AGENTS.md」页签后，text 仍是打开窗口时的 models.json
  // 快照，直接保存会把本窗口内对模型表单的改动静默冲掉（保存成功但模型行回退）。
  // 这些页签下一律按当前表单重新序列化；JSON 页签下 cfgContent 就是用户正在编辑的
  // models.json，保持原样不动。
  if (cfgTarget.value.id === 'pi' && cfgMode.value !== 'json') {
    try { text = piBuildModelsText() } catch (e) { toast(t('sync.jsonError')); return }
  }
  // pi 全局设置源码模式：校验 + 自动格式化后再保存
  if (cfgTarget.value.id === 'pi' && cfgMode.value === 'globjson') {
    if (piSettingsContent.value.trim() && !autoFormatJson(piSettingsContent)) { const p = jsonErrorPos(piSettingsContent.value); toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError')); return }
  }
  // agy 合并：模型映射随「保存」一并落盘——先解析映射文本并回写路由器服务商 aliases，
  // 失败（目标模型解析不到已启用服务商等）即中止整次保存，避免无效映射仍写入 settings.json
  if (cfgTarget.value.id === 'agy') {
    cfgSaving.value = true
    let ae = ''
    // try/finally：映射回写抛异常时不能把 cfgSaving 卡在 true（保存按钮会永久禁用）
    try { ae = await applyAgyAliases() } catch (e) { ae = String(e?.message || e) } finally { cfgSaving.value = false }
    if (ae) { toast(t('common.fail', { msg: ae })); return }
  }
  // deepcode 合并：内置 ID → 上游模型的映射同样随「保存」落盘（定点回写服务商 aliases），
  // 失败即中止整次保存，避免无效映射仍写进 settings.json
  if (cfgTarget.value.id === 'deepcode') {
    cfgSaving.value = true
    let de = ''
    try { de = await applyDeepcodeAliases() } catch (e) { de = String(e?.message || e) } finally { cfgSaving.value = false }
    if (de) { toast(t('common.fail', { msg: de })); return }
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
      // 填 command-code（官方云端订阅）时保持用户手动选择，不强行干预。
      // 用户手动关过开关（ccNoLoginTouched && !ccNoLogin）时不再自动打开——否则
      // 面板上的开关会被保存动作静默覆盖成“开”，用户永远关不掉。
      if (!saveErr) {
        const prov = String(ccForm.value.provider || '').trim() || String(ccForm.value.endpointId || '').trim() || 'llmrouter'
        const userTurnedOff = ccNoLoginTouched.value && !ccNoLogin.value
        if (prov !== 'command-code' && !userTurnedOff) {
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
      // 模型映射随保存一并落盘：行回写启用服务商 aliases（@服务商 后缀定位归属）
      const mm = await applyCodexMappings()
      if (mm && !saveErr) saveErr = mm
    }
    // agy 合并：modelProvider 为 gemini 时自动写接入环境变量（agy 官方契约：
    // 端点读 GOOGLE_GEMINI_BASE_URL、API Key 读 GEMINI_API_KEY，settings.json 的
    // baseUrl/apiKey 字段不被 CLI 读取，仅靠这两个变量让 CLI 连到本路由器）
    if (cfgTarget.value.id === 'agy') {
      // modelProvider 以「本次要落盘的内容」为准：表单模式下 cfgObj 已由 buildFormText()
      // 同步；JSON 源码模式下 cfgObj 还是载入时的旧值，若在源码里改成 gemini，
      // 按旧值判定就会出现「文件写了 gemini、环境变量却没自动写」的不一致。
      let mp = String((cfgObj.value && cfgObj.value.modelProvider) || '').trim().toLowerCase()
      if (cfgMode.value !== 'form') {
        try { mp = String((JSON.parse(text) || {}).modelProvider || '').trim().toLowerCase() } catch (e) {}
      }
      if (mp === 'gemini') {
        const aew = await writeAgyEnv()
        if (aew && !saveErr) saveErr = aew
      }
    }
    // codebuddy 合并：models.json（端点行，表单内始终重建）与 mcp.json（源码直编）
    // 随主配置 settings.json 一同保存；mcp 源码非法时拦截（与源码页签保存前校验一致）
    if (cfgTarget.value.id === 'codebuddy') {
      const cmt = String(cbMcpContent.value || '').trim()
      if (cmt) {
        try { JSON.parse(cmt) } catch (e) {
          const p = jsonErrorPos(cbMcpContent.value)
          toast(p ? t('sync.jsonLinePos', { line: p.line, col: p.col, msg: p.msg }) : t('sync.jsonError'))
          return
        }
      }
      if (!saveErr) {
        const mr = await api.SaveCodebuddyModels(cbModelsBuildText())
        if (mr && !mr.ok) saveErr = mr?.error || t('sync.saveFailModels')
      }
      if (!saveErr && cmt) {
        const mc = await api.SaveCodebuddyMcp(cbMcpContent.value)
        if (mc && !mc.ok) saveErr = mc?.error || t('sync.saveFailMcp')
      }
    }
    if (r && r.ok && !saveErr) { toast(t('sync.configSaved')); if (!stayOpen) cfgEdit.value = false }
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
  setMimoMuted(true) // 同上：静默 mimoForm 联动，避免载入时重写 mimocode.jsonc
  cfgTarget.value = tg
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
      } else if (tg.id === 'codebuddy') {
        // JSON：直接读写真实 settings.json，解析进 cfgObj 供表单与源码页签使用
        if (hasContent) {
          try { const v = JSON.parse(r.content); cfgObj.value = v && typeof v === 'object' ? v : {} } catch (e) { cfgObj.value = {} }
        } else {
          cfgObj.value = { ...codebuddyPreset() }
        }
        initCodebuddyForm()
        syncToJson()
        // 同时加载 models.json（~/.codebuddy/models.json）端点行，并入同一编辑页
        cbModelsObj.value = { models: [] }
        cbModelsContent.value = ''
        cbModelsExists.value = false
        cbModelsFile.value = '~/.codebuddy/models.json'
        try {
          const mr = await api.GetCodebuddyModels()
          const hasM = !!(mr && mr.ok && mr.content)
          cbModelsExists.value = !!(mr && mr.ok && mr.exists)
          if (mr && mr.ok && mr.file) cbModelsFile.value = mr.file
          const raw = hasM ? mr.content : ''
          if (cbModelsFromText(raw)) cbModelsContent.value = raw || cbModelsBuildText()
          else { cbModelsFromText('{}'); cbModelsContent.value = cbModelsBuildText() }
        } catch (e) { cbModelsFromText('{}'); cbModelsContent.value = cbModelsBuildText() }
        // 同时加载 mcp.json（~/.codebuddy/mcp.json）源码页签
        cbMcpContent.value = ''
        cbMcpExists.value = false
        cbMcpFile.value = '~/.codebuddy/mcp.json'
        try {
          const mcpR = await api.GetCodebuddyMcp()
          const hasMcp = !!(mcpR && mcpR.ok && mcpR.content)
          cbMcpExists.value = !!(mcpR && mcpR.ok && mcpR.exists)
          if (mcpR && mcpR.ok && mcpR.file) cbMcpFile.value = mcpR.file
          cbMcpContent.value = hasMcp ? mcpR.content : JSON.stringify({ mcpServers: {} }, null, 2) + '\n'
        } catch (e) { cbMcpExists.value = false; cbMcpContent.value = JSON.stringify({ mcpServers: {} }, null, 2) + '\n' }
      } else if (tg.id === 'mimocode') {
        // JSONC：真实文件缺失时后端返回首次初始值模板内容（同样可编辑部署）。
        // 表单与源码直接读写真实 mimocode.jsonc：含注释/尾逗号走 parseJsonc 宽容解析
        // 进 cfgObj 供表单与 JSON 页签使用；cfgContent 保留原文，用户改表单后
        // 由 mimo watcher 重写（保留未管字段），避免打开即丢失 JSONC 注释。
        if (hasContent) {
          try {
            const v = parseJsonc(r.content)
            cfgObj.value = v && typeof v === 'object' ? v : {}
          } catch (e) { cfgObj.value = {} }
        } else {
          cfgObj.value = {}
        }
        cfgContent.value = hasContent ? (r.content || '') : ''
        setMimoMuted(true)
        initMimoForm()
        nextTick(() => { setMimoMuted(false) })
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
        if (tg.id === 'deepcode') {
          // 首次（无 settings.json）时给一份可用初始值：接入地址指向本路由器
          if (!hasContent) {
            cfgObj.value = {
              model: '', env: { BASE_URL: routerHintForTarget(), API_KEY: '' },
              thinkingEnabled: true, reasoningEffort: 'high',
            }
          }
          setDeepcodeMuted(true)
          initDeepcodeForm()
          syncToJson()
          nextTick(() => { setDeepcodeMuted(false) })
          // 同时加载「内置 ID → 上游模型」映射面板（写服务商 aliases）
          try { await loadDeepcodeAliasPanel() } catch (e) {}
        } else {
          syncToJson()
        }
      }
      if (tg.id === 'codex') {
        // TOML：原文编辑 + 结构化表单；无内容时使用预设模板；同时加载 AGENTS.md 页签
        cfgContent.value = hasContent ? (r.content || '') : codexTomlPreset()
        setCodexMuted(true)
        codexInitFrom(cfgContent.value)
        nextTick(() => { setCodexMuted(false) })
        // 加载模型映射行（启用服务商的 aliases 合并展示，保存时按行回写）
        loadCodexMappings()
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
    nextTick(() => { setGenericMuted(false); setGrokMuted(false); setKimiMuted(false) })
    // Copilot CLI：无主配置表单，打开即进入「配置选项」BYOK 面板并回显当前值
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
    else if (id === 'opencode') initOcForm()
    else if (id === 'commandcode') initCcForm()
    else if (id === 'agy') initAgyForm()
    else if (id === 'codebuddy') initCodebuddyForm()
    else if (id === 'deepcode') initDeepcodeForm()
    nextTick(() => { setJsonEcho(false) })
  })
}
