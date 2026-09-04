<script setup>
import { onMounted, computed, ref } from 'vue'
import { t, i18nState } from '../i18n'
import { copyText, jsonErrorPos, repairJsonText, pruneIllegalTemplateKeys } from '../utils'
import { toast } from '../toast'
import { OPT_KEYS } from './sync/optmap'
// 下拉/单选选项显示本地化:命中 OPT_KEYS 走词典,未命中(模型名/品牌/协议标识等)回退原文
const optTxt = v => {
  const k = v != null ? OPT_KEYS[v] : undefined
  return k ? t(k) : String(v ?? '')
}
import CodeArea from './CodeArea.vue'
import PermList from './sync/PermList.vue'

// ---- 共享工具与常量 ----
import {
  ACCESS_OPTIONS, PERM_OPTIONS, getPath, setPath, srcObj, taWheelStop,
  permToggleRule, permRuleOn, tq, targetLogo, permRowsFromLists, permRowsToLists,
} from './sync/shared'
// ---- 中央状态与通用辅助 ----
import {
  cfgEdit, cfgInfoOpen, cfgTarget, cfgLoading, cfgSaving, cfgMode, cfgFormat, cfgFile, cfgExists,
  cfgContent, cfgObj, targets, routerBase, loadTargets, modelOptions, modelLoading,
  currentAccessFmt, routerHintForTarget, derivedBaseUrl, syncToJson, srcBarName, revealFile,
  targetRealPaths, ensureClientPaths, targetRealFile, syncToObj,
} from './sync/store'
// ---- 公共模型下拉 ----
import { modelSelOpen, cfgModelOpen, modelGroupsByLetter, pickModel, pickDefaultModel } from './sync/model-select'
// ---- 各客户端模块 ----
import {
  claudeAccessMode, claudeEnvModels, normalizeClaudeEnvModels, claudeDefaultModel, claudePermRules,
  claudePermRows,
  initClaudeExtra, initClaude, claudeExtra,
  CLAUDE_EXTRA_GROUPS, claudeMcpServerNames, newClaudeEnvModelRow, CLAUDE_ENV_KEY_SLOTS,
  claudeMdContent, claudeMdExists, claudeMdFile, loadClaudeMarkdown, saveClaudeMarkdown,
} from './sync/claude'
import {
  PI_CONTEXT_OPTIONS, PI_SETTINGS_THINKING_OPTIONS, PI_THEME_OPTIONS, PI_STEERING_OPTIONS, PI_TRUST_OPTIONS,
  PI_PROVIDER_API_OPTIONS, piSettingsContent, piSettingsExists, piSettingsFile, piMcpContent, piMcpExists,
  piMcpFile, piAgentsContent, piAgentsExists, piAgentsFile, piM, initPiForm, piBuildModelsText,
  piBaseModels, piFetchLoading, fetchPiBaseModels, initPi, piSettingsBuildText, syncSettingsFromSource,
  piSettingsPreset, initPiSettingsForm, piSettings, newPiModelRow,
} from './sync/pi'
import {
  grokForm, grokUiExtras, grokPermRows,
  grokBlocks, grokOrder, grokAgentsContent, grokAgentsExists, grokAgentsFile, grokReadToml,
  selectGrokModel, addGrokModel, grokBuildToml, grokBaseModels, grokFetchLoading, fetchGrokBaseModels,
  grokModelOpen, grokModelSearch, grokModelGroups, pickGrokModel, grokModelKeyNav, grokDefaultCandidates, pickGrokDefault, pickGrokWebSearch, initGrok,
  GROK_PERM_RULES,
} from './sync/grok'
import {
  KIMI_EFFORT_LEVELS, KIMI_PERM_RULES, kimiForm, kimiProviderRows, kimiModelRows, kimiThinking, kimiPermRows,
  kimiUnmanaged, kimiModelOpen, kimiCustomPattern, kimiAgentsContent, kimiAgentsExists, kimiAgentsFile,
  kimiTomlPreset, kimiReadToml, kimiBuildToml, kimiAddProvider, kimiAddModel, kimiDefaultCandidates,
  kimiRuleDecision, kimiToggleRule, kimiCustomRows, kimiAddCustomRule, kimiPermDecision, initKimi,
} from './sync/kimi'
import {
  geminiEnvContent, geminiEnvExists, geminiEnvFile, geminiAgentsContent, geminiAgentsExists, geminiAgentsFile,
  geminiApiKey, geminiApiKeyFromEnv, geminiSettingsPreset, geminiForm, initGeminiForm, initGemini,
} from './sync/gemini'
import {
  copilotBase, copilotType, copilotModel, copilotApiKey, copilotEffort, copilotBypass, copilotAllow,
  copilotDeny, copilotSandboxEna, copilotSandboxBypass, copilotSandboxNet, copilotLoading, copilotSaving,
  copilotModelOpen, COPILOT_EFFORT_OPTIONS, COPILOT_BYPASS_OPTIONS, copilotPermContent, copilotPermExists,
  copilotPermFile, copilotRouterUrl, loadCopilotEnv, loadCopilotPerm, writeCopilotEnv, clearCopilotEnv,
  copilotTools, copilotDirs,
  copilotUrlList,
} from './sync/copilot'
import {
  ocAgentsContent, ocAgentsExists, ocAgentsFile, ocAccessMode, ocApiKey, ocNpm, ocName, ocModels,
  OC_PERM_KEYS, ocPerms, ocPickMode, initOcForm, ocApplyToObj, initOpencode,
} from './sync/opencode'
import {
  clineModelsContent, clineModelsExists, clineModelsFile, clineProvidersContent, clineProvidersExists,
  clineProvidersFile, clineAgentsContent, clineAgentsExists, clineAgentsFile, clineGlobalStateContent,
  clineGlobalStateExists, clineGlobalStateFile, CLINE_CAP_KEYS, CLINE_REASONING_EFFORT_OPTIONS,
  newClineModelRow, clineModelsForm, clineDefModelOpen, clineModelSel, clinePanel,
  clinePanelOrder, clineTogglePanel, clinePanelIdx, clineConnModelOpen, clineConnForm, connBaseUrlText,
  clineGlobalActOpen, clineGlobalPlanOpen, clineGlobalForm, clineModelsRow,
  initClineModelsForm,
  applyClineModelsForm, initCline, CLINE_GLOBAL_TERMINAL_OPTIONS, CLINE_AA_ACTIONS, clineGlobalActions,
  CLINE_GLOBAL_PROVIDER_OPTIONS, CLINE_GLOBAL_TERMINAL_EXEC_OPTIONS,
  clineLastUsed, clineProviderKeys, clineCaps, clineToggleCap,
} from './sync/cline'
import {
  codexAgentsContent, codexAgentsExists, codexAgentsFile, codexForm, codexPermRows, codexQuickOpen, codexQuickFocus,
  codexToggleRule, codexRuleOn, CODEX_RESERVED_PROVIDERS, codexModelOpen, codexInitFrom, initCodex,
} from './sync/codex'
import {
  CC_EFFORT_LEVELS, CC_PERM_MODES, CC_DEFAULT_KEY_REF, ccIsApiKeyRef, PERM_COMMON_RULES, ccAddPermRule,
  ccRuleOn,
  ccQuickOpen, ccQuickQFocus, ccForm, ccSettingsFile, ccSettingsExists, ccSettingsContent, ccProvidersFile,
  ccProvidersExists, ccProvidersContent, ccNoLogin, ccNoLoginMsg, ccToggleNoLogin, ccModelOpen, ccEffortOpen,
  ccEffortPick, initCcForm, ccApplyToObj, ccApplyToSettings, ccApplyToProviders, ccLoadNoLogin, initCommandCode,
  ccPermRows,
} from './sync/cc'
import {
  hermesForm, hermesModelOpen, HERMES_MODEL_KEYS, hermesInitFrom, hermesWriteYaml, hermesYamlPreset, initHermes,
} from './sync/hermes'
import {
  kiloProviderKey, kiloApiKey, kiloNpm, kiloName, kiloModels, KILO_PERM_KEYS, kiloPerms, kiloPickMode,
  kiloModel, kiloSmallModel, kiloSubagentModel, kiloMcpServerNames, kiloQualifyModel, kiloProviderPrefix,
  kiloModelRowOpen, kiloModelOpen, kiloSmallOpen, kiloSubOpen, kiloAgentsContent, kiloAgentsFile,
  initKiloForm, kiloApplyToObj, initKilo,
} from './sync/kilo'
import {
  agyForm, initAgyForm, initAgy, agyMcpContent, agyMcpExists, agyMcpFile,
  agyEnvKey, agyEnvSaving, agyRouterUrl, writeAgyEnv, clearAgyEnv, agyEnvBase, agyEnvKeyLoaded,
  agyAliasText, agyAliasSaving, agyBuiltinModels, agyAliasMap, agySetAlias,
  agyRouterModels, agyToOpenFor, agyToGroups, pickAgyTo, agyToKeyNav,
  loadAgyAliasPanel, saveAgyAliases,
} from './sync/agy'
// ---- 中枢动作（页签/构建/保存/打开/通用联动/统一模型下拉） ----
import {
  currentCfgFile, currentCfgExists, switchMode, tabJsonLabel, claudeModelOpen, ocModelOpen, geminiModelOpen,
  pickClaudeModel, pickOcModel, pickGeminiModel, buildFormText, saveTargetConfig, openTargetConfig,
  initSyncController,
} from './sync/controller'

// ---- watchers 注册（在组件 setup 上下文内建立，随组件生命周期清理） ----
initClaude()
initPi()
initGrok()
initCodex()
initKimi()
initGemini()
initOpencode()
initCline()
initCommandCode()
initHermes()
initKilo()
initAgy()
initSyncController()

// agy 接入环境变量写/清：清除走应用内确认弹窗（替代原生 confirm）
async function agyWriteEnv() {
  if (agyEnvSaving.value) return
  agyEnvSaving.value = true
  try {
    const err = await writeAgyEnv()
    if (!err) toast(t('sync.agy.envWritten'))
    else toast(t('common.fail', { msg: err }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { agyEnvSaving.value = false }
}
const agyClearOpen = ref(false)
const copilotClearOpen = ref(false)
function agyClearEnv() { agyClearOpen.value = true }
async function agyClearEnvConfirm() {
  agyClearOpen.value = false
  agyEnvSaving.value = true
  try {
    const err = await clearAgyEnv()
    if (!err) toast(t('sync.agy.envCleared'))
    else toast(t('common.fail', { msg: err }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '?' })) } finally { agyEnvSaving.value = false }
}
function copilotClearAsk() { copilotClearOpen.value = true }
async function copilotClearConfirm() {
  copilotClearOpen.value = false
  await clearCopilotEnv()
}
// agy 环境变量单按钮：已配置（env 已写入）且 API Key 未改动 → 显示「清除接入配置」；否则显示「写入环境变量」
const agyEnvShowClear = computed(() =>
  String(agyEnvBase.value || '').trim() !== '' && String(agyEnvKey.value || '') === String(agyEnvKeyLoaded.value || ''))

// 非 form 模式（json / claudemd / climodels 等源码页签）走源码编辑排版
const isSrcMode = computed(() => cfgMode.value !== 'form')

onMounted(() => { loadTargets() })

// ---- 通用（未特化）客户端权限规则：卡片三态点选列表 ↔ cfgObj.permissions.{allow,ask,deny} ----
// get 侧把三段数组并成 [{ text, mode }] 行列表；set 侧按三态分组写回（非空才保留对应键）。
const genericPermRows = computed({
  get() {
    const c = cfgObj.value || {}
    const perms = (c.permissions && typeof c.permissions === 'object' && !Array.isArray(c.permissions)) ? c.permissions : {}
    return permRowsFromLists(perms)
  },
  set(rows) {
    const c = cfgObj.value
    if (!c || typeof c !== 'object') return
    const prevPerm = (c.permissions && typeof c.permissions === 'object' && !Array.isArray(c.permissions)) ? c.permissions : {}
    const perms = { ...prevPerm }
    const lists = permRowsToLists(rows)
    for (const k of ['allow', 'ask', 'deny']) {
      if (lists[k].length) perms[k] = lists[k]
      else delete perms[k]
    }
    c.permissions = perms
  },
})

// Command Code 服务商下拉选项：本地 BYOK 条目（随服务商标识联动）+ 官方云端订阅
const ccProviderOptions = computed(() => {
  const pid = String(ccForm.value.endpointId || '').trim() || 'llmrouter'
  const list = [pid]
  if (pid !== 'llmrouter') list.push('llmrouter')
  list.push('command-code')
  return list
})

// Claude 高级字段按类型排宽：bool/select/数字 窄排并排，文本中等，list 独占一行
const claudeFieldWidth = f =>
  f.type === 'list' ? 'fw-full'
  : (f.type === 'bool' || f.type === 'select' || f.type === 'number') ? 'fw-s'
  : 'fw-m'
// Claude 权限规则：预设规则常驻展示为双态（允许/拒绝）点选卡片
const claudeRuleMode = rule => {
  const r = claudePermRows.value.find(r => String(r.text || '') === rule)
  return r ? r.mode : ''
}
function claudeRulePick(rule, mode) {
  const i = claudePermRows.value.findIndex(r => String(r.text || '') === rule)
  if (i >= 0) {
    if (claudePermRows.value[i].mode === mode) claudePermRows.value.splice(i, 1)
    else claudePermRows.value[i].mode = mode
  } else {
    claudePermRows.value.push({ text: rule, mode })
  }
}
// 非预设的既有自定义规则：额外展示为卡片（保留不被丢弃），可删除
const customClaudeRows = computed(() =>
  claudePermRows.value.filter(r => !PERM_COMMON_RULES.some(p => p.v === String(r.text || '')))
)
// Grok 权限规则：预设规则常驻卡片三态点选（allow/ask/deny），自定义规则卡片可追加/删除
const grokRuleMode = rule => {
  const r = grokPermRows.value.find(r => String(r.text || '') === rule)
  return r ? r.mode : ''
}
function grokRulePick(rule, mode) {
  const i = grokPermRows.value.findIndex(r => String(r.text || '') === rule)
  if (i >= 0) {
    if (grokPermRows.value[i].mode === mode) grokPermRows.value.splice(i, 1)
    else grokPermRows.value[i].mode = mode
  } else {
    grokPermRows.value.push({ text: rule, mode })
  }
}
const customGrokRows = computed(() =>
  grokPermRows.value.filter(r => !GROK_PERM_RULES.some(p => p.v === String(r.text || '')))
)
// MCP 服务器面板：当前已配置的服务器名（对 cfgObj 响应式，随改动自动刷新）
const claudeMcps = computed(() => claudeMcpServerNames())
</script>

<template>
  <div>
    <!-- 客户端卡片网格 -->
    <div class="sync-grid">
      <div v-for="tg in targets" :key="tg.id" class="sync-card" :class="{ ph: tg.placeholder }">
        <div class="sync-card-main" @click="!tg.placeholder && openTargetConfig(tg)">
          <div class="sync-card-head">
            <div class="sync-client-avatar" :class="'avatar-' + tg.id">
              <img v-if="targetLogo(tg.id)" class="sync-client-logo" :src="targetLogo(tg.id)" alt="" draggable="false" />
              <span v-else-if="tg.id.startsWith('pi')">π</span>
              <span v-else>◈</span>
            </div>
            <div class="sync-title-text">
              <span class="sync-card-name">{{ tg.name }}</span>
              <span v-if="!tg.placeholder" class="sync-card-arrow">›</span>
            </div>
          </div>
          <div class="sync-card-body">
            <div class="sync-card-tags">
              <span v-if="tg.skillsSupported" class="sync-tag t-skills">⚡ skills</span>
              <span v-if="tg.mcpSupported" class="sync-tag t-mcp">🔌 mcp</span>
              <span v-if="!tg.skillsSupported && !tg.mcpSupported" class="sync-tag">⚙ config</span>
            </div>
          </div>
        </div>
        <span v-if="tg.placeholder" class="sync-ph">{{ t('sync.notConnected') }}</span>
      </div>
    </div>

    <!-- 配置编辑弹窗 -->
    <div v-if="cfgEdit" class="dialog-overlay open">
      <div class="dialog cfg-dialog">
        <div class="cfg-head">
          <div class="cfg-head-top">
            <div class="cfg-title">{{ t('sync.configTitle', { name: cfgTarget?.name }) }}</div>
            <div class="cfg-head-actions">
              <button class="cfg-info-btn" type="button" :class="{ on: cfgInfoOpen }" :title="t('sync.configDetail')" @click="cfgInfoOpen = !cfgInfoOpen">
                <svg viewBox="0 0 24 24" width="13" height="13" aria-hidden="true"><circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="1.8"/><rect x="11" y="10.5" width="2" height="6" rx="1" fill="currentColor"/><circle cx="12" cy="7.6" r="1.15" fill="currentColor"/></svg>
              </button>
              <div v-if="cfgInfoOpen" class="cfg-info-mask" @click="cfgInfoOpen=false"></div>
              <div v-if="cfgInfoOpen" class="cfg-info-pop">
                <div class="ip-row">
                  <span class="ip-k">{{ t('sync.routerUrl') }}</span>
                  <code>{{ routerHintForTarget() }}</code>
                  <button class="copy-btn" @click="copyText(routerHintForTarget(), $event.currentTarget)">{{ t('sync.copy') }}</button>
                </div>
              </div>
              <button class="btn btn-green btn-sm cfg-save" type="button" :disabled="cfgSaving || cfgLoading" @click="saveTargetConfig">{{ cfgSaving ? '…' : t('common.save') }}</button>
              <span class="cfg-actions-divider" aria-hidden="true"></span>
              <button class="cfg-close" type="button" @click="cfgEdit=false" :title="t('sync.close')">
                <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>
              </button>
            </div>
          </div>
          <div class="cfg-tabs">
            <button v-if="cfgTarget?.id!=='copilot'" class="cfg-tab" :class="{on:cfgMode==='form'}" @click="switchMode('form')">{{ t('sync.tabForm') }}</button>
            <button class="cfg-tab" :class="{on:cfgMode==='json'}" @click="switchMode('json')">{{ tabJsonLabel }}</button>
            <button v-if="cfgTarget?.id==='opencode'" class="cfg-tab" :class="{on:cfgMode==='openagents'}" @click="switchMode('openagents')" :title="'~/.config/opencode/AGENTS.md ' + t('sync.src')">AGENTS.md</button>
            <button v-if="cfgTarget?.id==='kilo'" class="cfg-tab" :class="{on:cfgMode==='kiloagents'}" @click="switchMode('kiloagents')" :title="'~/.config/kilo/AGENTS.md ' + t('sync.src')">AGENTS.md</button>
            <template v-if="cfgTarget?.id==='pi'">
              <button class="cfg-tab" :class="{on:cfgMode==='globjson'}" @click="switchMode('globjson')" :title="'~/.pi/agent/settings.json ' + t('sync.src')">settings.json</button>
              <button class="cfg-tab" :class="{on:cfgMode==='pimcp'}" @click="switchMode('pimcp')" :title="'~/.pi/agent/mcp.json ' + t('sync.src')">mcp.json</button>
              <button class="cfg-tab" :class="{on:cfgMode==='agentsmd'}" @click="switchMode('agentsmd')" :title="'~/.pi/agent/AGENTS.md ' + t('sync.src')">AGENTS.md</button>
            </template>
            <button v-if="cfgTarget?.id==='grok-build'" class="cfg-tab" :class="{on:cfgMode==='grokagents'}" @click="switchMode('grokagents')" :title="'~/.grok/AGENTS.md ' + t('sync.src')">AGENTS.md</button>
            <button v-if="cfgTarget?.id==='codex'" class="cfg-tab" :class="{on:cfgMode==='codexagents'}" @click="switchMode('codexagents')" :title="'~/.codex/AGENTS.md ' + t('sync.src')">AGENTS.md</button>
            <button v-if="cfgTarget?.id==='kimi'" class="cfg-tab" :class="{on:cfgMode==='kimiagents'}" @click="switchMode('kimiagents')" :title="'~/.kimi-code/AGENTS.md ' + t('sync.src')">AGENTS.md</button>
            <template v-if="cfgTarget?.id==='commandcode'">
              <button class="cfg-tab" :class="{on:cfgMode==='ccsettings'}" @click="switchMode('ccsettings')" :title="'~/.commandcode/settings.json ' + t('sync.src')">settings.json</button>
              <button class="cfg-tab" :class="{on:cfgMode==='ccproviders'}" @click="switchMode('ccproviders')" :title="'~/.commandcode/providers.json ' + t('sync.src')">providers.json</button>
            </template>
            <template v-if="cfgTarget?.id==='cline'">
              <button class="cfg-tab" :class="{on:cfgMode==='clinemodels'}" @click="switchMode('clinemodels')" :title="'~/.cline/data/settings/models.json ' + t('sync.src')">models.json</button>
              <button class="cfg-tab" :class="{on:cfgMode==='clineproviders'}" @click="switchMode('clineproviders')" :title="'~/.cline/data/settings/providers.json ' + t('sync.src')">providers.json</button>
              <button class="cfg-tab" :class="{on:cfgMode==='clineagents'}" @click="switchMode('clineagents')" :title="'~/.agents/AGENTS.md ' + t('sync.src')">AGENTS.md</button>
              <button class="cfg-tab" :class="{on:cfgMode==='clineglobalstate'}" @click="switchMode('clineglobalstate')" :title="'~/.cline/data/globalState.json ' + t('sync.src')">globalState.json</button>
            </template>
            <template v-if="cfgTarget?.id==='gemini'">
              <button class="cfg-tab" :class="{on:cfgMode==='geminienv'}" @click="switchMode('geminienv')" :title="'~/.gemini/.env ' + t('sync.src')">.env</button>
              <button class="cfg-tab" :class="{on:cfgMode==='geminimd'}" @click="switchMode('geminimd')" :title="'~/.gemini/GEMINI.md ' + t('sync.src')">GEMINI.md</button>
            </template>
            <button v-if="cfgTarget?.id==='claude-code' || cfgTarget?.id==='claude-desktop'" class="cfg-tab" :class="{on:cfgMode==='claudemd'}" @click="switchMode('claudemd')" :title="'~/.claude/CLAUDE.md ' + t('sync.src')">CLAUDE.md</button>
            <button v-if="cfgTarget?.id==='copilot'" class="cfg-tab" :class="{on:cfgMode==='copilotenv'}" @click="switchMode('copilotenv')" :title="t('sync.copilot.tabTip')">{{ t('sync.copilot.title') }}</button>
            <button v-if="cfgTarget?.id==='copilot'" class="cfg-tab" :class="{on:cfgMode==='copilotperm'}" @click="switchMode('copilotperm')" :title="t('sync.copilot.permTabTip')">{{ t('sync.copilot.permTab') }}</button>
            <button v-if="cfgTarget?.id==='agy'" class="cfg-tab" :class="{on:cfgMode==='agymcp'}" @click="switchMode('agymcp')" :title="'~/.gemini/config/mcp_config.json ' + t('sync.src')">mcp_config.json</button>
          </div>
        </div>

        <div class="cfg-body" :class="{ 'cfg-body-src': isSrcMode }">
          <div v-if="cfgLoading" style="padding:20px;text-align:center;color:var(--t3)">…</div>
          <div v-else-if="cfgMode==='form'" class="cfg-form" style="display:flex;flex-direction:column;gap:6px">

            <!-- Claude Code / Desktop -->
            <template v-if="cfgTarget?.id==='claude-code' || cfgTarget?.id==='claude-desktop'">
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="field-row" style="margin-top:2px">
                  <div class="field fw-l">
                    <label>{{ t('sync.claude.baseUrlEnv') }} <span class="tip"><i>?</i><em>env</em></span></label>
                    <input :value="routerHintForTarget()" readonly />
                  </div>
                  <div class="field fw-m">
                    <label>{{ t('sync.claude.tokenEnv') }} <span class="tip"><i>?</i><em>env</em></span></label>
                    <input :value="getPath(cfgObj,'env.ANTHROPIC_AUTH_TOKEN') || ''" @input="e=> setPath(cfgObj,'env.ANTHROPIC_AUTH_TOKEN', e.target.value)" :placeholder="t('sync.routeToken')" />
                  </div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.claude.endpointTip') }}</em></div>
              </section>

              <section class="card cfg-card">
                <div v-for="(row, i) in claudeEnvModels" :key="row" class="model-row">
                  <div class="field"><label>{{ t('sync.envKey') }}</label><input v-model="row.envKey" class="env-ro" readonly /></div>
                  <div class="field model-sel"><label>{{ t('sync.models') }}</label>
                    <div class="pi-sel">
                      <input v-model="row.modelId" :placeholder="t('sync.pickModel')" @focus="claudeModelOpen=i" @click.stop="claudeModelOpen=i" @input="claudeModelOpen=i" @blur="setTimeout(()=>{ if(claudeModelOpen===i) claudeModelOpen=-1 },120)" />
                      <div v-show="claudeModelOpen===i" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(row.modelId)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickClaudeModel(i, m)">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(row.modelId).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field ctx-len"><label>{{ t('sync.contextLen') }}</label><input type="number" v-model="row.contextWindow" placeholder="200000" /></div>
                  <button type="button" class="btn btn-xs btn-ghost row-del" @click="claudeEnvModels.splice(i,1)">×</button>
                </div>
                <button v-if="claudeEnvModels.length < CLAUDE_ENV_KEY_SLOTS.length" type="button" class="btn btn-xs btn-ghost" style="margin-top:2px" @click="claudeEnvModels.push(newClaudeEnvModelRow())">{{ t('sync.addMapping') }}</button>
              </section>

              <section class="card cfg-card">
                <div class="field" style="margin-top:2px">
                  <label>{{ t('sync.model') }} <span class="tip"><i>?</i><em>settings.json 顶层，生效于未走环境变量映射的场景</em></span></label>
                  <div class="pi-sel">
                    <input v-model="claudeDefaultModel" :placeholder="t('sync.pickModel')" @focus="claudeModelOpen=-2" @click.stop="claudeModelOpen=-2" @input="claudeModelOpen=-2" @blur="setTimeout(()=>{ if(claudeModelOpen===-2) claudeModelOpen=-1 },120)" />
                    <div v-show="claudeModelOpen===-2" class="pi-sel-drop">
                      <div v-for="g in modelGroupsByLetter(claudeDefaultModel)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                        <div class="pi-sel-letter">{{ g.letter }}</div>
                        <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="claudeDefaultModel=m; claudeModelOpen=-1">{{ m }}</button>
                      </div>
                      <div v-if="!modelGroupsByLetter(claudeDefaultModel).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                    </div>
                  </div>
                </div>
              </section>

              <div class="panel-head ph-2">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <div class="perm-mode">
                  <button v-for="o in PERM_OPTIONS" :key="o.value" type="button" class="perm-mode-card"
                    :class="{ on: (getPath(cfgObj,'permissions.defaultMode')||getPath(cfgObj,'permission')||'default')===o.value }"
                    @click="setPath(cfgObj,'permissions.defaultMode',o.value)">
                    <span class="pm-name">{{ o.name() }}</span>
                    <span v-if="i18nState.lang==='en'" class="pm-desc">{{ o.desc() }}</span>
                  </button>
                </div>
                <div class="field" style="margin:4px 0"><label>{{ t('sync.claude.permRulesLabel') }}</label>
                  <div class="perm-grid">
                    <div v-for="r in PERM_COMMON_RULES" :key="r.v" class="perm-rule-card" :class="{ on: claudeRuleMode(r.v) }">
                      <span class="perm-rule-text" :title="r.tip()">{{ r.v }}</span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="r.v">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{ on: claudeRuleMode(r.v)==='allow' }" :aria-checked="claudeRuleMode(r.v)==='allow'" @click="claudeRulePick(r.v, 'allow')">{{ t('sync.allow') }}</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{ on: claudeRuleMode(r.v)==='deny' }" :aria-checked="claudeRuleMode(r.v)==='deny'" @click="claudeRulePick(r.v, 'deny')">{{ t('sync.deny') }}</button>
                        <button type="button" class="pcl-opt ask" role="radio" :class="{ on: claudeRuleMode(r.v)==='ask' }" :aria-checked="claudeRuleMode(r.v)==='ask'" @click="claudeRulePick(r.v, 'ask')">{{ t('sync.ask') }}</button>
                      </div>
                    </div>
                    <div v-for="row in customClaudeRows" :key="row" class="perm-rule-card" :class="{ on: row.mode }">
                      <span class="perm-rule-text" :title="row.text">{{ row.text }}</span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="row.text">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{ on: row.mode === 'allow' }" :aria-checked="row.mode === 'allow'" @click="claudeRulePick(row.text, 'allow')">{{ t('sync.allow') }}</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{ on: row.mode === 'deny' }" :aria-checked="row.mode === 'deny'" @click="claudeRulePick(row.text, 'deny')">{{ t('sync.deny') }}</button>
                        <button type="button" class="pcl-opt ask" role="radio" :class="{ on: row.mode === 'ask' }" :aria-checked="row.mode === 'ask'" @click="claudeRulePick(row.text, 'ask')">{{ t('sync.ask') }}</button>
                      </div>
                      <button type="button" class="pcl-del" :title="t('sync.delete')" @click="claudePermRows.splice(claudePermRows.indexOf(row),1)">×</button>
                    </div>
                  </div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.claude.permTip') }}</em></div>
              </section>

              <div class="panel-head ph-3">{{ t('sync.claude.mcpTitle') }}<span v-if="claudeMcps.length" class="cfg-head-count">{{ claudeMcps.length }}</span></div>
              <section class="card cfg-card">
                <div v-if="claudeMcps.length" class="cfg-mcp-list">
                  <span v-for="n in claudeMcps" :key="n" class="cfg-mcp-chip">{{ n }}</span>
                </div>
                <div v-else class="cfg-mcp-empty">{{ t('sync.claude.mcpEmpty') }}</div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.claude.mcpTip') }}</em></div>
              </section>

              <template v-for="(g, gi) in CLAUDE_EXTRA_GROUPS" :key="g.id">
                <div class="panel-head" :class="gi === 0 ? 'ph-3' : 'ph-4'">{{ t('sync.claude.group.' + g.id) }}</div>
                <section class="card cfg-card">
                  <div class="cfg-grid">
                    <div v-for="f in g.fields" :key="f.key" class="field" :class="claudeFieldWidth(f)">
                      <label>{{ t('sync.claude.x.' + f.key) }} <span class="tip"><i>?</i><em>{{ f.key }}</em></span></label>
                      <select v-if="f.type==='bool'" v-model="claudeExtra[f.key]">
                        <option value="">{{ t('sync.default') }}</option>
                        <option value="true">{{ t('sync.opt.true') }}</option>
                        <option value="false">{{ t('sync.opt.false') }}</option>
                      </select>
                      <select v-else-if="f.type==='select'" v-model="claudeExtra[f.key]">
                        <option value=""></option>
                        <option v-for="o in f.options" :key="o" :value="o">{{ optTxt(o) }}</option>
                      </select>
                      <textarea v-else-if="f.type==='list'" v-model="claudeExtra[f.key]" rows="2" class="perm-ta" @wheel="taWheelStop"></textarea>
                      <input v-else :type="f.type==='number' ? 'number' : 'text'" v-model="claudeExtra[f.key]" />
                    </div>
                  </div>
                </section>
              </template>
            </template>

            <!-- pi -->
            <template v-else-if="cfgTarget?.id==='pi'">
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card pi-card" id="pi-panel-models">
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.providerKey') }}</label><input v-model="piM.key" placeholder="LLM" /></div>
                  <div class="field fw-m"><label>api（{{ t('sync.required') }}）</label>
                    <select v-model="piM.api">
                      <option v-for="o in PI_PROVIDER_API_OPTIONS" :key="o.value" :value="o.value">{{ o.label() }}</option>
                    </select>
                  </div>
                  <div class="field fw-l"><label>{{ t('sync.baseUrl') }}</label><input v-model="piM.baseUrl" placeholder="http://127.0.0.1:3000" /></div>
                  <div class="field fw-m"><label>{{ t('sync.apiKey') }}</label><input v-model="piM.apiKey" :placeholder="t('sync.routeToken')" /></div>
                </div>
                <div class="pi-fetch-bar">
                  <span v-if="piFetchLoading" class="pi-fetch-loading">{{ t('sync.piFetching') }}</span>
                  <template v-else>
                    <span class="tip"><i>?</i><em>{{ piBaseModels.length ? '已按当前 baseUrl 拉取 ' + piBaseModels.length + ' 个模型' : '模型下拉按当前填写的 baseUrl 自动拉取（尚无结果）' }}</em></span>
                    <button type="button" class="btn btn-xs btn-ghost" @click.stop="fetchPiBaseModels">{{ t('sync.refetch') }}</button>
                  </template>
                </div>
                <div v-for="(row, i) in piM.models" :key="row" class="pi-model-block">
                  <div class="pi-model-head"><strong>#{{ i+1 }}</strong><button type="button" class="btn btn-xs btn-ghost row-del" @click="piM.models.splice(i,1)">{{ t('sync.delete') }}</button></div>
                  <div class="field-row">
                    <div class="field fw-l"><label>{{ t('sync.id') }} <span class="tip"><i>?</i><em>{{ t('sync.modelsList') }}</em></span></label>
                      <div class="pi-sel">
                        <input v-model="row.id" :placeholder="t('sync.pickModel')" @focus="modelSelOpen=i" @click.stop="modelSelOpen=i" @input="modelSelOpen=i" @blur="setTimeout(()=>{ if(modelSelOpen===i) modelSelOpen=-1 },120)" />
                        <div v-show="modelSelOpen===i" class="pi-sel-drop">
                          <div v-for="g in modelGroupsByLetter(row.id)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                            <div class="pi-sel-letter">{{ g.letter }}</div>
                            <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickModel(i, m)">{{ m }}</button>
                          </div>
                          <div v-if="!modelGroupsByLetter(row.id).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                        </div>
                      </div>
                    </div>
                    <div class="field fw-m"><label>{{ t('sync.name') }}</label><input v-model="row.name" :placeholder="t('sync.namePh')" /></div>
                    <div class="field fw-s"><label>{{ t('sync.thinking') }}</label>
                      <select v-model="row.thinkingLevel"><option value="">{{ t('sync.default') }}</option><option value="low">{{ t('sync.opt.low') }}</option><option value="high">{{ t('sync.opt.high') }}</option><option value="max">{{ t('sync.opt.max') }}</option></select>
                    </div>
                    <div class="field fw-s"><label>{{ t('sync.contextLen') }}</label>
                      <select v-model="row.contextWindow"><option value=""></option><option v-for="v in PI_CONTEXT_OPTIONS" :key="v" :value="String(v)">{{ v }}</option></select>
                    </div>
                  </div>
                  <div class="field-row">
                    <div class="field fw-s"><label>{{ t('sync.thinking') }}</label>
                      <select v-model="row.reasoning"><option :value="true">{{ t('sync.opt.true') }}</option><option :value="false">{{ t('sync.opt.false') }}</option></select>
                    </div>
                    <div class="field fw-s"><label>{{ t('sync.supportsTools') }}</label>
                      <select v-model="row.supportsTools"><option :value="true">{{ t('sync.opt.true') }}</option><option :value="false">{{ t('sync.opt.false') }}</option></select>
                    </div>
                    <div class="field fw-s"><label>{{ t('sync.supportsVision') }}</label>
                      <select v-model="row.supportsVision"><option :value="true">{{ t('sync.opt.true') }}</option><option :value="false">{{ t('sync.opt.false') }}</option></select>
                    </div>
                  </div>
                </div>
                <button type="button" class="btn btn-xs btn-ghost" style="align-self:flex-start" @click="piM.models.push(newPiModelRow({}))">{{ t('sync.addModel') }}</button>
              </section>

              <section class="card cfg-card oc-card" id="pi-panel-default">
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.defaultProvider') }} <span class="tip"><i>?</i><em>{{ t('sync.pi.defaultProviderTip') }}</em></span></label><input v-model="piSettings.defaultProvider" placeholder="LLM" /></div>
                  <div class="field fw-l"><label>{{ t('sync.defaultModel') }}</label>
                    <div class="pi-sel">
                      <input v-model="piSettings.defaultModel" placeholder="x-preview-f-free" @focus="cfgModelOpen=true" @click.stop="cfgModelOpen=true" @input="cfgModelOpen=true" @blur="setTimeout(()=>{ cfgModelOpen=false },120)" />
                      <div v-show="cfgModelOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(piSettings.defaultModel)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickDefaultModel(m)">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(piSettings.defaultModel).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.defaultThinkingLevel') }}</label>
                    <select v-model="piSettings.defaultThinkingLevel"><option value=""></option><option v-for="o in PI_SETTINGS_THINKING_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                </div>
              </section>

              <div class="panel-head ph-2">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card oc-card" id="pi-panel-perm">
                <div class="perm-list">
                  <div class="perm-row-wrap" :class="{ on: piSettings.steeringMode }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ t('sync.steeringMode') }}</span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="t('sync.steeringMode')">
                        <button v-for="o in PI_STEERING_OPTIONS" :key="o" type="button" class="pcl-opt rules" role="radio" :class="{on: piSettings.steeringMode===o}" :aria-checked="piSettings.steeringMode===o" @click="piSettings.steeringMode = piSettings.steeringMode===o ? '' : o">{{ optTxt(o) }}</button>
                      </div>
                    </div>
                  </div>
                  <div class="perm-row-wrap" :class="{ on: piSettings.followUpMode }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ t('sync.followUpMode') }}</span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="t('sync.followUpMode')">
                        <button v-for="o in PI_STEERING_OPTIONS" :key="o" type="button" class="pcl-opt rules" role="radio" :class="{on: piSettings.followUpMode===o}" :aria-checked="piSettings.followUpMode===o" @click="piSettings.followUpMode = piSettings.followUpMode===o ? '' : o">{{ optTxt(o) }}</button>
                      </div>
                    </div>
                  </div>
                  <div class="perm-row-wrap" :class="{ on: piSettings.defaultProjectTrust }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ t('sync.defaultProjectTrust') }}</span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="t('sync.defaultProjectTrust')">
                        <button v-for="o in PI_TRUST_OPTIONS" :key="o" type="button" class="pcl-opt rules" role="radio" :class="{on: piSettings.defaultProjectTrust===o}" :aria-checked="piSettings.defaultProjectTrust===o" @click="piSettings.defaultProjectTrust = piSettings.defaultProjectTrust===o ? '' : o">{{ optTxt(o) }}</button>
                      </div>
                    </div>
                  </div>
                </div>
              </section>

              <div class="panel-head ph-3">{{ t('sync.panelStyle') }}</div>
              <section class="card cfg-card oc-card" id="pi-panel-style">
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.theme') }}</label>
                    <select v-model="piSettings.theme"><option value=""></option><option v-for="o in PI_THEME_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.lastChangelog') }} <span class="tip"><i>?</i><em>{{ t('sync.pi.lastChangelogTip') }}</em></span></label><input v-model="piSettings.lastChangelogVersion" placeholder="0.74.0" /></div>
                </div>
              </section>
            </template>

            <!-- grok-build -->
            <template v-else-if="cfgTarget?.id==='grok-build'">
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card" id="grok-panel-model">
                <div class="field-row" style="margin-top:6px">
                  <div class="field fw-m">
                    <label>{{ t('sync.grok.currentBlock') }} <span class="tip"><i>?</i><em>{{ t('sync.grok.currentBlockTip') }}</em></span></label>
                    <select :value="grokForm.modelKey" @change="e=>selectGrokModel(e.target.value)">
                      <option v-for="k in grokOrder" :key="k" :value="k">{{ k }}{{ k===grokForm.default ? t('sync.defaultMark') : '' }}</option>
                      <option v-if="!grokOrder.includes(grokForm.modelKey)" :value="grokForm.modelKey">{{ grokForm.modelKey || '…' }}</option>
                    </select>
                  </div>
                  <div class="field fw-m">
                    <label>[models] default <span class="tip"><i>?</i><em>{{ t('sync.grok.defaultTip') }}</em></span></label>
                    <select :value="grokForm.default" @change="e=>pickGrokDefault(e.target.value)"><option value=""></option><option v-for="k in grokDefaultCandidates()" :key="k" :value="k">{{ k }}</option></select>
                  </div>
                  <div class="field fw-m">
                    <label>{{ t('sync.grok.newBlock') }} <span class="tip"><i>?</i><em>{{ t('sync.grok.newBlockTip') }}</em></span></label>
                    <input :placeholder="t('sync.grok.newBlockPh')" @keydown.enter.prevent="addGrokModel($event.target.value); $event.target.value=''" />
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-l">
                    <label>{{ t('sync.baseUrl') }} <span class="tip"><i>?</i><em>{{ t('sync.grok.baseUrlTip') }}</em></span></label>
                    <div class="ro-url">
                      <input :value="derivedBaseUrl(grokForm.accessMode)" readonly />
                      <button type="button" class="btn btn-xs btn-ghost" @click="copyText(derivedBaseUrl(grokForm.accessMode), $event.currentTarget)">{{ t('sync.copy') }}</button>
                    </div>
                  </div>
                  <div class="field fw-l"><label>{{ t('sync.grok.upstreamModel') }}</label>
                    <div class="pi-sel">
                      <input v-model="grokModelSearch" :placeholder="grokForm.model || t('sync.pickModel')" @focus="grokModelOpen=true" @click.stop="grokModelOpen=true" @input="grokModelOpen=true" @blur="setTimeout(()=>{ grokModelOpen=false },120)" @keydown="grokModelKeyNav" />
                      <div v-show="grokModelOpen" class="pi-sel-drop">
                        <div v-for="g in grokModelGroups()" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickGrokModel(m)">{{ m }}</button>
                        </div>
                        <div v-if="!grokModelGroups().length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.thinking') }}</label>
                    <select v-model="grokForm.reasoningEffort">
                      <option value="">{{ t('sync.default') }}</option>
                      <option value="low">{{ t('sync.opt.low') }}</option>
                      <option value="high">{{ t('sync.opt.high') }}</option>
                      <option value="max">{{ t('sync.opt.max') }}</option>
                    </select>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.name') }}</label><input v-model="grokForm.name" :placeholder="t('sync.namePh')" /></div>
                  <div class="field fw-l"><label>{{ t('sync.grok.description') }}</label><input v-model="grokForm.description" placeholder="Grok 4.5 from xAI" /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.contextLen') }}</label><input v-model="grokForm.contextWindow" type="number" min="1" placeholder="1000000" /></div>
                  <div class="field fw-m"><label>{{ t('sync.grok.webSearch') }} <span class="tip"><i>?</i><em>{{ t('sync.grok.webSearchTip') }}</em></span></label>
                    <select :value="grokForm.webSearch" @change="e=>pickGrokWebSearch(e.target.value)"><option value="">—</option><option v-for="k in grokDefaultCandidates()" :key="k" :value="k">{{ k }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.grok.apiBackend') }}</label>
                    <select v-model="grokForm.apiBackend"><option value="">{{ t('sync.default') }}</option><option value="responses">responses</option><option value="chat">chat</option></select>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-s"><label>{{ t('sync.temperature') }}</label><input v-model="grokForm.temperature" type="number" step="0.05" min="0" max="2" placeholder="0.7" /></div>
                  <div class="field fw-s"><label>{{ t('sync.topP') }}</label><input v-model="grokForm.topP" type="number" step="0.05" min="0" max="1" placeholder="0.95" /></div>
                  <div class="field fw-s"><label>{{ t('sync.maxTokens') }}</label><input v-model="grokForm.maxTokens" type="number" min="1" placeholder="8192" /></div>
                  <div class="field fw-s"><label>{{ t('sync.grok.supportsBackendSearch') }}</label>
                    <select v-model="grokForm.supportsBackendSearch"><option value="">{{ t('sync.default') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-full"><label>{{ t('sync.apiKey') }} <span class="tip"><i>?</i><em>{{ t('sync.grok.apiKeyTip') }}</em></span></label><input v-model="grokForm.apiKey" :placeholder="t('sync.routeToken')" /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-full"><label>{{ t('sync.grok.extraHeaders') }} <span class="tip"><i>?</i><em>TOML inline table</em></span></label><input v-model="grokForm.extraHeaders" placeholder='{ "x-api-key" = "..." }' /></div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.grok.blocksTip') }}</em></div>
              </section>

              <div class="panel-head ph-2">{{ t('sync.grok.panelUi') }}</div>
              <section class="card cfg-card" id="grok-panel-ui">
                <div class="cfg-grid">
                  <div class="field fw-m"><label>{{ t('sync.theme') }}</label>
                    <select v-model="grokForm.uiTheme"><option value=""></option><option value="auto">{{ t('sync.opt.auto') }}</option><option value="dark">{{ t('sync.opt.dark') }}</option><option value="light">{{ t('sync.opt.light') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.simpleMode') }}</label>
                    <select v-model="grokForm.uiSimpleMode"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.vimMode') }}</label>
                    <select v-model="grokForm.uiVim"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.scrollSpeed') }}</label><input type="number" v-model="grokForm.uiScrollSpeed" placeholder="50" />
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.compactMode') }}</label>
                    <select v-model="grokForm.uiCompact"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.grok.showThinking') }}</label>
                    <select v-model="grokForm.uiThinking"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.maxThoughtsWidth') }}</label><input type="number" v-model="grokForm.uiMaxThoughtsWidth" placeholder="120" />
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.grok.defaultPerm') }}</label><input v-model="grokForm.uiDefaultPerm" placeholder="always_allow_all_sessions" />
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.rememberApprovals') }}</label>
                    <select v-model="grokForm.uiRememberApprovals"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.groupVerbs') }}</label>
                    <select v-model="grokForm.uiGroupVerbs"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.collapsedEdit') }}</label>
                    <select v-model="grokForm.uiCollapsedEdit"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.pageFlip') }}</label>
                    <select v-model="grokForm.uiPageFlip"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.grok.screenMode') }}</label><input v-model="grokForm.uiScreenMode" placeholder="fullscreen" />
                  </div>
                </div>
              </section>

              <div class="panel-head ph-3">{{ t('sync.grok.panelToolset') }}</div>
              <section class="card cfg-card" id="grok-panel-toolset">
                <div class="cfg-grid">
                  <div class="field fw-m"><label>{{ t('sync.grok.fileToolset') }}</label><input v-model="grokForm.tsFileToolset" placeholder="standard" />
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.grok.hashLine') }}/scheme</label><input v-model="grokForm.hlScheme" placeholder="chunk" />
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.hashLine') }}/hash_len</label><input type="number" v-model="grokForm.hlHashLen" placeholder="3" />
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.hashLine') }}/chunk_size</label><input type="number" v-model="grokForm.hlChunkSize" placeholder="8" />
                  </div>
                </div>
              </section>

              <div class="panel-head ph-4">{{ t('sync.grok.panelOther') }}</div>
              <section class="card cfg-card" id="grok-panel-other">
                <div class="cfg-grid">
                  <div class="field fw-m"><label>{{ t('sync.grok.autoUpdate') }}</label>
                    <select v-model="grokForm.autoUpdate"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.grok.autoCompact') }}</label><input type="number" v-model="grokForm.autoCompactThreshold" placeholder="85" /></div>
                </div>
              </section>

              <div class="panel-head ph-5">{{ t('sync.grok.panelFeatures') }}</div>
              <section class="card cfg-card" id="grok-panel-features">
                <div class="cfg-grid">
                  <div class="field fw-s"><label>{{ t('sync.grok.telemetry') }}</label>
                    <select v-model="grokForm.featTelemetry"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.feedback') }}</label>
                    <select v-model="grokForm.featFeedback"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.lspTools') }}</label>
                    <select v-model="grokForm.featLsp"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.codebaseIndexing') }}</label>
                    <select v-model="grokForm.featIndexing"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.twoPassCompaction') }}</label>
                    <select v-model="grokForm.featTwoPass"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.remoteFetch') }}</label>
                    <select v-model="grokForm.featRemoteFetch"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                </div>
              </section>

              <div class="panel-head ph-6">{{ t('sync.grok.panelCompat') }}</div>
              <section class="card cfg-card" id="grok-panel-compat">
                <div class="cfg-grid">
                  <div class="field fw-s"><label>{{ t('sync.grok.compatClaudeMcps') }}</label>
                    <select v-model="grokForm.compatClaudeMcps"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.grok.compatCursorMcps') }}</label>
                    <select v-model="grokForm.compatCursorMcps"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                </div>
              </section>

              <div class="panel-head ph-7">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card" id="grok-panel-perm">
                <div class="field-row">
                  <div class="field fw-full"><label>{{ t('sync.permMode') }} <span class="tip"><i>?</i><em>{{ t('sync.grok.permModeTip') }}</em></span></label>
                    <select v-model="grokForm.permissionMode">
                      <option value="">{{ t('sync.grok.permAskOpt') }}</option>
                      <option value="ask">{{ t('sync.askPerCall') }}</option>
                      <option value="auto">{{ t('sync.grok.autoOpt') }}</option>
                      <option value="always-approve">{{ t('sync.alwaysApprove') }}</option>
                    </select>
                  </div>
                </div>
                <div class="field" style="margin:4px 0"><label>{{ t('sync.permRules') }}</label>
                  <div class="perm-grid">
                    <template v-for="r in GROK_PERM_RULES" :key="r.v">
                      <div class="perm-rule-card" :class="{ on: grokRuleMode(r.v) }">
                        <span class="perm-rule-text" :title="r.tip()">{{ r.v }}</span>
                        <div class="pcl-opts" role="radiogroup" :aria-label="r.v">
                          <button type="button" class="pcl-opt allow" role="radio" :class="{ on: grokRuleMode(r.v)==='allow' }" :aria-checked="grokRuleMode(r.v)==='allow'" @click="grokRulePick(r.v, 'allow')">{{ t('sync.allow') }}</button>
                          <button type="button" class="pcl-opt ask" role="radio" :class="{ on: grokRuleMode(r.v)==='ask' }" :aria-checked="grokRuleMode(r.v)==='ask'" @click="grokRulePick(r.v, 'ask')">{{ t('sync.ask') }}</button>
                          <button type="button" class="pcl-opt deny" role="radio" :class="{ on: grokRuleMode(r.v)==='deny' }" :aria-checked="grokRuleMode(r.v)==='deny'" @click="grokRulePick(r.v, 'deny')">{{ t('sync.deny') }}</button>
                        </div>
                      </div>
                    </template>
                    <template v-for="row in customGrokRows" :key="row">
                      <div class="perm-rule-card" :class="{ on: row.mode }">
                        <span class="perm-rule-text" :title="row.text">{{ row.text }}</span>
                        <div class="pcl-opts" role="radiogroup" :aria-label="row.text">
                          <button type="button" class="pcl-opt allow" role="radio" :class="{ on: row.mode === 'allow' }" :aria-checked="row.mode === 'allow'" @click="grokRulePick(row.text, 'allow')">{{ t('sync.allow') }}</button>
                          <button type="button" class="pcl-opt ask" role="radio" :class="{ on: row.mode === 'ask' }" :aria-checked="row.mode === 'ask'" @click="grokRulePick(row.text, 'ask')">{{ t('sync.ask') }}</button>
                          <button type="button" class="pcl-opt deny" role="radio" :class="{ on: row.mode === 'deny' }" :aria-checked="row.mode === 'deny'" @click="grokRulePick(row.text, 'deny')">{{ t('sync.deny') }}</button>
                        </div>
                        <button type="button" class="pcl-del" :title="t('sync.delete')" @click="grokPermRows.splice(grokPermRows.indexOf(row),1)">×</button>
                      </div>
                    </template>
                  </div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.grok.permSyntaxTip') }}</em></div>
              </section>
            </template>

            <!-- gemini -->
            <template v-else-if="cfgTarget?.id==='gemini'">
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="cfg-grid">
                  <div class="field fw-l"><label>{{ t('sync.gemini.modelName') }}</label>
                    <div class="pi-sel">
                      <input v-model="geminiForm.modelName" placeholder="gemini-2.5-flash" @focus="geminiModelOpen=true" @click.stop="geminiModelOpen=true" @input="geminiModelOpen=true" @blur="setTimeout(()=>{ geminiModelOpen=false },120)" />
                      <div v-show="geminiModelOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(geminiForm.modelName)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickGeminiModel(m)">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(geminiForm.modelName).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.gemini.apiKeyEnv') }} <span class="tip"><i>?</i><em>{{ t('sync.gemini.apiKeyTip') }}</em></span></label>
                    <div class="pi-sel">
                      <input v-model="geminiApiKey" :placeholder="t('sync.routeToken')" />
                    </div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.gemini.authType') }}</label>
                    <select v-model="geminiForm.selectedAuthType">
                      <option value="gemini-api-key">{{ t('sync.opt.geminiApiKey') }}</option>
                      <option value="vertex-ai">{{ t('sync.opt.vertexAi') }}</option>
                      <option value="oauth-personal">{{ t('sync.opt.oauthPersonal') }}</option>
                      <option value="cloud-shell">{{ t('sync.opt.cloudShell') }}</option>
                    </select>
                  </div>
                </div>

                <div class="tip blk"><i>?</i><em>{{ t('sync.gemini.thinkingTip') }}</em></div>
                <div class="cfg-grid">
                  <div class="field fw-m"><label>{{ t('sync.gemini.thinkingConfig') }}</label>
                    <select v-model="geminiForm.thinking">
                      <option value="">{{ t('sync.default') }}</option>
                      <option value="off">{{ t('sync.gemini.optOff') }}</option>
                      <option value="low">{{ t('sync.opt.low') }}</option>
                      <option value="medium">{{ t('sync.opt.medium') }}</option>
                      <option value="high">{{ t('sync.opt.high') }}</option>
                    </select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.gemini.effortOpenai') }}</label>
                    <select v-model="geminiForm.effortOpenai">
                      <option value="">{{ t('sync.default') }}</option>
                      <option value="none">{{ t('sync.opt.none') }}</option>
                      <option value="minimal">{{ t('sync.opt.minimal') }}</option>
                      <option value="low">{{ t('sync.opt.low') }}</option>
                      <option value="medium">{{ t('sync.opt.medium') }}</option>
                      <option value="high">{{ t('sync.opt.high') }}</option>
                      <option value="xhigh">{{ t('sync.opt.xhigh') }}</option>
                    </select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.gemini.budgetAnthropic') }}</label>
                    <select v-model="geminiForm.budgetAnthropic">
                      <option value="">{{ t('sync.default') }}</option>
                      <option value="2048">{{ t('sync.gemini.low') }}</option>
                      <option value="8192">{{ t('sync.gemini.mid') }}</option>
                      <option value="24576">{{ t('sync.gemini.high') }}</option>
                    </select>
                  </div>
                </div>

                <div class="tip blk"><i>?</i><em>{{ t('sync.gemini.samplingTip') }}</em></div>
                <div class="cfg-grid">
                  <div class="field fw-s"><label>{{ t('sync.temperature') }}</label><input type="number" step="0.1" min="0" max="2" v-model="geminiForm.temperature" placeholder="如 0.7" /></div>
                  <div class="field fw-s"><label>{{ t('sync.topP') }}</label><input type="number" step="0.05" min="0" max="1" v-model="geminiForm.topP" placeholder="如 1" /></div>
                  <div class="field fw-s"><label>{{ t('sync.topK') }}</label><input type="number" min="1" v-model="geminiForm.topK" placeholder="如 40" /></div>
                  <div class="field fw-s"><label>{{ t('sync.maxTokens') }}</label><input type="number" min="1" v-model="geminiForm.maxOutputTokens" placeholder="如 8192" /></div>
                </div>
              </section>

              <div class="panel-head ph-2">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <div class="perm-list">
                  <div class="perm-row-wrap" :class="{ on: geminiForm.usageStatisticsEnabled }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ t('sync.gemini.usageStats') }}</span>
                      <span class="tip"><i>?</i><em>{{ t('sync.gemini.usageStatsTip') }}</em></span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="t('sync.gemini.usageStats')">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{on: geminiForm.usageStatisticsEnabled==='true'}" :aria-checked="geminiForm.usageStatisticsEnabled==='true'" @click="geminiForm.usageStatisticsEnabled = geminiForm.usageStatisticsEnabled==='true' ? '' : 'true'">true</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{on: geminiForm.usageStatisticsEnabled==='false'}" :aria-checked="geminiForm.usageStatisticsEnabled==='false'" @click="geminiForm.usageStatisticsEnabled = geminiForm.usageStatisticsEnabled==='false' ? '' : 'false'">false</button>
                      </div>
                    </div>
                  </div>
                  <div class="perm-row-wrap" :class="{ on: geminiForm.autoAccept }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ t('sync.gemini.autoAccept') }}</span>
                      <span class="tip"><i>?</i><em>{{ t('sync.gemini.autoAcceptTip') }}</em></span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="t('sync.gemini.autoAccept')">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{on: geminiForm.autoAccept==='true'}" :aria-checked="geminiForm.autoAccept==='true'" @click="geminiForm.autoAccept = geminiForm.autoAccept==='true' ? '' : 'true'">true</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{on: geminiForm.autoAccept==='false'}" :aria-checked="geminiForm.autoAccept==='false'" @click="geminiForm.autoAccept = geminiForm.autoAccept==='false' ? '' : 'false'">false</button>
                      </div>
                    </div>
                  </div>
                </div>
              </section>

              <div class="panel-head ph-3">{{ t('sync.panelStyle') }}</div>
              <section class="card cfg-card">
                <div class="cfg-grid">
                  <div class="field fw-m"><label>{{ t('sync.theme') }}</label>
                    <select v-model="geminiForm.theme"><option value=""></option><option value="dark">{{ t('sync.opt.dark') }}</option><option value="light">{{ t('sync.opt.light') }}</option><option value="auto">{{ t('sync.opt.auto') }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.gemini.hideTitle') }}</label><select v-model="geminiForm.hideWindowTitle"><option :value="true">{{ t('sync.opt.true') }}</option><option :value="false">{{ t('sync.opt.false') }}</option></select></div>
                </div>

                <div class="cfg-grid">
                  <div class="field fw-s"><label>{{ t('sync.gemini.vimMode') }}</label><select v-model="geminiForm.vimMode"><option :value="true">{{ t('sync.opt.true') }}</option><option :value="false">{{ t('sync.opt.false') }}</option></select></div>
                  <div class="field fw-m"><label>{{ t('sync.gemini.disableUpdate') }}</label><select v-model="geminiForm.disableAutoUpdate"><option :value="true">{{ t('sync.opt.true') }}</option><option :value="false">{{ t('sync.opt.false') }}</option></select></div>
                  <div class="field fw-s"><label>{{ t('sync.gemini.debugMode') }}</label><select v-model="geminiForm.debugMode"><option :value="true">{{ t('sync.opt.true') }}</option><option :value="false">{{ t('sync.opt.false') }}</option></select></div>
                </div>
              </section>
            </template>

            <!-- agy (Antigravity CLI) -->
            <template v-else-if="cfgTarget?.id==='agy'">
              <section class="card cfg-card">
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.agy.envBaseLabel') }} <span class="tip"><i>?</i><em>{{ t('sync.agy.envBaseTip') }}</em></span></label>
                    <div class="ro-url">
                      <input :value="agyRouterUrl()" readonly />
                      <button type="button" class="btn btn-xs btn-ghost" @click="copyText(agyRouterUrl(), $event.currentTarget)">{{ t('sync.copy') }}</button>
                    </div>
                  </div>
                  <div class="field fw-l"><label>{{ t('sync.agy.envKeyLabel') }} <span class="tip"><i>?</i><em>{{ t('sync.agy.envKeyTip') }}</em></span></label>
                    <input v-model="agyEnvKey" :placeholder="t('sync.routeToken')" />
                  </div>
                </div>
                <div class="field-row" style="margin-top:6px;align-items:center">
                  <button v-if="agyEnvShowClear" type="button" class="cc-toggle danger" :disabled="agyEnvSaving" @click="agyClearEnv()">{{ t('sync.copilot.clear') }}</button>
                  <button v-else type="button" class="cc-toggle on" :disabled="agyEnvSaving" @click="agyWriteEnv()">{{ t('sync.agy.envWrite') }}</button>
                  <span class="tip" style="margin-left:4px"><i>?</i><em>{{ t('sync.agy.envActTip') }}</em></span>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.agy.modelAliasTip') }}</em></div>
              </section>

              <div class="panel-head ph-8">{{ t('sync.agy.mapPanel') }}</div>
              <section class="card cfg-card">
                <div class="agy-map-main">
                  <!-- 内置 ID 全量列出：每行 内置ID = 映射目标文本框，留空 = 不映射 -->
                  <div class="agy-map-list">
                    <div class="agy-map-head">
                      <span>aliases</span>
                      <span class="tip"><i>?</i><em>{{ t('sync.agy.mapTip') }}</em></span>
                    </div>
                    <div class="agy-map-rows">
                      <div v-for="m in agyBuiltinModels" :key="m" class="agy-map-row">
                        <span class="agy-map-builtin">{{ m }}</span>
                        <span class="agy-map-eq">=</span>
                        <div class="pi-sel agy-map-val">
                          <input :value="agyAliasMap[m] || ''" :placeholder="t('sync.agy.mapToPh')"
                            @input="agySetAlias(m, $event.target.value); agyToOpenFor = m; agyToFilter = $event.target.value"
                            @focus="agyToOpenFor = m; agyToFilter = ''"
                            @blur="setTimeout(() => { if (agyToOpenFor === m) agyToOpenFor = '' }, 140)"
                            @keydown="agyToKeyNav" />
                          <div v-show="agyToOpenFor === m" class="pi-sel-drop">
                            <div v-for="g in agyToGroups()" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                              <div class="pi-sel-letter">{{ g.letter }}</div>
                              <button v-for="opt in g.items" :key="opt" type="button" @mousedown.prevent="pickAgyTo(m, opt)">{{ opt }}</button>
                            </div>
                            <div v-if="!agyToGroups().length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                          </div>
                        </div>
                      </div>
                    </div>
                    <div class="agy-map-foot">
                      <button type="button" class="btn btn-green btn-sm" :disabled="agyAliasSaving" @click="saveAgyAliases()">{{ agyAliasSaving ? '…' : t('sync.agy.mapSave') }}</button>
                    </div>
                  </div>
                </div>
              </section>

              <div class="panel-head ph-3">{{ t('sync.agy.parametersPanel') }}</div>
              <section class="card cfg-card">
                <div class="cfg-grid">
                  <div class="field fw-s"><label>{{ t('sync.temperature') }}</label><input type="number" step="0.1" min="0" max="2" v-model="agyForm.temperature" placeholder="如 0.7" /></div>
                  <div class="field fw-s"><label>{{ t('sync.maxTokens') }}</label><input type="number" min="1" v-model="agyForm.maxOutputTokens" placeholder="如 8192" /></div>
                  <div class="field fw-s"><label>{{ t('sync.topP') }}</label><input type="number" step="0.05" min="0" max="1" v-model="agyForm.topP" placeholder="如 1" /></div>
                  <div class="field fw-s"><label>{{ t('sync.topK') }}</label><input type="number" min="1" v-model="agyForm.topK" placeholder="如 40" /></div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.agy.samplingTip') }}</em></div>
              </section>

              <div class="panel-head ph-4">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <div class="cfg-grid">
                  <div class="field fw-m"><label>{{ t('sync.agy.toolPermission') }} <span class="tip"><i>?</i><em>{{ t('sync.agy.toolPermissionTip') }}</em></span></label>
                    <select v-model="agyForm.toolPermission">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="strict">{{ t('sync.opt.strict') }}</option>
                      <option value="request-review">{{ t('sync.opt.requestReview') }}</option>
                      <option value="always-proceed">{{ t('sync.opt.alwaysProceed') }}</option>
                      <option value="proceed-in-sandbox">{{ t('sync.opt.proceedInSandbox') }}</option>
                    </select>
                  </div>
                </div>
                <div class="cfg-grid">
                  <div class="field fw-l"><label>{{ t('sync.allow') }}</label>
                    <textarea v-model="agyForm.allowText" rows="3" class="perm-ta" placeholder="Read(src/**)" @wheel="taWheelStop"></textarea>
                  </div>
                  <div class="field fw-l"><label>{{ t('sync.deny') }}</label>
                    <textarea v-model="agyForm.denyText" rows="3" class="perm-ta" placeholder="Bash(rm -rf *)" @wheel="taWheelStop"></textarea>
                  </div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.agy.permTip') }}</em></div>
              </section>

              <div class="panel-head ph-5">{{ t('sync.agy.safetyPanel') }}</div>
              <section class="card cfg-card">
                <div class="perm-list">
                  <div class="perm-row-wrap" :class="{ on: agyForm.enableTerminalSandbox }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ t('sync.agy.enableTerminalSandbox') }}</span>
                      <span class="tip"><i>?</i><em>{{ t('sync.agy.sandboxTip') }}</em></span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="t('sync.agy.enableTerminalSandbox')">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{on: agyForm.enableTerminalSandbox==='true'}" :aria-checked="agyForm.enableTerminalSandbox==='true'" @click="agyForm.enableTerminalSandbox = agyForm.enableTerminalSandbox==='true' ? '' : 'true'">true</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{on: agyForm.enableTerminalSandbox==='false'}" :aria-checked="agyForm.enableTerminalSandbox==='false'" @click="agyForm.enableTerminalSandbox = agyForm.enableTerminalSandbox==='false' ? '' : 'false'">false</button>
                      </div>
                    </div>
                  </div>
                  <div class="perm-row-wrap" :class="{ on: agyForm.allowNonWorkspaceAccess }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ t('sync.agy.allowNonWorkspaceAccess') }}</span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="t('sync.agy.allowNonWorkspaceAccess')">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{on: agyForm.allowNonWorkspaceAccess==='true'}" :aria-checked="agyForm.allowNonWorkspaceAccess==='true'" @click="agyForm.allowNonWorkspaceAccess = agyForm.allowNonWorkspaceAccess==='true' ? '' : 'true'">true</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{on: agyForm.allowNonWorkspaceAccess==='false'}" :aria-checked="agyForm.allowNonWorkspaceAccess==='false'" @click="agyForm.allowNonWorkspaceAccess = agyForm.allowNonWorkspaceAccess==='false' ? '' : 'false'">false</button>
                      </div>
                    </div>
                  </div>
                  <div class="perm-row-wrap" :class="{ on: agyForm.useG1Credits }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ t('sync.agy.useG1Credits') }}</span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="t('sync.agy.useG1Credits')">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{on: agyForm.useG1Credits==='true'}" :aria-checked="agyForm.useG1Credits==='true'" @click="agyForm.useG1Credits = agyForm.useG1Credits==='true' ? '' : 'true'">true</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{on: agyForm.useG1Credits==='false'}" :aria-checked="agyForm.useG1Credits==='false'" @click="agyForm.useG1Credits = agyForm.useG1Credits==='false' ? '' : 'false'">false</button>
                      </div>
                    </div>
                  </div>
                  <div class="perm-row-wrap" :class="{ on: agyForm.enableTelemetry }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ t('sync.agy.enableTelemetry') }}</span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="t('sync.agy.enableTelemetry')">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{on: agyForm.enableTelemetry==='true'}" :aria-checked="agyForm.enableTelemetry==='true'" @click="agyForm.enableTelemetry = agyForm.enableTelemetry==='true' ? '' : 'true'">true</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{on: agyForm.enableTelemetry==='false'}" :aria-checked="agyForm.enableTelemetry==='false'" @click="agyForm.enableTelemetry = agyForm.enableTelemetry==='false' ? '' : 'false'">false</button>
                      </div>
                    </div>
                  </div>
                </div>
              </section>

              <div class="panel-head ph-6">{{ t('sync.panelStyle') }}</div>
              <section class="card cfg-card">
                <div class="cfg-grid">
                  <div class="field fw-m"><label>{{ t('sync.theme') }}</label>
                    <select v-model="agyForm.colorScheme">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="terminal">{{ t('sync.opt.terminal') }}</option>
                      <option value="dark">{{ t('sync.opt.dark') }}</option>
                      <option value="light">{{ t('sync.opt.light') }}</option>
                      <option value="solarized dark">solarized dark</option>
                      <option value="solarized light">solarized light</option>
                      <option value="tokyo night">tokyo night</option>
                    </select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.agy.altScreenMode') }}</label>
                    <select v-model="agyForm.altScreenMode">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="default">{{ t('sync.opt.default') }}</option>
                      <option value="always">{{ t('sync.opt.always') }}</option>
                      <option value="never">{{ t('sync.opt.never') }}</option>
                    </select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.agy.runningLightSpeed') }}</label>
                    <select v-model="agyForm.runningLightSpeed">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option v-for="o in ['fast','medium','slow','off']" :key="o" :value="o">{{ optTxt(o) }}</option>
                    </select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.agy.verbosity') }}</label>
                    <select v-model="agyForm.verbosity">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="high">{{ t('sync.opt.high') }}</option>
                      <option value="low">{{ t('sync.opt.low') }}</option>
                    </select>
                  </div>
                  <div class="field fw-l"><label>{{ t('sync.agy.editor') }}</label>
                    <select v-model="agyForm.editor">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="auto">{{ t('sync.opt.auto') }}</option>
                      <option value="vim">vim</option>
                      <option value="emacs">emacs</option>
                    </select>
                  </div>
                </div>
              </section>

              <div class="panel-head ph-7">{{ t('sync.agy.subagentsPanel') }}</div>
              <section class="card cfg-card">
                <div class="cfg-grid">
                  <div class="field fw-m"><label>{{ t('sync.agy.subagentsEnabled') }}</label>
                    <select v-model="agyForm.subagentsEnabled">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">{{ t('sync.opt.true') }}</option>
                      <option value="false">{{ t('sync.opt.false') }}</option>
                    </select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.agy.subagentsMaxSteps') }}</label><input type="number" min="1" v-model="agyForm.subagentsMaxSteps" placeholder="如 10" /></div>
                  <div class="field fw-m"><label>{{ t('sync.agy.subagentsAuto') }}</label>
                    <select v-model="agyForm.subagentsAutoExecuteTools">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">{{ t('sync.opt.true') }}</option>
                      <option value="false">{{ t('sync.opt.false') }}</option>
                    </select>
                  </div>
                </div>
              </section>
            </template>

            <!-- opencode -->
            <template v-else-if="cfgTarget?.id==='opencode'">
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.baseUrl') }} <span class="tip"><i>?</i><em>provider.llm</em></span></label>
                    <div class="ro-url"><input :value="derivedBaseUrl(ocAccessMode)" readonly /><button type="button" class="btn btn-xs btn-ghost" @click="copyText(derivedBaseUrl(ocAccessMode), $event.currentTarget)">{{ t('sync.copy') }}</button></div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.apiKey') }}</label><input v-model="ocApiKey" :placeholder="t('sync.routeToken')" /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.npm') }}</label><input v-model="ocNpm" placeholder="@ai-sdk/openai-compatible" /></div>
                  <div class="field fw-m"><label>{{ t('sync.name') }}</label><input v-model="ocName" placeholder="LLM" /></div>
                </div>
                <div class="field"><label>{{ t('sync.oc.modelSet') }}</label>
                  <div v-for="(row, i) in ocModels" :key="row" class="model-row">
                    <div class="field fw-l"><label>{{ t('sync.id') }}</label>
                      <div class="pi-sel">
                        <input v-model="row.id" :placeholder="t('sync.pickModel')" @focus="ocModelOpen=i" @click.stop="ocModelOpen=i" @input="ocModelOpen=i" @blur="setTimeout(()=>{ if(ocModelOpen===i) ocModelOpen=-1 },120)" />
                        <div v-show="ocModelOpen===i" class="pi-sel-drop">
                          <div v-for="g in modelGroupsByLetter(row.id)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                            <div class="pi-sel-letter">{{ g.letter }}</div>
                            <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickOcModel(i, m)">{{ m }}</button>
                          </div>
                          <div v-if="!modelGroupsByLetter(row.id).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                        </div>
                      </div>
                    </div>
                    <div class="field fw-m"><label>{{ t('sync.oc.displayName') }}</label><input v-model="row.name" /></div>
                    <div class="field fw-s"><label>{{ t('sync.thinking') }}</label>
                      <select v-model="row.reasoning"><option value="">{{ t('sync.default') }}</option><option value="minimal">{{ t('sync.opt.minimal') }}</option><option value="low">{{ t('sync.opt.low') }}</option><option value="medium">{{ t('sync.opt.medium') }}</option><option value="high">{{ t('sync.opt.high') }}</option></select>
                    </div>
                    <div class="field fw-s"><label>{{ t('sync.temperature') }} <span class="tip"><i>?</i><em>{{ t('sync.samplingTemp') }}</em></span></label><input type="number" step="0.1" min="0" max="2" v-model="row.temperature" placeholder="如 0.7" /></div>
                    <button type="button" class="btn btn-xs btn-ghost row-del" @click="ocModels.splice(i,1)">×</button>
                  </div>
                  <button type="button" class="btn btn-xs btn-ghost" @click="ocModels.push({ id: '', name: '', reasoning: '', temperature: '' })">{{ t('sync.addModel') }}</button>
                </div>
              </section>
              <div class="panel-head ph-2">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <div class="perm-list">
                  <div v-for="p in OC_PERM_KEYS" :key="p.k" class="perm-row-wrap" :class="{ on: ocPerms[p.k].mode }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ p.k }}</span>
                      <span class="tip"><i>?</i><em>{{ t(p.hintKey) }}</em></span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="p.k">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{ on: ocPerms[p.k].mode==='allow' }" :aria-checked="ocPerms[p.k].mode==='allow'" @click="ocPickMode(p.k, 'allow')">{{ t('sync.oc.optAllow') }}</button>
                        <button type="button" class="pcl-opt ask" role="radio" :class="{ on: ocPerms[p.k].mode==='ask' }" :aria-checked="ocPerms[p.k].mode==='ask'" @click="ocPickMode(p.k, 'ask')">{{ t('sync.oc.optAsk') }}</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{ on: ocPerms[p.k].mode==='deny' }" :aria-checked="ocPerms[p.k].mode==='deny'" @click="ocPickMode(p.k, 'deny')">{{ t('sync.oc.optDeny') }}</button>
                      </div>
                    </div>
                  </div>
                </div>
              </section>
            </template>

            <!-- kilo -->
            <template v-else-if="cfgTarget?.id==='kilo'">
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.kilo.providerKey') }} <span class="tip"><i>?</i><em>{{ t('sync.kilo.providerKeyTip') }}</em></span></label><input v-model="kiloProviderKey" placeholder="llm" /></div>
                  <div class="field fw-m"><label>{{ t('sync.npm') }}</label><input v-model="kiloNpm" placeholder="@ai-sdk/openai-compatible" /></div>
                  <div class="field fw-m"><label>{{ t('sync.name') }}</label><input v-model="kiloName" placeholder="LLM Router" /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.baseUrl') }}</label>
                    <div class="ro-url"><input :value="derivedBaseUrl()" readonly /><button type="button" class="btn btn-xs btn-ghost" @click="copyText(derivedBaseUrl(), $event.currentTarget)">{{ t('sync.copy') }}</button></div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.apiKey') }} <span class="tip"><i>?</i><em>{{ t('sync.kilo.apiKeyTip') }}</em></span></label><input v-model="kiloApiKey" :placeholder="t('sync.routeToken')" /></div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.kilo.tip') }}</em></div>
              </section>

              <div class="panel-head ph-2">{{ t('sync.kilo.defaultModelPanel') }}</div>
              <section class="card cfg-card">
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.model') }} <span class="tip"><i>?</i><em>model</em></span></label>
                    <div class="pi-sel">
                      <input v-model="kiloModel" :placeholder="kiloQualifyModel('') || t('sync.pickModel')" @focus="kiloModelOpen=true" @click.stop="kiloModelOpen=true" @input="kiloModelOpen=true" @blur="setTimeout(()=>{ kiloModelOpen=false },120)" />
                      <div v-show="kiloModelOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(String(kiloModel || '').split('/').pop())" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="kiloModel = kiloQualifyModel(m); kiloModelOpen = false">{{ kiloQualifyModel(m) }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(String(kiloModel || '').split('/').pop()).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.kilo.smallModel') }} <span class="tip"><i>?</i><em>small_model</em></span></label>
                    <div class="pi-sel">
                      <input v-model="kiloSmallModel" :placeholder="kiloQualifyModel('') || t('sync.pickModel')" @focus="kiloSmallOpen=true" @click.stop="kiloSmallOpen=true" @input="kiloSmallOpen=true" @blur="setTimeout(()=>{ kiloSmallOpen=false },120)" />
                      <div v-show="kiloSmallOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(String(kiloSmallModel || '').split('/').pop())" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="kiloSmallModel = kiloQualifyModel(m); kiloSmallOpen = false">{{ kiloQualifyModel(m) }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(String(kiloSmallModel || '').split('/').pop()).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.kilo.subagentModel') }} <span class="tip"><i>?</i><em>subagent_model</em></span></label>
                    <div class="pi-sel">
                      <input v-model="kiloSubagentModel" :placeholder="kiloQualifyModel('') || t('sync.pickModel')" @focus="kiloSubOpen=true" @click.stop="kiloSubOpen=true" @input="kiloSubOpen=true" @blur="setTimeout(()=>{ kiloSubOpen=false },120)" />
                      <div v-show="kiloSubOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(String(kiloSubagentModel || '').split('/').pop())" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="kiloSubagentModel = kiloQualifyModel(m); kiloSubOpen = false">{{ kiloQualifyModel(m) }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(String(kiloSubagentModel || '').split('/').pop()).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.kilo.defaultPrefix') }} <span class="tip"><i>?</i><em>{{ t('sync.kilo.defaultPrefixTip') }}</em></span></label><input :value="kiloProviderPrefix()" readonly /></div>
                </div>
              </section>

              <div class="panel-head ph-3">{{ t('sync.oc.modelSet') }}</div>
              <section class="card cfg-card kmodel-wrap">
                <div v-for="(row, i) in kiloModels" :key="row" class="kilo-model-card">
                  <div class="kilo-model-head">
                    <span class="kilo-model-idx">#{{ i + 1 }}</span>
                    <span class="kilo-model-title">{{ row.id || t('sync.pickModel') }}</span>
                    <button type="button" class="btn btn-xs btn-ghost row-del" @click="kiloModels.splice(i,1)">×</button>
                  </div>
                  <div class="kilo-model-body">
                    <div class="field-row">
                      <div class="field fw-m"><label>{{ t('sync.oc.displayName') }}</label><input v-model="row.name" /></div>
                      <div class="field fw-s"><label>{{ t('sync.kilo.reasoning') }}</label>
                        <select v-model="row.reasoning"><option value="">{{ t('sync.default') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                      </div>
                      <div class="field fw-s"><label>{{ t('sync.kilo.effort') }} <span class="tip"><i>?</i><em>options.reasoningEffort</em></span></label>
                        <select v-model="row.effort">
                          <option value="">{{ t('sync.default') }}</option>
                          <option value="minimal">{{ t('sync.opt.minimal') }}</option>
                          <option value="low">{{ t('sync.opt.low') }}</option>
                          <option value="medium">{{ t('sync.opt.medium') }}</option>
                          <option value="high">{{ t('sync.opt.high') }}</option>
                          <option value="max">{{ t('sync.opt.max') }}</option>
                        </select>
                      </div>
                      <div class="field fw-s"><label>{{ t('sync.kilo.vision') }}</label>
                        <select v-model="row.vision"><option value="">{{ t('sync.default') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                      </div>
                      <div class="field fw-l"><label>{{ t('sync.id') }}</label>
                        <div class="pi-sel">
                          <input v-model="row.id" :placeholder="t('sync.pickModel')" @focus="kiloModelRowOpen=i" @click.stop="kiloModelRowOpen=i" @input="kiloModelRowOpen=i" @blur="setTimeout(()=>{ if(kiloModelRowOpen===i) kiloModelRowOpen=-1 },120)" />
                          <div v-show="kiloModelRowOpen===i" class="pi-sel-drop">
                            <div v-for="g in modelGroupsByLetter(row.id)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                              <div class="pi-sel-letter">{{ g.letter }}</div>
                              <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="row.id = m">{{ m }}</button>
                            </div>
                            <div v-if="!modelGroupsByLetter(row.id).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
                <button type="button" class="btn btn-xs btn-ghost" @click="kiloModels.push({ id: '', name: '', reasoning: '', effort: '', vision: '' })">{{ t('sync.addModel') }}</button>
              </section>

              <div class="panel-head ph-4">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <div class="perm-list">
                  <div v-for="p in KILO_PERM_KEYS" :key="p.k" class="perm-row-wrap" :class="{ on: kiloPerms[p.k].mode }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ p.k }}</span>
                      <span class="tip"><i>?</i><em>{{ t(p.hintKey) }}</em></span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="p.k">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{ on: kiloPerms[p.k].mode==='allow' }" :aria-checked="kiloPerms[p.k].mode==='allow'" @click="kiloPickMode(p.k, 'allow')">{{ t('sync.oc.optAllow') }}</button>
                        <button type="button" class="pcl-opt ask" role="radio" :class="{ on: kiloPerms[p.k].mode==='ask' }" :aria-checked="kiloPerms[p.k].mode==='ask'" @click="kiloPickMode(p.k, 'ask')">{{ t('sync.oc.optAsk') }}</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{ on: kiloPerms[p.k].mode==='deny' }" :aria-checked="kiloPerms[p.k].mode==='deny'" @click="kiloPickMode(p.k, 'deny')">{{ t('sync.oc.optDeny') }}</button>
                      </div>
                    </div>
                  </div>
                </div>
              </section>

              <div class="panel-head ph-5">{{ t('sync.kilo.mcpTitle') }}<span v-if="kiloMcpServerNames().length" class="cfg-head-count">{{ kiloMcpServerNames().length }}</span></div>
              <section class="card cfg-card">
                <div v-if="kiloMcpServerNames().length" class="cfg-mcp-list">
                  <span v-for="n in kiloMcpServerNames()" :key="n" class="cfg-mcp-chip">{{ n }}</span>
                </div>
                <div v-else class="cfg-mcp-empty">{{ t('sync.kilo.mcpEmpty') }}</div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.kilo.mcpTip') }}</em></div>
              </section>
            </template>

            <!-- codex -->
            <template v-else-if="cfgTarget?.id==='codex'">
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="field" style="margin-top:2px">
                  <label>{{ t('sync.baseUrl') }} <span class="tip"><i>?</i><em>{{ t('sync.baseUrlRouter') }}</em></span></label>
                  <div class="ro-url">
                    <input :value="derivedBaseUrl()" readonly />
                    <button type="button" class="btn btn-xs btn-ghost" @click="copyText(derivedBaseUrl(), $event.currentTarget)">{{ t('sync.copy') }}</button>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.defaultModel') }} <span class="tip"><i>?</i><em>model</em></span></label>
                    <div class="pi-sel">
                      <input v-model="codexForm.model" :placeholder="t('sync.pickModel')" @focus="codexModelOpen=true" @click.stop="codexModelOpen=true" @input="codexModelOpen=true" @blur="setTimeout(()=>{ codexModelOpen=false },120)" />
                      <div v-show="codexModelOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(codexForm.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="codexForm.model = m; codexModelOpen = false">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(codexForm.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.thinking') }} <span class="tip"><i>?</i><em>model_reasoning_effort</em></span></label>
                    <select v-model="codexForm.reasoning">
                      <option value="">{{ t('sync.default') }}</option>
                      <option value="minimal">{{ t('sync.opt.minimal') }}</option>
                      <option value="low">{{ t('sync.opt.low') }}</option>
                      <option value="medium">{{ t('sync.opt.medium') }}</option>
                      <option value="high">{{ t('sync.opt.high') }}</option>
                    </select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.temperature') }} <span class="tip"><i>?</i><em>model_temperature</em></span></label><input type="number" step="0.1" min="0" max="2" v-model="codexForm.temperature" placeholder="如 0.7" /></div>
                  <div class="field fw-s"><label>{{ t('sync.topP') }} <span class="tip"><i>?</i><em>model_top_p</em></span></label><input type="number" step="0.05" min="0" max="1" v-model="codexForm.topP" placeholder="如 1" /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.codex.providerLabel') }} <span class="tip"><i>?</i><em>{{ t('sync.codex.providerLabelTip') }}</em></span></label><input v-model="codexForm.providerKey" placeholder="llmrouter" /></div>
                  <div class="field fw-m"><label>{{ t('sync.codex.displayLabel') }} <span class="tip"><i>?</i><em>name</em></span></label><input v-model="codexForm.name" placeholder="LLM Router" /></div>
                  <div class="field fw-m"><label>{{ t('sync.codex.protocolLabel') }} <span class="tip"><i>?</i><em>{{ t('sync.codex.protocolTip') }}</em></span></label>
                    <select v-model="codexForm.wireApi">
                      <option value="responses">responses</option>
                    </select>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.apiKey') }} <span class="tip"><i>?</i><em>{{ t('sync.codex.apiKeyTip') }}</em></span></label><input v-model="codexForm.apiKey" :placeholder="t('sync.routeToken')" /></div>
                  <div class="field fw-m"><label>{{ t('sync.envKey') }} <span class="tip"><i>?</i><em>{{ t('sync.codex.envKeyTip') }}</em></span></label><input v-model="codexForm.envKey" placeholder="LLMROUTER_API_KEY" /></div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.codex.authTip') }}</em></div>
              </section>
              <section class="card cfg-card">
                <div class="field-row">
                  <div class="field fw-s"><label>{{ t('sync.codex.reqRetry') }} <span class="tip"><i>?</i><em>request_max_retries</em></span></label><input v-model="codexForm.reqRetry" inputmode="numeric" placeholder="如 4" /></div>
                  <div class="field fw-s"><label>{{ t('sync.codex.streamRetry') }} <span class="tip"><i>?</i><em>stream_max_retries</em></span></label><input v-model="codexForm.streamRetry" inputmode="numeric" placeholder="如 10" /></div>
                  <div class="field fw-s"><label>{{ t('sync.codex.idleTimeout') }} <span class="tip"><i>?</i><em>stream_idle_timeout_ms</em></span></label><input v-model="codexForm.idleTimeout" inputmode="numeric" placeholder="如 300000" /></div>
                </div>
              </section>
              <div class="panel-head ph-2">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <div class="cfg-grid">
                  <div class="field fw-m"><label>{{ t('sync.codex.approvalLabel') }} <span class="tip"><i>?</i><em>{{ t('sync.codex.approvalPolicyTip') }}</em></span></label>
                    <select v-model="codexForm.approvalPolicy">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="untrusted">{{ t('sync.codex.optUntrusted') }}</option>
                      <option value="on-request">{{ t('sync.codex.optOnRequest') }}</option>
                      <option value="never">{{ t('sync.codex.optNever') }}</option>
                    </select>
                  </div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.codex.rulesTip') }}</em></div>
                <div class="field" style="margin:4px 0"><label>{{ t('sync.permRules') }}</label>
                  <PermList v-model="codexPermRows" placeholder="Bash(npm run test)" />
                </div>
                <div class="field" style="margin-top:4px"><label>{{ t('sync.disableTool') }}</label>
                  <textarea v-model="codexForm.permDisable" rows="2" placeholder="Bash(shutdown)" @wheel="taWheelStop" class="perm-ta"></textarea>
                </div>
              </section>
            </template>

            <!-- cline：三个配置文件各为一个可展开面板（models.json / providers.json / globalState.json） -->
            <template v-else-if="cfgTarget?.id==='cline'">
              <div class="cline-panels">
                <div class="cline-panel" :class="{on: clinePanel==='models'}" :style="{order: clinePanelIdx('models')}">
                  <button type="button" class="cline-panel-head" @click="clineTogglePanel('models')">
                    <span class="cline-panel-fn" title="models.json">模型清单</span>
                    <b class="cline-panel-caret">▾</b>
                  </button>
                  <div v-show="clinePanel==='models'" class="cline-panel-body">
                    <section class="card cfg-card">
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.providerKey') }}</label><input v-model="clineModelsForm.key" placeholder="openai-compatible" /></div>
                  <div class="field fw-m"><label>{{ t('sync.displayName') }}</label><input v-model="clineModelsForm.providerName" placeholder="OpenAI Compatible" /></div>
                  <div class="field fw-l"><label>{{ t('sync.baseUrl') }}</label><input v-model="clineModelsForm.baseUrl" placeholder="http://127.0.0.1:3000/oai" /></div>
                  <div class="field fw-m"><label>{{ t('sync.defaultModel') }}</label>
                    <div class="pi-sel">
                      <input v-model="clineModelsForm.defaultModelId" :placeholder="t('sync.pickModel')" @focus="clineDefModelOpen=true" @click.stop="clineDefModelOpen=true" @input="clineDefModelOpen=true" @blur="setTimeout(()=>{ clineDefModelOpen=false },120)" />
                      <div v-show="clineDefModelOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(clineModelsForm.defaultModelId)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="clineModelsForm.defaultModelId=m; clineDefModelOpen=false">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(clineModelsForm.defaultModelId).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                </div>
                <div v-for="(row, i) in clineModelsForm.models" :key="row" class="pi-model-block">
                  <div class="pi-model-head">
                    <strong>#{{ i+1 }}</strong>
                    <button type="button" class="btn btn-xs btn-ghost row-del" @click="clineModelsForm.models.splice(i,1)">{{ t('sync.delete') }}</button>
                  </div>
                  <div class="field-row">
                    <div class="field fw-l"><label>{{ t('sync.id') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.idTip') }}</em></span></label>
                      <div class="pi-sel">
                        <input v-model="row.id" :placeholder="t('sync.pickModel')" @focus="clineModelSel=row" @click.stop="clineModelSel=row" @input="clineModelSel=row" @blur="setTimeout(()=>{ if(clineModelSel===row) clineModelSel=null },120)" />
                        <div v-show="clineModelSel===row" class="pi-sel-drop">
                          <div v-for="g in modelGroupsByLetter(row.id)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                            <div class="pi-sel-letter">{{ g.letter }}</div>
                            <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="row.id=m; clineModelSel=null">{{ m }}</button>
                          </div>
                          <div v-if="!modelGroupsByLetter(row.id).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                        </div>
                      </div>
                    </div>
                    <div class="field fw-m"><label>{{ t('sync.name') }}</label><input v-model="row.name" :placeholder="t('sync.namePh')" /></div>
                    <div class="field fw-s"><label>{{ t('sync.contextLen') }}</label><input type="number" v-model="row.contextWindow" placeholder="128000" /></div>
                    <div class="field fw-s"><label>{{ t('sync.maxInputTokens') }}</label><input type="number" v-model="row.maxInputTokens" placeholder="128000" /></div>
                  </div>
                  <div class="field-row">
                    <div class="field fw-s"><label>{{ t('sync.thinking') }} {{ t('sync.enabled') }}</label>
                      <select v-model="row.reasoningEnabled"><option :value="false">{{ t('sync.opt.false') }}</option><option :value="true">{{ t('sync.opt.true') }}</option></select>
                    </div>
                    <div class="field fw-m"><label>{{ t('sync.clineEffort') }}</label>
                      <select v-model="row.reasoningEffort"><option value="">{{ t('sync.default') }}</option><option v-for="o in CLINE_REASONING_EFFORT_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                    </div>
                  </div>
                </div>
                <button type="button" class="btn btn-xs btn-ghost" style="align-self:flex-start" @click="clineModelsForm.models.push(newClineModelRow())">{{ t('sync.addModel') }}</button>
                    </section>
                  </div>
                </div>
                <div class="cline-panel" :class="{on: clinePanel==='caps'}" :style="{order: clinePanelIdx('caps')}">
                  <button type="button" class="cline-panel-head" @click="clineTogglePanel('caps')">
                    <span class="cline-panel-fn" title="capabilities">模型能力</span>
                    <b class="cline-panel-caret">▾</b>
                  </button>
                  <div v-show="clinePanel==='caps'" class="cline-panel-body">
                    <section class="card cfg-card">
                      <div class="perm-grid">
                        <div v-for="k in CLINE_CAP_KEYS" :key="k" class="perm-rule-card" :class="{ on: clineCaps.includes(k) }" :title="k" @click="clineToggleCap(k)">
                          <span class="perm-rule-text">{{ t('sync.cline.cap.' + k) }}</span>
                          <span class="perm-cap-mark">{{ clineCaps.includes(k) ? '✓' : '＋' }}</span>
                        </div>
                      </div>
                      <div class="tip blk"><i>?</i><em>{{ t('sync.cline.capTip') }}</em></div>
                    </section>
                  </div>
                </div>
                <div class="cline-panel" :class="{on: clinePanel==='providers'}" :style="{order: clinePanelIdx('providers')}">
                  <button type="button" class="cline-panel-head" @click="clineTogglePanel('providers')">
                    <span class="cline-panel-fn" title="providers.json">接入配置</span>
                    <b class="cline-panel-caret">▾</b>
                  </button>
                  <div v-show="clinePanel==='providers'" class="cline-panel-body">
                    <section class="card cfg-card">
                <div class="field-row" style="margin-top:2px">
                  <div class="field fw-m"><label>{{ t('sync.cline.lastUsed') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.lastUsedTip') }}</em></span></label>
                    <select v-model="clineLastUsed"><option value=""></option><option v-for="k in clineProviderKeys" :key="k" :value="k">{{ k }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cline.connTokenSource') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.connTokenSourceTip') }}</em></span></label>
                    <select v-model="clineConnForm.tokenSource"><option value=""></option><option value="manual">{{ t('sync.opt.manual') }}</option><option value="migration">{{ t('sync.opt.migration') }}</option><option value="env">{{ t('sync.opt.env') }}</option></select>
                  </div>
                  <div class="field fw-l"><label>{{ t('sync.cline.connUpdatedAt') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.connUpdatedAtTip') }}</em></span></label><input :value="clineConnForm.updatedAt" readonly /></div>
                </div>
                <div class="field" style="margin-top:2px">
                  <label>{{ t('sync.baseUrl') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.baseUrlTip') }}</em></span></label>
                  <input :value="connBaseUrlText" readonly style="width:100%;padding:3px 6px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--t2);font-size:11px;box-sizing:border-box" />
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.apiKey') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.connKeyTip') }}</em></span></label><input v-model="clineConnForm.apiKey" :placeholder="t('sync.routeToken')" /></div>
                  <div class="field fw-l"><label>{{ t('sync.cline.defModel') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.connModelTip') }}</em></span></label>
                    <div class="pi-sel">
                      <input v-model="clineConnForm.model" :placeholder="t('sync.pickModel')" @focus="clineConnModelOpen=true" @click.stop="clineConnModelOpen=true" @input="clineConnModelOpen=true" @blur="setTimeout(()=>{ clineConnModelOpen=false },120)" />
                      <div v-show="clineConnModelOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(clineConnForm.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="clineConnForm.model=m; clineConnModelOpen=false">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(clineConnForm.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-s"><label>{{ t('sync.enabled') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.connReasoningTip') }}</em></span></label>
                    <select v-model="clineConnForm.reasoningEnabled"><option :value="false">{{ t('sync.opt.false') }}</option><option :value="true">{{ t('sync.opt.true') }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.effort') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.connReasoningTip') }}</em></span></label>
                    <select v-model="clineConnForm.reasoningEffort"><option value="">{{ t('sync.default') }}</option><option v-for="o in CLINE_REASONING_EFFORT_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.headers') }} <span class="tip"><i>?</i><em>请求头对象</em></span></label>
                    <textarea v-model="clineConnForm.headersText" rows="2" @wheel="taWheelStop" class="perm-ta" placeholder='{&#10;  "X-Custom": "value"&#10;}'></textarea>
                  </div>
                  <div class="field fw-l"><label>{{ t('sync.options') }} <span class="tip"><i>?</i><em>SDK 选项对象</em></span></label>
                    <textarea v-model="clineConnForm.optionsText" rows="2" @wheel="taWheelStop" class="perm-ta" placeholder='{&#10;  "temperature": 0.7&#10;}'></textarea>
                  </div>
                </div>
                <div class="tip blk"><i>?</i><em>Cline 可能自行重写该文件；非法 JSON 源码暂不覆盖旧值。</em></div>
                    </section>
                  </div>
                </div>
                <div class="cline-panel" :class="{on: clinePanel==='globalstate'}" :style="{order: clinePanelIdx('globalstate')}">
                  <button type="button" class="cline-panel-head" @click="clineTogglePanel('globalstate')">
                    <span class="cline-panel-fn" title="globalState.json">全局默认</span>
                    <b class="cline-panel-caret">▾</b>
                  </button>
                  <div v-show="clinePanel==='globalstate'" class="cline-panel-body">
                    <section class="card cfg-card">
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.cline.actModel') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.actModelTip') }}</em></span></label>
                    <div class="pi-sel">
                      <input v-model="clineGlobalForm.actModel" :placeholder="t('sync.pickModel')" @focus="clineGlobalActOpen=true" @click.stop="clineGlobalActOpen=true" @input="clineGlobalActOpen=true" @blur="setTimeout(()=>{ clineGlobalActOpen=false },120)" />
                      <div v-show="clineGlobalActOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(clineGlobalForm.actModel)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="clineGlobalForm.actModel=m; clineGlobalActOpen=false">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(clineGlobalForm.actModel).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field fw-l"><label>{{ t('sync.cline.planModel') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.planModelTip') }}</em></span></label>
                    <div class="pi-sel">
                      <input v-model="clineGlobalForm.planModel" :placeholder="t('sync.pickModel')" @focus="clineGlobalPlanOpen=true" @click.stop="clineGlobalPlanOpen=true" @input="clineGlobalPlanOpen=true" @blur="setTimeout(()=>{ clineGlobalPlanOpen=false },120)" />
                      <div v-show="clineGlobalPlanOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(clineGlobalForm.planModel)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="clineGlobalForm.planModel=m; clineGlobalPlanOpen=false">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(clineGlobalForm.planModel).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.cline.actEffort') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.actEffortTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.actEffort"><option value="">{{ t('sync.default') }}</option><option v-for="o in CLINE_REASONING_EFFORT_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cline.planEffort') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.planEffortTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.planEffort"><option value="">{{ t('sync.default') }}</option><option v-for="o in CLINE_REASONING_EFFORT_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                </div>
                <div class="cline-subhead">{{ t('sync.cline.gTitle') }}</div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.cline.gMode') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gModeTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.mode"><option value=""></option><option value="act">{{ t('sync.opt.act') }}</option><option value="plan">{{ t('sync.opt.plan') }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cline.gActProvider') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gActProviderTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.actProvider"><option value=""></option><option v-for="o in CLINE_GLOBAL_PROVIDER_OPTIONS" :key="o" :value="o">{{ o }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cline.gPlanProvider') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gPlanProviderTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.planProvider"><option value=""></option><option v-for="o in CLINE_GLOBAL_PROVIDER_OPTIONS" :key="o" :value="o">{{ o }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cline.gTerminal') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gTerminalTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.defaultTerminalProfile"><option value=""></option><option v-for="o in CLINE_GLOBAL_TERMINAL_OPTIONS" :key="o" :value="o">{{ o }}</option></select>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.cline.gMcpDisplay') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gMcpDisplayTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.mcpDisplayMode"><option value=""></option><option value="rich">{{ t('sync.opt.rich') }}</option><option value="compact">{{ t('sync.opt.compact') }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cline.gTelemetry') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gTelemetryTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.telemetrySetting"><option value=""></option><option value="enabled">{{ t('sync.opt.enabled') }}</option><option value="disabled">{{ t('sync.opt.disabled') }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cline.gTerminalExec') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gTerminalExecTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.vscodeTerminalExec"><option value=""></option><option v-for="o in CLINE_GLOBAL_TERMINAL_EXEC_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.cline.gMaxRequests') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gMaxRequestsTip') }}</em></span></label><input type="number" min="1" v-model="clineGlobalForm.maxRequests" placeholder="20" /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.cline.gLiteBase') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gLiteBaseTip') }}</em></span></label><input :value="clineGlobalForm.liteBaseUrl" readonly /></div>
                  <div class="field fw-l"><label>{{ t('sync.cline.gLanguage') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gLanguageTip') }}</em></span></label><input v-model="clineGlobalForm.preferredLanguage" placeholder="Simplified Chinese - 简体中文" /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-s"><label>{{ t('sync.cline.gAutoApprove') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gAutoApproveTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.autoApproveEnabled"><option value="">{{ t('sync.notWrite') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.cline.gSubagents') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gSubagentsTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.subagentsEnabled"><option value="">{{ t('sync.notWrite') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.cline.gBackgroundEdit') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gBackgroundEditTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.backgroundEditEnabled"><option value="">{{ t('sync.notWrite') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.cline.gFeatureTips') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gFeatureTipsTip') }}</em></span></label>
                    <select v-model="clineGlobalForm.showFeatureTips"><option value="">{{ t('sync.notWrite') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                </div>
                <div class="field">
                  <label>{{ t('sync.cline.gActions') }} <span class="tip"><i>?</i><em>{{ t('sync.cline.gActionsTip') }}</em></span></label>
                  <div class="perm-grid">
                    <div v-for="k in CLINE_AA_ACTIONS" :key="k" class="perm-rule-card" :class="{ on: clineGlobalActions[k] }">
                      <span class="perm-rule-text" :title="k">{{ t('sync.cline.aa.' + k) }}</span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="k">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{ on: clineGlobalActions[k]==='true' }" :aria-checked="clineGlobalActions[k]==='true'" @click="clineGlobalActions[k] = clineGlobalActions[k]==='true' ? '' : 'true'">{{ t('sync.allow') }}</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{ on: clineGlobalActions[k]==='false' }" :aria-checked="clineGlobalActions[k]==='false'" @click="clineGlobalActions[k] = clineGlobalActions[k]==='false' ? '' : 'false'">{{ t('sync.deny') }}</button>
                        <button type="button" class="pcl-opt ask" role="radio" :class="{ on: !clineGlobalActions[k] }" :aria-checked="!clineGlobalActions[k]" @click="clineGlobalActions[k] = ''">{{ t('sync.ask') }}</button>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.cline.globalTip') }}</em></div>
                    </section>
                  </div>
                </div>
              </div>
            </template>

            <!-- commandcode -->
            <template v-else-if="cfgTarget?.id==='commandcode'">
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="cc-grid4">
                  <div class="field"><label>{{ t('sync.baseUrl') }} <span class="tip"><i>?</i><em>{{ t('sync.baseUrlRouter') }}</em></span></label>
                    <input :value="derivedBaseUrl()" placeholder="http://127.0.0.1:3000/router" />
                  </div>
                  <div class="field"><label>{{ t('sync.cc.providerId') }} <span class="tip"><i>?</i><em>{{ t('sync.cc.providerIdTip') }}</em></span></label><input v-model="ccForm.endpointId" placeholder="llmrouter" /></div>
                  <div class="field"><label>{{ t('sync.protocol') }}</label>
                    <select v-model="ccForm.endpointApi">
                      <option value="openai-completions">{{ t('sync.cc.optOpenai') }}</option>
                      <option value="anthropic-messages">{{ t('sync.cc.optAnthropic') }}</option>
                    </select>
                  </div>
                  <div class="field"><label>{{ t('sync.apiKey') }} <span class="tip"><i>?</i><em>{{ t('sync.cc.apiKeyTip') }}</em></span></label><input v-model="ccForm.endpointKey" placeholder="$COMMAND_CODE_API_KEY / {env:VAR} / !command" /></div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.cc.apiKeyBlk') }}</em></div>
                <div class="cc-grid4">
                  <div class="field"><label>{{ t('sync.provider') }} <span class="tip"><i>?</i><em>{{ t('sync.cc.providerTip') }}</em></span></label>
                    <select v-model="ccForm.provider"><option v-for="p in ccProviderOptions" :key="p" :value="p">{{ p }}</option></select>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.defaultModel') }} <span class="tip"><i>?</i><em>{{ t('sync.cc.modelTip') }}</em></span></label>
                    <div class="pi-sel">
                      <input v-model="ccForm.model" :placeholder="t('sync.pickModel')" @focus="ccModelOpen=-2" @click.stop="ccModelOpen=-2" @input="ccModelOpen=-2" @blur="setTimeout(()=>{ if(ccModelOpen===-2) ccModelOpen=-1 },120)" />
                      <div v-show="ccModelOpen===-2" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(ccForm.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="ccForm.model=m; ccModelOpen=-1">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(ccForm.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.globalTier') }} <span class="tip"><i>?</i><em>{{ t('sync.cc.globalTierTip') }}</em></span></label>
                    <select v-model="ccForm.globalEffort"><option value="">{{ t('sync.none') }}</option><option v-for="o in CC_EFFORT_LEVELS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <div class="field"><label>{{ t('sync.noLogin') }} <span class="tip"><i>?</i><em>{{ t('sync.cc.nologinTip') }}</em></span></label>
                    <button type="button" class="cc-toggle" :class="{ on: ccNoLogin }" @click="ccNoLogin = !ccNoLogin; ccToggleNoLogin()">{{ ccNoLogin ? t('sync.enabled') : t('sync.disabled') }}</button>
                    <span v-if="ccNoLoginMsg" class="cc-nologin-msg">{{ ccNoLoginMsg }}</span>
                  </div>
                </div>
                <div v-for="(row, i) in ccForm.effortRows" :key="row" class="cc-grid4">
                  <div class="field cc-span2"><label>{{ t('sync.cc.modelIdLabel') }} <span class="tip"><i>?</i><em>{{ t('sync.cc.modelIdTip') }}</em></span></label>
                    <div class="pi-sel">
                      <input v-model="row.model" :placeholder="t('sync.pickModel')" @focus="ccEffortOpen=i" @click.stop="ccEffortOpen=i" @input="ccEffortOpen=i" @blur="setTimeout(()=>{ if(ccEffortOpen===i) ccEffortOpen=-1 },120)" />
                      <div v-show="ccEffortOpen===i" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(row.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="ccEffortPick(i, m)">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(row.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.tierLabel') }}</label>
                    <select v-model="row.level"><option value="">{{ t('sync.none') }}</option><option v-for="o in CC_EFFORT_LEVELS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <button type="button" class="cc-del-pill" @click="ccForm.effortRows.splice(i,1)">{{ t('common.delete') }}</button>
                </div>
                <button type="button" class="btn btn-xs btn-ghost" style="align-self:flex-start" @click="ccForm.effortRows.push({ model: '', level: '' })">{{ t('sync.addMapping') }}</button>
                <div class="tip blk"><i>?</i><em>{{ t('sync.cc.effortTip') }}</em></div>
              </section>

              <div class="panel-head ph-2">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <div class="cfg-grid">
                  <div class="field fw-m"><label>{{ t('sync.defaultMode') }}</label>
                    <select v-model="ccForm.permMode">
                      <option value="default">{{ t('sync.cc.optDefault') }}</option>
                      <option value="auto-accept">{{ t('sync.cc.optAutoAccept') }}</option>
                      <option value="plan">{{ t('sync.cc.optPlan') }}</option>
                      <option value="bypass">{{ t('sync.cc.optBypass') }}</option>
                    </select>
                  </div>
                </div>
                <div class="field" style="margin:4px 0"><label>{{ t('sync.permRules') }}</label>
                  <PermList v-model="ccPermRows" placeholder="Bash(npm test)" no-add />
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.cc.permTip') }}</em></div>
              </section>
            </template>

            <!-- hermes -->
            <template v-else-if="cfgTarget?.id==='hermes'">
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="field" style="margin-top:2px">
                  <label>{{ t('sync.baseUrl') }} <span class="tip"><i>?</i><em>{{ t('sync.baseUrlRouter') }}</em></span></label>
                  <div class="ro-url">
                    <input :value="derivedBaseUrl()" readonly />
                    <button type="button" class="btn btn-xs btn-ghost" @click="copyText(derivedBaseUrl(), $event.currentTarget)">{{ t('sync.copy') }}</button>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.provider') }}</label><input v-model="hermesForm.provider" placeholder="openai-compatible" /></div>
                  <div class="field fw-m"><label>{{ t('sync.apiKey') }}</label><input v-model="hermesForm.apiKey" :placeholder="t('sync.routeToken')" /></div>
                </div>
              </section>
              <section class="card cfg-card">
                <div class="field-row">
                  <div class="field fw-l"><label>{{ t('sync.model') }} <span class="tip"><i>?</i><em>{{ t('sync.hermes.modelTip') }}</em></span></label>
                    <div class="pi-sel">
                      <input v-model="hermesForm.model" :placeholder="t('sync.pickModel')" @focus="hermesModelOpen=true" @click.stop="hermesModelOpen=true" @input="hermesModelOpen=true" @blur="setTimeout(()=>{ hermesModelOpen=false },120)" />
                      <div v-show="hermesModelOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(hermesForm.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="hermesForm.model=m; hermesModelOpen=false">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(hermesForm.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.api') }} <span class="tip"><i>?</i><em>{{ t('sync.hermes.apiTip') }}</em></span></label>
                    <select v-model="hermesForm.api">
                      <option value="openai">openai</option>
                      <option value="anthropic">anthropic</option>
                      <option value="gemini">gemini</option>
                    </select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.temperature') }}</label><input type="number" step="0.1" min="0" max="2" v-model="hermesForm.temperature" placeholder="如 0.7" /></div>
                  <div class="field fw-s"><label>{{ t('sync.topP') }}</label><input type="number" step="0.05" min="0" max="1" v-model="hermesForm.topP" placeholder="如 1" /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-s"><label>{{ t('sync.maxTokens') }}</label><input type="number" min="1" v-model="hermesForm.maxOutputTokens" placeholder="如 8192" /></div>
                  <div class="field fw-s"><label>{{ t('sync.maxCtxTokens') }}</label><input type="number" min="1" v-model="hermesForm.maxCtxTokens" placeholder="如 128000" /></div>
                  <div class="field fw-s"><label>{{ t('sync.thinkingFlag') }}</label>
                    <select v-model="hermesForm.thinking"><option value="">{{ t('sync.notWrite') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.thinkingEffort') }} <span class="tip"><i>?</i><em>thinking=true 时生效</em></span></label>
                    <select v-model="hermesForm.thinkingEffort"><option value="">{{ t('sync.default') }}</option><option value="low">{{ t('sync.opt.low') }}</option><option value="medium">{{ t('sync.opt.medium') }}</option><option value="high">{{ t('sync.opt.high') }}</option></select>
                  </div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.hermes.paramsTip') }}</em></div>
              </section>
            </template>

            <!-- kimi -->
            <template v-else-if="cfgTarget?.id==='kimi'">
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="field" style="max-width:320px"><label>{{ t('sync.defaultModel') }} <span class="tip"><i>?</i><em>{{ t('sync.kimi.defaultTip') }}</em></span></label>
                  <select v-model="kimiForm.defaultModel">
                    <option value="">{{ t('sync.none') }}</option>
                    <option v-for="a in kimiDefaultCandidates()" :key="a" :value="a">{{ a }}</option>
                  </select>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.kimi.tip') }}</em></div>
              </section>

              <div class="panel-head ph-2">{{ t('sync.provider') }}</div>
              <section class="card cfg-card">
                <div v-for="(row, i) in kimiProviderRows" :key="i" class="pi-model-block">
                  <div class="pi-model-head">
                    <strong>#{{ i + 1 }}</strong>
                    <button type="button" class="btn btn-xs btn-ghost row-del" @click="kimiProviderRows.splice(i,1)">{{ t('sync.delete') }}</button>
                  </div>
                  <div class="field-row">
                    <div class="field fw-m"><label>{{ t('sync.name') }}</label><input v-model="row.key" placeholder="llm-router" /></div>
                    <div class="field fw-m"><label>{{ t('sync.providerType') }} <span class="tip"><i>?</i><em>{{ t('sync.kimi.providerTypeTip') }}</em></span></label>
                      <select v-model="row.type">
                        <option value="kimi">kimi</option>
                        <option value="openai">openai</option>
                        <option value="anthropic">anthropic</option>
                        <option value="openai_responses">openai_responses</option>
                        <option value="google-genai">google-genai</option>
                      </select>
                    </div>
                  </div>
                  <div class="field-row">
                    <div class="field fw-l"><label>{{ t('sync.baseUrl') }}</label><input v-model="row.baseUrl" :placeholder="derivedBaseUrl()" /></div>
                    <div class="field fw-l"><label>{{ t('sync.apiKey') }}</label><input v-model="row.apiKey" :placeholder="t('sync.routeToken')" /></div>
                  </div>
                </div>
                <button type="button" class="btn btn-xs btn-ghost" style="align-self:flex-start" @click="kimiAddProvider()">{{ t('sync.addProvider') }}</button>
                <div class="tip blk"><i>?</i><em>{{ t('sync.kimi.providerTip') }}</em></div>
              </section>

              <div class="panel-head ph-3">{{ t('sync.model') }}</div>
              <section class="card cfg-card">
                <div v-for="(row, i) in kimiModelRows" :key="i" class="pi-model-block">
                  <div class="pi-model-head">
                    <strong>#{{ i + 1 }}</strong>
                    <button type="button" class="btn btn-xs btn-ghost row-del" @click="kimiModelRows.splice(i,1)">{{ t('sync.delete') }}</button>
                  </div>
                  <div class="field-row">
                    <div class="field fw-m"><label>{{ t('sync.kimi.modelAlias') }} <span class="tip"><i>?</i><em>{{ t('sync.kimi.aliasTip') }}</em></span></label><input v-model="row.alias" placeholder="llm" /></div>
                    <div class="field fw-m"><label>{{ t('sync.provider') }}</label>
                      <select v-model="row.provider">
                        <option value=""></option>
                        <option v-for="p in kimiProviderRows" :key="p.key" :value="p.key">{{ p.key }}</option>
                      </select>
                    </div>
                    <div class="field fw-s"><label>{{ t('sync.kimi.contextWindow') }}</label><input type="number" min="1" v-model="row.context" placeholder="128000" /></div>
                  </div>
                  <div class="field-row">
                    <div class="field fw-l"><label>{{ t('sync.grok.upstreamModel') }} <span class="tip"><i>?</i><em>{{ t('sync.kimi.upstreamTip') }}</em></span></label>
                      <div class="pi-sel">
                        <input v-model="row.model" :placeholder="t('sync.pickModel')" @focus="kimiModelOpen=i" @click.stop="kimiModelOpen=i" @input="kimiModelOpen=i" @blur="setTimeout(()=>{ if(kimiModelOpen===i) kimiModelOpen=-1 },120)" />
                        <div v-show="kimiModelOpen===i" class="pi-sel-drop">
                          <div v-for="g in modelGroupsByLetter(row.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                            <div class="pi-sel-letter">{{ g.letter }}</div>
                            <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="row.model=m; kimiModelOpen=-1">{{ m }}</button>
                          </div>
                          <div v-if="!modelGroupsByLetter(row.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                        </div>
                      </div>
                    </div>
                    <div class="field fw-m"><label>{{ t('sync.kimi.modelEffort') }} <span class="tip"><i>?</i><em>{{ t('sync.kimi.modelEffortTip') }}</em></span></label>
                      <select v-model="row.effort">
                        <option value="">{{ t('sync.default') }}</option>
                        <option v-for="o in KIMI_EFFORT_LEVELS" :key="o" :value="o">{{ optTxt(o) }}</option>
                      </select>
                    </div>
                    <div class="field fw-m"><label>{{ t('sync.displayName') }} <span class="tip"><i>?</i><em>{{ t('sync.kimi.displayTip') }}</em></span></label><input v-model="row.display" :placeholder="t('sync.kimi.displayPh')" /></div>
                  </div>
                </div>
                <button type="button" class="btn btn-xs btn-ghost" style="align-self:flex-start" @click="kimiAddModel()">{{ t('sync.addModel') }}</button>
                <div class="tip blk"><i>?</i><em>{{ t('sync.kimi.modelTip') }}</em></div>
              </section>

              <div class="panel-head ph-4">{{ t('sync.kimi.thinkingPanel') }}</div>
              <section class="card cfg-card">
                <div class="field-row">
                  <div class="field fw-s"><label>{{ t('sync.kimi.thinkingEnabled') }} <span class="tip"><i>?</i><em>{{ t('sync.kimi.thinkingEnabledTip') }}</em></span></label>
                    <select v-model="kimiThinking.enabled">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">{{ t('sync.opt.true') }}</option>
                      <option value="false">{{ t('sync.opt.false') }}</option>
                    </select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.kimi.thinkingEffort') }} <span class="tip"><i>?</i><em>{{ t('sync.kimi.thinkingEffortTip') }}</em></span></label>
                    <select v-model="kimiThinking.effort">
                      <option value="">{{ t('sync.default') }}</option>
                      <option v-for="o in KIMI_EFFORT_LEVELS" :key="o" :value="o">{{ optTxt(o) }}</option>
                    </select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.kimi.thinkingKeep') }} <span class="tip"><i>?</i><em>{{ t('sync.kimi.thinkingKeepTip') }}</em></span></label>
                    <select v-model="kimiThinking.keep">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="all">{{ t('sync.opt.all') }}</option>
                      <option value="off">{{ t('sync.opt.off') }}</option>
                    </select>
                  </div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.kimi.thinkingTip') }}</em></div>
              </section>

              <div class="panel-head ph-5">{{ t('sync.kimi.permPanel') }}</div>
              <section class="card cfg-card">
                <div class="field" style="max-width:280px"><label>{{ t('sync.kimi.permMode') }} <span class="tip"><i>?</i><em>{{ t('sync.kimi.permModeTip') }}</em></span></label>
                  <select v-model="kimiForm.permMode">
                    <option value="">{{ t('sync.notWrite') }}</option>
                    <option value="manual">{{ t('sync.opt.manual') }}</option>
                    <option value="yolo">{{ t('sync.opt.yolo') }}</option>
                    <option value="auto">{{ t('sync.opt.auto') }}</option>
                  </select>
                </div>
                <div class="perm-list">
                  <div v-for="p in KIMI_PERM_RULES" :key="'p-' + p" class="perm-row-wrap" :class="{ on: kimiRuleDecision(p) }">
                    <div class="pcl-head">
                      <span class="pcl-key">{{ p }}</span>
                      <span class="tip"><i>?</i><em>{{ t('sync.kimi.permRuleTip') }}</em></span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="p">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{on: kimiRuleDecision(p)==='allow'}" :aria-checked="kimiRuleDecision(p)==='allow'" @click="kimiToggleRule(p,'allow')">{{ t('sync.allow') }}</button>
                        <button type="button" class="pcl-opt ask" role="radio" :class="{on: kimiRuleDecision(p)==='ask'}" :aria-checked="kimiRuleDecision(p)==='ask'" @click="kimiToggleRule(p,'ask')">{{ t('sync.ask') }}</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{on: kimiRuleDecision(p)==='deny'}" :aria-checked="kimiRuleDecision(p)==='deny'" @click="kimiToggleRule(p,'deny')">{{ t('sync.deny') }}</button>
                      </div>
                    </div>
                  </div>
                  <div v-for="row in kimiCustomRows()" :key="row" class="perm-row-wrap" :class="{ on: row.decision }">
                    <div class="pcl-head">
                      <input class="pcl-input" v-model="row.pattern" :placeholder="t('sync.kimi.permPatternPh')" @wheel.stop />
                      <span class="tip"><i>?</i><em>{{ t('sync.kimi.permRuleTip') }}</em></span>
                      <div class="pcl-opts" role="radiogroup" :aria-label="row.pattern">
                        <button type="button" class="pcl-opt allow" role="radio" :class="{on: row.decision==='allow'}" :aria-checked="row.decision==='allow'" @click="kimiPermDecision(row,'allow')">{{ t('sync.allow') }}</button>
                        <button type="button" class="pcl-opt ask" role="radio" :class="{on: row.decision==='ask'}" :aria-checked="row.decision==='ask'" @click="kimiPermDecision(row,'ask')">{{ t('sync.ask') }}</button>
                        <button type="button" class="pcl-opt deny" role="radio" :class="{on: row.decision==='deny'}" :aria-checked="row.decision==='deny'" @click="kimiPermDecision(row,'deny')">{{ t('sync.deny') }}</button>
                      </div>
                      <button type="button" class="pcl-del" :title="t('sync.delete')" @click="kimiPermRows.splice(kimiPermRows.indexOf(row), 1)">×</button>
                    </div>
                  </div>
                  <div class="pcl-add">
                    <input class="pcl-input" v-model="kimiCustomPattern" :placeholder="t('sync.kimi.permAddCustom')" @keyup.enter="kimiAddCustomRule" @wheel.stop />
                    <button type="button" class="btn btn-xs btn-ghost" @click="kimiAddCustomRule">{{ t('common.add') }}</button>
                  </div>
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.kimi.permTip') }}</em></div>
              </section>
            </template>
            <template v-else>
              <div class="panel-head ph-1">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="field">
                  <label>{{ t('sync.fieldProvider') }}</label>
                  <input :value="getPath(cfgObj,'provider')||getPath(cfgObj,'providerName')||''" @input="e=> setPath(cfgObj,'provider',e.target.value)" :placeholder="t('sync.providerPlaceholder')" />
                </div>
              </section>

              <section class="card cfg-card">
                <div class="rf-group">
                  <label v-for="o in ACCESS_OPTIONS" :key="o.value">
                    <input type="radio" :checked="(getPath(cfgObj,'accessMode')||cfgTarget?.apiFormat)==o.value" @change="setPath(cfgObj,'accessMode',o.value)" :name="'access-'+cfgTarget?.id" />{{ o.label() }}
                  </label>
                </div>
              </section>

              <div class="panel-head ph-2">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <div class="perm-mode">
                  <button v-for="o in PERM_OPTIONS" :key="o.value" type="button" class="perm-mode-card"
                    :class="{ on: (getPath(cfgObj,'permissions.defaultMode')||getPath(cfgObj,'permission')||'default')===o.value }"
                    @click="setPath(cfgObj,'permissions.defaultMode',o.value)">
                    <span class="pm-name">{{ o.name() }}</span>
                    <span v-if="i18nState.lang==='en'" class="pm-desc">{{ o.desc() }}</span>
                  </button>
                </div>
                <div class="field" style="margin:4px 0 2px"><label>{{ t('sync.permRules') }}</label>
                  <PermList v-model="genericPermRows" placeholder="Bash(npm test)" />
                </div>
                <div class="tip blk"><i>?</i><em>{{ t('sync.permRulesTip') }}</em></div>
              </section>
            </template>

          </div>

          <!-- 源码页签：统一根地址下各客户端的原始配置文件 -->
          <div v-else-if="cfgMode==='json'" class="cfg-src">
            <CodeArea v-model="cfgContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + targetRealFile()" @click="revealFile(targetRealFile())"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(targetRealFile()) }}</button></div>
          </div>

          <!-- Claude Code CLAUDE.md 源码：~/.claude/CLAUDE.md -->
          <div v-else-if="cfgMode==='claudemd'" class="cfg-src">
            <CodeArea v-model="claudeMdContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + claudeMdFile" @click="revealFile(claudeMdFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(claudeMdFile) }}</button></div>
          </div>

          <!-- pi 全局设置源码：~/.pi/agent/settings.json -->
          <div v-else-if="cfgMode==='globjson'" class="cfg-src">
            <CodeArea v-model="piSettingsContent" />
          </div>

          <!-- pi mcp.json 源码：~/.pi/agent/mcp.json -->
          <div v-else-if="cfgMode==='pimcp'" class="cfg-src">
            <CodeArea v-model="piMcpContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + piMcpFile" @click="revealFile(piMcpFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(piMcpFile) }}</button></div>
          </div>

          <!-- pi AGENTS.md 源码：~/.pi/agent/AGENTS.md -->
          <div v-else-if="cfgMode==='agentsmd'" class="cfg-src">
            <CodeArea v-model="piAgentsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + piAgentsFile" @click="revealFile(piAgentsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(piAgentsFile) }}</button></div>
          </div>

          <!-- grok AGENTS.md 源码：~/.grok/AGENTS.md -->
          <div v-else-if="cfgMode==='grokagents'" class="cfg-src">
            <CodeArea v-model="grokAgentsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + grokAgentsFile" @click="revealFile(grokAgentsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(grokAgentsFile) }}</button></div>
          </div>

          <!-- Codex AGENTS.md 源码：~/.codex/AGENTS.md -->
          <div v-else-if="cfgMode==='codexagents'" class="cfg-src">
            <CodeArea v-model="codexAgentsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + codexAgentsFile" @click="revealFile(codexAgentsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(codexAgentsFile) }}</button></div>
          </div>

          <!-- Kimi AGENTS.md 源码：~/.kimi-code/AGENTS.md -->
          <div v-else-if="cfgMode==='kimiagents'" class="cfg-src">
            <CodeArea v-model="kimiAgentsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + kimiAgentsFile" @click="revealFile(kimiAgentsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(kimiAgentsFile) }}</button></div>
          </div>

          <!-- Gemini .env 源码：~/.gemini/.env -->
          <div v-else-if="cfgMode==='geminienv'" class="cfg-src">
            <CodeArea v-model="geminiEnvContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + geminiEnvFile" @click="revealFile(geminiEnvFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(geminiEnvFile) }}</button></div>
          </div>

          <!-- Gemini GEMINI.md 源码：~/.gemini/GEMINI.md -->
          <div v-else-if="cfgMode==='geminimd'" class="cfg-src">
            <CodeArea v-model="geminiAgentsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + geminiAgentsFile" @click="revealFile(geminiAgentsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(geminiAgentsFile) }}</button></div>
          </div>

          <!-- OpenCode AGENTS.md 源码：~/.config/opencode/AGENTS.md -->
          <div v-else-if="cfgMode==='openagents'" class="cfg-src">
            <CodeArea v-model="ocAgentsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + ocAgentsFile" @click="revealFile(ocAgentsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(ocAgentsFile) }}</button></div>
          </div>

          <!-- Kilo AGENTS.md 源码：~/.config/kilo/AGENTS.md -->
          <div v-else-if="cfgMode==='kiloagents'" class="cfg-src">
            <CodeArea v-model="kiloAgentsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + kiloAgentsFile" @click="revealFile(kiloAgentsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(kiloAgentsFile) }}</button></div>
          </div>

          <!-- Cline models.json 源码：~/.cline/data/settings/models.json -->
          <div v-else-if="cfgMode==='clinemodels'" class="cfg-src">
            <CodeArea v-model="clineModelsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + clineModelsFile" @click="revealFile(clineModelsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(clineModelsFile) }}</button></div>
          </div>

          <!-- Cline providers.json 源码：~/.cline/data/settings/providers.json -->
          <div v-else-if="cfgMode==='clineproviders'" class="cfg-src">
            <CodeArea v-model="clineProvidersContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + clineProvidersFile" @click="revealFile(clineProvidersFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(clineProvidersFile) }}</button></div>
          </div>

          <!-- Cline AGENTS.md 源码：~/.agents/AGENTS.md -->
          <div v-else-if="cfgMode==='clineagents'" class="cfg-src">
            <CodeArea v-model="clineAgentsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + clineAgentsFile" @click="revealFile(clineAgentsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(clineAgentsFile) }}</button></div>
          </div>

          <!-- Cline globalState.json 源码：~/.cline/data/globalState.json（Cline 运行时状态，真实文件直读） -->
          <div v-else-if="cfgMode==='clineglobalstate'" class="cfg-src">
            <CodeArea v-model="clineGlobalStateContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + clineGlobalStateFile" @click="revealFile(clineGlobalStateFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(clineGlobalStateFile) }}</button></div>
          </div>

          <!-- Command Code settings.json 源码：~/.commandcode/settings.json -->
          <div v-else-if="cfgMode==='ccsettings'" class="cfg-src">
            <CodeArea v-model="ccSettingsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + ccSettingsFile" @click="revealFile(ccSettingsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(ccSettingsFile) }}</button></div>
          </div>

          <!-- Command Code providers.json 源码：~/.commandcode/providers.json -->
          <div v-else-if="cfgMode==='ccproviders'" class="cfg-src">
            <CodeArea v-model="ccProvidersContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + ccProvidersFile" @click="revealFile(ccProvidersFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(ccProvidersFile) }}</button></div>
          </div>

          <!-- Copilot CLI 环境变量 BYOK：写入/清除用户环境变量（接入本路由器） -->
          <div v-else-if="cfgMode==='copilotenv'" class="cfg-form" style="display:flex;flex-direction:column;gap:6px">
            <div class="tip blk"><i>?</i><em>{{ t('sync.copilot.tip') }}</em></div>
            <section class="card cfg-card">
              <div class="field-row">
                <div class="field fw-l"><label>COPILOT_PROVIDER_BASE_URL <span class="tip"><i>?</i><em>{{ t('sync.copilot.baseUrlTip') }}</em></span></label>
                  <div class="ro-url">
                    <input :value="copilotRouterUrl()" readonly />
                    <button type="button" class="btn btn-xs btn-ghost" @click="copyText(copilotRouterUrl(), $event.currentTarget)">{{ t('sync.copy') }}</button>
                  </div>
                </div>
                <div class="field fw-m"><label>COPILOT_PROVIDER_TYPE <span class="tip"><i>?</i><em>{{ t('sync.copilot.typeTip') }}</em></span></label>
                  <div class="ro-url"><input value="openai" readonly /></div>
                </div>
              </div>
              <div class="field-row">
                <div class="field fw-l"><label>model（settings.json） <span class="tip"><i>?</i><em>{{ t('sync.copilot.modelTip') }}</em></span></label>
                  <div class="pi-sel">
                    <input v-model="copilotModel" :placeholder="t('sync.copilot.modelPh')" @focus="copilotModelOpen=true" @click.stop="copilotModelOpen=true" @input="copilotModelOpen=true" @blur="setTimeout(()=>{ copilotModelOpen=false },120)" />
                    <div v-show="copilotModelOpen" class="pi-sel-drop">
                      <div v-for="g in modelGroupsByLetter(copilotModel)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                        <div class="pi-sel-letter">{{ g.letter }}</div>
                        <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="copilotModel=m; copilotModelOpen=false">{{ m }}</button>
                      </div>
                      <div v-if="!modelGroupsByLetter(copilotModel).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                    </div>
                  </div>
                </div>
                <div class="field fw-l"><label>COPILOT_PROVIDER_API_KEY <span class="tip"><i>?</i><em>{{ t('sync.copilot.apiKeyTip') }}</em></span></label>
                  <input v-model="copilotApiKey" :placeholder="t('sync.routeToken')" />
                </div>
              </div>
              <div class="field-row" style="margin-top:10px;align-items:center">
                <button type="button" class="btn btn-green btn-sm" :disabled="copilotSaving" @click="writeCopilotEnv()">{{ copilotSaving ? '…' : t('sync.copilot.write') }}</button>
                <button type="button" class="btn btn-sm btn-ghost" :disabled="copilotSaving" @click="copilotClearAsk()">{{ t('sync.copilot.clear') }}</button>
                <span class="tip" style="margin-left:4px"><i>?</i><em>{{ t('sync.copilot.actTip') }}</em></span>
              </div>
            </section>

            <!-- 高级：推理深度与权限（写 settings.json，空值 = 移除对应键） -->
            <section class="card cfg-card">
              <div class="panel-head ph-2" style="margin-bottom:6px">{{ t('sync.copilot.advanced') }}</div>
              <div class="field-row">
                <div class="field fw-m"><label>effortLevel <span class="tip"><i>?</i><em>{{ t('sync.copilot.effortTip') }}</em></span></label>
                  <select v-model="copilotEffort">
                    <option value="">{{ t('sync.notWrite') }}</option>
                    <option v-for="o in COPILOT_EFFORT_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option>
                  </select>
                </div>
                <div class="field fw-m"><label>permissions.disableBypassPermissionsMode <span class="tip"><i>?</i><em>{{ t('sync.copilot.bypassTip') }}</em></span></label>
                  <select v-model="copilotBypass">
                    <option value="">{{ t('sync.notWrite') }}</option>
                    <option v-for="o in COPILOT_BYPASS_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option>
                  </select>
                </div>
              </div>
              <div class="field-row">
                <div class="field fw-l"><label>allowedUrls <span class="tip"><i>?</i><em>{{ t('sync.copilot.urlsTip') }}</em></span></label>
                  <textarea v-model="copilotAllow" rows="3" placeholder="https://api.example.com&#10;*.github.com" @wheel="taWheelStop" class="perm-ta"></textarea>
                </div>
                <div class="field fw-l"><label>deniedUrls <span class="tip"><i>?</i><em>{{ t('sync.copilot.urlsTip') }}</em></span></label>
                  <textarea v-model="copilotDeny" rows="3" placeholder="https://evil.example.com" @wheel="taWheelStop" class="perm-ta"></textarea>
                </div>
              </div>
              <div class="panel-head ph-3" style="margin:0 0 4px">{{ t('sync.copilot.sandbox') }}</div>
              <div class="field-row">
                <div class="field fw-m"><label>sandbox.enabled <span class="tip"><i>?</i><em>{{ t('sync.copilot.sandboxTip') }}</em></span></label>
                  <select v-model="copilotSandboxEna">
                    <option value="">{{ t('sync.notWrite') }}</option>
                    <option value="true">{{ t('sync.opt.true') }}</option>
                    <option value="false">{{ t('sync.opt.false') }}</option>
                  </select>
                </div>
                <div class="field fw-m"><label>sandbox.allowBypass <span class="tip"><i>?</i><em>{{ t('sync.copilot.sandboxBypassTip') }}</em></span></label>
                  <select v-model="copilotSandboxBypass">
                    <option value="">{{ t('sync.notWrite') }}</option>
                    <option value="true">{{ t('sync.opt.true') }}</option>
                    <option value="false">{{ t('sync.opt.false') }}</option>
                  </select>
                </div>
                <div class="field fw-m"><label>sandbox.userPolicy.network.allowLocalNetwork <span class="tip"><i>?</i><em>{{ t('sync.copilot.allowLocalTip') }}</em></span></label>
                  <select v-model="copilotSandboxNet">
                    <option value="">{{ t('sync.notWrite') }}</option>
                    <option value="true">{{ t('sync.opt.true') }}</option>
                    <option value="false">{{ t('sync.opt.false') }}</option>
                  </select>
                </div>
              </div>
            </section>
          </div>

          <!-- Copilot CLI 工具审批权限：~/.copilot/permissions-config.json（点选列表 + 目录白名单） -->
          <div v-else-if="cfgMode==='copilotperm'" class="cfg-src">
            <div class="tip blk" style="margin:0 0 6px"><i>?</i><em>{{ t('sync.copilot.permTip') }}</em></div>
            <section class="card cfg-card">
              <div class="field" style="margin:4px 0"><label>{{ t('sync.permRules') }}</label>
                <PermList v-model="copilotTools" placeholder="Bash(npm test)" />
              </div>
              <div class="field" style="margin-top:4px"><label>{{ t('sync.additionalDirs') }}</label>
                <textarea v-model="copilotDirs" rows="2" class="perm-ta" placeholder="../shared-lib"></textarea>
              </div>
            </section>
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + copilotPermFile" @click="revealFile(copilotPermFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(copilotPermFile) }}</button></div>
          </div>

          <!-- Antigravity CLI 全局 MCP 源码：~/.gemini/config/mcp_config.json -->
          <div v-else-if="cfgMode==='agymcp'" class="cfg-src">
            <CodeArea v-model="agyMcpContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + agyMcpFile" @click="revealFile(agyMcpFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(agyMcpFile) }}</button></div>
          </div>
        </div>
      </div>
    </div>

    <!-- 清除接入配置确认（应用内弹窗，替代原生 confirm）：Antigravity CLI -->
    <!-- v-if 挂载：Clients.vue 的 scoped 样式对 .dialog-overlay 强制 display:flex!important，不能依赖 open 类切换 -->
    <div v-if="agyClearOpen" class="dialog-overlay open" @click.self="agyClearOpen = false">
      <div class="dialog" style="width:320px">
        <h3>{{ t('sync.copilot.clear') }}</h3>
        <p class="delete-desc">{{ t('sync.agy.envClearConfirm') }}</p>
        <div class="dialog-foot">
          <span></span>
          <div class="right">
            <button class="btn btn-ghost btn-sm" type="button" @click="agyClearOpen = false">{{ t('dlg.cancel') }}</button>
            <button class="btn btn-danger btn-sm" type="button" @click="agyClearEnvConfirm">{{ t('dlg.confirm') }}</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 清除接入配置确认（应用内弹窗，替代原生 confirm）：Copilot CLI -->
    <div v-if="copilotClearOpen" class="dialog-overlay open" @click.self="copilotClearOpen = false">
      <div class="dialog" style="width:320px">
        <h3>{{ t('sync.copilot.clear') }}</h3>
        <p class="delete-desc">{{ t('sync.copilot.confirmClear') }}</p>
        <div class="dialog-foot">
          <span></span>
          <div class="right">
            <button class="btn btn-ghost btn-sm" type="button" @click="copilotClearOpen = false">{{ t('dlg.cancel') }}</button>
            <button class="btn btn-danger btn-sm" type="button" @click="copilotClearConfirm">{{ t('dlg.confirm') }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ===== 客户端卡片网格 ===== */
.sync-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:10px}
.sync-card{
  position:relative;overflow:hidden;border:1px solid var(--border);border-radius:var(--radius);
  background:linear-gradient(160deg,var(--s2) 0%,var(--surface) 60%);
  cursor:pointer;
  transition:transform var(--transition),border-color var(--transition),box-shadow var(--transition)
}
.sync-card::before{content:"";position:absolute;top:0;left:0;right:0;height:2px;background:linear-gradient(90deg,var(--accent),var(--accent-2));opacity:0;transition:opacity var(--transition)
}
.sync-card:hover{transform:translateY(-2px);border-color:var(--accent-ring);box-shadow:0 10px 26px rgba(0,0,0,.35)
}
.sync-card:hover::before{opacity:.9}
.sync-card.ph{opacity:.5;cursor:not-allowed;background:var(--surface)}
.sync-card.ph:hover{transform:none;border-color:var(--border);box-shadow:none}
.sync-card.ph::before{display:none}
.sync-card-main{padding:12px}
.sync-card-head{display:flex;align-items:center;gap:10px}
.sync-client-avatar{
  width:34px;height:34px;border-radius:9px;display:flex;align-items:center;justify-content:center;
  font-weight:800;font-size:14px;flex:none;color:var(--t3);
  background:linear-gradient(135deg,#33363c,#1d2026);
  box-shadow:inset 0 1px 0 rgba(255,255,255,.18),0 2px 8px rgba(0,0,0,.25)
}
.avatar-claude-code,.avatar-claude-desktop{background:rgba(217,119,87,.14);box-shadow:inset 0 0 0 1px rgba(217,119,87,.22)}
.avatar-gemini{background:rgba(91,155,240,.12);box-shadow:inset 0 0 0 1px rgba(212,167,44,.25)}
.avatar-opencode{background:linear-gradient(135deg,#232a38,#10141d)}
.avatar-grok-build{background:linear-gradient(135deg,#1b2130,#0b0e15)}
.avatar-kilo{background:linear-gradient(135deg,#20301f,#0e1710)}
.avatar-agy{background:linear-gradient(135deg,#20273a,#10131f)}
.sync-client-logo{width:19px;height:19px;object-fit:contain;display:block;pointer-events:none}
.sync-title-text{min-width:0;display:flex;align-items:center;gap:6px;flex:1}
.sync-card-name{font-weight:700;font-size:13px;color:var(--text);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.sync-card-arrow{margin-left:auto;font-size:14px;font-weight:700;color:var(--accent);opacity:0;transform:translateX(-4px);transition:opacity var(--transition),transform var(--transition)
}
.sync-card:hover .sync-card-arrow{opacity:1;transform:none}
.sync-ph{
  position:absolute;top:8px;right:8px;margin:0;
  font-size:9px;font-weight:700;letter-spacing:.5px;padding:2px 8px;border-radius:999px;
  color:#d97706;background:rgba(217,119,6,.1);border:1px solid rgba(217,119,6,.35)
}
.sync-card-tags{margin-top:10px;display:flex;gap:6px;flex-wrap:wrap}
.sync-tag{font-size:10px;padding:2px 8px;border-radius:999px;background:var(--s3,var(--bg));color:var(--t2);border:1px solid var(--border)}
.sync-tag.t-skills{color:var(--amber);background:rgba(251,191,36,.09);border-color:rgba(251,191,36,.28)}
.sync-tag.t-mcp{color:var(--violet);background:rgba(212,167,44,.1);border-color:rgba(212,167,44,.3)}

/* ===== 弹窗 ===== */
.dialog-overlay{position:fixed;inset:0;background:rgba(0,0,0,.45);z-index:100;display:flex !important;align-items:center;justify-content:center}
.cfg-dialog{width:min(720px,90vw);height:min(560px,84vh);padding:0;display:flex;flex-direction:column;background:var(--surface);border-radius:var(--radius);box-shadow:0 18px 50px rgba(0,0,0,.35)}
.cfg-head{padding:6px 10px;border-bottom:1px solid var(--border);position:relative;z-index:30}
.cfg-head-top{display:flex;align-items:center;justify-content:space-between}
.cfg-title{font-weight:800;font-size:15px;color:var(--text)}
.cfg-head-actions{position:relative;display:flex;align-items:center;gap:8px}
/* 保存按钮固定宽度：保存↔… 文字切换时不引起头部抖动 */
.cfg-save{min-width:58px;justify-content:center}
.cfg-info-btn{
  width:24px;height:24px;padding:0;border-radius:50%;
  display:flex;align-items:center;justify-content:center;flex:none;
  background:transparent;border:1px solid var(--border);
  color:var(--t2);cursor:pointer;
  transition:color 0.15s, border-color 0.15s, background-color 0.15s;
}
.cfg-info-btn:hover,.cfg-info-btn.on{
  color:var(--accent);border-color:rgba(52,211,153,.5);background:rgba(52,211,153,.08);
}
.cfg-actions-divider{width:1px;height:16px;background:var(--border);margin:0 2px;flex:none}
.cfg-info-mask{position:fixed;inset:0;z-index:5}
.cfg-info-pop{
  position:absolute;top:30px;right:-4px;z-index:60;
  width:min(430px,78vw);
  background:var(--s3,var(--surface));
  border:1px solid var(--border-hover);border-radius:var(--radius-sm);
  box-shadow:0 12px 32px rgba(0,0,0,.45);
  padding:10px 12px;display:flex;flex-direction:column;gap:8px;
}
.ip-row{display:flex;align-items:center;gap:6px;font-size:11px;min-width:0}
.ip-k{color:var(--t3);font-size:10px;flex:none}
.ip-row code{
  flex:1;min-width:0;background:var(--bg);padding:2px 6px;border-radius:5px;
  font-size:10.5px;color:var(--t2);
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;
}
.ip-note{font-size:10px;color:var(--t3);line-height:1.5;border-top:1px dashed var(--border);padding-top:6px}
.cfg-file code,.cfg-ref code{background:var(--bg);padding:2px 6px;border-radius:5px;margin:0 4px}
.copy-btn{background:transparent;border:1px solid var(--border);border-radius:5px;font-size:10px;padding:1px 6px;cursor:pointer;color:var(--t2)}
.cfg-notexist{color:#d97706;font-size:10px;margin-left:4px}
.cfg-close{
  width:24px;height:24px;padding:0;border-radius:7px;flex:none;
  display:flex;align-items:center;justify-content:center;
  background:transparent;border:1px solid transparent;
  color:var(--t3);cursor:pointer;
  transition:color 0.15s, border-color 0.15s, background-color 0.15s, transform 0.1s;
}
.cfg-close:hover{color:#f87171;border-color:rgba(239,68,68,.4);background:rgba(239,68,68,.12)}
.cfg-close:active{transform:scale(0.92)}

/* ===== 页签 ===== */
.cfg-tabs{display:flex;align-items:center;gap:5px;margin-top:4px;flex-wrap:nowrap;overflow-x:auto;scrollbar-width:none}
.cfg-tab{border:1px solid var(--border);background:transparent;color:var(--t2);font-size:11px;padding:2px 9px;border-radius:7px;cursor:pointer;font-family:inherit}
.cfg-tab:hover{border-color:var(--border-hover);color:var(--text)}
.cfg-tab.on{border-color:transparent;color:#1c1206;background:linear-gradient(135deg,var(--accent),var(--accent-2))}

/* ===== 主体 ===== */
.cfg-body{padding:6px 8px;overflow:auto;flex:1;min-height:0;background:var(--bg);scrollbar-width:thin;scrollbar-color:var(--border-hover) transparent}
.cfg-body-src{display:flex;flex-direction:column;padding:6px 8px 0;min-height:0;background:var(--bg)}
/* 面板卡片：更强的独立边界（较亮描边 + 同色系底色 + 轻阴影），与对话框背景分层 */
.cfg-card{padding:6px 9px;border:1px solid var(--border-hover);border-radius:calc(var(--radius-sm) + 1px);background:var(--s2,var(--surface));box-shadow:0 1px 3px rgba(0,0,0,.10),0 3px 8px rgba(0,0,0,.12)}
.card-head{font-weight:800;font-size:11.5px;color:var(--text);margin-bottom:3px}
/* 面板分组标题：加粗分隔条（渐变绿底 + 左侧强调线），面板之间边界一目了然 */
.panel-head{font-size:11px;font-weight:800;letter-spacing:.6px;padding:5px 10px;background:linear-gradient(90deg,rgba(34,197,94,.26),rgba(34,197,94,.1));border:1px solid rgba(34,197,94,.55);border-left:3px solid rgba(74,222,128,.9);border-radius:var(--radius-sm);color:#86efac;margin:9px 0 3px}
/* ph-1 ~ ph-5 仅保留类名兼容模板引用，颜色统一为绿色 */
.panel-head.ph-1,.panel-head.ph-2,.panel-head.ph-3,.panel-head.ph-4,.panel-head.ph-5{background:linear-gradient(90deg,rgba(34,197,94,.26),rgba(34,197,94,.1));border-color:rgba(34,197,94,.55);border-left-color:rgba(74,222,128,.9);color:#86efac}
/* MCP 服务器（mcpServers）只读展示：已配置的服务器以胶囊列出，未配置显示占位 */
.cfg-mcp-list{display:flex;flex-wrap:wrap;gap:4px;margin-bottom:2px}
.cfg-mcp-chip{font-size:10px;font-family:var(--mono);padding:2px 8px;border-radius:999px;background:rgba(212,167,44,.1);border:1px solid rgba(212,167,44,.3);color:var(--t2)}
.cfg-mcp-empty{font-size:10px;color:var(--t3)}
.cfg-head-count{display:inline-flex;align-items:center;justify-content:center;min-width:16px;height:16px;padding:0 5px;margin-left:6px;border-radius:999px;background:rgba(134,239,172,.16);border:1px solid rgba(134,239,172,.4);color:#d3fce8;font-size:9.5px;font-weight:800;vertical-align:middle}

/* ===== 字段 ===== */
/* 配置项按类集中：同卡内字段间距收紧，标签与输入更贴近 */
.cfg-form .field{display:flex;flex-direction:column;gap:2px;min-width:0;margin-bottom:2px}
.cfg-form .field label{font-size:9px;color:var(--t3)}
.cfg-form .field input,.cfg-form .field select,.cfg-form .field textarea{width:100%;padding:3px 6px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:11px;font-family:inherit;box-sizing:border-box}
.field-row{display:flex;gap:5px;flex-wrap:wrap;margin-bottom:0}
.cfg-form .field-row{margin-bottom:1px}
.cfg-grid{display:flex;gap:4px;flex-wrap:wrap}
/* commandcode 配置行：统一 4 列等宽栅格，各行列边对齐 */
.cc-grid4{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:5px 10px;align-items:end}
.cc-grid4 .cc-span2{grid-column:span 2}
.rf-group{display:flex;gap:6px;flex-wrap:wrap;font-size:11px;color:var(--t2)}
.rf-group label{display:flex;align-items:center;gap:3px;cursor:pointer}
/* 权限模式点选卡片：单选双态，选中态高亮描边+浅色底 */
.perm-mode{display:grid;grid-template-columns:repeat(auto-fit,minmax(116px,1fr));gap:5px;margin-bottom:4px}
.perm-mode-card{display:flex;flex-direction:column;align-items:flex-start;gap:1px;padding:4px 8px;text-align:left;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);cursor:pointer;font-family:inherit;transition:var(--transition)}
.perm-mode-card:hover{border-color:var(--border-hover)}
.perm-mode-card:active{transform:scale(0.98)}
.perm-mode-card .pm-name{font-size:10.5px;font-weight:700;line-height:1.4;color:var(--t2)}
.perm-mode-card .pm-desc{font-size:9px;line-height:1.4;color:var(--t3)}
.perm-mode-card.on{background:var(--accent-soft);border-color:var(--accent)}
.perm-mode-card.on .pm-name{color:var(--accent)}
.perm-mode-card.on .pm-desc{color:var(--t2)}
.model-row{display:flex;gap:5px;align-items:flex-end;flex-wrap:wrap;margin-bottom:3px}
.row-del{color:var(--t3)}
/* Kilo 模型集合：每个模型一张独立卡片分割展示 */
.kmodel-wrap{display:flex;flex-direction:column;gap:6px}
.kilo-model-card{border:1px solid var(--border);border-radius:calc(var(--radius-sm) + 1px);background:var(--s3,var(--bg));padding:0;overflow:hidden}
.kilo-model-card:hover{border-color:var(--border-hover)}
.kilo-model-head{display:flex;align-items:center;gap:8px;padding:5px 9px;background:var(--s2,var(--surface));border-bottom:1px solid var(--border)}
.kilo-model-idx{font-size:10px;font-weight:700;color:var(--t3);letter-spacing:.5px}
.kilo-model-title{flex:1;min-width:0;font-weight:700;font-size:12px;color:var(--text);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.kilo-model-body{padding:6px 9px 8px}
/* 说明提示：统一收纳为问号图标，光标悬停才显示气泡文本 */
.tip{position:relative;display:inline-flex;align-items:center;justify-content:center;flex:none;width:13px;height:13px;margin:0 0 0 3px;vertical-align:middle;border:1px solid var(--border-hover);border-radius:50%;cursor:help;line-height:1;font-weight:400;text-transform:none;letter-spacing:normal;transition:var(--transition)}
.tip:hover{border-color:var(--accent);background:var(--accent-soft)}
.tip>i{font-style:normal;font-size:8.5px;font-weight:800;line-height:1;color:var(--t3)}
.tip:hover>i{color:var(--accent)}
.tip>em{display:none;position:absolute;left:0;top:calc(100% + 6px);z-index:300;font-style:normal;font-weight:400;letter-spacing:normal;text-transform:none;white-space:normal;text-align:left;width:max-content;max-width:min(320px,70vw);padding:6px 9px;font-size:9.5px;line-height:1.55;color:var(--text);background:var(--s3);border:1px solid var(--border-hover);border-radius:8px;box-shadow:var(--shadow)}
.tip:hover>em{display:block}
.tip.blk{margin:2px 0 0}
.tip>em code{font-family:var(--mono);font-size:9px;background:rgba(128,128,128,.15);padding:0 3px;border-radius:3px}
.ro-url{display:flex;gap:6px;align-items:center}
.ro-url input{flex:1;background:var(--bg);border:1px dashed var(--border);border-radius:var(--radius-sm);padding:3px 6px;color:var(--t2);font-size:11px}
/* 宽度分档：S 短字段（数字/开关）/ M 常规 / L 长字段（URL、模型 id、JSON）。
   行内按基准宽自动排满，放不下才换行，避免"过挤"与"独占一行留白"。 */
.cfg-form .fw-s{flex:1 1 96px}
.cfg-form .fw-m{flex:2 1 144px}
.cfg-form .fw-l{flex:3 1 208px}
.cfg-form .fw-full{flex:1 1 100%;min-width:100%}
/* model-row 内未标注宽度的字段（envKey / 模型选择 / 上下文长度）
   envKey 加宽下限，保证 ANTHROPIC_DEFAULT_SONNET_... 类长变量名完整可见 */
.cfg-form .model-row > .field{flex:1.3 1 172px}
.cfg-form .model-row > .field.model-sel{flex:2 1 190px}
.cfg-form .model-row > .field.ctx-len{flex:1 1 100px}
/* 环境变量名只读（预填钥匙不再可改），视觉弱化表明不可编辑 */
.cfg-form .env-ro{background:var(--s3,var(--bg));color:var(--t3);cursor:not-allowed}

/* ===== 权限规则编辑 ===== */
.perm-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(190px,1fr));gap:5px}
/* grok 权限：permission_mode 独占一行，allow/ask/deny 三列同一行等宽 */
.perm-grid.perm-3{grid-template-columns:repeat(3,minmax(0,1fr));align-items:start}
/* 权限规则点选列表样式见 public/style.css（全局，供 opencode 与各客户端共用） */
.perm-ta{font-family:var(--mono);resize:none;min-height:34px;max-height:110px;line-height:1.35}
/* Claude 权限规则独立卡片：规则文本 + 允许/拒绝双态点选（预设常驻，自定义可删除） */
.perm-rule-card{display:flex;align-items:center;gap:6px;padding:4px 6px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);transition:var(--transition)}
.perm-rule-card:hover{border-color:var(--border-hover)}
.perm-rule-card.on{border-color:var(--accent);background:var(--accent-soft)}
.perm-rule-text{flex:1;min-width:0;font-size:10px;font-family:var(--mono);color:var(--t2);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
/* 能力卡片右侧状态标记（✓=开 / ＋=关） */
.perm-cap-mark{flex:none;font-size:10px;font-weight:800;color:var(--t3)}
.perm-rule-card.on .perm-cap-mark{color:var(--green,#2dd4bf)}
/* commandcode 常用规则快选：紧凑胶囊双列浮层（向上展开防超窗），点击加入/移出上方文本框 */
.cc-quick{position:relative}
/* commandcode 胶囊开关（启动免登录）/ 胶囊删除按钮：与权限规则三态胶囊同款视觉 */
.cc-toggle{align-self:flex-start;padding:2px 14px;font-size:9.5px;line-height:1.6;border:1px solid var(--border);border-radius:999px;background:var(--surface);color:var(--t2);cursor:pointer;transition:var(--transition)}
.cc-toggle.on{background:var(--green-soft);border-color:var(--green);color:var(--green);box-shadow:0 0 8px rgba(52,211,153,.32);font-weight:750}
.cc-del-pill{align-self:end;justify-self:center;margin-bottom:3px;padding:2px 14px;font-size:9.5px;line-height:1.6;border:1px solid var(--border);border-radius:999px;background:var(--surface);color:var(--t3);cursor:pointer;transition:var(--transition)}
.cc-del-pill:hover{background:var(--red-soft);border-color:var(--red);color:var(--red)}
/* 胶囊按钮危险态（清除类动作）：默认灰字，悬停红色高亮 */
.cc-toggle.danger:hover:not(:disabled){background:var(--red-soft);border-color:var(--red);color:var(--red)}
.cc-nologin-msg{font-size:9.5px;color:var(--t3)}
/* ===== Cline 编辑页：三配置文件可展开面板（models.json / providers.json / globalState.json） ===== */
/* Cline 三面板纵向排列：每项一行占满整宽；点开的面板置顶（z-index），
   保证面板内的「接入方式点选」浮层与模型下拉不被相邻面板/边界遮挡 */
.cline-panels{display:flex;flex-direction:column;gap:6px}
.cline-panel{position:relative;border:1px solid var(--border);border-radius:var(--radius);background:var(--surface)}
.cline-panel.on{z-index:30}
.cline-panel-head{display:flex;align-items:center;gap:8px;width:100%;padding:8px 10px;background:var(--s2);border:0;border-radius:calc(var(--radius) - 1px) calc(var(--radius) - 1px) 0 0;color:var(--text);font-size:12px;font-weight:700;cursor:pointer;text-align:left;font-family:inherit;transition:var(--transition)}
.cline-panel-head:hover{background:var(--s3)}
.cline-panel.on .cline-panel-head{background:var(--s3);color:var(--accent)}
.cline-panel-fn{font-family:var(--mono);font-size:12px}
.cline-panel-caret{color:var(--t3);font-size:11px;transition:transform var(--transition)}
.cline-panel.on .cline-panel-caret{transform:rotate(180deg)}
.cline-panel-body{padding:8px;background:var(--surface)}
/* Cline 全局默认面板内的小分组标题 */
.cline-subhead{font-size:9.5px;font-weight:800;color:var(--t3);margin:4px 0 3px;padding-top:4px;border-top:1px dashed var(--border)}
/* ===== 能力多选 chips（cline models） ===== */
.cap-chips{display:flex;flex-wrap:wrap;gap:3px;padding:2px 5px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);min-height:20px;align-items:center}
.cap-chips label{display:inline-flex;align-items:center;gap:3px;font-size:9.5px;color:var(--t2);cursor:pointer;padding:0 5px;border-radius:5px;background:var(--s2)}
.cap-chips label:has(input:checked){background:var(--accent-soft);color:var(--accent)}
.cap-chips input{accent-color:var(--accent);margin:0}
/* ---- agy 模型映射面板：配对行 + aliases 行编辑器 ---- */
.agy-map-main{display:flex;flex-direction:column;gap:8px}
.agy-map-main .agy-map-list{border:1px solid var(--border);border-radius:var(--radius-sm);overflow:hidden}
.agy-map-main .agy-map-head{display:flex;align-items:center;gap:6px;padding:4px 8px;background:var(--s2);font-size:10px;font-weight:700;color:var(--t2);border-bottom:1px solid var(--border)}
.agy-map-main .agy-map-rows{padding:4px 6px}
.agy-map-main .agy-map-row{display:flex;align-items:center;gap:6px;margin-bottom:4px}
.agy-map-main .agy-map-row:last-child{margin-bottom:0}
/* 内置 ID 只读标签：等宽字体、固定宽度，右侧文本框为映射目标 */
.agy-map-main .agy-map-builtin{flex:0 0 150px;min-width:0;padding:2px 5px;background:var(--s2);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:10px;font-family:var(--mono);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.agy-map-main .agy-map-val{flex:1;min-width:0;padding:2px 5px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:10px;font-family:var(--mono)}
.agy-map-main .agy-map-val:focus{outline:none;border-color:var(--accent-ring)}
.agy-map-main .agy-map-eq{color:var(--t3);font-size:10px;font-family:var(--mono)}
.agy-map-main .agy-map-foot{display:flex;align-items:center;gap:6px;padding:4px 8px;background:var(--s2);border-top:1px solid var(--border)}
.agy-map-main .agy-map-foot .btn-green{margin-left:auto}

/* ===== pi/grok 模型下拉 ===== */
.pi-sel{position:relative}
.pi-sel > input{width:100%;padding:3px 6px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:11px;box-sizing:border-box}
.pi-sel > input:focus{outline:none;border-color:var(--accent-ring)}
.pi-sel-drop{position:absolute;z-index:50;top:calc(100% + 2px);left:0;right:0;max-height:150px;overflow:auto;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius-sm);box-shadow:0 8px 24px rgba(0,0,0,.25);padding:2px 0}
.pi-sel-letter{padding:1px 8px;font-size:9px;font-weight:800;color:var(--t3);text-transform:uppercase}
.pi-sel-group button{display:block;width:100%;text-align:left;padding:3px 10px;background:transparent;border:0;color:var(--text);font-size:11px;cursor:pointer}
.pi-sel-group button:hover{background:var(--accent-soft);color:var(--accent)}
.pi-sel-empty{color:var(--t3);font-size:10px;padding:6px;text-align:center}

/* ===== 细滚动条（下拉面板 / 配置主体），避免 Windows 默认粗滚动条 ===== */
.pi-sel-drop,.cfg-body{scrollbar-width:thin;scrollbar-color:var(--border-hover) transparent}
.pi-sel-drop::-webkit-scrollbar{width:6px;height:6px}
.pi-sel-drop::-webkit-scrollbar-track{background:transparent}
.pi-sel-drop::-webkit-scrollbar-thumb{background:var(--border-hover,#666);border-radius:3px}
.pi-sel-drop::-webkit-scrollbar-thumb:hover{background:var(--t3,#999)}
.cfg-body::-webkit-scrollbar{width:8px;height:8px}
.cfg-body::-webkit-scrollbar-track{background:transparent}
.cfg-body::-webkit-scrollbar-thumb{background:var(--border-hover,#666);border-radius:4px}
.cfg-body::-webkit-scrollbar-thumb:hover{background:var(--t3,#999)}
.pi-fetch-bar{display:flex;align-items:center;gap:6px;font-size:10px;color:var(--t3);margin:2px 0}
.pi-fetch-loading{color:var(--accent)}
.pi-model-block{border:1px solid var(--border);border-radius:var(--radius-sm);padding:5px;margin-bottom:4px}
.pi-model-head{display:flex;align-items:center;justify-content:space-between;margin-bottom:3px}

/* ===== 源码模式 ===== */
.cfg-src{display:flex;flex-direction:column;flex:1;min-height:0}
.cfg-ta{flex:1;min-height:0;width:100%;background:var(--bg);color:var(--text);border:1px solid var(--border);border-radius:var(--radius-sm);padding:10px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px;line-height:1.55;resize:none;box-sizing:border-box}
.cfg-src-bar{display:flex;align-items:center;justify-content:flex-end;padding-top:6px;gap:8px;margin-top:auto}
.cfg-file-btn{
  display:inline-flex;align-items:center;gap:5px;
  background:transparent;border:1px solid var(--border);border-radius:7px;
  padding:3px 9px;font-size:10.5px;line-height:1;color:var(--t2);cursor:pointer;font-family:inherit;
  transition:color 0.15s, border-color 0.15s, background-color 0.15s;
}
.cfg-file-btn:hover{color:var(--text);border-color:var(--border-hover);background:var(--s2)}
.cfg-file-btn:active{transform:scale(0.96)}
.cfg-file-btn svg{flex:none;opacity:.75}

/* ===== 深黑主题：配置窗口内文本统一纯白 ===== */
.cfg-dialog{color:#ffffff}
.cfg-dialog .cfg-title,
.cfg-dialog .cfg-form .field label,
.cfg-dialog .cfg-form .field .hint,
.cfg-dialog .panel-head,
.cfg-dialog .panel-head.ph-1,.cfg-dialog .panel-head.ph-2,.cfg-dialog .panel-head.ph-3,
.cfg-dialog .panel-head.ph-4,.cfg-dialog .panel-head.ph-5,.cfg-dialog .panel-head.ph-6,.cfg-dialog .panel-head.ph-7,
.cfg-dialog .panel-head.ph-8,.cfg-dialog .panel-head.ph-9,.cfg-dialog .panel-head.ph-10,
.cfg-dialog .pcl-key,
.cfg-dialog .perm-rule-text,
.cfg-dialog .cfg-mcp-chip,
.cfg-dialog .cfg-mcp-empty,
.cfg-dialog .cfg-head-count,
.cfg-dialog .pm-name,
.cfg-dialog .pm-desc,
.cfg-dialog .pi-sel-letter,
.cfg-dialog .pi-sel-empty,
.cfg-dialog .pi-fetch-bar,
.cfg-dialog .pi-fetch-loading,
.cfg-dialog .cline-subhead,
.cfg-dialog .cline-panel-caret,
.cfg-dialog .cc-nologin-msg,
.cfg-dialog .perm-cap-mark,
.cfg-dialog .row-del,
.cfg-dialog .cfg-file-btn,
.cfg-dialog .ip-k,
.cfg-dialog .ip-row code,
.cfg-dialog .copy-btn,
.cfg-dialog .cap-chips label,
.cfg-dialog .cfg-tabs .cfg-tab{color:#ffffff}
/* 保留状态/强调色，避免“统一白色”破坏功能性高亮 */
.cfg-dialog .cfg-tabs .cfg-tab.on{color:#1c1206}
</style>
