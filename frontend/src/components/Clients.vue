<script setup>
import { onMounted, onBeforeUnmount, computed, ref } from 'vue'
import { t, i18nState } from '../i18n'
import { copyText, delay, jsonErrorPos, repairJsonText, pruneIllegalTemplateKeys } from '../utils'
import { toast } from '../toast'
import api from '../api'
// 面板共用的枚举常量与展示工具集中在 sync/ui.js（各客户端面板组件同样从那里引入）
import {
  optTxt, coSbTxt, permCapCls, permCapWord, claudeFieldWidth, pmModeCls, toModeOpts,
  AGY_SAFE_KEYS, AGY_TOOL_PERM_MODES, AGY_ALL_CMD_MODES,
  CB_DEFAULT_MODES, CB_CMD_MODES, CC_PM_OPTIONS, CX_PERM_MODES, CX_PM_CLASS,
  CO_PERM_MODES, CO_BYPASS_MODES, CLAUDE_CMD_RULE, CLAUDE_CMD_MODES,
} from './sync/ui'
import { ChevronRightIcon, ChevronDownIcon, CheckIcon, XMarkIcon, CommandLineIcon } from '@heroicons/vue/24/outline'
// Copilot 沙箱三下拉的开关状态（0=全关，1/2/3=OS沙箱/沙箱提权/沙箱本地网络）
const coSbOpen = ref(0)
// 配置抽屉滑动动画进行中标记：滑动期间关闭 backdrop-filter（深色主题玻璃模糊）
// 并挂 will-change，避免全高面板逐帧重采样模糊导致掉帧卡顿
const cfgAnim = ref(false)
// 内部分级入场：抽屉滑到位后头部/页签/面板按序弹出（cfg-staged 由 after-enter 挂上，
// 动画播完后撤掉——避免之后切换页签时重播分级动画）；cfgDir 区分进入/离开，
// 内容块只在进入滑动期间先隐藏，离开滑动期间保持可见随面板整体滑出
const cfgStaged = ref(false)
const cfgDir = ref('')
let cfgStageTimer = null
// 收起方向：true = 保存后向左滑出；false = 点关闭（或其它途径）向下滑出
const cfgExitLeft = ref(false)
// 配置内容滚动中：滚动期间隐藏右缘的保存/关闭浮钮（停手 0.7s 后恢复），避免遮挡阅读
const cfgScrolling = ref(false)
let cfgScrollTimer = null
function onCfgScroll() {
  cfgScrolling.value = true
  if (cfgScrollTimer) clearTimeout(cfgScrollTimer)
  cfgScrollTimer = setTimeout(() => { cfgScrolling.value = false; cfgScrollTimer = null }, 700)
}
const onDrawerBeforeEnter = () => {
  cfgAnim.value = true; cfgStaged.value = false; cfgDir.value = 'enter'
  if (cfgStageTimer) { clearTimeout(cfgStageTimer); cfgStageTimer = null }
}
const onDrawerAfterEnter = () => {
  cfgAnim.value = false; cfgStaged.value = true
  cfgStageTimer = setTimeout(() => { cfgStaged.value = false; cfgStageTimer = null }, 340)
}
const onDrawerBeforeLeave = () => {
  cfgAnim.value = true; cfgDir.value = 'leave'; cfgStaged.value = false
  if (cfgStageTimer) { clearTimeout(cfgStageTimer); cfgStageTimer = null }
}
const onDrawerAfterLeave = () => { cfgAnim.value = false; cfgDir.value = '' }
import CodeArea from './CodeArea.vue'
import PermList from './sync/PermList.vue'
// 统一权限面板：权限模式胶囊 + 规则表（处置三态）+ 客户端特有附加项
import PermPanel from './sync/perm/PermPanel.vue'
// 客户端面板：一个客户端一个 SFC，模板自 Clients.vue 抽出（状态仍来自 sync/clients/*）
import OpencodePanel from './sync/panels/OpencodePanel.vue'
import DeepcodePanel from './sync/panels/DeepcodePanel.vue'

// ---- 共享工具与常量 ----
import {
  ACCESS_OPTIONS, PERM_OPTIONS, getPath, setPath, srcObj, taWheelStop,
  permModeNext, tq, targetLogo, permRowsFromLists, permRowsToLists,
} from './sync/shared'
// ---- 中央状态与通用辅助 ----
import {
  cfgEdit, cfgTarget, cfgLoading, cfgSaving, cfgMode, cfgFormat, cfgFile, cfgExists,
  cfgContent, cfgObj, targets, loadTargets, modelOptions, modelLoading,
  currentAccessFmt, routerHintForTarget, derivedBaseUrl, syncToJson, srcBarName, revealFile,
  targetRealPaths, ensureClientPaths, targetRealFile, syncToObj,
} from './sync/store'
// ---- 公共模型下拉 ----
import { modelSelOpen, cfgModelOpen, modelGroupsByLetter, pickModel, pickDefaultModel } from './sync/model-select'
// ---- 各客户端模块 ----
import {
  claudeAccessMode, claudeEnvModels, normalizeClaudeEnvModels, claudeDefaultModel, claudePermRules,
  claudePermRows, claudePermMode, claudeDisableBypass, CLAUDE_PERM_QUICK,
  initClaudeExtra, initClaude, claudeExtra,
  CLAUDE_EXTRA_GROUPS, newClaudeEnvModelRow, CLAUDE_ENV_KEY_SLOTS,
  claudeMdContent, claudeMdExists, claudeMdFile, loadClaudeMarkdown, saveClaudeMarkdown,
} from './sync/clients/claude'
import {
  PI_CONTEXT_OPTIONS, PI_SETTINGS_THINKING_OPTIONS, PI_THEME_OPTIONS, PI_STEERING_OPTIONS, PI_TRUST_OPTIONS,
  PI_PROVIDER_API_OPTIONS, piSettingsContent, piSettingsExists, piSettingsFile, piMcpContent, piMcpExists,
  piMcpFile, piAgentsContent, piAgentsExists, piAgentsFile, piM, initPiForm, piBuildModelsText,
  piBaseModels, piFetchLoading, fetchPiBaseModels, initPi, piSettingsBuildText, syncSettingsFromSource,
  piSettingsPreset, initPiSettingsForm, piSettings, newPiModelRow,
} from './sync/clients/pi'
import {
  grokForm, grokUiExtras,
  grokBlocks, grokOrder, grokAgentsContent, grokAgentsExists, grokAgentsFile, grokReadToml,
  selectGrokModel, addGrokModel, grokBuildToml, grokBaseModels, grokFetchLoading, fetchGrokBaseModels,
  grokModelOpen, grokModelSearch, grokModelGroups, pickGrokModel, grokModelKeyNav, grokDefaultCandidates, pickGrokDefault, pickGrokWebSearch, initGrok,
  GROK_PERM_MODES, grokPermRows, grokCmdMode, grokCmdSet,
} from './sync/clients/grok'
import {
  KIMI_EFFORT_LEVELS, KIMI_PERM_RULES, KIMI_PERM_MODES, kimiForm, kimiProviderRows, kimiModelRows, kimiThinking, kimiPermRows,
  kimiUnmanaged, kimiModelOpen, kimiCustomPattern, kimiAgentsContent, kimiAgentsExists, kimiAgentsFile,
  kimiTomlPreset, kimiReadToml, kimiBuildToml, kimiAddProvider, kimiAddModel, kimiDefaultCandidates,
  kimiRuleDecision, kimiToggleRule, kimiCustomRows, kimiAddCustomRule, initKimi,
} from './sync/clients/kimi'
import {
  copilotBase, copilotType, copilotModel, copilotApiKey, copilotEffort, copilotBypass, copilotAllow,
  copilotDeny, copilotAllowOpen, copilotDenyOpen, copilotAllowCount, copilotDenyCount, copilotSandboxEna, copilotSandboxBypass, copilotSandboxNet, copilotLoading, copilotSaving,
  copilotGeneral, COPILOT_THEMES, COPILOT_BANNERS, COPILOT_KEEPALIVE, COPILOT_CHANNELS,
  copilotModelOpen, COPILOT_EFFORT_OPTIONS, copilotPermContent, copilotPermExists,
  copilotPermLoc, copilotPermMode, copilotPermSetMode,
  copilotLocCands, copilotLocOpen, copilotLocFiltered,
  copilotPermFile, copilotRouterUrl, loadCopilotEnv, loadCopilotPerm, writeCopilotEnv, clearCopilotEnv, copilotCleared,
  copilotUrlList,
} from './sync/clients/copilot'
import {
  ocAgentsContent, ocAgentsExists, ocAgentsFile, initOcForm, ocApplyToObj, initOpencode,
} from './sync/clients/opencode'
import {
  codexAgentsContent, codexAgentsExists, codexAgentsFile, codexForm, codexQuickOpen, codexQuickFocus,
  codexToggleRule, codexRuleOn, CODEX_RESERVED_PROVIDERS, codexModelOpen, codexInitFrom, initCodex,
  codexCmdMode, codexCmdSet,
  codexMapRows, codexMapOpen, codexMapSearch, codexMapFocusRow, codexMapPick, codexMapGroups,
  codexMapSetVal, codexPermRows,
} from './sync/clients/codex'
import {
  CC_EFFORT_LEVELS, CC_PERM_MODES, CC_DEFAULT_KEY_REF, ccIsApiKeyRef, ccAddPermRule,
  ccRuleOn, CC_THEMES, CC_COMPACT_MODES, CC_TREE_FILTERS, CC_EXPORT_FORMATS, ccFeatModels,
  ccQuickOpen, ccQuickQFocus, ccForm, ccSettingsFile, ccSettingsExists, ccSettingsContent, ccProvidersFile,
  ccProvidersExists, ccProvidersContent, ccNoLogin, ccNoLoginMsg, ccToggleNoLogin, ccModelOpen, ccEffortOpen,
  ccEffortPick, initCcForm, ccApplyToObj, ccApplyToSettings, ccApplyToProviders, ccLoadNoLogin, initCommandCode,
  ccPermRows, ccCmdMode, ccCmdSet,
} from './sync/clients/cc'
import {
  mimoProviderKey, mimoApiKey, mimoApi, mimoName, mimoTimeout, mimoHeaderTimeout, mimoChunkTimeout,
  mimoSetCacheKey, mimoEffort, mimoModels, mimoModel, mimoSmallModel,
  mimoModelRowOpen, mimoModelOpen, mimoSmallOpen, mimoQualifyModel, mimoProviderPrefix, initMimo, mimoMcpServerNames,
  MIMO_PERM_MODES, MIMO_BASH_OPTS, mimoPerms, mimoPermMode, mimoPickMode,
} from './sync/clients/mimocode'
import {
  agyForm, initAgyForm, initAgy, agyMcpContent, agyMcpExists, agyMcpFile,
  agyEnvKey, agyEnvSaving, agyRouterUrl, writeAgyEnv, clearAgyEnv, agyEnvBase, agyEnvKeyLoaded,
  agyMpOpen,
  agyBuiltinModels, agyAliasMap, agySetAlias,
  agyRouterModels, agyToOpenFor, agyToFilter, agyToGroups, pickAgyTo, agyToKeyNav,
  loadAgyAliasPanel,
} from './sync/clients/agy'
import {
  cbForm, initCodebuddy, cbConnUrl, cbConnKey,
  cbCmdMode, cbCmdSet, cbPermRows,
  cbModelsObj, cbModelsContent, cbModelsExists, cbModelsFile,
  cbMcpContent, cbMcpExists, cbMcpFile, cbAddModel, cbDelModel,
} from './sync/clients/codebuddy'
import {
  deepcodeForm, DEEPCODE_EFFORTS, deepcodeModelOpen, pickDeepcodeModel,
  initDeepcodeForm, deepcodeApplyToObj, initDeepcode, setDeepcodeMuted,
  loadDeepcodeAliasPanel,
} from './sync/clients/deepcode'
// ---- 中枢动作（页签/构建/保存/打开/通用联动/统一模型下拉） ----
import {
  currentCfgFile, currentCfgExists, switchMode, tabJsonLabel, claudeModelOpen,
  pickClaudeModel, buildFormText, saveTargetConfig, openTargetConfig,
  initSyncController,
} from './sync/controller'

// 环境变量名输入框统一宽度：以最长槽位名（ANTHROPIC_DEFAULT_SONNET_MODEL）为标准，
// 各行等宽对齐；(长度+1)ch 预留 1 字符余量，14px = 左右内边距 + 边框（.env-ro 等宽字体下 ch=字符宽）
const claudeEnvKeyWidth = 'calc(' + (Math.max(...CLAUDE_ENV_KEY_SLOTS.map(s => s.length)) + 1) + 'ch + 14px)'

// ---- 客户端卡片下半区：安装 / 更新 / 版本号 ----
// 版本来源分两级：后端列表接口已给 npm 全局包版本（读文件，毫秒级）；读不到
// （非 npm 安装 / 没装 npm）时点「版本号」按需现场执行 `<cli> --version` 补。
// cliBusy：当前正在执行的动作（'' | 'install' | 'update' | 'version'），用于禁用与「进行中」文案。
const cliBusy = ref({})
// cliProbed：现场探测得到的版本（覆盖列表接口的空版本）
const cliProbed = ref({})
// cliLatest：registry 最新版本探测结果（{id: {installed, latest, outdated, error}}）
const cliLatest = ref({})
const cliChecking = ref(false)
// 是否已跑过至少一次更新检查：用于首屏稳定——结果回来前不分组，避免先按名称排一遍、
// 检查回来后又整片重排
const cliChecked = ref(false)
// 后台检查一次「哪些客户端有新版本」（并行 HTTP，不打断页面）；离线时静默失败
async function cliCheckUpdates() {
  if (cliChecking.value) return
  cliChecking.value = true
  try {
    const r = await api.CheckClientUpdates()
    if (r && r.ok && r.updates) cliLatest.value = { ...r.updates }
  } catch (e) {} finally { cliChecking.value = false; cliChecked.value = true }
}
// 卡片网格自动归类：按「需要操作（未安装 / 有更新）→ 已是最新 → 未接入」分组。
// 同一组内每张卡片要么都带操作条、要么都不带，因此组内等高，不再高矮混排。
// 更新检查出结果前（cliChecked=false）不分组、不出标题，保持后端原顺序——否则首屏会先按
// 「全都已是最新」铺一遍，检查结果回来后再整片重排。只有一个分组时也不出标题。
const targetGroups = computed(() => {
  const all = targets.value || []
  if (!cliChecked.value) return all.length ? [{ key: 'all', title: '', items: all }] : []
  const todo = [], ok = [], ph = []
  for (const tg of all) {
    if (tg.placeholder) ph.push(tg)
    else if (!tg.installed || cliIsOutdated(tg)) todo.push(tg)
    else ok.push(tg)
  }
  const many = [todo, ok, ph].filter(g => g.length).length > 1
  const groups = []
  if (todo.length) groups.push({ key: 'todo', title: many ? t('sync.groupTodo') : '', items: todo })
  if (ok.length) groups.push({ key: 'ok', title: many ? t('sync.groupOk') : '', items: ok })
  // 未接入组不出标题：卡片自身已显示「未接入」，再加一行标题是重复
  if (ph.length) groups.push({ key: 'ph', title: '', items: ph })
  return groups
})
// 「需要处理」分组（未安装/有更新）默认收起，点分组标题行展开/收起；
// 组内有安装/更新进行中时自动展开（便于看到进度），结束后回到手动状态
const todoCollapsed = ref(true)
const todoCollapsedEff = computed(() => {
  if (!todoCollapsed.value) return false
  const todo = targetGroups.value.find(g => g.key === 'todo')
  if (!todo) return true
  for (const x of todo.items) {
    if (cliProgress.value[x.id] != null || cliBusy.value[x.id]) return false
  }
  return true
})
// 安装出的 npm 包版本 / 探测版本，统一给展示层
function cliVersion(tg) {
  return String(cliProbed.value[tg.id] || tg.version || '')
}
// 是否支持一键安装/更新（后端登记了安装方式：npm 包名或官方命令）
function cliCan(tg) {
  return !tg.placeholder && !!tg.canInstall
}
// 已安装 → 「安装」不可点（要更新走「更新」）
function cliCanInstall(tg) {
  return cliCan(tg) && !tg.installed
}
// 「更新」可点条件：已安装，且（还没查到 registry 结果，或确实落后）
function cliCanUpdate(tg) {
  if (!cliCan(tg) || !tg.installed) return false
  const u = cliLatest.value[tg.id]
  if (!u || u.error || !u.latest) return true // 未检查/离线：保持可用
  return !!u.outdated
}
// 是否落后于 registry 最新版（用于给「更新」上主色高亮）
function cliIsOutdated(tg) {
  const u = cliLatest.value[tg.id]
  return !!(u && u.outdated)
}
// 安装/更新/版本条是否展示：仅「未安装」或「有新版本」时才出现——已装且最新时无需
// 任何动作，这条不占卡片空间。安装/更新/版本探测进行中也显示，以便看到进行中状态
//（卸载进行中不算：那张卡片此刻与这条无关）。
function cliShowActions(tg) {
  const b = cliBusy.value[tg.id]
  if (b === 'install' || b === 'update' || b === 'version') return true
  return !tg.installed || cliIsOutdated(tg)
}
// ---- 水位进度：后端 InstallClientCLI 是阻塞调用、npm 非 TTY 也没有真实百分比，
// 故按「该客户端上一次的真实耗时」做指数缓升估算（越接近预估完成拖尾越慢），
// 结束立即满格、停留 0.8s 后收回。若未来后端改为流式输出，把估算换成真实事件即可。
const cliProgress = ref({})                          // id -> 0..100（卡片背景水位）
const cliProg = { start: {}, dur: {}, doneAt: {} }   // 非响应式辅助
let cliProgTimer = null
function cliProgStart(id) {
  cliProg.start[id] = Date.now()
  if (!cliProg.dur[id]) cliProg.dur[id] = 20000      // 无历史耗时时的默认预估
  cliProgress.value = { ...cliProgress.value, [id]: 6 }
  if (!cliProgTimer) cliProgTimer = setInterval(cliProgTick, 250)
}
// 结束时记录真实耗时，作为该客户端下一次的预估时长（自适应）
function cliProgEnd(id) {
  const ms = Date.now() - (cliProg.start[id] || Date.now())
  if (ms > 1500) cliProg.dur[id] = ms
}
function cliProgTick() {
  const now = Date.now()
  const next = { ...cliProgress.value }
  let alive = false
  for (const id of Object.keys(cliProg.start)) {
    if (cliBusy.value[id]) {
      const p = 6 + 84 * (1 - Math.exp(-(now - cliProg.start[id]) / (cliProg.dur[id] * 0.55)))
      next[id] = Math.max(next[id] || 0, Math.min(90, Math.round(p)))
      alive = true
    } else {
      next[id] = 100
      if (!cliProg.doneAt[id]) cliProg.doneAt[id] = now
      if (now - cliProg.doneAt[id] > 800) { // 满格停留 0.8s 再收
        delete next[id]; delete cliProg.start[id]; delete cliProg.doneAt[id]
      } else alive = true
    }
  }
  cliProgress.value = next
  if (!alive && cliProgTimer) { clearInterval(cliProgTimer); cliProgTimer = null }
}
// 版本号单元格文案：有版本=「vX.Y.Z」；没装=未安装；装了但版本未知=读取中/已安装
function cliVerText(tg) {
  if (cliBusy.value[tg.id] === 'version') return t('sync.cli.reading')
  const v = cliVersion(tg)
  if (v) return 'v' + v
  return tg.installed ? t('sync.cli.installed') : t('sync.cli.notInstalled')
}
// 安装/更新按钮提示：说明将执行的命令或不可点的原因
function cliActTip(tg, update) {
  if (cliBusy.value[tg.id]) return t('sync.cli.busy')
  if (!cliCan(tg)) return t('sync.cli.noInstaller')
  if (!update) {
    if (tg.installed) return t('sync.cli.alreadyInstalled', { ver: 'v' + cliVersion(tg) })
    return t('sync.cli.installTip') + ' ' + (tg.installCmd || '')
  }
  if (!tg.installed) return t('sync.cli.updateNeedInstall')
  const u = cliLatest.value[tg.id]
  if (u && u.outdated) return t('sync.cli.hasUpdate', { ver: 'v' + u.latest }) + '：' + (tg.updateCmd || '')
  if (u && u.latest && !u.error) return t('sync.cli.upToDate', { ver: 'v' + (u.latest || cliVersion(tg)) })
  return t('sync.cli.updateTip') + ' ' + (tg.updateCmd || '')
}
// 版本单元格提示：显示 npm 包名与版本，或说明可点击读取
function cliVerTip(tg) {
  if (cliBusy.value[tg.id] === 'version') return t('sync.cli.reading')
  const v = cliVersion(tg)
  if (v) return 'npm ' + (tg.pkg || tg.bin || '') + ' · ' + v
  if (!tg.installed) return t('sync.cli.notInstalledTip')
  return t('sync.cli.readTip')
}
// 安装（update=false）/ 更新（update=true）：npm 全局安装该客户端 CLI，完成后刷新列表版本
async function cliRun(tg, update) {
  if (!cliCan(tg) || cliBusy.value[tg.id]) return
  cliBusy.value = { ...cliBusy.value, [tg.id]: update ? 'update' : 'install' }
  cliProgStart(tg.id)
  try {
    const r = await api.InstallClientCLI(tg.id, !!update)
    if (r && r.ok) {
      toast(t(update ? 'sync.cli.updateOk' : 'sync.cli.installOk', { name: tg.name, ver: r.version ? 'v' + r.version : '' }))
      await loadTargets()
      cliCheckUpdates() // 重新对齐「是否最新版」，更新按钮回到置灰
    } else {
      toast(t('common.fail', { msg: (r && r.error) || '?' }))
    }
  } catch (e) {
    toast(t('common.fail', { msg: e?.message || '?' }))
  } finally {
    cliBusy.value = { ...cliBusy.value, [tg.id]: '' }
    cliProgEnd(tg.id)
  }
}
// 待更新清单（顶部工具条「一键更新」的计数与目标）：已安装、可自动更新、且落后于最新版
const cliUpdatingAll = ref(false)
const cliOutdatedList = computed(() => (targets.value || []).filter(tg =>
  !tg.placeholder && tg.installed && cliCan(tg) && cliIsOutdated(tg)))
// 一键更新：先弹应用内确认（更新会覆盖本地修改过的配置文件），确认后再执行
const cliUpdateAllOpen = ref(false)
function cliUpdateAll() {
  if (cliUpdatingAll.value) return
  if (!cliOutdatedList.value.length) { toast(t('sync.cli.updateAllNone')); return }
  cliUpdateAllOpen.value = true
}
// 把清单里的客户端**串行**更新（串行可让每张卡片各显各的「更新中」，也避免
// 同时并发多个 npm 进程互相干扰），全部结束后统一刷新版本与更新检查、只弹一条汇总提示。
async function doCliUpdateAll() {
  cliUpdateAllOpen.value = false
  const list = cliOutdatedList.value
  if (!list.length) return
  cliUpdatingAll.value = true
  let n = 0, fail = 0
  try {
    for (const tg of list) {
      cliBusy.value = { ...cliBusy.value, [tg.id]: 'update' }
      cliProgStart(tg.id)
      try {
        const r = await api.InstallClientCLI(tg.id, true)
        if (r && r.ok) n++
        else fail++
      } catch (e) { fail++ } finally {
        cliBusy.value = { ...cliBusy.value, [tg.id]: '' }
        cliProgEnd(tg.id)
      }
    }
    await loadTargets()
    cliCheckUpdates()
    toast(fail ? t('sync.cli.updateAllFail', { n, fail }) : t('sync.cli.updateAllOk', { n }))
  } finally {
    cliUpdatingAll.value = false
  }
}
// 卸载图标提示：说明将执行的命令（图标仅已安装的卡片显示，未装场景不会再进到这里）
function cliUninTip(tg) {
  if (cliBusy.value[tg.id]) return t('sync.cli.busy')
  return t('sync.cli.uninstall') + '：' + cliUninCmdText(tg)
}
// 启动图标：弹系统终端列表，由用户选一个启动客户端 CLI
const cliTermPicker = ref(null) // { target, terminals }
async function cliPickTerm(tg) {
  try {
    const list = await api.ListSystemTerminals()
    if (!Array.isArray(list) || !list.length) {
      toast(t('common.fail', { msg: t('sync.term.none') }))
      return
    }
    cliTermPicker.value = { target: tg, terminals: list }
  } catch (e) {
    toast(t('common.fail', { msg: e?.message || '?' }))
  }
}
function termName(term) {
  const key = 'sync.term.' + term.id
  const text = t(key)
  return text === key ? term.name : text
}
async function cliLaunchInTerm(term) {
  const p = cliTermPicker.value
  if (!p || cliBusy.value[p.target.id]) return
  cliBusy.value[p.target.id] = 'launch'
  cliTermPicker.value = null
  try {
    const r = await api.LaunchClientTerminalIn(p.target.id, term.id)
    if (!r || !r.ok) toast(t('common.fail', { msg: (r && r.error) || '?' }))
  } catch (e) {
    toast(t('common.fail', { msg: e?.message || '?' }))
  } finally {
    delete cliBusy.value[p.target.id]
  }
}
// 卸载将执行的操作：npm 客户端显示 npm 命令，单二进制客户端显示要删除的二进制路径
function cliUninCmdText(tg) {
  if (!tg) return ''
  if (tg.pkg) return 'npm uninstall -g ' + tg.pkg
  return t('sync.cli.uninstallRemovePaths') + ' ' + (tg.uninstallPaths || '')
}
// 点卸载：先弹确认（卸载本身不可逆，且可选是否连配置一起清理）
const cliUninstallTarget = ref(null)
function cliAskUninstall(tg) {
  if (cliBusy.value[tg.id]) return
  cliUninstallTarget.value = tg
}
// 执行卸载：purge=true 连配置残留一起清理
async function cliRunUninstall(purge) {
  const tg = cliUninstallTarget.value
  if (!tg) return
  cliBusy.value = { ...cliBusy.value, [tg.id]: 'uninstall' }
  try {
    const r = await api.UninstallClientCLI(tg.id, !!purge)
    if (r && r.ok) {
      toast(t('sync.cli.uninstallOk', { name: tg.name, n: (r.removed || []).length }))
      cliUninstallTarget.value = null
      await loadTargets()
    } else {
      toast(t('common.fail', { msg: (r && r.error) || '?' }))
    }
  } catch (e) {
    toast(t('common.fail', { msg: e?.message || '?' }))
  } finally {
    cliBusy.value = { ...cliBusy.value, [tg.id]: '' }
  }
}
// 版本号：已知版本时仅作展示（不再重复起进程）；未安装时点击重新检测安装状态；
// 装了但版本未知（非 npm 安装）时现场执行 `<cli> --version` 探测一次
async function cliProbe(tg) {
  if (cliBusy.value[tg.id]) return
  if (!tg.installed) { await loadTargets(); return }
  if (cliVersion(tg)) return
  cliBusy.value = { ...cliBusy.value, [tg.id]: 'version' }
  try {
    const r = await api.GetClientVersion(tg.id)
    if (r && r.ok) cliProbed.value = { ...cliProbed.value, [tg.id]: r.version }
    else toast(t('common.fail', { msg: (r && r.error) || '?' }))
  } catch (e) {
    toast(t('common.fail', { msg: e?.message || '?' }))
  } finally {
    cliBusy.value = { ...cliBusy.value, [tg.id]: '' }
  }
}

// ---- watchers 注册（在组件 setup 上下文内建立，随组件生命周期清理） ----
initClaude()
initPi()
initGrok()
initCodex()
initKimi()
initOpencode()
initCommandCode()
initMimo()
initAgy()
initCodebuddy()
initDeepcode()
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

// agy 安全区四个布尔开关：每个用下拉框在 默认('') / 是('true') / 否('false') 三态选择
const cbModelOpen = ref(false)  // 默认模型（settings.json model）下拉
const cbLiteOpen = ref(false)   // 快速档模型（variantModels.lite）下拉
const cbReasonOpen = ref(false) // 深度档模型（variantModels.reasoning）下拉
const cbRowOpen = ref(-1)       // models.json 模型行「标识」下拉：当前打开行的索引（-1=全部收起）

// mimocode：所有命令（bash）三态单选 + 当前模式回显
const mimoBashMode = computed(() => {
  const st = mimoPerms.value['bash']
  return st && !st.obj ? st.mode : ''
})
function mimoPickBash(v) {
  const st = mimoPerms.value['bash']
  if (!st || st.obj) return
  st.mode = st.mode === v ? '' : v
}
// kimi：预设键循环到「未设」沿用 kimiToggleRule 的清除规则（无保留内容整行移除）；
// 自定义行 decision 支持 ''（默认处置，行保留），继续点按可回到 允许
function kimiRuleCycle(rule) {
  const cur = kimiRuleDecision(rule)
  const next = permModeNext(cur)
  kimiToggleRule(rule, next || cur)
}
function kimiCustomCycle(row) {
  if (!row) return
  row.decision = permModeNext(String(row.decision || ''))
}

// 非 form 模式（json / claudemd / climodels 等源码页签）走源码编辑排版
const isSrcMode = computed(() => cfgMode.value !== 'form')

// 作用位置候选弹层：无输入框可 blur，改为全局 mousedown 点击外部关闭（弹层内部与锚点列内不关）
function onCfgPopOut(e) {
  if (!copilotLocOpen.value) return
  const el = e.target
  if (el instanceof Element && el.closest('.co-loc-col')) return
  copilotLocOpen.value = false
}
onMounted(() => {
  loadTargets().then(() => cliCheckUpdates())
  document.addEventListener('mousedown', onCfgPopOut, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onCfgPopOut, true)
  if (cfgScrollTimer) { clearTimeout(cfgScrollTimer); cfgScrollTimer = null }
})

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

// Claude「命令」权限总状态：允许 / 拒绝 / 询问 三态胶囊，只维护权限通配行
//（裸工具名 Bash = 该工具全部终端命令，与 Claude 官方规则语法一致）：
// 允许 → permissions.allow 含 Bash；拒绝 → deny 含 Bash；询问 → ask 含 Bash；
// 再次点击当前档 = 取消该行（回到不接管）。其余工具规则行（Read/Edit/WebFetch/自定义）
// 仍在 claudePermRows 中保留写回，不在本面板展示编辑。
const claudeCmdMode = () => {
  const r = claudePermRows.value.find(r => String(r.text || '') === CLAUDE_CMD_RULE)
  return r ? r.mode : ''
}
function claudeCmdSet(mode) {
  const i = claudePermRows.value.findIndex(r => String(r.text || '') === CLAUDE_CMD_RULE)
  if (i >= 0) {
    if (claudePermRows.value[i].mode === mode) claudePermRows.value.splice(i, 1)
    else claudePermRows.value[i].mode = mode
  } else if (mode) {
    claudePermRows.value.push({ text: CLAUDE_CMD_RULE, mode })
  }
}

// 供父级面板标题栏（App.vue 的 .page-head）调用：「一键更新」按钮的计数/进行中状态与动作
defineExpose({ cliUpdateAll, cliOutdatedList, cliUpdatingAll })
</script>

<template>
  <div>
    <!-- 客户端卡片网格：按「需要处理 / 已是最新 / 未接入」自动归类（见 targetGroups）。
         分组标题作为整行元素插在同一个网格里（grid-column:1/-1），不必拆成多个网格 -->
    <div class="sync-grid">
      <template v-for="g in targetGroups" :key="g.key">
      <div v-if="g.title || g.key === 'todo'" class="sync-group-head" :class="{ tog: g.key === 'todo' }"
        :role="g.key === 'todo' ? 'button' : undefined" :tabindex="g.key === 'todo' ? 0 : -1"
        :title="g.key === 'todo' ? t('sync.groupToggle') : undefined"
        @click="g.key === 'todo' && (todoCollapsed = !todoCollapsed)"
        @keydown.enter.prevent="g.key === 'todo' && (todoCollapsed = !todoCollapsed)">
        <ChevronRightIcon v-if="g.key === 'todo' && todoCollapsedEff" class="ico sc-grp-caret" />
        <ChevronDownIcon v-else-if="g.key === 'todo'" class="ico sc-grp-caret" />
        <span>{{ g.title || t('sync.groupTodo') }}</span>
        <span v-if="g.key === 'todo'" class="sc-grp-count">{{ g.items.length }}</span>
      </div>
      <template v-if="g.key !== 'todo' || !todoCollapsedEff">
      <div v-for="tg in g.items" :key="tg.id" class="sync-card"
        :class="{ ph: tg.placeholder, 'sc-busy': cliProgress[tg.id] != null }"
        :style="{ '--sc-lvl': (cliProgress[tg.id] || 0) + '%' }"
        @contextmenu.prevent="!tg.placeholder && openTargetConfig(tg)">
        <!-- 安装/更新中：卡片背景即进度条——绿色自左向右填充，宽度（--sc-lvl）随估算进度增长 -->
        <span v-if="cliProgress[tg.id] != null" class="sc-progress" aria-hidden="true"></span>
        <div class="sync-card-main" @click="!tg.placeholder && openTargetConfig(tg)">
          <div class="sync-card-head">
            <div class="sync-client-avatar" :class="'avatar-' + tg.id">
              <img v-if="targetLogo(tg.id)" class="sync-client-logo" :src="targetLogo(tg.id)" alt="" draggable="false" />
              <span v-else-if="tg.id.startsWith('pi')">π</span>
              <span v-else>◈</span>
            </div>
            <span class="sync-card-sep" aria-hidden="true"></span>
            <div class="sync-title-text">
              <span class="sync-card-name">{{ tg.name }}</span>
            </div>
            <!-- 卡片右侧图标组（名称区 flex:1 顶到边缘）：终端启动 + 卸载。
                 均不触发"打开配置"（@click.stop）；划入卡片才浮现（见样式）。
                 启动仅已安装且登记了 CLI 的客户端显示；卸载仅已安装且支持自动卸载的通道显示 -->
            <div class="sync-card-tools">
              <button v-if="!tg.placeholder && tg.installed && tg.bin" type="button" class="sync-card-run"
                :title="t('sync.cli.launch', { bin: tg.bin })" @click.stop="cliPickTerm(tg)">
                <svg viewBox="0 0 12 12" aria-hidden="true">
                  <rect x="1.2" y="1.7" width="9.6" height="8.6" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.2" />
                  <path d="M3.4 4.5 5 6 3.4 7.5M6.3 7.7h2.3" fill="none" stroke="currentColor" stroke-width="1.2"
                    stroke-linecap="round" stroke-linejoin="round" />
                </svg>
              </button>
              <button v-if="tg.canUninstall && tg.installed" type="button" class="sync-card-unin" :class="{ on: cliBusy[tg.id] === 'uninstall' }"
                :disabled="!!cliBusy[tg.id]" :title="cliUninTip(tg)" @click.stop="cliAskUninstall(tg)">
                <svg viewBox="0 0 12 12" aria-hidden="true">
                  <path d="M2 3.4h8M4.6 3.4V2.2c0-.3.3-.6.6-.6h1.6c.3 0 .6.3.6.6v1.2M3.2 3.4l.5 6.2c0 .5.4.8.9.8h2.8c.5 0 .9-.3.9-.8l.5-6.2"
                    fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" />
                  <path d="M5 5.2v3.2M7 5.2v3.2" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
                </svg>
              </button>
            </div>
          </div>
        </div>
        <!-- 下半区：安装 / 更新 / 版本号（水平分隔线即 .sync-card-actions 的上边框）。
             三个动作都不触发「打开配置」（@click.stop），避免误点抽屉。
             整条仅在「未安装」或「有新版本」时出现（见 cliShowActions），已装且最新则隐藏；
             未安装的卡片只显示一个居中的「安装」按钮（不再并列「未安装」版本徽标，
             一个按钮已说明状态）；已安装才显示版本徽标，更新场景与版本并列 -->
        <div v-if="!tg.placeholder && cliShowActions(tg)" class="sync-card-actions">
          <button v-if="cliCanInstall(tg) || cliBusy[tg.id] === 'install'" type="button" class="sc-act"
            :class="{ on: cliBusy[tg.id] === 'install' }"
            :disabled="!cliCanInstall(tg) || !!cliBusy[tg.id]" :title="cliActTip(tg, false)" @click.stop="cliRun(tg, false)">
            <svg class="sc-ico" viewBox="0 0 12 12" aria-hidden="true">
              <path d="M6 1.8v5.4M3.5 4.9 6 7.4l2.5-2.5M2.2 10.2h7.6" fill="none" stroke="currentColor"
                stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span class="sc-txt">{{ cliBusy[tg.id] === 'install' ? t('sync.cli.installing') : t('sync.cli.install') }}</span>
          </button>
          <button v-if="cliCanUpdate(tg) || cliBusy[tg.id] === 'update'" type="button" class="sc-act"
            :class="{ on: cliBusy[tg.id] === 'update', up: cliIsOutdated(tg) }"
            :disabled="!cliCanUpdate(tg) || !!cliBusy[tg.id]" :title="cliActTip(tg, true)" @click.stop="cliRun(tg, true)">
            <svg class="sc-ico" viewBox="0 0 12 12" aria-hidden="true">
              <path d="M9.9 5.1a3.9 3.9 0 1 0 .1 1.6" fill="none" stroke="currentColor" stroke-width="1.3"
                stroke-linecap="round" stroke-linejoin="round" />
              <path d="M10.3 1.9v3.2H7.1" fill="none" stroke="currentColor" stroke-width="1.3"
                stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span class="sc-txt">{{ cliBusy[tg.id] === 'update' ? t('sync.cli.updating') : t('sync.cli.update') }}</span>
          </button>
          <button v-if="tg.installed || cliBusy[tg.id] === 'version'" type="button" class="sc-act sc-ver"
            :class="{ on: cliBusy[tg.id] === 'version', known: !!cliVersion(tg), outdated: cliIsOutdated(tg) }"
            :title="cliVerTip(tg)" @click.stop="cliProbe(tg)">
            <svg class="sc-ico" viewBox="0 0 12 12" aria-hidden="true">
              <path d="M1.6 6.2V2.6c0-.5.4-.9.9-.9h3.6c.2 0 .4.1.6.2l3.5 3.5c.3.3.3.9 0 1.2L6.4 10.4c-.3.3-.9.3-1.2 0L1.9 7.1c-.2-.2-.3-.4-.3-.9Z"
                fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round" />
              <circle cx="4.1" cy="4.1" r=".85" fill="currentColor" />
            </svg>
            <span class="sc-txt sc-ver-txt">{{ cliVerText(tg) }}</span>
          </button>
        </div>
        <span v-if="tg.placeholder" class="sync-ph">{{ t('sync.notConnected') }}</span>
      </div>
      </template>
      </template>
    </div>

    <!-- 配置编辑抽屉：不 Teleport 到 body——留在本页容器内，由 .right-pane 的 transform
         充当 fixed 的包含块；抽屉铺满本面板（含顶部页签条）从右缘滑入，把后面的面板整个盖住
         （即「从面板右侧弹出、全部覆盖后方面板」）。保存 / 关闭按钮浮在配置页左缘内侧、垂直居中。
         不做「点击遮罩关闭」：点抽屉外区域不收回，避免误触丢编辑内容 -->
    <Transition name="cfg-drawer" @before-enter="onDrawerBeforeEnter" @after-enter="onDrawerAfterEnter"
                @before-leave="onDrawerBeforeLeave" @after-leave="onDrawerAfterLeave">
    <div v-if="cfgEdit" class="dialog-overlay open cfg-drawer-ov">
      <div class="dialog cfg-dialog" :class="{ 'cfg-anim': cfgAnim, 'cfg-staged': cfgStaged, 'cfg-dir-enter': cfgDir === 'enter', 'cfg-exit-left': cfgExitLeft }" @scroll.capture.passive="onCfgScroll">
        <!-- 保存 / 关闭：浮在配置页右缘内侧、垂直居中（上下排列），图标用 Heroicons。
             收起方向：保存 → 向左滑出；关闭 → 向下滑出 -->
        <div class="cfg-drawer-actions" :class="{ 'is-hidden': cfgScrolling }">
          <button class="cfg-drawer-btn save" type="button" :disabled="cfgSaving || cfgLoading" :title="t('sync.saveOnlyTip')" @click="cfgExitLeft = true; saveTargetConfig()">
            <CheckIcon class="ico" />
          </button>
          <button class="cfg-drawer-btn close" type="button" :title="t('sync.closeTip')" @click="cfgExitLeft = false; cfgEdit = false">
            <XMarkIcon class="ico" />
          </button>
        </div>
        <div class="cfg-head">
          <div class="cfg-head-top">
            <div class="cfg-title">
              <img v-if="targetLogo(cfgTarget?.id)" class="cfg-title-logo" :src="targetLogo(cfgTarget?.id)" :alt="cfgTarget?.name || ''" :title="cfgTarget?.name || ''" draggable="false" />
              <span v-else>{{ cfgTarget?.name }}</span>
            </div>
          <div class="cfg-tabs">
            <button v-if="cfgTarget?.id!=='copilot'" class="cfg-tab" :class="{on:cfgMode==='form'}" @click="switchMode('form')">{{ t('sync.tabForm') }}</button>
            <button v-if="cfgTarget?.id==='copilot'" class="cfg-tab" :class="{on:cfgMode==='copilotenv'}" @click="switchMode('copilotenv')" :title="t('sync.copilot.tabTip')">{{ t('sync.copilot.title') }}</button>
            <button class="cfg-tab" :class="{on:cfgMode==='json'}" @click="switchMode('json')">{{ tabJsonLabel }}</button>
            <button v-if="cfgTarget?.id==='opencode'" class="cfg-tab" :class="{on:cfgMode==='openagents'}" @click="switchMode('openagents')" :title="'~/.config/opencode/AGENTS.md ' + t('sync.src')">AGENTS.md</button>
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
            <button v-if="cfgTarget?.id==='claude-code'" class="cfg-tab" :class="{on:cfgMode==='claudemd'}" @click="switchMode('claudemd')" :title="'~/.claude/CLAUDE.md ' + t('sync.src')">CLAUDE.md</button>
            <button v-if="cfgTarget?.id==='copilot'" class="cfg-tab" :class="{on:cfgMode==='copilotperm'}" @click="switchMode('copilotperm')" :title="t('sync.copilot.permTabTip')">{{ t('sync.copilot.permTab') }}</button>
            <button v-if="cfgTarget?.id==='agy'" class="cfg-tab" :class="{on:cfgMode==='agymcp'}" @click="switchMode('agymcp')" :title="'~/.gemini/config/mcp_config.json ' + t('sync.src')">mcp_config.json</button>
            <template v-if="cfgTarget?.id==='codebuddy'">
              <button class="cfg-tab" :class="{on:cfgMode==='cbmodels'}" @click="switchMode('cbmodels')" :title="'~/.codebuddy/models.json ' + t('sync.src')">models.json</button>
              <button class="cfg-tab" :class="{on:cfgMode==='cbmcp'}" @click="switchMode('cbmcp')" :title="'~/.codebuddy/mcp.json ' + t('sync.src')">mcp.json</button>
            </template>
          </div>
          </div>
        </div>

        <div class="cfg-body" :class="{ 'cfg-body-src': isSrcMode }">
          <div v-if="cfgLoading" style="padding:20px;text-align:center;color:var(--t3)">…</div>
          <div v-else-if="cfgMode==='form'" class="cfg-form">

            <!-- Claude Code -->
            <template v-if="cfgTarget?.id==='claude-code'">
              <div class="panel-head" :title="t('sync.claude.modelHeadTip') + t('sync.claude.endpointTip')">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="field-row fld-inline-row" style="margin-top:2px;align-items:center">
                  <div class="field fw-l">
                    <label>{{ t('sync.claude.baseUrlEnv') }}</label>
                    <input :value="routerHintForTarget()" readonly />
                  </div>
                  <div class="field fw-m">
                    <label>{{ t('sync.claude.tokenEnv') }}</label>
                    <input :value="getPath(cfgObj,'env.ANTHROPIC_AUTH_TOKEN') || ''" @input="e=> setPath(cfgObj,'env.ANTHROPIC_AUTH_TOKEN', e.target.value)" :placeholder="t('sync.routeToken')" />
                  </div>
                  <div class="field fw-m">
                    <label>{{ t('sync.model') }}</label>
                    <div class="pi-sel">
                      <input v-model="claudeDefaultModel" :placeholder="t('sync.pickModel')" @focus="claudeModelOpen=-2" @click.stop="claudeModelOpen=-2" @input="claudeModelOpen=-2" @blur="delay(()=>{ if(claudeModelOpen===-2) claudeModelOpen=-1 },120)" />
                      <div v-show="claudeModelOpen===-2" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(claudeDefaultModel)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="claudeDefaultModel=m; claudeModelOpen=-1">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(claudeDefaultModel).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                </div>
                <!-- 环境变量模型映射与上方连接信息合并同一卡片（接入协议说明并入标题栏问号） -->
                <div v-for="(row, i) in claudeEnvModels" :key="row" class="model-row fld-inline-row" style="margin-top:4px">
                  <div class="field env-key"><label>{{ t('sync.envKey') }}</label><input v-model="row.envKey" class="env-ro" readonly :style="{ width: claudeEnvKeyWidth }" /></div>
                  <div class="field model-sel"><label>{{ t('sync.models') }}</label>
                    <div class="pi-sel">
                      <input v-model="row.modelId" :placeholder="t('sync.pickModel')" @focus="claudeModelOpen=i" @click.stop="claudeModelOpen=i" @input="claudeModelOpen=i" @blur="delay(()=>{ if(claudeModelOpen===i) claudeModelOpen=-1 },120)" />
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

              <template v-for="(g, gi) in CLAUDE_EXTRA_GROUPS" :key="g.id">
                <div class="panel-head" :title="t('sync.claude.keysTip') + g.fields.map(f => f.key).join(' / ')">{{ t('sync.claude.group.' + g.id) }}</div>
                <section class="card cfg-card">
                  <div :class="g.id==='template' ? 'claude-grid' : (g.id==='mcplists' ? 'claude-one-line' : 'field-row fld-inline-row')">
                    <div v-for="f in g.fields" :key="f.key" class="field" :class="claudeFieldWidth(f)">
                      <label>{{ t('sync.claude.x.' + f.key) }}</label>
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

              <div class="panel-head" :title="t('sync.claude.permTip')">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <PermPanel v-model:mode="claudePermMode" v-model:rows="claudePermRows"
                  :modes="PERM_OPTIONS.map(o => ({ v: o.value, label: o.name() }))"
                  :row-placeholder="t('sync.permRulePh')" :quick="CLAUDE_PERM_QUICK">
                  <template #extra>
                    <div class="field">
                      <label :title="t('sync.claude.addDirsTip')">{{ t('sync.claude.addDirs') }}</label>
                      <textarea v-model="claudePermRules.addDirs" rows="2" class="perm-ta" @wheel="taWheelStop"
                        :placeholder="t('sync.claude.addDirsPh')"></textarea>
                    </div>
                    <div class="field">
                      <label :title="t('sync.claude.disableBypassTip')">{{ t('sync.claude.disableBypass') }}</label>
                      <select v-model="claudeDisableBypass">
                        <option value="">{{ t('sync.notWrite') }}</option>
                        <option value="true">{{ t('sync.opt.true') }}</option>
                      </select>
                    </div>
                  </template>
                </PermPanel>
              </section>
            </template>

            <!-- pi -->
            <template v-else-if="cfgTarget?.id==='pi'">
              <div class="panel-head">{{ t('sync.panelModel') }}</div>
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
                <div class="pi-fetch-bar" :title="piBaseModels.length ? t('sync.piFetched', { n: piBaseModels.length }) : t('sync.piFetchEmpty')">
                  <span v-if="piFetchLoading" class="pi-fetch-loading">{{ t('sync.piFetching') }}</span>
                  <template v-else>
                    <button type="button" class="btn btn-xs btn-ghost" @click.stop="fetchPiBaseModels">{{ t('sync.refetch') }}</button>
                  </template>
                </div>
                <div v-for="(row, i) in piM.models" :key="row" class="pi-model-block">
                  <div class="pi-model-head"><strong>#{{ i+1 }}</strong><button type="button" class="btn btn-xs btn-ghost row-del" @click="piM.models.splice(i,1)">{{ t('sync.delete') }}</button></div>
                  <div class="field-row">
                    <div class="field fw-l"><label :title="t('sync.modelsList')">{{ t('sync.id') }}</label>
                      <div class="pi-sel">
                        <input v-model="row.id" :placeholder="t('sync.pickModel')" @focus="modelSelOpen=i" @click.stop="modelSelOpen=i" @input="modelSelOpen=i" @blur="delay(()=>{ if(modelSelOpen===i) modelSelOpen=-1 },120)" />
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
                  <div class="field fw-m"><label :title="t('sync.pi.defaultProviderTip')">{{ t('sync.defaultProvider') }}</label><input v-model="piSettings.defaultProvider" placeholder="LLM" /></div>
                  <div class="field fw-l"><label>{{ t('sync.defaultModel') }}</label>
                    <div class="pi-sel">
                      <input v-model="piSettings.defaultModel" placeholder="x-preview-f-free" @focus="cfgModelOpen=true" @click.stop="cfgModelOpen=true" @input="cfgModelOpen=true" @blur="delay(()=>{ cfgModelOpen=false },120)" />
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

              <div class="panel-head">{{ t('sync.panelStyle') }}</div>
              <section class="card cfg-card oc-card" id="pi-panel-style">
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.theme') }}</label>
                    <select v-model="piSettings.theme"><option value=""></option><option v-for="o in PI_THEME_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <div class="field fw-m"><label :title="t('sync.pi.lastChangelogTip')">{{ t('sync.lastChangelog') }}</label><input v-model="piSettings.lastChangelogVersion" placeholder="0.74.0" /></div>
                </div>
              </section>
              <div class="panel-head">{{ t('sync.panelPerm') }}</div>
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

            </template>

            <!-- grok-build -->
            <template v-else-if="cfgTarget?.id==='grok-build'">
              <div class="panel-head" :title="t('sync.grok.blocksTip')">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card cp-lab-left gm-row" id="grok-panel-model">
                <div class="field-row" style="margin-top:6px">
                  <div class="field gm-blk">
                    <label :title="t('sync.grok.currentBlockTip')">{{ t('sync.grok.currentBlock') }}</label>
                    <select :value="grokForm.modelKey" @change="e=>selectGrokModel(e.target.value)">
                      <option v-for="k in grokOrder" :key="k" :value="k">{{ k }}{{ k===grokForm.default ? t('sync.defaultMark') : '' }}</option>
                      <option v-if="!grokOrder.includes(grokForm.modelKey)" :value="grokForm.modelKey">{{ grokForm.modelKey || '…' }}</option>
                    </select>
                  </div>
                  <div class="field gm-def">
                    <label :title="t('sync.grok.defaultTip')">{{ t('sync.grok.blockDefault') }}</label>
                    <select :value="grokForm.default" @change="e=>pickGrokDefault(e.target.value)"><option value=""></option><option v-for="k in grokDefaultCandidates()" :key="k" :value="k">{{ k }}</option></select>
                  </div>
                  <div class="field gm-new">
                    <label :title="t('sync.grok.newBlockTip')">{{ t('sync.grok.newBlock') }}</label>
                    <input :placeholder="t('sync.grok.newBlockPh')" @keydown.enter.prevent="addGrokModel($event.target.value); $event.target.value=''" />
                  </div>
                </div>
                <div class="field-row">
                  <div class="field gm-name"><label>{{ t('sync.name') }}</label><input v-model="grokForm.name" :placeholder="t('sync.namePh')" /></div>
                  <div class="field gm-desc"><label>{{ t('sync.grok.description') }}</label><input v-model="grokForm.description" placeholder="Grok 4.5 from xAI" /></div>
                </div>
                <div class="field-row">
                  <div class="field gm-ctx"><label>{{ t('sync.contextLen') }}</label><input v-model="grokForm.contextWindow" type="number" min="1" placeholder="1000000" /></div>
                  <div class="field gm-ws"><label :title="t('sync.grok.webSearchTip')">{{ t('sync.grok.webSearch') }}</label>
                    <select :value="grokForm.webSearch" @change="e=>pickGrokWebSearch(e.target.value)"><option value="">—</option><option v-for="k in grokDefaultCandidates()" :key="k" :value="k">{{ k }}</option></select>
                  </div>
                  <div class="field gm-api"><label>{{ t('sync.grok.apiBackend') }}</label>
                    <select v-model="grokForm.apiBackend"><option value="">{{ t('sync.default') }}</option><option value="responses">responses</option><option value="chat">chat</option></select>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field gm-eff"><label>{{ t('sync.thinking') }}</label>
                    <select v-model="grokForm.reasoningEffort">
                      <option value="">{{ t('sync.default') }}</option>
                      <option value="low">{{ t('sync.opt.low') }}</option>
                      <option value="high">{{ t('sync.opt.high') }}</option>
                      <option value="max">{{ t('sync.opt.max') }}</option>
                    </select>
                  </div>
                  <div class="field gm-temp"><label>{{ t('sync.temperature') }}</label><input v-model="grokForm.temperature" type="number" step="0.05" min="0" max="2" placeholder="0.7" /></div>
                  <div class="field gm-top"><label>{{ t('sync.topP') }}</label><input v-model="grokForm.topP" type="number" step="0.05" min="0" max="1" placeholder="0.95" /></div>
                  <div class="field gm-max"><label>{{ t('sync.maxTokens') }}</label><input v-model="grokForm.maxTokens" type="number" min="1" placeholder="8192" /></div>
                  <div class="field gm-bs"><label>{{ t('sync.grok.supportsBackendSearch') }}</label>
                    <select v-model="grokForm.supportsBackendSearch"><option value="">{{ t('sync.default') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                </div>
                <!-- 长值字段（接入地址 / 上游模型）：按「短框在上、长框在下」排布——
                     从面板第 2 行下移到这里，与 API 密钥、额外请求头这些长框同组 -->
                <div class="field-row">
                  <div class="field gm-url">
                    <label :title="t('sync.grok.baseUrlTip')">{{ t('sync.baseUrl') }}</label>
                    <div class="ro-url">
                      <input :value="derivedBaseUrl(grokForm.accessMode)" readonly />
                    </div>
                  </div>
                  <div class="field gm-up"><label>{{ t('sync.grok.upstreamModel') }}</label>
                    <div class="pi-sel">
                      <input v-model="grokModelSearch" :placeholder="grokForm.model || t('sync.pickModel')" @focus="grokModelOpen=true" @click.stop="grokModelOpen=true" @input="grokModelOpen=true" @blur="delay(()=>{ grokModelOpen=false },120)" @keydown="grokModelKeyNav" />
                      <div v-show="grokModelOpen" class="pi-sel-drop">
                        <div v-for="g in grokModelGroups()" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickGrokModel(m)">{{ m }}</button>
                        </div>
                        <div v-if="!grokModelGroups().length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field gm-full"><label :title="t('sync.grok.apiKeyTip')">{{ t('sync.apiKey') }}</label><input v-model="grokForm.apiKey" :placeholder="t('sync.routeToken')" /></div>
                </div>
                <div class="field-row">
                  <div class="field gm-full"><label :title="t('sync.grok.extraHeadersTip')">{{ t('sync.grok.extraHeaders') }}</label><input v-model="grokForm.extraHeaders" placeholder='{ "x-api-key" = "..." }' /></div>
                </div>
              </section>

              <div class="panel-head">{{ t('sync.grok.panelUi') }}</div>
              <section class="card cfg-card cp-lab-left gm-row" id="grok-panel-ui">
                <div class="cc-grid4 cc-lab-left gm-fit">
                  <div class="field fw-fit"><label>{{ t('sync.theme') }}</label>
                    <select v-model="grokForm.uiTheme"><option value=""></option><option value="auto">{{ t('sync.opt.auto') }}</option><option value="dark">{{ t('sync.opt.dark') }}</option><option value="light">{{ t('sync.opt.light') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.simpleMode') }}</label>
                    <select v-model="grokForm.uiSimpleMode"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.vimMode') }}</label>
                    <select v-model="grokForm.uiVim"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.scrollSpeed') }}</label><input type="number" v-model="grokForm.uiScrollSpeed" placeholder="50" />
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.compactMode') }}</label>
                    <select v-model="grokForm.uiCompact"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.showThinking') }}</label>
                    <select v-model="grokForm.uiThinking"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.maxThoughtsWidth') }}</label><input type="number" v-model="grokForm.uiMaxThoughtsWidth" placeholder="120" />
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.grok.defaultPerm') }}</label><input v-model="grokForm.uiDefaultPerm" placeholder="always_allow_all_sessions" />
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.rememberApprovals') }}</label>
                    <select v-model="grokForm.uiRememberApprovals"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.groupVerbs') }}</label>
                    <select v-model="grokForm.uiGroupVerbs"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.collapsedEdit') }}</label>
                    <select v-model="grokForm.uiCollapsedEdit"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.pageFlip') }}</label>
                    <select v-model="grokForm.uiPageFlip"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.screenMode') }}</label><input v-model="grokForm.uiScreenMode" placeholder="fullscreen" />
                  </div>
                </div>
              </section>

              <div class="panel-head">{{ t('sync.grok.panelToolset') }}</div>
              <section class="card cfg-card cp-lab-left gm-row" id="grok-panel-toolset">
                <div class="cc-grid4 cc-lab-left gm-fit">
                  <div class="field fw-fit"><label>{{ t('sync.grok.fileToolset') }}</label><input v-model="grokForm.tsFileToolset" placeholder="standard" />
                  </div>
                  <div class="field fw-fit"><label title="[toolset.hashline] scheme">{{ t('sync.grok.hlScheme') }}</label><input v-model="grokForm.hlScheme" placeholder="chunk" />
                  </div>
                  <div class="field fw-fit"><label title="[toolset.hashline] hash_len">{{ t('sync.grok.hlHashLen') }}</label><input type="number" v-model="grokForm.hlHashLen" placeholder="3" />
                  </div>
                  <div class="field fw-fit"><label title="[toolset.hashline] chunk_size">{{ t('sync.grok.hlChunkSize') }}</label><input type="number" v-model="grokForm.hlChunkSize" placeholder="8" />
                  </div>
                </div>
              </section>

              <div class="panel-head">{{ t('sync.grok.panelOther') }}</div>
              <section class="card cfg-card cp-lab-left gm-row" id="grok-panel-other">
                <div class="cc-grid4 cc-lab-left gm-fit">
                  <div class="field fw-fit"><label>{{ t('sync.grok.autoUpdate') }}</label>
                    <select v-model="grokForm.autoUpdate"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.autoCompact') }}</label><input type="number" v-model="grokForm.autoCompactThreshold" placeholder="85" /></div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.compatClaudeMcps') }}</label>
                    <select v-model="grokForm.compatClaudeMcps"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.compatCursorMcps') }}</label>
                    <select v-model="grokForm.compatCursorMcps"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                </div>
              </section>

              <div class="panel-head">{{ t('sync.grok.panelFeatures') }}</div>
              <section class="card cfg-card cp-lab-left gm-row" id="grok-panel-features">
                <div class="cc-grid4 cc-lab-left gm-fit">
                  <div class="field fw-fit"><label>{{ t('sync.grok.telemetry') }}</label>
                    <select v-model="grokForm.featTelemetry"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.feedback') }}</label>
                    <select v-model="grokForm.featFeedback"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.lspTools') }}</label>
                    <select v-model="grokForm.featLsp"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.codebaseIndexing') }}</label>
                    <select v-model="grokForm.featIndexing"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.twoPassCompaction') }}</label>
                    <select v-model="grokForm.featTwoPass"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                  <div class="field fw-fit"><label>{{ t('sync.grok.remoteFetch') }}</label>
                    <select v-model="grokForm.featRemoteFetch"><option value=""></option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                </div>
              </section>

              <div class="panel-head" :title="t('sync.grok.permSyntaxTip')">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card" id="grok-panel-perm">
                <PermPanel v-model:mode="grokForm.permissionMode" v-model:rows="grokPermRows"
                  :modes="toModeOpts(GROK_PERM_MODES)" :row-placeholder="t('sync.permRulePh')" />
              </section>
            </template>

            <!-- agy (Antigravity CLI) -->
            <template v-else-if="cfgTarget?.id==='agy'">
              <div class="panel-head" :title="t('sync.agy.modelAliasTip')">{{ t('sync.agy.mapPanel') }}</div>
              <section class="card cfg-card">
                <div class="agy-map-main">
                  <!-- 接入与凭据：供应商 / 路由器地址 / 路由令牌 三列等宽网格；操作按钮单独一行右对齐 -->
                  <div class="agy-sec">{{ t('sync.agy.secConnect') }}</div>
                  <div class="agy-grid c3">
                    <div class="field"><label>{{ t('sync.agy.modelProvider') }}</label>
                      <div class="pi-sel">
                        <!-- 打开/关闭只由 click 切换 + blur 兜底：不写 @focus，
                             否则首击会「focus 置开 → 同一次 click 又切回关」而看似点不开 -->
                        <input readonly :value="agyForm.modelProvider || t('sync.notWrite')" :title="t('sync.agy.modelProviderTip')"
                          @click.stop="agyMpOpen = !agyMpOpen"
                          @blur="delay(() => { agyMpOpen = false }, 140)" />
                        <div v-show="agyMpOpen" class="pi-sel-drop">
                          <button type="button" :class="{ on: agyForm.modelProvider === '' }" @mousedown.prevent="agyForm.modelProvider = ''; agyMpOpen = false">{{ t('sync.notWrite') }}</button>
                          <button type="button" :class="{ on: agyForm.modelProvider === 'gemini' }" @mousedown.prevent="agyForm.modelProvider = 'gemini'; agyMpOpen = false">gemini</button>
                        </div>
                      </div>
                    </div>
                    <div class="field"><label>{{ t('sync.agy.envBaseLabel') }}</label>
                      <input :value="agyRouterUrl()" readonly />
                    </div>
                    <div class="field"><label>{{ t('sync.agy.envKeyLabel') }}</label>
                      <input v-model="agyEnvKey" :placeholder="t('sync.routeToken')" />
                    </div>
                  </div>
                  <div class="agy-actions">
                    <button v-if="agyEnvShowClear" type="button" class="cc-toggle danger" :disabled="agyEnvSaving" @click="agyClearEnv()">{{ t('sync.copilot.clear') }}</button>
                    <button v-else type="button" class="cc-toggle on" :disabled="agyEnvSaving" @click="agyWriteEnv()">{{ t('sync.agy.envWrite') }}</button>
                  </div>
                  <!-- 内置 ID 映射：表头 + 每行「内置 ID = 目标模型」，留空 = 不映射 -->
                  <div class="agy-sec">{{ t('sync.agy.secMap') }}</div>
                  <div class="agy-map-list">
                    <div class="agy-map-head"><span>{{ t('sync.agy.mapFrom') }}</span><span class="agy-map-eq">=</span><span>{{ t('sync.agy.mapTo') }}</span></div>
                    <div class="agy-map-rows">
                      <div v-for="m in agyBuiltinModels" :key="m" class="agy-map-row">
                        <span class="agy-map-builtin">{{ m }}</span>
                        <span class="agy-map-eq">=</span>
                        <div class="pi-sel agy-map-val">
                          <input :value="agyAliasMap[m] || ''" :placeholder="t('sync.agy.mapToPh')"
                            @input="agySetAlias(m, $event.target.value); agyToOpenFor = m; agyToFilter = $event.target.value"
                            @focus="agyToOpenFor = m; agyToFilter = ''"
                            @blur="delay(() => { if (agyToOpenFor === m) agyToOpenFor = '' }, 140)"
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
                  </div>
                </div>
              </section>

              <div class="panel-head" :title="t('sync.agy.samplingTip')">{{ t('sync.agy.parametersPanel') }}</div>
              <section class="card cfg-card">
                <div class="agy-grid c4">
                  <div class="field"><label>{{ t('sync.temperature') }}</label><input type="number" step="0.1" min="0" max="2" v-model="agyForm.temperature" :placeholder="t('sync.phExample', { v: '0.7' })" /></div>
                  <div class="field"><label>{{ t('sync.maxTokens') }}</label><input type="number" min="1" v-model="agyForm.maxOutputTokens" :placeholder="t('sync.phExample', { v: '8192' })" /></div>
                  <div class="field"><label>{{ t('sync.topP') }}</label><input type="number" step="0.05" min="0" max="1" v-model="agyForm.topP" :placeholder="t('sync.phExample', { v: '1' })" /></div>
                  <div class="field"><label>{{ t('sync.topK') }}</label><input type="number" min="1" v-model="agyForm.topK" :placeholder="t('sync.phExample', { v: '40' })" /></div>
                </div>
              </section>


              <div class="panel-head" :title="t('sync.agy.safetyHeadTip')">{{ t('sync.agy.safetyPanel') }}</div>
              <section class="card cfg-card">
                <div class="agy-grid c4">
                  <div v-for="k in AGY_SAFE_KEYS" :key="k" class="field fw-m">
                    <label>{{ t('sync.agy.' + k) }}</label>
                    <select v-model="agyForm[k]">
                      <option value="">{{ t('sync.opt.default') }}</option>
                      <option value="true">{{ t('sync.opt.yes') }}</option>
                      <option value="false">{{ t('sync.opt.no') }}</option>
                    </select>
                  </div>
                </div>
              </section>

              <div class="panel-head" :title="t('sync.agy.styleHeadTip')">{{ t('sync.panelStyle') }}</div>
              <section class="card cfg-card">
                <!-- agy 外观与终端行为：5 个下拉按 5 列网格对齐 -->
                <div class="agy-grid c5">
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

              <div class="panel-head" :title="t('sync.agy.subHeadTip')">{{ t('sync.agy.subagentsPanel') }}</div>
              <section class="card cfg-card">
                <div class="agy-grid c3">
                  <div class="field fw-m"><label>{{ t('sync.agy.subagentsEnabled') }}</label>
                    <select v-model="agyForm.subagentsEnabled">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">{{ t('sync.opt.true') }}</option>
                      <option value="false">{{ t('sync.opt.false') }}</option>
                    </select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.agy.subagentsMaxSteps') }}</label><input type="number" min="1" v-model="agyForm.subagentsMaxSteps" :placeholder="t('sync.phExample', { v: '10' })" /></div>
                  <div class="field fw-m"><label>{{ t('sync.agy.subagentsAuto') }}</label>
                    <select v-model="agyForm.subagentsAutoExecuteTools">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">{{ t('sync.opt.true') }}</option>
                      <option value="false">{{ t('sync.opt.false') }}</option>
                    </select>
                  </div>
                </div>
              </section>

              <div class="panel-head" :title="t('sync.agy.permHeadTip')">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <!-- 权限配置：两组单选各占一行，组名定宽左对齐、选项区等宽撑满（比并成一条长胶囊更整齐） -->
                <div class="agy-perm-rows">
                  <div class="agy-perm-col">
                    <span class="agy-perm-name" :title="t('sync.agy.toolPermName')">{{ t('sync.agy.toolPermName') }}</span>
                    <div class="agy-perm-box" role="radiogroup" :aria-label="t('sync.agy.toolPermName')">
                      <label v-for="m in AGY_TOOL_PERM_MODES" :key="m.v" class="agy-box-opt" :title="t(m.label)">
                        <input type="radio" name="agyToolPerm" :value="m.v" v-model="agyForm.toolPermission" />
                        <span>{{ t(m.label) }}</span>
                      </label>
                    </div>
                  </div>
                  <div class="agy-perm-col">
                    <span class="agy-perm-name" :title="t('sync.agy.allCmdName')">{{ t('sync.agy.allCmdName') }}</span>
                    <div class="agy-perm-box" role="radiogroup" :aria-label="t('sync.agy.allCmdName')">
                      <label v-for="m in AGY_ALL_CMD_MODES" :key="'cmd-'+m.cls" class="agy-box-opt" :class="'cmd-'+m.cls" :title="t(m.label)">
                        <input type="radio" name="agyAllCmd" :value="m.v" v-model="agyForm.allCommands" />
                        <span>{{ t(m.label) }}</span>
                      </label>
                    </div>
                  </div>
                </div>
              </section>
            </template>

            <!-- codebuddy (CodeBuddy CLI) -->
            <template v-else-if="cfgTarget?.id==='codebuddy'">
              <!-- ① 接入端点与模型（models.json）：顶部统一端点地址/密钥 + 下方模型配置行（保存时连接铺写到每行 url/apiKey） -->
              <div class="panel-head" :title="t('sync.cb.endpointTip')">{{ t('sync.cb.endpoints') }}</div>
              <section class="card cfg-card">
                <div class="field-row fld-inline-row" style="margin-top:2px">
                  <div class="field fw-l"><label>{{ t('sync.cb.url') }}</label><input v-model="cbConnUrl" placeholder="http://127.0.0.1:3000/router" /></div>
                  <div class="field fw-l"><label>{{ t('sync.apiKey') }}</label><input v-model="cbConnKey" :placeholder="t('sync.routeToken')" /></div>
                </div>
                <div v-if="cbModelsObj.models && cbModelsObj.models.length" class="cb-mrows" style="margin-top:8px">
                  <div v-for="(row, i) in cbModelsObj.models" :key="row.dbk || (row.id || row.name || '') || '#' + i" class="pi-model-block cb-mrow">
                    <div class="pi-model-head">
                      <strong>#{{ i + 1 }}</strong>
                      <button type="button" class="btn btn-xs btn-ghost row-del" @click="cbDelModel(i)">{{ t('sync.delete') }}</button>
                    </div>
                    <div class="field-row fld-inline-row">
                      <div class="field fw-l"><label>{{ t('sync.id') }}</label>
                        <div class="pi-sel">
                          <input v-model="row.id" :placeholder="t('sync.pickModel')" @focus="cbRowOpen=i" @click.stop="cbRowOpen=i" @input="cbRowOpen=i" @blur="delay(()=>{ if(cbRowOpen===i) cbRowOpen=-1 },120)" />
                          <div v-show="cbRowOpen===i" class="pi-sel-drop">
                            <div v-for="g in modelGroupsByLetter(row.id)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                              <div class="pi-sel-letter">{{ g.letter }}</div>
                              <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="row.id=m; cbRowOpen=-1">{{ m }}</button>
                            </div>
                            <div v-if="!modelGroupsByLetter(row.id).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                          </div>
                        </div>
                      </div>
                      <div class="field fw-l"><label>{{ t('sync.name') }}</label><input v-model="row.name" :placeholder="t('sync.namePh')" /></div>
                      <div class="field fw-s fw-fit"><label>{{ t('sync.provider') }}</label><input v-model="row.vendor" placeholder="OpenAI" /></div>
                      <div class="field fw-s fw-fit"><label>{{ t('sync.supportsTools') }}</label>
                        <select v-model="row.supportsToolCall"><option :value="true">{{ t('sync.opt.true') }}</option><option :value="false">{{ t('sync.opt.false') }}</option></select>
                      </div>
                      <div class="field fw-s fw-fit"><label>{{ t('sync.cb.images') }}</label>
                        <select v-model="row.supportsImages"><option :value="true">{{ t('sync.opt.true') }}</option><option :value="false">{{ t('sync.opt.false') }}</option></select>
                      </div>
                      <div class="field fw-s fw-fit"><label>{{ t('sync.cb.supportsReasoning') }}</label>
                        <select v-model="row.supportsReasoning"><option :value="true">{{ t('sync.opt.true') }}</option><option :value="false">{{ t('sync.opt.false') }}</option></select>
                      </div>
                    </div>
                  </div>
                </div>
                <button type="button" class="btn btn-xs btn-ghost" style="align-self:flex-start;margin-top:4px" @click="cbAddModel()">{{ t('sync.addModel') }}</button>
              </section>

              <!-- ② 默认模型与常规（settings.json） -->
              <div class="panel-head" :title="t('sync.cb.generalTip')">{{ t('sync.cb.general') }}</div>
              <section class="card cfg-card">
                <div class="field-row fld-inline-row" style="margin-top:2px">
                  <div class="field fw-l"><label>{{ t('sync.defaultModel') }}</label>
                    <div class="pi-sel">
                      <input v-model="cbForm.model" :placeholder="t('sync.pickModel')" @focus="cbModelOpen=true" @click.stop="cbModelOpen=true" @input="cbModelOpen=true" @blur="delay(()=>{ cbModelOpen=false },120)" />
                      <div v-show="cbModelOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(cbForm.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="cbForm.model=m; cbModelOpen=false">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(cbForm.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cb.lite') }}</label>
                    <div class="pi-sel">
                      <input v-model="cbForm.liteModel" :placeholder="t('sync.pickModel')" @focus="cbLiteOpen=true" @click.stop="cbLiteOpen=true" @input="cbLiteOpen=true" @blur="delay(()=>{ cbLiteOpen=false },120)" />
                      <div v-show="cbLiteOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(cbForm.liteModel)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="cbForm.liteModel=m; cbLiteOpen=false">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(cbForm.liteModel).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cb.reasoning') }}</label>
                    <div class="pi-sel">
                      <input v-model="cbForm.reasoningModel" :placeholder="t('sync.pickModel')" @focus="cbReasonOpen=true" @click.stop="cbReasonOpen=true" @input="cbReasonOpen=true" @blur="delay(()=>{ cbReasonOpen=false },120)" />
                      <div v-show="cbReasonOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(cbForm.reasoningModel)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="cbForm.reasoningModel=m; cbReasonOpen=false">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(cbForm.reasoningModel).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="field-row fld-inline-row" style="margin-top:2px">
                  <div class="field fw-m"><label>{{ t('sync.cb.reasoningEffort') }}</label>
                    <select v-model="cbForm.reasoningEffort">
                      <option value="">{{ t('sync.opt.auto') }}</option>
                      <option value="low">{{ t('sync.opt.low') }}</option>
                      <option value="medium">{{ t('sync.opt.medium') }}</option>
                      <option value="high">{{ t('sync.opt.high') }}</option>
                      <option value="xhigh">{{ t('sync.opt.xhigh') }}</option>
                    </select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cb.alwaysThinking') }}</label>
                    <select v-model="cbForm.alwaysThinking">
                      <option value="">{{ t('sync.opt.default') }}</option>
                      <option value="true">{{ t('sync.opt.yes') }}</option>
                      <option value="false">{{ t('sync.opt.no') }}</option>
                    </select>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.cb.cleanupDays') }}</label><input type="number" min="1" v-model="cbForm.cleanupDays" placeholder="30" /></div>
                  <div class="field fw-s"><label>{{ t('sync.cb.coauth') }}</label>
                    <select v-model="cbForm.coauth">
                      <option value="">{{ t('sync.opt.default') }}</option>
                      <option value="true">{{ t('sync.opt.yes') }}</option>
                      <option value="false">{{ t('sync.opt.no') }}</option>
                    </select>
                  </div>
                  <div class="field fw-l"><label>{{ t('sync.cb.trustedDirs') }}</label>
                    <textarea v-model="cbForm.trustedDirsText" rows="1" class="perm-ta cb-ta-1line" @wheel="taWheelStop" placeholder="C:/path/proj"></textarea>
                  </div>
                </div>
              </section>

              <!-- ③ 沙箱（settings.json sandbox）：放在权限之前，权限配置统一收到页面最后 -->
              <div class="panel-head" :title="t('sync.cb.sandboxTip')">{{ t('sync.cb.sandboxPanel') }}</div>
              <section class="card cfg-card">
                <div class="field-row fld-inline-row cb-lab-fix">
                  <div class="field fw-m"><label>{{ t('sync.cb.sandboxEnabled') }}</label>
                    <select v-model="cbForm.sandboxEnabled">
                      <option value="">{{ t('sync.opt.default') }}</option>
                      <option value="true">{{ t('sync.opt.yes') }}</option>
                      <option value="false">{{ t('sync.opt.no') }}</option>
                    </select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cb.sandboxAuto') }}</label>
                    <select v-model="cbForm.autoAllowBash">
                      <option value="">{{ t('sync.opt.default') }}</option>
                      <option value="true">{{ t('sync.opt.yes') }}</option>
                      <option value="false">{{ t('sync.opt.no') }}</option>
                    </select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.cb.localBinding') }}</label>
                    <select v-model="cbForm.allowLocalBinding">
                      <option value="">{{ t('sync.opt.default') }}</option>
                      <option value="true">{{ t('sync.opt.yes') }}</option>
                      <option value="false">{{ t('sync.opt.no') }}</option>
                    </select>
                  </div>
                </div>
                <div class="field-row fld-inline-row cb-lab-fix" style="margin-top:6px">
                  <div class="field fw-l"><label>{{ t('sync.cb.excludedCmds') }}</label>
                    <textarea v-model="cbForm.excludedCmdsText" rows="1" class="perm-ta cb-ta-1line" @wheel="taWheelStop" placeholder="docker&#10;git"></textarea>
                  </div>
                  <div class="field fw-l"><label>{{ t('sync.cb.unixSockets') }}</label>
                    <textarea v-model="cbForm.unixSocketsText" rows="1" class="perm-ta cb-ta-1line" @wheel="taWheelStop" placeholder="/var/run/docker.sock"></textarea>
                  </div>
                </div>
              </section>

              <!-- ④ 权限配置：defaultMode 单选胶囊 + 「执行所有命令」三态三选（Bash 通配写盘）；
                   不提供逐条自定义命令规则（磁盘里既有的其它规则行原样保留写回） -->
              <div class="panel-head" :title="t('sync.cb.permHeadTip')">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <PermPanel v-model:mode="cbForm.defaultMode" v-model:rows="cbPermRows"
                  :modes="toModeOpts(CB_DEFAULT_MODES)" :row-placeholder="t('sync.permRulePh')" />
              </section>

            </template>

            <!-- opencode -->
            <OpencodePanel v-else-if="cfgTarget?.id==='opencode'" />

            <!-- mimocode -->
            <template v-else-if="cfgTarget?.id==='mimocode'">
              <div class="panel-head">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card cp-lab-left">
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.mimo.providerKey') }}</label><input v-model="mimoProviderKey" placeholder="llm" /></div>
                  <div class="field fw-m"><label>{{ t('sync.apiKey') }}</label><input v-model="mimoApiKey" :placeholder="t('sync.routeToken')" /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-m" style="flex:0 1 240px"><label>{{ t('sync.baseUrl') }}</label>
                    <input :value="derivedBaseUrl()" readonly />
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.mimo.defaultPrefix') }}</label><input :value="mimoProviderPrefix()" readonly /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.mimo.apiType') }}</label><input v-model="mimoApi" placeholder="openai" /></div>
                  <div class="field fw-m"><label>{{ t('sync.name') }}</label><input v-model="mimoName" placeholder="LLM Router" /></div>
                </div>
                <div class="field-row">
                  <div class="field fw-m"><label>{{ t('sync.mimo.timeout') }}</label><input v-model="mimoTimeout" placeholder="300000 / false" /></div>
                  <div class="field fw-m"><label>{{ t('sync.mimo.headerTimeout') }}</label><input v-model="mimoHeaderTimeout" placeholder="300000 / false" /></div>
                  <div class="field fw-m"><label>{{ t('sync.mimo.chunkTimeout') }}</label><input v-model="mimoChunkTimeout" placeholder="300000" /></div>
                  <div class="field fw-s"><label>{{ t('sync.mimo.setCacheKey') }}</label>
                    <select v-model="mimoSetCacheKey"><option value="">{{ t('sync.default') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                  </div>
                </div>
                <div class="field-row">
                  <div class="field fw-m" style="flex:0 1 240px"><label>{{ t('sync.model') }}</label>
                    <div class="pi-sel">
                      <input v-model="mimoModel" :placeholder="mimoQualifyModel('') || t('sync.pickModel')" @focus="mimoModelOpen=true" @click.stop="mimoModelOpen=true" @input="mimoModelOpen=true" @blur="delay(()=>{ mimoModelOpen=false },120)" />
                      <div v-show="mimoModelOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(String(mimoModel || '').split('/').pop())" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="mimoModel = mimoQualifyModel(m); mimoModelOpen = false">{{ mimoQualifyModel(m) }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(String(mimoModel || '').split('/').pop()).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field fw-s"><label>{{ t('sync.mimo.effort') }}</label>
                    <select v-model="mimoEffort">
                      <option value="">{{ t('sync.default') }}</option>
                      <option value="minimal">{{ t('sync.opt.minimal') }}</option>
                      <option value="low">{{ t('sync.opt.low') }}</option>
                      <option value="medium">{{ t('sync.opt.medium') }}</option>
                      <option value="high">{{ t('sync.opt.high') }}</option>
                      <option value="xhigh">xhigh</option>
                    </select>
                  </div>
                  <div class="field fw-m"><label>{{ t('sync.mimo.smallModel') }}</label>
                    <div class="pi-sel">
                      <input v-model="mimoSmallModel" :placeholder="mimoQualifyModel('') || t('sync.pickModel')" @focus="mimoSmallOpen=true" @click.stop="mimoSmallOpen=true" @input="mimoSmallOpen=true" @blur="delay(()=>{ mimoSmallOpen=false },120)" />
                      <div v-show="mimoSmallOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(String(mimoSmallModel || '').split('/').pop())" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="mimoSmallModel = mimoQualifyModel(m); mimoSmallOpen = false">{{ mimoQualifyModel(m) }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(String(mimoSmallModel || '').split('/').pop()).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                </div>
              </section>

              <div class="panel-head">{{ t('sync.oc.modelSet') }}</div>
              <section class="card cfg-card kmodel-wrap cp-lab-left">
                <div v-for="(row, i) in mimoModels" :key="row" class="mimo-model-card">
                  <div class="mimo-model-head">
                    <span class="mimo-model-idx">#{{ i + 1 }}</span>
                    <span class="mimo-model-title">{{ row.id || t('sync.pickModel') }}</span>
                    <button type="button" class="btn btn-xs btn-ghost row-del" @click="mimoModels.splice(i,1)">×</button>
                  </div>
                  <div class="mimo-model-body">
                    <div class="field-row">
                      <div class="field fw-m"><label>{{ t('sync.oc.displayName') }}</label><input v-model="row.name" /></div>
                      <div class="field fw-s"><label>{{ t('sync.mimo.effort') }}</label>
                        <select v-model="row.effort">
                          <option value="">{{ t('sync.default') }}</option>
                          <option value="minimal">{{ t('sync.opt.minimal') }}</option>
                          <option value="low">{{ t('sync.opt.low') }}</option>
                          <option value="medium">{{ t('sync.opt.medium') }}</option>
                          <option value="high">{{ t('sync.opt.high') }}</option>
                          <option value="xhigh">xhigh</option>
                        </select>
                      </div>
                      <div class="field fw-s"><label>{{ t('sync.mimo.reasoning') }}</label>
                        <select v-model="row.reasoning"><option value="">{{ t('sync.default') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                      </div>
                      <div class="field fw-s"><label>{{ t('sync.mimo.toolCall') }}</label>
                        <select v-model="row.toolCall"><option value="">{{ t('sync.default') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                      </div>
                      <div class="field fw-s"><label>{{ t('sync.mimo.attachment') }}</label>
                        <select v-model="row.attachment"><option value="">{{ t('sync.default') }}</option><option value="true">{{ t('sync.opt.true') }}</option><option value="false">{{ t('sync.opt.false') }}</option></select>
                      </div>
                    </div>
                    <div class="field-row">
                      <div class="field fw-s"><label>{{ t('sync.mimo.cacheTTL') }}</label>
                        <select v-model="row.cacheTTL"><option value="">{{ t('sync.default') }}</option><option value="5m">5m</option><option value="1h">1h</option></select>
                      </div>
                      <div class="field fw-s"><label>{{ t('sync.mimo.limitContext') }}</label><input v-model="row.limitContext" placeholder="128000" /></div>
                      <div class="field fw-s"><label>{{ t('sync.mimo.limitOutput') }}</label><input v-model="row.limitOutput" placeholder="8192" /></div>
                      <div class="field fw-m" style="flex:0 1 240px"><label>{{ t('sync.id') }}</label>
                        <div class="pi-sel">
                          <input v-model="row.id" :placeholder="t('sync.pickModel')" @focus="mimoModelRowOpen=i" @click.stop="mimoModelRowOpen=i" @input="mimoModelRowOpen=i" @blur="delay(()=>{ if(mimoModelRowOpen===i) mimoModelRowOpen=-1 },120)" />
                          <div v-show="mimoModelRowOpen===i" class="pi-sel-drop pi-sel-up">
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
                <button type="button" class="btn btn-xs btn-ghost" @click="mimoModels.push({ id: '', name: '', reasoning: '', toolCall: '', attachment: '', effort: '', limitContext: '', limitOutput: '', cacheTTL: '' })">{{ t('sync.addModel') }}</button>
              </section>

              <div class="panel-head" :title="t('sync.mimo.mcpTip')">{{ t('sync.mimo.mcpTitle') }}<span v-if="mimoMcpServerNames().length" class="cfg-head-count">{{ mimoMcpServerNames().length }}</span></div>
              <section class="card cfg-card">
                <div v-if="mimoMcpServerNames().length" class="cfg-mcp-list">
                  <span v-for="n in mimoMcpServerNames()" :key="n" class="cfg-mcp-chip">{{ n }}</span>
                </div>
                <div v-else class="cfg-mcp-empty">{{ t('sync.mimo.mcpEmpty') }}</div>
              </section>

              <div class="panel-head">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <!-- 单行权限条：权限模式预设 + 所有命令（bash）三态，竖线分隔 -->
                <div class="perm-bar">
                  <span class="perm-bar-label">{{ t('sync.mimo.permMode') }}</span>
                  <div class="perm-caps" role="radiogroup" :aria-label="t('sync.mimo.permMode')">
                    <button v-for="m in MIMO_PERM_MODES" :key="m.id" type="button" class="perm-cap" role="radio"
                      :class="permCapCls(mimoPermMode === m.id ? 'allow' : '')" :aria-checked="mimoPermMode === m.id"
                      @click="mimoPickMode(m.id)">
                      <span class="perm-cap-dot"></span>
                      <span class="perm-cap-text">{{ m.label() }}</span>
                    </button>
                  </div>
                  <span class="perm-bar-sep"></span>
                  <span class="perm-bar-label">{{ t('sync.mimo.allBash') }}</span>
                  <div class="perm-caps" role="radiogroup" :aria-label="t('sync.mimo.allBash')">
                    <button v-for="opt in MIMO_BASH_OPTS" :key="opt.v" type="button" class="perm-cap" role="radio"
                      :class="permCapCls(mimoBashMode === opt.v ? opt.v : '')" :aria-checked="mimoBashMode === opt.v"
                      @click="mimoPickBash(opt.v)">
                      <span class="perm-cap-dot"></span>
                      <span class="perm-cap-text">{{ t(opt.label) }}</span>
                    </button>
                  </div>
                </div>
              </section>
            </template>

            <!-- codex -->
            <template v-else-if="cfgTarget?.id==='codex'">
              <div class="panel-head" :title="t('sync.codex.authTip')">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <!-- 三行左标签横排：控件宽度按内容配比（该短则短该长则长），单行不换行 -->
                <div class="field-row fld-inline-row" style="margin-top:2px">
                  <div class="field" style="flex:270 1 0"><label>{{ t('sync.baseUrl') }}</label><input :value="derivedBaseUrl()" readonly /></div>
                  <div class="field" style="flex:100 1 0"><label>{{ t('sync.apiKey') }}</label><input v-model="codexForm.apiKey" :placeholder="t('sync.routeToken')" /></div>
                  <div class="field" style="flex:135 1 0"><label>{{ t('sync.envKey') }}</label><input v-model="codexForm.envKey" placeholder="LLMROUTER_API_KEY" /></div>
                </div>
                <div class="field-row fld-inline-row">
                  <div class="field" style="flex:110 1 0"><label>{{ t('sync.defaultModel') }}</label>
                    <div class="pi-sel">
                      <input v-model="codexForm.model" :placeholder="t('sync.pickModel')" @focus="codexModelOpen=true" @click.stop="codexModelOpen=true" @input="codexModelOpen=true" @blur="delay(()=>{ codexModelOpen=false },120)" />
                      <div v-show="codexModelOpen" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(codexForm.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="codexForm.model = m; codexModelOpen = false">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(codexForm.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field" style="flex:75 1 0"><label>{{ t('sync.codex.thinking') }}</label>
                    <select v-model="codexForm.reasoning">
                      <option value="">{{ t('sync.default') }}</option>
                      <option value="minimal">{{ t('sync.opt.minimal') }}</option>
                      <option value="low">{{ t('sync.opt.low') }}</option>
                      <option value="medium">{{ t('sync.opt.medium') }}</option>
                      <option value="high">{{ t('sync.opt.high') }}</option>
                    </select>
                  </div>
                  <div class="field" style="flex:60 1 0"><label>{{ t('sync.temperature') }}</label><input type="number" step="0.1" min="0" max="2" v-model="codexForm.temperature" :placeholder="t('sync.phExample', { v: '0.7' })" /></div>
                  <div class="field" style="flex:60 1 0"><label>{{ t('sync.topP') }}</label><input type="number" step="0.05" min="0" max="1" v-model="codexForm.topP" :placeholder="t('sync.phExample', { v: '1' })" /></div>
                  <div class="field" style="flex:85 1 0"><label>{{ t('sync.codex.providerLabel') }}</label><input v-model="codexForm.providerKey" placeholder="llmrouter" /></div>
                </div>
                <div class="field-row fld-inline-row">
                  <div class="field" style="flex:80 1 0"><label>{{ t('sync.codex.displayLabel') }}</label><input v-model="codexForm.name" placeholder="LLM Router" /></div>
                  <div class="field" style="flex:85 1 0"><label>{{ t('sync.codex.protocolLabel') }}</label>
                    <select v-model="codexForm.wireApi">
                      <option value="responses">responses</option>
                    </select>
                  </div>
                  <div class="field" style="flex:65 1 0"><label>{{ t('sync.codex.reqRetry') }}</label><input v-model="codexForm.reqRetry" inputmode="numeric" :placeholder="t('sync.phExample', { v: '4' })" /></div>
                  <div class="field" style="flex:65 1 0"><label>{{ t('sync.codex.streamRetry') }}</label><input v-model="codexForm.streamRetry" inputmode="numeric" :placeholder="t('sync.phExample', { v: '10' })" /></div>
                  <div class="field" style="flex:95 1 0"><label>{{ t('sync.codex.idleTimeout') }}</label><input v-model="codexForm.idleTimeout" inputmode="numeric" :placeholder="t('sync.phExample', { v: '300000' })" /></div>
                </div>
                <div class="field-row fld-inline-row">
                  <div class="field" style="flex:90 1 0"><label>{{ t('sync.codex.verbosity') }}</label>
                    <select v-model="codexForm.modelVerbosity">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="low">low</option>
                      <option value="medium">medium</option>
                      <option value="high">high</option>
                    </select>
                  </div>
                  <div class="field" style="flex:105 1 0"><label>{{ t('sync.codex.reasoningSummary') }}</label>
                    <select v-model="codexForm.reasoningSummary">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="auto">auto</option>
                      <option value="concise">concise</option>
                      <option value="detailed">detailed</option>
                    </select>
                  </div>
                  <div class="field" style="flex:105 1 0"><label>{{ t('sync.codex.reviewModel') }}</label><input v-model="codexForm.reviewModel" :placeholder="t('sync.pickModel')" /></div>
                  <div class="field" style="flex:85 1 0"><label>{{ t('sync.codex.contextWindow') }}</label><input v-model="codexForm.contextWindow" inputmode="numeric" :placeholder="t('sync.phExample', { v: '400000' })" /></div>
                  <div class="field" style="flex:95 1 0"><label>{{ t('sync.codex.autoCompact') }}</label><input v-model="codexForm.autoCompact" inputmode="numeric" :placeholder="t('sync.phExample', { v: '300000' })" /></div>
                </div>
                <!-- 模型映射：codex 内置客户端模型（键只读）→ 路由器真实模型（写入启用服务商 aliases，随保存落盘；
                     CLI /model 选中的名字经路由器按别名改写后转发到目标模型；留空 = 不映射） -->
                <div class="codex-map">
                  <div class="codex-map-head">
                    <span>{{ t('sync.codex.mapTitle') }}</span>
                  </div>
                  <div v-for="(r, mi) in codexMapRows" :key="'cxm-' + r.k" class="codex-map-row">
                    <input :value="r.k" class="codex-map-key" readonly :title="t('sync.codex.mapKeyRo')" />
                    <span class="codex-map-eq">=</span>
                    <div class="pi-sel codex-map-val">
                      <input :value="codexMapOpen===mi ? codexMapSearch : r.v" :placeholder="t('sync.pickModel')" @focus="codexMapFocusRow(mi)" @click.stop="codexMapFocusRow(mi)" @input="codexMapSetVal(mi, $event.target.value); codexMapSearch = $event.target.value; codexMapOpen = mi" @blur="delay(()=>{ if(codexMapOpen===mi) codexMapOpen=-1 },140)" />
                      <div v-show="codexMapOpen===mi" class="pi-sel-drop">
                        <div v-for="g in codexMapGroups()" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="codexMapPick(mi, m)">{{ m }}</button>
                        </div>
                        <div v-if="!codexMapGroups().length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                </div>
              </section>
              <div class="panel-head" :title="t('sync.codex.panelDisplayTip')">{{ t('sync.codex.panelDisplay') }}</div>
              <section class="card cfg-card">
                <div class="field-row fld-inline-row">
                  <div class="field" style="flex:110 1 0"><label>{{ t('sync.codex.tuiTheme') }}</label><input v-model="codexForm.tuiTheme" placeholder="dracula" /></div>
                  <div class="field" style="flex:150 1 0"><label>{{ t('sync.codex.tuiStatusLine') }}</label><input v-model="codexForm.tuiStatusLine" placeholder="model, tokens, cost, git-branch" /></div>
                  <div class="field" style="flex:110 1 0"><label>{{ t('sync.codex.tuiNotifications') }}</label><input v-model="codexForm.tuiNotifications" placeholder="true / agent-turn-complete" /></div>
                </div>
                <div class="field-row fld-inline-row">
                  <div class="field" style="flex:70 1 0"><label>{{ t('sync.codex.tuiAnim') }}</label>
                    <select v-model="codexForm.tuiAnim">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  </div>
                  <div class="field" style="flex:70 1 0"><label>{{ t('sync.codex.tuiTooltips') }}</label>
                    <select v-model="codexForm.tuiTooltips">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  </div>
                  <div class="field" style="flex:80 1 0"><label>{{ t('sync.codex.tuiAltScreen') }}</label>
                    <select v-model="codexForm.tuiAltScreen">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="auto">auto</option>
                      <option value="always">always</option>
                      <option value="never">never</option>
                    </select>
                  </div>
                  <div class="field" style="flex:80 1 0"><label>{{ t('sync.codex.tuiNotifyMethod') }}</label>
                    <select v-model="codexForm.tuiNotifyMethod">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="auto">auto</option>
                      <option value="osc9">osc9</option>
                      <option value="bel">bel</option>
                    </select>
                  </div>
                </div>
                <div class="field-row fld-inline-row">
                  <div class="field" style="flex:105 1 0"><label>{{ t('sync.codex.hideReasoning') }}</label>
                    <select v-model="codexForm.hideReasoning">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  </div>
                  <div class="field" style="flex:120 1 0"><label>{{ t('sync.codex.showRawReasoning') }}</label>
                    <select v-model="codexForm.showRawReasoning">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  </div>
                  <div class="field" style="flex:105 1 0"><label>{{ t('sync.codex.noPasteBurst') }}</label>
                    <select v-model="codexForm.noPasteBurst">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  </div>
                </div>
                <div class="field-row fld-inline-row">
                  <div class="field"><label>{{ t('sync.codex.disableResponseStorage') }}</label>
                    <select v-model="codexForm.disableResponseStorage">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  </div>
                  <div class="field"><label>{{ t('sync.codex.includeApplyPatch') }}</label>
                    <select v-model="codexForm.includeApplyPatch">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  </div>
                  <div class="field"><label>{{ t('sync.codex.preferredAuth') }}</label>
                    <select v-model="codexForm.preferredAuth">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="apikey">apikey</option>
                      <option value="chatgpt">chatgpt</option>
                    </select>
                  </div>
                </div>
              </section>
              <div class="panel-head" :title="t('sync.codex.panelSandboxTip')">{{ t('sync.codex.panelSandbox') }}</div>
              <section class="card cfg-card">
                <div class="field-row fld-inline-row">
                  <div class="field" style="flex:150 1 0"><label>{{ t('sync.codex.sandboxMode') }}</label>
                    <select v-model="codexForm.sandboxMode">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="read-only">read-only</option>
                      <option value="workspace-write">workspace-write</option>
                      <option value="danger-full-access">danger-full-access</option>
                    </select>
                  </div>
                  <div class="field" style="flex:85 1 0"><label>{{ t('sync.codex.wwNetwork') }}</label>
                    <select v-model="codexForm.wwNetwork">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  </div>
                  <div class="field" style="flex:85 1 0"><label>{{ t('sync.codex.webSearch') }}</label>
                    <select v-model="codexForm.webSearch">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  </div>
                  <div class="field" style="flex:85 1 0"><label>{{ t('sync.codex.fileOpener') }}</label><input v-model="codexForm.fileOpener" placeholder="vscode" /></div>
                </div>
              </section>
              <div class="panel-head" :title="t('sync.codex.panelInstructTip')">{{ t('sync.codex.panelInstruct') }}</div>
              <section class="card cfg-card">
                <div class="field-row fld-inline-row">
                  <div class="field fw-m" style="flex:180 1 0"><label>{{ t('sync.codex.devInstructions') }}</label>
                    <textarea v-model="codexForm.devInstructions" rows="2" class="perm-ta" @wheel="taWheelStop"></textarea>
                  </div>
                  <div class="field" style="flex:95 1 0"><label>{{ t('sync.codex.histPersistence') }}</label>
                    <select v-model="codexForm.histPersistence">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="save-all">save-all</option>
                      <option value="none">none</option>
                    </select>
                  </div>
                  <div class="field" style="flex:90 1 0"><label>{{ t('sync.codex.histMaxBytes') }}</label><input v-model="codexForm.histMaxBytes" inputmode="numeric" :placeholder="t('sync.phExample', { v: '52428800' })" /></div>
                </div>
                <div class="field-row fld-inline-row">
                  <div class="field" style="flex:150 1 0"><label>{{ t('sync.codex.notifyCmd') }}</label><input v-model="codexForm.notify" placeholder="notify-send, Codex" /></div>
                  <div class="field"><label>{{ t('sync.codex.projectDocMaxBytes') }}</label><input v-model="codexForm.projectDocMaxBytes" inputmode="numeric" placeholder="32768" /></div>
                  <div class="field"><label>{{ t('sync.codex.projectDocFallback') }}</label><input v-model="codexForm.projectDocFallback" placeholder="AGENTS.md, CLAUDE.md" /></div>
                </div>
              </section>
              <div class="panel-head" :title="t('sync.codex.permHeadTip')">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <PermPanel v-model:mode="codexForm.approvalPolicy" v-model:rows="codexPermRows"
                  :modes="toModeOpts(CX_PERM_MODES)" :row-placeholder="t('sync.permRulePh')" />
              </section>
            </template>

            <!-- commandcode -->
            <template v-else-if="cfgTarget?.id==='commandcode'">
              <div class="panel-head">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="cc-grid4 cc-lab-left">
                  <div class="field"><label>{{ t('sync.baseUrl') }}</label>
                    <input :value="derivedBaseUrl()" placeholder="http://127.0.0.1:3000/router" />
                  </div>
                  <div class="field"><label>{{ t('sync.cc.providerId') }}</label><input v-model="ccForm.endpointId" placeholder="llmrouter" /></div>
                  <div class="field"><label>{{ t('sync.protocol') }}</label>
                    <select v-model="ccForm.endpointApi">
                      <option value="openai-completions">{{ t('sync.cc.optOpenai') }}</option>
                      <option value="anthropic-messages">{{ t('sync.cc.optAnthropic') }}</option>
                    </select>
                  </div>
                  <div class="field"><label>{{ t('sync.apiKey') }}</label><input v-model="ccForm.endpointKey" placeholder="$COMMAND_CODE_API_KEY / {env:VAR} / !command" /></div>
                </div>
                <div class="cc-grid4 cc-lab-left">
                  <div class="field"><label>{{ t('sync.provider') }}</label>
                    <select v-model="ccForm.provider"><option v-for="p in ccProviderOptions" :key="p" :value="p">{{ p }}</option></select>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.defaultModel') }}</label>
                    <div class="pi-sel">
                      <input v-model="ccForm.model" :placeholder="t('sync.pickModel')" @focus="ccModelOpen=-2" @click.stop="ccModelOpen=-2" @input="ccModelOpen=-2" @blur="delay(()=>{ if(ccModelOpen===-2) ccModelOpen=-1 },120)" />
                      <div v-show="ccModelOpen===-2" class="pi-sel-drop">
                        <div v-for="g in modelGroupsByLetter(ccForm.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                          <div class="pi-sel-letter">{{ g.letter }}</div>
                          <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="ccForm.model=m; ccModelOpen=-1">{{ m }}</button>
                        </div>
                        <div v-if="!modelGroupsByLetter(ccForm.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.globalTier') }}</label>
                    <select v-model="ccForm.globalEffort"><option value="">{{ t('sync.none') }}</option><option v-for="o in CC_EFFORT_LEVELS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <div class="field">
                    <button type="button" class="cc-toggle cc-toggle-lab" :class="{ on: ccNoLogin }" :title="t('sync.cc.nologinTip')" @click="ccNoLogin = !ccNoLogin; ccToggleNoLogin()">{{ t('sync.noLogin') }} · {{ ccNoLogin ? t('sync.enabled') : t('sync.disabled') }}</button>
                    <span v-if="ccNoLoginMsg" class="cc-nologin-msg">{{ ccNoLoginMsg }}</span>
                  </div>
                </div>
                <!-- featureModels（config.json 官方键）：后台功能模型覆盖，空=删键恢复官方默认 -->
                <div class="cc-grid4 cc-lab-left">
                  <div class="field cc-span2"><label>{{ t('sync.cc.fmTitle') }}</label><input v-model="ccFeatModels.titleGeneration" :placeholder="t('sync.cc.fmDefault')" /></div>
                  <div class="field cc-span2"><label>{{ t('sync.cc.fmCompaction') }}</label><input v-model="ccFeatModels.compaction" :placeholder="t('sync.cc.fmDefault')" /></div>
                  <div class="field cc-span2"><label>{{ t('sync.cc.fmToolDesc') }}</label><input v-model="ccFeatModels.toolDescription" :placeholder="t('sync.cc.fmDefault')" /></div>
                  <div class="field cc-span2"><label>{{ t('sync.cc.fmTasteOn') }}</label><input v-model="ccFeatModels.tasteOnboarding" :placeholder="t('sync.cc.fmDefault')" /></div>
                  <div class="field cc-span2"><label>{{ t('sync.cc.fmTasteLearn') }}</label><input v-model="ccFeatModels.tasteLearning" :placeholder="t('sync.cc.fmDefault')" /></div>
                  <div class="field cc-span2"><label>{{ t('sync.cc.fmBranch') }}</label><input v-model="ccFeatModels.branchSummarization" :placeholder="t('sync.cc.fmDefault')" /></div>
                </div>
                <!-- 推理深度映射（reasoningEffort）：动态增删行区放面板底部；模型标识列吃富余、删除胶囊按内容收缩 -->
                <div v-for="(row, i) in ccForm.effortRows" :key="row" class="cc-grid4 cc-lab-left" style="grid-template-columns:minmax(0,2fr) minmax(0,1fr) auto">
                  <div class="field"><label>{{ t('sync.cc.modelIdLabel') }}</label>
                    <div class="pi-sel">
                      <input v-model="row.model" :placeholder="t('sync.pickModel')" @focus="ccEffortOpen=i" @click.stop="ccEffortOpen=i" @input="ccEffortOpen=i" @blur="delay(()=>{ if(ccEffortOpen===i) ccEffortOpen=-1 },120)" />
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
                  <button type="button" class="btn btn-xs btn-ghost row-del" @click="ccForm.effortRows.splice(i,1)">×</button>
                </div>
                <button type="button" class="btn btn-xs btn-ghost" style="align-self:flex-start" @click="ccForm.effortRows.push({ model: '', level: '' })">{{ t('sync.addMapping') }}</button>
              </section>

              <!-- ② 通用设置：config.json 偏好 + settings.json 行为开关（官方 settings 文档实用子集） -->
              <div class="panel-head">{{ t('sync.cc.generalPanel') }}</div>
              <section class="card cfg-card">
                <div class="cc-grid4 cc-lab-left">
                  <div class="field"><label>{{ t('sync.cc.theme') }}</label>
                    <select v-model="ccForm.theme"><option value="">{{ t('sync.opt.default') }}</option><option v-for="o in CC_THEMES" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.compactMode') }}</label>
                    <select v-model="ccForm.compactMode"><option value="">{{ t('sync.opt.default') }}</option><option v-for="o in CC_COMPACT_MODES" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.collapsePaste') }}</label>
                    <select v-model="ccForm.collapsePastedText"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.tasteLearning') }}</label>
                    <select v-model="ccForm.tasteLearning"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                  </div>
                </div>
                <div class="cc-grid4 cc-lab-left">
                  <div class="field"><label>{{ t('sync.cc.treeFilter') }}</label>
                    <select v-model="ccForm.treeFilterMode"><option value="">{{ t('sync.opt.default') }}</option><option v-for="o in CC_TREE_FILTERS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.exportFormat') }}</label>
                    <select v-model="ccForm.exportFormat"><option value="">{{ t('sync.opt.default') }}</option><option v-for="o in CC_EXPORT_FORMATS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.noScratchpad') }}</label>
                    <select v-model="ccForm.disableScratchpad"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.noSkillShell') }}</label>
                    <select v-model="ccForm.disableSkillShell"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                  </div>
                </div>
                <div class="cc-grid4 cc-lab-left">
                  <div class="field cc-span2"><label>{{ t('sync.cc.attr') }}</label>
                    <input v-model="ccForm.attribution" :placeholder="t('sync.cc.attrPh')" />
                  </div>
                  <div class="field"><label>{{ t('sync.cc.ideCtx') }}</label>
                    <select v-model="ccForm.ideContext"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                  </div>
                  <div class="field"><label>{{ t('sync.cc.autoExt') }}</label>
                    <select v-model="ccForm.autoInstallExt"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                  </div>
                </div>
              </section>

              <div class="panel-head">{{ t('sync.panelPerm') }}</div>
              <section class="card cfg-card">
                <PermPanel v-model:mode="ccForm.permMode" v-model:rows="ccPermRows"
                  :modes="toModeOpts(CC_PM_OPTIONS)" :row-placeholder="t('sync.permRulePh')">
                  <template #extra>
                    <div class="field">
                      <label :title="t('sync.cc.disableBypassTip')">{{ t('sync.cc.disableBypass') }}</label>
                      <select v-model="ccForm.disableBypass">
                        <option value="false">{{ t('sync.opt.no') }}</option>
                        <option value="true">{{ t('sync.opt.yes') }}</option>
                      </select>
                    </div>
                  </template>
                </PermPanel>
              </section>
            </template>

            <!-- kimi -->
            <template v-else-if="cfgTarget?.id==='kimi'">
              <div class="panel-head" :title="t('sync.kimi.tip')">{{ t('sync.panelModel') }}</div>
              <section class="card cfg-card">
                <div class="field"><label :title="t('sync.kimi.defaultTip')">{{ t('sync.defaultModel') }}</label>
                  <select v-model="kimiForm.defaultModel">
                    <option value="">{{ t('sync.none') }}</option>
                    <option v-for="a in kimiDefaultCandidates()" :key="a" :value="a">{{ a }}</option>
                  </select>
                </div>
              </section>

              <div class="panel-head" :title="t('sync.kimi.providerTip')">{{ t('sync.provider') }}</div>
              <section class="card cfg-card">
                <div v-for="(row, i) in kimiProviderRows" :key="row.kbk || ('kbp#' + i)" class="pi-model-block">
                  <div class="pi-model-head">
                    <strong>#{{ i + 1 }}</strong>
                    <button type="button" class="btn btn-xs btn-ghost row-del" @click="kimiProviderRows.splice(i,1)">{{ t('sync.delete') }}</button>
                  </div>
                  <div class="field-row">
                    <div class="field fw-m"><label>{{ t('sync.name') }}</label><input v-model="row.key" placeholder="llm-router" /></div>
                    <div class="field fw-m"><label :title="t('sync.kimi.providerTypeTip')">{{ t('sync.providerType') }}</label>
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
              </section>

              <div class="panel-head" :title="t('sync.kimi.modelTip')">{{ t('sync.model') }}</div>
              <section class="card cfg-card">
                <div v-for="(row, i) in kimiModelRows" :key="row.kbk || ('kbm#' + i)" class="pi-model-block">
                  <div class="pi-model-head">
                    <strong>#{{ i + 1 }}</strong>
                    <button type="button" class="btn btn-xs btn-ghost row-del" @click="kimiModelRows.splice(i,1)">{{ t('sync.delete') }}</button>
                  </div>
                  <div class="field-row">
                    <div class="field fw-m"><label :title="t('sync.kimi.aliasTip')">{{ t('sync.kimi.modelAlias') }}</label><input v-model="row.alias" placeholder="llm" /></div>
                    <div class="field fw-m"><label>{{ t('sync.provider') }}</label>
                      <select v-model="row.provider">
                        <option value=""></option>
                        <option v-for="p in kimiProviderRows" :key="p.key" :value="p.key">{{ p.key }}</option>
                      </select>
                    </div>
                    <div class="field fw-s"><label>{{ t('sync.kimi.contextWindow') }}</label><input type="number" min="1" v-model="row.context" placeholder="128000" /></div>
                  </div>
                  <div class="field-row">
                    <div class="field fw-l"><label :title="t('sync.kimi.upstreamTip')">{{ t('sync.grok.upstreamModel') }}</label>
                      <div class="pi-sel">
                        <input v-model="row.model" :placeholder="t('sync.pickModel')" @focus="kimiModelOpen=i" @click.stop="kimiModelOpen=i" @input="kimiModelOpen=i" @blur="delay(()=>{ if(kimiModelOpen===i) kimiModelOpen=-1 },120)" />
                        <div v-show="kimiModelOpen===i" class="pi-sel-drop">
                          <div v-for="g in modelGroupsByLetter(row.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                            <div class="pi-sel-letter">{{ g.letter }}</div>
                            <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="row.model=m; kimiModelOpen=-1">{{ m }}</button>
                          </div>
                          <div v-if="!modelGroupsByLetter(row.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                        </div>
                      </div>
                    </div>
                    <div class="field fw-m"><label :title="t('sync.kimi.modelEffortTip')">{{ t('sync.kimi.modelEffort') }}</label>
                      <select v-model="row.effort">
                        <option value="">{{ t('sync.default') }}</option>
                        <option v-for="o in KIMI_EFFORT_LEVELS" :key="o" :value="o">{{ optTxt(o) }}</option>
                      </select>
                    </div>
                    <div class="field fw-m"><label :title="t('sync.kimi.displayTip')">{{ t('sync.displayName') }}</label><input v-model="row.display" :placeholder="t('sync.kimi.displayPh')" /></div>
                  </div>
                </div>
                <button type="button" class="btn btn-xs btn-ghost" style="align-self:flex-start" @click="kimiAddModel()">{{ t('sync.addModel') }}</button>
              </section>

              <div class="panel-head" :title="t('sync.kimi.thinkingTip')">{{ t('sync.kimi.thinkingPanel') }}</div>
              <section class="card cfg-card">
                <div class="field-row">
                  <div class="field fw-s"><label :title="t('sync.kimi.thinkingEnabledTip')">{{ t('sync.kimi.thinkingEnabled') }}</label>
                    <select v-model="kimiThinking.enabled">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="true">{{ t('sync.opt.true') }}</option>
                      <option value="false">{{ t('sync.opt.false') }}</option>
                    </select>
                  </div>
                  <div class="field fw-s"><label :title="t('sync.kimi.thinkingEffortTip')">{{ t('sync.kimi.thinkingEffort') }}</label>
                    <select v-model="kimiThinking.effort">
                      <option value="">{{ t('sync.default') }}</option>
                      <option v-for="o in KIMI_EFFORT_LEVELS" :key="o" :value="o">{{ optTxt(o) }}</option>
                    </select>
                  </div>
                  <div class="field fw-s"><label :title="t('sync.kimi.thinkingKeepTip')">{{ t('sync.kimi.thinkingKeep') }}</label>
                    <select v-model="kimiThinking.keep">
                      <option value="">{{ t('sync.notWrite') }}</option>
                      <option value="all">{{ t('sync.opt.all') }}</option>
                      <option value="off">{{ t('sync.opt.off') }}</option>
                    </select>
                  </div>
                </div>
              </section>

              <div class="panel-head">{{ t('sync.kimi.permPanel') }}</div>
              <section class="card cfg-card">
                <PermPanel v-model:mode="kimiForm.permMode" v-model:rows="kimiPermRows"
                  text-key="pattern" mode-key="decision"
                  :modes="toModeOpts(KIMI_PERM_MODES)" :row-placeholder="t('sync.kimi.permPatternPh')" />
              </section>
            </template>
            <!-- deepcode -->
            <DeepcodePanel v-else-if="cfgTarget?.id==='deepcode'" />
            <template v-else>
              <div class="panel-head">{{ t('sync.panelModel') }}</div>
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

              <div class="panel-head">{{ t('sync.panelPerm') }}</div>
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

          <!-- OpenCode AGENTS.md 源码：~/.config/opencode/AGENTS.md -->
          <div v-else-if="cfgMode==='openagents'" class="cfg-src">
            <CodeArea v-model="ocAgentsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + ocAgentsFile" @click="revealFile(ocAgentsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(ocAgentsFile) }}</button></div>
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

          <!-- Copilot CLI「配置选项」BYOK：写入/清除用户环境变量 + 通用设置 + 权限配置（接入本路由器） -->
          <div v-else-if="cfgMode==='copilotenv'" class="cfg-form cp-lab-left">
            <!-- ① 接入配置：标题在卡片外（与 codebuddy 等客户端统一），写入/清除按钮行在卡片首行 -->
            <div class="panel-head" :title="t('sync.copilot.tip')">{{ t('sync.copilot.envPanel') }}</div>
            <section class="card cfg-card">
              <div class="field-row" style="align-items:flex-end">
                <!-- 写入/清除合二为一：未接入显示绿色「写入」，已接入（检测到端点环境变量）显示「清除」，点击执行对应操作；与首行字段同行显示 -->
                <div class="field fw-l"><label>{{ t('sync.copilot.lBaseUrl') }}</label>
                  <div class="ro-url"><input :value="copilotRouterUrl()" readonly /></div>
                </div>
                <div class="field fw-m"><label>{{ t('sync.copilot.lType') }}</label>
                  <div class="ro-url"><input value="openai" readonly /></div>
                </div>
                <button v-if="!copilotBase" type="button" class="btn btn-green btn-sm" style="flex:0 0 auto" :disabled="copilotSaving" @click="copilotCleared = false; writeCopilotEnv()">{{ copilotSaving ? '…' : t('sync.copilot.write') }}</button>
                <button v-else type="button" class="btn btn-sm btn-ghost" style="flex:0 0 auto" :disabled="copilotSaving" @click="copilotClearAsk()">{{ t('sync.copilot.clear') }}</button>
              </div>
              <div class="field-row">
                <div class="field fw-l"><label>{{ t('sync.copilot.lModel') }}</label>
                  <div class="pi-sel">
                    <input v-model="copilotModel" :placeholder="t('sync.copilot.modelPh')" @focus="copilotModelOpen=true" @click.stop="copilotModelOpen=true" @input="copilotModelOpen=true" @blur="delay(()=>{ copilotModelOpen=false },120)" />
                    <div v-show="copilotModelOpen" class="pi-sel-drop">
                      <div v-for="g in modelGroupsByLetter(copilotModel)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                        <div class="pi-sel-letter">{{ g.letter }}</div>
                        <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="copilotModel=m; copilotModelOpen=false">{{ m }}</button>
                      </div>
                      <div v-if="!modelGroupsByLetter(copilotModel).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                    </div>
                  </div>
                </div>
                <div class="field fw-l"><label>{{ t('sync.copilot.lApiKey') }}</label>
                  <input v-model="copilotApiKey" :placeholder="t('sync.routeToken')" />
                </div>
                <div class="field fw-m"><label :title="t('sync.copilot.effortTip')">{{ t('sync.copilot.lEffort') }}</label>
                  <select v-model="copilotEffort">
                    <option value="">{{ t('sync.notWrite') }}</option>
                    <option v-for="o in COPILOT_EFFORT_OPTIONS" :key="o" :value="o">{{ optTxt(o) }}</option>
                  </select>
                </div>
              </div>
            </section>

            <!-- ② 通用设置（settings.json 官方偏好键）：空=删键回官方默认 -->
            <div class="panel-head">{{ t('sync.copilot.generalPanel') }}</div>
            <section class="card cfg-card">
              <div class="cc-grid4 cc-lab-left">
                <div class="field"><label>{{ t('sync.copilot.gTheme') }}</label>
                  <select v-model="copilotGeneral.theme"><option value="">{{ t('sync.opt.default') }}</option><option v-for="o in COPILOT_THEMES" :key="o" :value="o">{{ o }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gBanner') }}</label>
                  <select v-model="copilotGeneral.banner"><option value="">{{ t('sync.opt.default') }}</option><option v-for="o in COPILOT_BANNERS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gKeepAlive') }}</label>
                  <select v-model="copilotGeneral.keepAlive"><option value="">{{ t('sync.opt.default') }}</option><option v-for="o in COPILOT_KEEPALIVE" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gChannel') }}</label>
                  <select v-model="copilotGeneral.autoUpdatesChannel"><option value="">{{ t('sync.opt.default') }}</option><option v-for="o in COPILOT_CHANNELS" :key="o" :value="o">{{ optTxt(o) }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gStream') }}</label>
                  <select v-model="copilotGeneral.stream"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gCompact') }}</label>
                  <select v-model="copilotGeneral.compactPaste"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gNotif') }}</label>
                  <select v-model="copilotGeneral.notifications"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gBeep') }}</label>
                  <select v-model="copilotGeneral.beep"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gAutoUpdate') }}</label>
                  <select v-model="copilotGeneral.autoUpdate"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gMemory') }}</label>
                  <select v-model="copilotGeneral.memory"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gAskUser') }}</label>
                  <select v-model="copilotGeneral.askUser"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gCoAuthored') }}</label>
                  <select v-model="copilotGeneral.includeCoAuthoredBy"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gContAuto') }}</label>
                  <select v-model="copilotGeneral.continueOnAutoMode"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gExperimental') }}</label>
                  <select v-model="copilotGeneral.experimental"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gMouse') }}</label>
                  <select v-model="copilotGeneral.mouse"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gRenderMd') }}</label>
                  <select v-model="copilotGeneral.renderMarkdown"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gTitle') }}</label>
                  <select v-model="copilotGeneral.updateTerminalTitle"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gProgress') }}</label>
                  <select v-model="copilotGeneral.terminalProgress"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gIdeConn') }}</label>
                  <select v-model="copilotGeneral.ideAutoConnect"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
                <div class="field"><label>{{ t('sync.copilot.gIdeDiff') }}</label>
                  <select v-model="copilotGeneral.ideOpenDiff"><option value="">{{ t('sync.opt.default') }}</option><option value="true">{{ t('sync.opt.yes') }}</option><option value="false">{{ t('sync.opt.no') }}</option></select>
                </div>
              </div>
            </section>

            <!-- ③ 权限配置：按位置持久化批准（permissions-config.json 仅支持 allow）+ 提权限制 + 沙箱 + URL 白/黑名单（均写 settings.json） -->
            <div class="panel-head" :title="t('sync.copilot.permPanelTip') + t('sync.copilot.permLocAutoTip') + t('sync.copilot.sandboxTip') + t('sync.copilot.sandboxBypassTip') + t('sync.copilot.allowLocalTip') + t('sync.copilot.urlsTip')">{{ t('sync.panelPerm') }}</div>
            <section class="card cfg-card">
              <div class="perm-inline perm-bar cc-perm-bar co-perm-bar">
                <div class="agy-perm-col co-loc-col">
                  <span class="agy-perm-name" :title="t('sync.copilot.permLocAutoTip')">{{ t('sync.copilot.permLoc') }}</span>
                  <div class="co-loc-ctl">
                    <button type="button" class="co-loc-btn" :class="{ on: copilotLocOpen }" :title="t('sync.copilot.permLocAutoTip') + (copilotLocCands.length ? '（' + copilotLocCands.length + '）' : '')" @mousedown.prevent @click="copilotLocOpen=!copilotLocOpen">
                      <svg viewBox="0 0 24 24" width="13" height="13" aria-hidden="true"><path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" fill="none" stroke="currentColor" stroke-width="1.7"/></svg>
                    </button>
                    <span v-if="copilotPermLoc" class="co-loc-cur" :title="copilotPermLoc">{{ copilotPermLoc.split('\\').pop() }}</span>
                    <div v-show="copilotLocOpen" class="pi-sel-drop pi-sel-up loc-opt co-loc-pop">
                      <button v-for="p in copilotLocFiltered()" :key="p" type="button" :class="{ sel: p === copilotPermLoc }" :title="p" @mousedown.prevent="copilotPermLoc=p; copilotLocOpen=false">{{ p }}</button>
                      <div v-if="!copilotLocFiltered().length" class="pi-sel-empty">{{ t('sync.copilot.permLocPh') }}</div>
                    </div>
                  </div>
                </div>
                <span class="agy-perm-sep" aria-hidden="true"></span>
                <div class="agy-perm-col">
                  <span class="agy-perm-name" :title="t('sync.permMode')">{{ t('sync.permMode') }}</span>
                  <div class="perm-mode" role="radiogroup" :aria-label="t('sync.permMode')">
                    <label v-for="o in CO_PERM_MODES" :key="o.v" class="perm-mode-card"
                      :class="[pmModeCls(o.cls), { on: copilotPermMode() === o.v }]" :title="o.desc()">
                      <input type="radio" name="coPermMode" :value="o.v" :checked="copilotPermMode() === o.v" @click="copilotPermSetMode(o.v)" />
                      <span class="pm-name">{{ o.name() }}</span>
                    </label>
                  </div>
                </div>
                <span class="agy-perm-sep" aria-hidden="true"></span>
                <div class="agy-perm-col">
                  <span class="agy-perm-name" :title="t('sync.copilot.lBypass')">{{ t('sync.copilot.lBypass') }}</span>
                  <div class="agy-perm-box" role="radiogroup" :aria-label="t('sync.copilot.lBypass')">
                    <label v-for="m in CO_BYPASS_MODES" :key="m.v" class="agy-box-opt" :title="t(m.label)">
                      <input type="radio" name="coBypass" :value="m.v" :checked="copilotBypass === m.v" @click="copilotBypass = (copilotBypass === m.v ? '' : m.v)" />
                      <span>{{ t(m.label) }}</span>
                    </label>
                  </div>
                </div>
              </div>
              <!-- URL 白/黑名单 + OS 沙箱三项（均写 settings.json，空 = 移除键）：URL 域为紧凑按钮，点击弹上拉小提示框编辑 -->
              <div class="field-row" style="margin-top:8px;align-items:flex-end">
                <div class="field fw-s"><label>{{ t('sync.copilot.lAllowUrls') }}</label>
                  <div class="url-pop-wrap">
                    <button type="button" class="btn btn-sm url-pop-btn" :class="{ on: copilotAllowOpen }" :title="t('sync.copilot.urlsTip')" @mousedown.prevent @click="copilotAllowOpen=!copilotAllowOpen; copilotDenyOpen=false">
                      {{ t('sync.copilot.urlEntries', { n: copilotAllowCount }) }} <span class="url-pop-caret">▲</span>
                    </button>
                    <div v-show="copilotAllowOpen" class="url-pop pi-sel-up">
                      <textarea v-model="copilotAllow" rows="5" :placeholder="'https://api.example.com\n*.github.com'" @wheel="taWheelStop" @blur="delay(()=>{ copilotAllowOpen=false },120)"></textarea>
                      <div class="url-pop-tip">{{ t('sync.copilot.urlsTip') }}</div>
                    </div>
                  </div>
                </div>
                <div class="field fw-s"><label>{{ t('sync.copilot.lDenyUrls') }}</label>
                  <div class="url-pop-wrap">
                    <button type="button" class="btn btn-sm url-pop-btn" :class="{ on: copilotDenyOpen }" :title="t('sync.copilot.urlsTip')" @mousedown.prevent @click="copilotDenyOpen=!copilotDenyOpen; copilotAllowOpen=false">
                      {{ t('sync.copilot.urlEntries', { n: copilotDenyCount }) }} <span class="url-pop-caret">▲</span>
                    </button>
                    <div v-show="copilotDenyOpen" class="url-pop pi-sel-up">
                      <textarea v-model="copilotDeny" rows="5" :placeholder="'https://evil.example.com'" @wheel="taWheelStop" @blur="delay(()=>{ copilotDenyOpen=false },120)"></textarea>
                      <div class="url-pop-tip">{{ t('sync.copilot.urlsTip') }}</div>
                    </div>
                  </div>
                </div>
                <div class="field fw-s"><label>{{ t('sync.copilot.lSandbox') }}</label>
                  <div class="pi-sel">
                    <input readonly :value="coSbTxt(copilotSandboxEna)" @focus="coSbOpen=1" @click.stop="coSbOpen = coSbOpen===1 ? 0 : 1" @blur="delay(()=>{ if(coSbOpen===1) coSbOpen=0 },120)" />
                    <div v-show="coSbOpen===1" class="pi-sel-drop pi-sel-up">
                      <button type="button" :class="{on: copilotSandboxEna===''}" @mousedown.prevent="copilotSandboxEna=''; coSbOpen=0">{{ t('sync.notWrite') }}</button>
                      <button type="button" :class="{on: copilotSandboxEna==='true'}" @mousedown.prevent="copilotSandboxEna='true'; coSbOpen=0">{{ t('sync.opt.true') }}</button>
                      <button type="button" :class="{on: copilotSandboxEna==='false'}" @mousedown.prevent="copilotSandboxEna='false'; coSbOpen=0">{{ t('sync.opt.false') }}</button>
                    </div>
                  </div>
                </div>
                <div class="field fw-s"><label>{{ t('sync.copilot.lSandboxBypass') }}</label>
                  <div class="pi-sel">
                    <input readonly :value="coSbTxt(copilotSandboxBypass)" @focus="coSbOpen=2" @click.stop="coSbOpen = coSbOpen===2 ? 0 : 2" @blur="delay(()=>{ if(coSbOpen===2) coSbOpen=0 },120)" />
                    <div v-show="coSbOpen===2" class="pi-sel-drop pi-sel-up">
                      <button type="button" :class="{on: copilotSandboxBypass===''}" @mousedown.prevent="copilotSandboxBypass=''; coSbOpen=0">{{ t('sync.notWrite') }}</button>
                      <button type="button" :class="{on: copilotSandboxBypass==='true'}" @mousedown.prevent="copilotSandboxBypass='true'; coSbOpen=0">{{ t('sync.opt.true') }}</button>
                      <button type="button" :class="{on: copilotSandboxBypass==='false'}" @mousedown.prevent="copilotSandboxBypass='false'; coSbOpen=0">{{ t('sync.opt.false') }}</button>
                    </div>
                  </div>
                </div>
                <div class="field fw-s"><label>{{ t('sync.copilot.lSandboxLocal') }}</label>
                  <div class="pi-sel">
                    <input readonly :value="coSbTxt(copilotSandboxNet)" @focus="coSbOpen=3" @click.stop="coSbOpen = coSbOpen===3 ? 0 : 3" @blur="delay(()=>{ if(coSbOpen===3) coSbOpen=0 },120)" />
                    <div v-show="coSbOpen===3" class="pi-sel-drop pi-sel-up">
                      <button type="button" :class="{on: copilotSandboxNet===''}" @mousedown.prevent="copilotSandboxNet=''; coSbOpen=0">{{ t('sync.notWrite') }}</button>
                      <button type="button" :class="{on: copilotSandboxNet==='true'}" @mousedown.prevent="copilotSandboxNet='true'; coSbOpen=0">{{ t('sync.opt.true') }}</button>
                      <button type="button" :class="{on: copilotSandboxNet==='false'}" @mousedown.prevent="copilotSandboxNet='false'; coSbOpen=0">{{ t('sync.opt.false') }}</button>
                    </div>
                  </div>
                </div>
              </div>
            </section>
          </div>

          <!-- Copilot CLI 工具审批权限源码：~/.copilot/permissions-config.json（源码直编，保留未知键） -->
          <div v-else-if="cfgMode==='copilotperm'" class="cfg-src">
            <CodeArea v-model="copilotPermContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + copilotPermFile" @click="revealFile(copilotPermFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(copilotPermFile) }}</button></div>
          </div>

          <!-- Antigravity CLI 全局 MCP 源码：~/.gemini/config/mcp_config.json -->
          <div v-else-if="cfgMode==='agymcp'" class="cfg-src">
            <CodeArea v-model="agyMcpContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + agyMcpFile" @click="revealFile(agyMcpFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(agyMcpFile) }}</button></div>
          </div>

          <!-- CodeBuddy models.json 源码：~/.codebuddy/models.json -->
          <div v-else-if="cfgMode==='cbmodels'" class="cfg-src">
            <CodeArea v-model="cbModelsContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + cbModelsFile" @click="revealFile(cbModelsFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(cbModelsFile) }}</button></div>
          </div>

          <!-- CodeBuddy mcp.json 源码：~/.codebuddy/mcp.json -->
          <div v-else-if="cfgMode==='cbmcp'" class="cfg-src">
            <CodeArea v-model="cbMcpContent" />
            <div class="cfg-src-bar"><button type="button" class="cfg-file-btn" :title="t('sync.openFolder') + cbMcpFile" @click="revealFile(cbMcpFile)"><svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M14 3v4h4" fill="none" stroke="currentColor" stroke-width="1.8"/></svg>{{ srcBarName(cbMcpFile) }}</button></div>
          </div>
        </div>
      </div>
    </div>
    </Transition>

    <!-- 以下其余弹层 Teleport 到 body：深色主题 .panel 带 backdrop-filter，会成为 fixed 子元素
         的包含块，留在面板内时遮罩只覆盖面板、弹窗按面板居中；脱离面板才覆盖整个窗口 -->
    <Teleport to="body">
    <!-- 一键更新确认：更新会覆盖本地已修改的配置文件 -->
    <div v-if="cliUpdateAllOpen" class="dialog-overlay open" @click.self="cliUpdateAllOpen = false">
      <div class="dialog" style="width:340px">
        <h3>{{ t('sync.cli.updateAllConfirmTitle') }}</h3>
        <p class="delete-desc">{{ t('sync.cli.updateAllConfirmDesc', { n: cliOutdatedList.length }) }}</p>
        <div class="dialog-foot">
          <button class="btn btn-ghost btn-sm" type="button" @click="cliUpdateAllOpen = false">{{ t('dlg.cancel') }}</button>
          <div class="right">
            <button class="btn btn-sm" type="button" @click="doCliUpdateAll">{{ t('dlg.confirm') }}</button>
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

    <!-- 选择系统终端启动客户端 CLI -->
    <div v-if="cliTermPicker" class="dialog-overlay open" @click.self="cliTermPicker = null">
      <div class="dialog" style="width:340px">
        <h3>{{ t('sync.term.pickTitle', { name: cliTermPicker.target.name }) }}</h3>
        <p class="delete-desc">{{ t('sync.term.pickDesc') }}</p>
        <div class="cli-term-list">
          <button v-for="term in cliTermPicker.terminals" :key="term.id" class="cli-term-item" type="button"
            @click="cliLaunchInTerm(term)">
            <CommandLineIcon class="ico" /><span>{{ termName(term) }}</span>
          </button>
        </div>
        <div class="dialog-foot">
          <button class="btn btn-ghost btn-sm" type="button" @click="cliTermPicker = null">{{ t('dlg.cancel') }}</button>
        </div>
      </div>
    </div>

    <!-- 卸载客户端 CLI 确认：不可逆，分「仅卸载 CLI」与「卸载并清除全部残留」两档 -->
    <div v-if="cliUninstallTarget" class="dialog-overlay open" @click.self="cliUninstallTarget = null">
      <div class="dialog" style="width:380px">
        <h3>{{ t('sync.cli.uninstallTitle', { name: cliUninstallTarget.name }) }}</h3>
        <p class="delete-desc">{{ t('sync.cli.uninstallDesc') }}</p>
        <p class="delete-desc cli-cmd">{{ cliUninCmdText(cliUninstallTarget) }}</p>
        <p class="delete-desc cli-note">{{ t('sync.cli.uninstallPurgeTip') }}</p>
        <div class="dialog-foot">
          <button class="btn btn-ghost btn-sm" type="button" @click="cliUninstallTarget = null">{{ t('dlg.cancel') }}</button>
          <div class="right">
            <button class="btn btn-sm" type="button" :disabled="!!cliBusy[cliUninstallTarget.id]" @click="cliRunUninstall(false)">{{ t('sync.cli.uninstallOnly') }}</button>
            <button class="btn btn-danger btn-sm" type="button" :disabled="!!cliBusy[cliUninstallTarget.id]" @click="cliRunUninstall(true)">{{ t('sync.cli.uninstallPurge') }}</button>
          </div>
        </div>
      </div>
    </div>

    </Teleport>
  </div>
</template>

<style scoped>
/* 临时对照：恢复 scoped：各客户端面板已拆成子组件（sync/panels/*.vue），scoped 只会把
   属性选择器加到本组件元素上，子组件内部拿不到 .field / .cfg-card / .perm-cap
   这些表单样式。选择器均以 cfg-/cli-/sync-/pi-/perm- 前缀为主，唯一通用项
   .panel-head 与其它页面的同名 scoped 规则不冲突（对方属性选择器优先级更高）。 */
/* ===== 客户端卡片网格 ===== */
.sync-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:10px}
/* 分组标题：跨整行的小字 + 一条延伸到底的细线，把「需要处理 / 已是最新 / 未接入」分开 */
.sync-group-head{grid-column:1/-1;display:flex;align-items:center;gap:8px;margin:2px 0 -3px;font-size:10px;color:var(--t3);letter-spacing:.04em}
/* 「需要处理」分组的标题行可点击：展开/收起未安装客户端卡片，收起时只留这一行 */
.sync-group-head.tog{cursor:pointer;user-select:none;transition:color .12s}
.sync-group-head.tog:hover{color:var(--text)}
.sc-grp-caret{width:12px;height:12px;flex:none}
.sc-grp-count{flex:none;padding:0 7px;border:1px solid var(--border);border-radius:999px;font-size:9px;line-height:14px;color:var(--t3)}
.sync-group-head::after{content:'';flex:1 1 auto;height:1px;background:var(--border)}
/* 安装/更新中：卡片背景水波 + 水位随进度上涨（--sc-lvl，由卡片 style 注入）。
   后端 InstallClientCLI 阻塞返回、npm 非 TTY 无真实百分比 → 水位由前端按该客户端
   上次真实耗时估算缓升（指数缓出），结束立即满格。
   动效：水体是一层「远大于卡片的圆角面」，它的上边即水面；对该面做 rotate，
   水面就会持续起伏并向一侧流动（纯 transform，走合成层，不重排不重绘）。
   两层错相 + 反向 → 波峰交错，动感明显；不用 box-shadow 发光（大面模糊阴影太贵）。 */
.sync-card.sc-busy{border-color:var(--green);box-shadow:inset 0 0 0 1px var(--green-ring)}
/* 安装/更新进度：卡片背景即进度条——绿色横向渐进填充（淡底）+ 底边实心进度条，
   宽度（--sc-lvl）按上次真实耗时估算缓升，结束立即满格 */
.sc-progress{position:absolute;inset:0;pointer-events:none;border-radius:inherit;overflow:hidden}
.sc-progress::before{content:'';position:absolute;left:0;top:0;bottom:0;width:var(--sc-lvl,0%);background:var(--green);opacity:.12;transition:width .45s ease}
.sc-progress::after{content:'';position:absolute;left:0;bottom:0;height:3px;width:var(--sc-lvl,0%);background:var(--green);border-radius:0 2px 2px 0;transition:width .45s ease}
/* 进度填充在内容之下（操作条背景是透明的，绿色能透出来） */
.sync-card.sc-busy .sync-card-main,.sync-card.sc-busy .sync-card-actions{position:relative;z-index:1}
@media (prefers-reduced-motion: reduce){
  .sc-progress::before,.sc-progress::after{transition:none}
}
.sync-card{
  position:relative;overflow:hidden;border:1px solid var(--border);border-radius:var(--radius);
  background:var(--surface);
  cursor:pointer;
  transition:border-color var(--transition),background var(--transition),box-shadow var(--transition)
}
/* hover：描边加亮 + 底面抬一档 + 一层极轻的投影（都无位移、无亮条、无入场动画）；
   卡片已无「›」箭头，靠这三点表示可点开配置 */
.sync-card:hover{border-color:var(--border-hover);background:var(--s2);box-shadow:var(--shadow-sm)}
.sync-card.ph{opacity:.5;cursor:not-allowed;background:var(--surface)}
.sync-card.ph:hover{border-color:var(--border);background:var(--surface);box-shadow:none}
/* 自定义客户端卡片：结构与常规卡片同构，名称旁挂一枚「自定义」小标签；
   头像内是一个加号图形（无官方 logo 可用） */
/* 「添加自定义客户端」卡片：虚线描边示意空位，hover 描边转主题色提示可点 */
/* 配置窗口必填标记 */
.req{color:var(--accent);font-style:normal;font-weight:800}
.sync-card-main{padding:12px}
.sync-card-head{display:flex;align-items:center;gap:10px}
.sync-client-avatar{
  width:34px;height:34px;border-radius:9px;display:flex;align-items:center;justify-content:center;
  font-weight:800;font-size:14px;flex:none;color:var(--t3);
  background:linear-gradient(135deg,var(--s3),var(--surface));
  box-shadow:inset 0 0 0 1px var(--border),0 2px 8px rgba(0,0,0,.25)
}
.avatar-claude-code{background:rgba(217,119,87,.14);box-shadow:inset 0 0 0 1px rgba(217,119,87,.22)}
.avatar-opencode{background:linear-gradient(135deg,#232a38,#10141d)}
.avatar-grok-build{background:linear-gradient(135deg,#1b2130,#0b0e15)}
.avatar-agy{background:linear-gradient(135deg,#20273a,#10131f)}
.sync-client-logo{width:19px;height:19px;object-fit:contain;display:block;pointer-events:none}
/* 图标与客户端名之间的竖线分隔（纯装饰，仅占 1px 不参与伸缩） */
.sync-card-sep{flex:none;width:1px;height:18px;background:var(--border-hover)}
.sync-title-text{min-width:0;display:flex;align-items:center;gap:6px;flex:1}
.sync-card-name{font-weight:700;font-size:13px;color:var(--text);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
/* 卡片右侧图标组：贴在卡片最右侧（名称区 flex:1 顶到边缘），启动 + 卸载两个图标按钮 */
.sync-card-tools{flex:none;display:flex;align-items:center;gap:2px;margin-left:auto;margin-right:-4px}
/* 启动图标：划入卡片才浮现（与卸载同族）；hover 绿色示意「拉起终端」 */
.sync-card-run{
  flex:none;display:flex;align-items:center;justify-content:center;
  width:26px;height:26px;padding:0;
  border:0;border-radius:6px;background:transparent;color:var(--t3);opacity:0;cursor:pointer;
  transition:color var(--transition),background var(--transition),opacity var(--transition)
}
.sync-card-run svg{width:16px;height:16px;display:block}
.sync-card-run:hover:not(:disabled){color:var(--green);background:var(--s2);opacity:1}
/* 卸载图标：默认不显形——否则一屏卡片挂满灰垃圾桶抢视线；
   光标划入卡片才浮现（16px 图 / 26px 按钮），hover 转红提示危险操作 */
.sync-card-unin{
  flex:none;display:flex;align-items:center;justify-content:center;
  width:26px;height:26px;padding:0;
  border:0;border-radius:6px;background:transparent;color:var(--t3);opacity:0;cursor:pointer;
  transition:color var(--transition),background var(--transition),opacity var(--transition)
}
.sync-card-unin svg{width:16px;height:16px;display:block}
/* 光标划入卡片浮现；键盘聚焦同样浮现（可达性） */
.sync-card:hover .sync-card-unin,
.sync-card-unin:focus-visible,
.sync-card:hover .sync-card-run,
.sync-card-run:focus-visible{opacity:1}
/* 卡片已划入但按钮此刻不可用（别处动作进行中）：给弱化提示，而不是完全浮现 */
.sync-card:hover .sync-card-unin:disabled,
.sync-card:hover .sync-card-run:disabled{opacity:.3}
.sync-card-unin:hover:not(:disabled){color:var(--red);background:var(--red-soft);opacity:1}
.sync-card-unin:disabled{cursor:not-allowed}
/* 卸载进行中：常显（绿），不随光标移开而消失 */
.sync-card:hover .sync-card-unin.on,
.sync-card-unin.on{color:var(--green);opacity:1}
/* 卸载确认弹窗里的命令与说明 */
.cli-cmd{font-family:var(--mono);font-size:10px;color:var(--t2);background:var(--s2);border:1px solid var(--border);border-radius:6px;padding:3px 6px;margin-top:6px;word-break:break-all}
.cli-note{color:var(--t3);margin-top:5px;line-height:1.5}
.sync-ph{
  position:absolute;top:8px;right:8px;margin:0;
  font-size:9px;font-weight:700;letter-spacing:.5px;padding:2px 8px;border-radius:999px;
  color:var(--amber);background:var(--amber-soft);border:1px solid rgba(210,153,34,.35)
}
/* 卡片下半区（安装 / 更新 / 版本号）：上边框即划分上下区域的水平线，
   三格等宽、格间细竖线，与卡片上半区的字号/配色同一档 */
.sync-card-actions{
  display:flex;align-items:stretch;
  border-top:1px solid var(--border);
  background:transparent; /* 跟随卡片面色：hover 时整卡一起抬一档 */
}
.sc-act{
  flex:1 1 0;min-width:0;display:flex;align-items:center;justify-content:center;gap:5px;
  padding:6px 4px;border:0;background:transparent;color:var(--t2);
  font-family:inherit;font-size:10.5px;font-weight:600;cursor:pointer;
  transition:background var(--transition),color var(--transition);
}
.sc-act + .sc-act{border-left:1px solid var(--border)}
.sc-act:hover:not(:disabled){background:var(--s3);color:var(--text)}
.sc-act:active:not(:disabled){background:var(--s3)}
.sc-act .sc-ico{flex:none;width:12px;height:12px;display:block;color:var(--accent)}
.sc-act .sc-txt{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.sc-act:disabled{opacity:.4;cursor:not-allowed}
.sc-act:disabled .sc-ico{color:var(--t3)}
/* 有新版本：文案与图标转主色（hover 也保持主色），与置灰的「已是最新」形成对比 */
.sc-act.up, .sc-act.up:hover:not(:disabled){color:var(--accent)}
.sc-act.up .sc-ico{color:var(--accent)}
/* 执行中：文字转绿、图标旋转，明确「正在跑」 */
.sc-act.on{color:var(--green)}
.sc-act.on .sc-ico{animation:sc-spin 1.05s linear infinite}
@keyframes sc-spin{to{transform:rotate(360deg)}}
/* 版本号格：已知版本为纯展示（等宽小字），未安装/未读取时才可点 */
.sc-act.sc-ver .sc-ico{color:var(--t3)}
.sc-act.sc-ver.known{cursor:default}
.sc-act.sc-ver.known:hover{background:transparent}
.sc-ver-txt{font-family:var(--mono);font-size:10px}
/* 有更新：版本号画一条删除线——表示这是即将被「更新」替换掉的当前版本；
   同时转次要色，避免与左侧主色的「更新」按钮抢注意力（未安装 / 已是最新时不加） */
.sc-act.sc-ver.outdated .sc-ver-txt{text-decoration:line-through;text-decoration-thickness:1px;color:var(--t3)}
@media (prefers-reduced-motion: reduce){
  .sc-act.on .sc-ico{animation:none}
}

/* ===== 弹窗 ===== */
.dialog-overlay{position:fixed;inset:0;background:rgba(0,0,0,.45);z-index:100;display:flex !important;align-items:center;justify-content:center}
.cli-term-list{display:flex;flex-direction:column;gap:6px;margin:12px 0}
.cli-term-item{display:flex;align-items:center;gap:10px;width:100%;padding:10px 12px;border:1px solid var(--line);border-radius:8px;background:var(--surface-2);color:var(--t1);cursor:pointer;font-size:12px;text-align:left}
.cli-term-item:hover{border-color:var(--accent);background:var(--surface-3)}
/* 配置抽屉：贴主窗口右缘、全高，滑入滑出（Transition name=cfg-drawer）。
   animation:none 压掉全局 .dialog 的 slideUp 入场动画——它会与抽屉的
   translateX 过渡同时播放（Y 轴位移+缩放+淡入打架），是弹出卡顿主因之一 */
.dialog-overlay.cfg-drawer-ov{align-items:stretch;justify-content:flex-end;backdrop-filter:none;-webkit-backdrop-filter:none}
.cfg-dialog{position:relative;width:100%;height:100%;padding:0;display:flex;flex-direction:column;background:var(--surface);border:none;border-radius:0;box-shadow:none;animation:none;will-change:transform}
/* 滑动进行中：内容层保持提升（整块位移走合成器，不逐帧重绘内部） */
.cfg-dialog.cfg-anim{will-change:transform}
/* 分级入场：只做「标题栏 → 页签 → 内容」三段纯淡入。
   原先逐卡片动画（.cfg-form>* 每个子元素一个 animation + 递增延迟）会把十几个元素
   同时提升为合成层、并连带整块内容重绘，是弹出/收起卡顿的主因；
   cfg-dir-enter 限定内容块只在进入滑动期间先隐藏，离开期间保持可见随面板整体滑出 */
.cfg-dialog.cfg-anim.cfg-dir-enter .cfg-head,.cfg-dialog.cfg-anim.cfg-dir-enter .cfg-tabs,
.cfg-dialog.cfg-anim.cfg-dir-enter .cfg-body{opacity:0}
.cfg-dialog.cfg-staged .cfg-head{animation:cfgStage .18s var(--ease) both}
.cfg-dialog.cfg-staged .cfg-tabs{animation:cfgStage .18s var(--ease) .04s both}
.cfg-dialog.cfg-staged .cfg-body{animation:cfgStage .2s var(--ease) .07s both}
/* 纯 opacity：全程走合成器，不触发布局与重绘 */
@keyframes cfgStage{from{opacity:0}to{opacity:1}}
@media (prefers-reduced-motion: reduce){
  .cfg-dialog .cfg-head,.cfg-dialog .cfg-tabs,.cfg-dialog .cfg-body{opacity:1 !important;animation:none !important}
}
/* 保存 / 关闭：浮在配置页右缘内侧、垂直居中，两个按钮上下排列。
   内容滚动时整体淡出（避免遮挡阅读），停手 0.7s 后淡回；光标划入时按钮轻微放大 */
.cfg-drawer-actions{position:absolute;right:8px;top:50%;transform:translateY(-50%);z-index:40;display:flex;flex-direction:column;gap:6px;transition:opacity .2s var(--ease),transform .2s var(--ease)}
.cfg-drawer-actions.is-hidden{opacity:0;transform:translateY(-50%) scale(.86);pointer-events:none}
.cfg-drawer-btn{width:26px;height:26px;padding:0;display:flex;align-items:center;justify-content:center;background:var(--surface);color:var(--t2);border:1px solid var(--border);border-radius:var(--radius-sm);cursor:pointer;box-shadow:0 2px 8px rgba(0,0,0,.35);transition:color .15s,background .15s,border-color .15s,transform .16s var(--ease)}
.cfg-drawer-btn>.ico{width:12px;height:12px}
.cfg-drawer-btn:hover{color:var(--text);background:var(--bg);border-color:var(--border-hover)}
.cfg-drawer-btn.save{color:var(--accent)}
.cfg-drawer-btn.save:hover{color:var(--accent);background:var(--bg)}
.cfg-drawer-btn.save:disabled{opacity:.45;cursor:default;color:var(--t3)}
.cfg-drawer-enter-active,.cfg-drawer-leave-active{transition:background .26s ease}
.cfg-drawer-enter-from,.cfg-drawer-leave-to{background:rgba(0,0,0,0) !important}
.cfg-drawer-enter-active .cfg-dialog,.cfg-drawer-leave-active .cfg-dialog{transition:transform .28s cubic-bezier(.22,.61,.36,1)}
/* 入场：自面板右缘滑入；收起：保存 → 向左滑出，关闭 → 向下滑出（.cfg-exit-left 由保存按钮置位） */
.cfg-drawer-enter-from .cfg-dialog{transform:translateX(105%)}
.cfg-drawer-leave-to .cfg-dialog{transform:translateY(105%)}
.cfg-drawer-leave-to .cfg-dialog.cfg-exit-left{transform:translateX(-105%)}
/* 标题栏首行与左「概览」标题栏同规格（min-height:32px + padding:0 10px）：两条顶部带齐平 */
.cfg-head{padding:0 10px;border-bottom:1px solid var(--border);position:relative;z-index:30}
.cfg-head-top{display:flex;align-items:center;gap:10px;min-height:32px;box-sizing:border-box}
/* 客户端图标位：order:2 将其排到页签右侧（DOM 顺序仍是"标题在前"，只改视觉次序） */
.cfg-title{flex:none;order:2;display:flex;align-items:center;min-height:20px;font-weight:800;font-size:15px;color:var(--text)}
/* 无图标的客户端回退为名称文本 */
.cfg-title-logo{height:20px;width:auto;max-width:132px;object-fit:contain;display:block;pointer-events:none}
.cfg-file code,.cfg-ref code{background:var(--bg);padding:2px 6px;border-radius:5px;margin:0 4px}
.cfg-notexist{color:var(--amber);font-size:10px;margin-left:4px}

/* ===== 页签 ===== */
.cfg-tabs{display:flex;align-items:center;gap:5px;flex:1;min-width:0;margin:0;flex-wrap:nowrap;overflow-x:auto;scrollbar-width:none}
/* 页签（所有客户端）：分段控件式（中性底 + 描边，选中态=主色文字与浅底，不用渐变） */
/* 未选中页签：纯文字（无底无边），让选中态成为标题栏里唯一的"亮块" */
.cfg-tab{border:1px solid transparent;background:transparent;color:var(--t3);font-size:11px;padding:3px 12px;border-radius:var(--radius-sm);cursor:pointer;font-family:inherit;white-space:nowrap;transition:var(--transition)}
.cfg-tab:hover{background:var(--s2);color:var(--text);border-color:transparent}
.cfg-tab.on{border-color:transparent;color:var(--accent);background:var(--accent-soft);font-weight:600}

/* ===== 主体 ===== */
.cfg-body{padding:10px 14px 18px;overflow:auto;flex:1;min-height:0;background:var(--surface);scrollbar-width:thin;scrollbar-color:var(--border-hover) transparent}
/* 源码页签窗口：隐藏滚动条、保留滚动能力（滚轮/触控板/键盘仍可滚，代码编辑观感更干净） */
.cfg-body-src{display:flex;flex-direction:column;padding:6px 8px 0;min-height:0;background:var(--bg);scrollbar-width:none;-ms-overflow-style:none}
/* 双类提升特异性：压过后文 .cfg-body::-webkit-scrollbar{width:8px} 的全局细条定义 */
.cfg-body.cfg-body-src::-webkit-scrollbar{width:0;height:0;display:none}
/* 面板卡片：一道弱描边圈定内容，不用投影、不用底色差，避免层层叠叠的「卡片感」 */
.cfg-card{padding:14px 16px;border:1px solid var(--border);border-radius:8px;background:var(--s2,var(--surface))}
.card-head{font-weight:600;font-size:11.5px;color:var(--text);margin-bottom:3px}
/* 面板分组标题：纯文本小标题，只靠字号/字重/留白分段，不画底色与边框 */
.panel-head{font-size:11px;font-weight:600;letter-spacing:0;padding:0;background:none;border:0;border-radius:0;color:var(--t2);margin:6px 0 0}
/* MCP 服务器（mcpServers）只读展示：已配置的服务器以胶囊列出，未配置显示占位 */
.cfg-mcp-list{display:flex;flex-wrap:wrap;gap:4px;margin-bottom:2px}
.cfg-mcp-chip{font-size:10px;font-family:var(--mono);padding:2px 8px;border-radius:var(--radius-sm);background:var(--s2);border:1px solid var(--border);color:var(--t2)}
.cfg-mcp-empty{font-size:10px;color:var(--t3)}
.cfg-head-count{display:inline-flex;align-items:center;justify-content:center;min-width:16px;height:16px;padding:0 5px;margin-left:6px;border-radius:999px;background:var(--accent-soft);border:1px solid var(--accent-ring);color:var(--accent);font-size:9.5px;font-weight:700;vertical-align:middle}
/* 作用位置图标按钮：展开上拉候选列表时的激活态 */

/* ===== 字段 ===== */
/* 配置项按类集中：同卡内字段间距收紧，标签与输入更贴近 */
.cfg-form .field{display:flex;flex-direction:column;gap:4px;min-width:0;margin-bottom:0}
.cfg-form .field label{font-size:10.5px;font-weight:600;letter-spacing:0;color:var(--t3)}
.cfg-form .field input,.cfg-form .field select,.cfg-form .field textarea{width:100%;min-height:26px;padding:3px 8px;background-color:var(--input-bg);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:11px;font-family:inherit;line-height:1.5;box-sizing:border-box;transition:border-color var(--transition),box-shadow var(--transition)}
/* select 不用 background 简写（会清掉全局自定义下拉箭头图），单独给箭头留出右内距 */
.cfg-form .field select{padding-right:28px}
.cfg-form .field input:hover:not(:focus):not([readonly]),.cfg-form .field select:hover:not(:focus),.cfg-form .field textarea:hover:not(:focus){border-color:var(--border-hover)}
.cfg-form .field input:focus,.cfg-form .field select:focus,.cfg-form .field textarea:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 2px var(--accent-soft)}
.field-row{display:flex;gap:6px 10px;flex-wrap:wrap;margin-bottom:0}
.cfg-form .field-row{margin-bottom:1px}
/* 配置表单容器：统一纵向节奏，占满配置区全宽（字段行内流式排布，越宽一行容纳的配置项越多） */
.cfg-form{display:flex;flex-direction:column;gap:10px;width:100%}
/* claude「MCP 服务器名单」2 项压缩为同一行：左标签横排放不下（会裁字），
   本组改标签在上、控件等分（fw-full 的独占行宽被本组规则覆盖） */
.cfg-form .claude-one-line{display:flex;gap:6px 10px;flex-wrap:wrap}
.cfg-form .claude-one-line > .field{flex:1 1 0;min-width:0;margin-bottom:0}
/* claude「模板顶层字段」7 项必须落在同一行：flex-wrap 版在这 7 列的最小宽度合计
   超过面板宽时会把第 7 项（包含 Git 指引）挤到第二行。改等宽栅格（沿用 .cc-grid4 的
   秩序：列宽等分、标签在上控件在下），列数由 grid-auto-flow:column 自动等于字段数，
   grid 永不换行，窄窗口下 7 列一起收缩而不是折行 */
.cfg-form .claude-grid{display:grid;grid-auto-flow:column;grid-auto-columns:minmax(0,1fr);gap:6px 10px;align-items:end}
.cfg-form .claude-grid > .field{margin-bottom:0}
/* 列宽等分后控件统一撑满列宽：number 类型的空数字框不受 field-sizing:content 影响，
   width:auto 会按 input 默认宽（约 150px）溢出到相邻列，故这里显式压成列宽 */
.cfg-form .claude-grid > .field > input,
.cfg-form .claude-grid > .field > select{width:100%;min-width:0;max-width:100%}
/* codex 模型映射：客户端模型名 → 路由器真实模型 的可编辑行（等宽字体键名、值带模型下拉） */
.codex-map{margin-top:8px;display:flex;flex-direction:column;gap:4px}
.codex-map-head{display:flex;align-items:center;justify-content:space-between;gap:8px}
.codex-map-head > span{font-size:9px;color:var(--t3)}
.codex-map-row{display:flex;gap:5px;align-items:center}
.codex-map-key{flex:0 0 170px;min-width:0;padding:4px 8px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:11px;font-family:var(--mono);box-sizing:border-box}
/* 内置 ID 键只读：弱化文字、禁用光标，与可编辑的映射目标区分 */
.codex-map-key[readonly]{color:var(--t2);cursor:default;opacity:.85}
.codex-map-eq{flex:none;color:var(--t3);font-size:10px}
.codex-map-val{flex:1;min-width:0}
/* 受信目录 textarea 收为单行，尺寸与同行 input 一致（等高、同内距）；
   .perm-ta 定义在本文件更后位置，双类选择器保证覆盖其 min-height */
.perm-ta.cb-ta-1line{min-height:0;max-height:none;padding:4px 8px;font-size:11px;line-height:1.5}
/* codebuddy 默认模型行：标签在左、输入框在右的横排形式 */
/* codebuddy 各行：标签在左、输入框在右的横排形式。
   带 .cfg-form 前缀提高特异性，压过后文 .cfg-form .fw-l/.fw-m/.fw-s 的
   flex-basis 分档（208/144/96px），否则 basis 合计超宽仍会换行。 */
.cfg-form .fld-inline-row > .field{flex:1 1 0;min-width:0;flex-direction:row;align-items:center;gap:6px}
.fld-inline-row > .field > label{flex:none;white-space:nowrap;font-size:11px;color:var(--t2);margin:0}
.fld-inline-row > .field > .pi-sel,
.fld-inline-row > .field > input,
.fld-inline-row > .field > select,
.fld-inline-row > .field > textarea{flex:1 1 auto;min-width:0}
/* 同行标签定宽（按卡内最长标签），使各列控件起点一致、文本框等宽 */
.cb-lab-fix > .field > label{flex:0 0 114px}
/* 连接行内 .ro-url（输入+复制按钮组合）也占满标签右侧余宽 */
.fld-inline-row > .field > .ro-url{flex:1 1 auto;min-width:0}
/* claude「MCP 服务器名单」：每个 list 字段独占一行，标签左、textarea 右 */
.fld-inline-col{display:flex;flex-direction:column;gap:6px}
.fld-inline-col > .field{display:flex;flex-direction:row;align-items:center;gap:6px;min-width:0}
.fld-inline-col > .field > label{flex:none;white-space:nowrap;font-size:11px;color:var(--t2);margin:0}
.fld-inline-col > .field > textarea{flex:1 1 auto;min-width:0}
.cfg-grid{display:flex;gap:4px;flex-wrap:wrap}
/* commandcode 配置行：统一 4 列等宽栅格，各行列边对齐 */
.cc-grid4{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:8px 12px;align-items:end}
/* Command Code 面板：标签居左、控件居右（无问号提示，用户指定布局）。
   注意必须显式 flex-direction:row 压掉 .cfg-form .field 的 column（同优先级） */
.cc-lab-left{align-items:center}
.cc-lab-left .field{display:flex;flex-direction:row;align-items:center;gap:8px}
.cc-lab-left .field>label{margin:0;flex:none;white-space:nowrap}
.cc-lab-left .field>input,.cc-lab-left .field>select,.cc-lab-left .field>.pi-sel{flex:1 1 0;min-width:0}
/* Copilot 配置选项页签：同 cc-lab-left，另支持只读地址组与多行文本域 */
.cp-lab-left .field{display:flex;flex-direction:row;align-items:center;gap:8px}
.cp-lab-left .field>label{margin:0;flex:none;white-space:nowrap}
.cp-lab-left .field>input,.cp-lab-left .field>select,.cp-lab-left .field>.pi-sel,.cp-lab-left .field>textarea,.cp-lab-left .field>.ro-url{flex:1 1 0;min-width:0}
/* deepcode 面板：文本标签居左、控件居右（用户指定布局）。必须显式 flex-direction:row
   压掉 .cfg-form .field 的 column——两者特异性相同(0,2,0)，本组靠源序覆盖 */
.dc-lab-left .field{display:flex;flex-direction:row;align-items:center;gap:8px}
.dc-lab-left .field>label{margin:0;flex:none;white-space:nowrap}
.dc-lab-left .field>input,.dc-lab-left .field>select,.dc-lab-left .field>.pi-sel{flex:1 1 0;min-width:0}
/* 「内置 ID 映射」是 4 行块：标签顶对齐（垂直居中会相对多行块漂移），块吃满右侧余宽 */
.dc-lab-left .field.dc-map-field{align-items:flex-start}
.dc-lab-left .field.dc-map-field>label{padding-top:2px}
.dc-lab-left .field.dc-map-field>.agy-map-main{flex:1 1 0;min-width:0}
.cc-lab-left .field>.cc-toggle{flex:none;align-self:center}
.cc-lab-left .field>.cc-nologin-msg{flex:1 1 0;min-width:0}
.cc-lab-left .cc-del-pill{align-self:center}
.rf-group{display:flex;gap:12px;flex-wrap:wrap;align-items:center;padding:3px 12px;border-radius:var(--radius);background:var(--s2);border:1px solid var(--border);font-size:11px;color:var(--t2)}
.rf-group label{display:inline-flex;align-items:center;gap:5px;cursor:pointer;white-space:nowrap;margin:0}
.rf-group label:has(input:checked){color:var(--text);font-weight:700}
.rf-group input{accent-color:var(--accent);margin:0;cursor:pointer}
/* 权限模式组容器：整组淡绿胶囊面板；通用兜底模板内保持卡片点选+绿渐变选中，
   claude 权限行内的选项样式由下方 .perm-inline 段覆盖（纯文字+语义色细描边） */
.perm-mode{display:flex;flex-wrap:wrap;gap:6px;align-items:center;margin-bottom:4px;padding:3px 10px;border-radius:var(--radius);background:var(--s2);border:1px solid var(--border)}
/* 权限模式胶囊：claude（权限模式四档 + 「执行命令」三档）与 CodeBuddy（仅权限模式四档）
   共用同一形态——一条淡绿胶囊承载所有分组，强制单行：组内与组间均不折行、
   不出水平滚动条；常规宽度（中文/英文短文案）内容完整放下。 */
.perm-inline{display:flex;flex-wrap:nowrap;align-items:center;gap:12px}
.perm-inline .perm-mode{margin-bottom:0}
.perm-inline>.agy-perm-line{margin-top:0;flex:none;flex-wrap:nowrap}
.perm-inline>.agy-perm-col,.perm-inline>.agy-perm-line>.agy-perm-col{flex:none}
.perm-inline .perm-mode,.perm-inline .agy-perm-box{flex-wrap:nowrap}
/* 与 agy 同口径：背景上移到 .perm-inline 成为一条胶囊，内部两组的
   .perm-mode / .agy-perm-box 只留选项（去背景/边框/内边距），不再有大小差异 */
.perm-inline.perm-bar{padding:0 10px;border-radius:var(--radius);background:var(--s2);border:1px solid var(--border);gap:8px}
.perm-inline.perm-bar .perm-mode,.perm-inline.perm-bar .agy-perm-box{background:transparent;border-color:transparent;padding:0;gap:12px}
/* 组名与选项组之间放宽（基础 .agy-perm-col 为 7px，此处加宽形成呼吸感） */
.perm-inline.perm-bar .agy-perm-col{gap:10px}
/* Command Code 胶囊条三组同行（四档+三态+三态）：紧凑间距变体，防止整行溢出 */
.perm-inline.perm-bar.cc-perm-bar{gap:5px;padding:0 8px}
.perm-inline.perm-bar.cc-perm-bar .perm-mode,.perm-inline.perm-bar.cc-perm-bar .agy-perm-box{gap:7px}
.perm-inline.perm-bar.cc-perm-bar .agy-perm-col{gap:5px}
.perm-inline.perm-bar.cc-perm-bar .perm-mode-card{padding:0 5px;gap:4px}
.perm-inline.perm-bar.cc-perm-bar .agy-box-opt{gap:4px}
.perm-inline.perm-bar.cc-perm-bar .agy-perm-name{font-size:10px}
/* grok 权限配置：与 codex 面板完全同构（一条淡绿胶囊 = 「权限模式」四档卡片 + 分隔符 + 「所有命令」三态框），
   故不再需要 grok 专属样式。原先那套「多行胶囊 + 11 枚逐条规则 chip」的 .grok-perm-bar/.grk-caps 规则已随之删除；
   逐条规则改由 TOML 源码页签编辑，面板只用三态单选框控制所有命令。 */
/* 通用设置流式排列：等分窗口宽度——每字段最小 96px 保证可读，装不下自动换行，每行字段均分行宽不留行尾空白 */
.cc-grid4.gm-fit{display:flex;flex-wrap:wrap;gap:6px 10px;align-items:center}
.cc-grid4.gm-fit .field{flex:1 1 0;min-width:96px;flex-direction:row;align-items:center;gap:6px}
.cc-grid4.gm-fit .field>label{flex:0 1 auto;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.cc-grid4.gm-fit .field>select,.cc-grid4.gm-fit .field>input{flex:1 1 0;width:auto;min-width:0;max-width:100%}
/* grok 紧凑档：.gm-row 卡片里的 gm-fit 改为「按内容定位、不参与等分拉伸」。
   gm-fit 的 flex:1 1 0 是流式排列的等分设计——控件随字段一起拉伸，
   行尾不会露白；grok 的控件是内容宽（field-sizing:content），字段若仍被等分，
   整块余量就堆在控件后面，看起来「配置项之间缝隙很大」。
   这里让字段与控件都回到内容宽（6ch~24ch 夹取，24ch 为容纳
   「always_allow_all_sessions」这类长占位符），项间只留 10px 列距，
   一行能多容纳几项，右侧留白只出现一次而不是每项一次。
   注：field-sizing:content 是 width/height 一起跟随内容——空 option 的 select、
   无内容的 input 高度会塌陷到只剩内边距（实测 8px vs 正常 22px），同行控件因此
   对不齐甚至看不见，故凡内容自适应的控件一律补 min-height:2em 兜底。 */
.card.gm-row .cc-grid4.gm-fit .field{flex:0 0 auto;min-width:0}
.card.gm-row .cc-grid4.gm-fit .field>input,
.card.gm-row .cc-grid4.gm-fit .field>select{flex:0 0 auto;width:auto;field-sizing:content;min-width:52px;max-width:24ch;min-height:24px}
/* 面板字段行：每个配置项尽量同行显示——行内项目不换行、允许收缩，标签过长省略号兜底 */
.card.gm-row .field-row{flex-wrap:nowrap}
.card.gm-row .field-row .field>label{flex:0 1 auto;min-width:0;overflow:hidden;text-overflow:ellipsis}
/* fw-fit 字段（温度/TopP/TopK/最大输出/轮数/压缩阈值等短内容）：输入框按内容自适应宽度，剩余空间让给 fw-l/fw-m 字段 */
.card.gm-row .field.fw-fit{flex:0 1 auto;width:auto}
.card.gm-row .field.fw-fit>input,.card.gm-row .field.fw-fit>select{flex:0 1 auto;width:auto;field-sizing:content;min-width:6ch;max-width:22ch;min-height:24px}
/* grok 模型卡「上下文长度 / [models] web_search」两列按需求配平：
   数字列给足最小宽度（对齐 claude 模型映射行 96px 的惯例，仍随数值增长到 160px 上限），
   搜索列回到内容宽（随所选模型名伸缩，封顶 200px），不再独占整行余量。
   本行控件宽度都由内容决定，行尾留白与下一行「温度/Top P/…」一致，同属内容宽左对齐排布。
   注意：这两条必须排在上一条 .fw-fit 规则之后——同特异性下靠源序覆盖 */
.card.gm-row .field.gm-ctx{flex:0 0 auto}
.card.gm-row .field.gm-ctx>input{min-width:112px;max-width:160px}
.card.gm-row .field.gm-ws{flex:0 1 auto;min-width:0}
.card.gm-row .field.gm-ws>label{flex:none;overflow:visible;text-overflow:clip}
.card.gm-row .field.gm-ws>select{flex:0 1 auto;width:auto;field-sizing:content;min-width:6ch;max-width:200px;min-height:24px}
/* gm-wrap 行：窗口装不下时字段整体折行（不压缩字段），标签始终完整显示 */
.card.gm-row .field-row.gm-wrap{flex-wrap:wrap;row-gap:8px}
.card.gm-row .field-row.gm-wrap .field{flex:0 0 auto}
.card.gm-row .field-row.gm-wrap .field>label{flex:none;overflow:visible;text-overflow:clip}
/* ===== grok 模型卡：按各字段「可能值」的实测字宽给框宽 =====
   此前所有控件共用 fw-* 分档：短值（推理深度/温度/API 后端/后端搜索）被压到 6ch≈39px
   （连「默认」两字都显示不全），长值字段（当前块）却被均分拉到 175px，宽窄正好反了。
   现统一 field-sizing:content 跟随当前值伸缩，再用 min-width 预留「空值时的占位文本 /
   常见最长值」所需宽度、max-width 兜住超长值；字段一律 flex:0 0 auto 不参与等分拉伸。
   min 值均为实测（字体 Microsoft YaHei UI 11px）：文本框 = 文本宽 + 14px（内边距 6+6、
   边框 1+1），下拉 = 文本宽 + 34px（再加箭头 20px），数字框另有 15px 步进器。
   仅「接入地址 / 上游模型 / 模型描述」三个值本身可很长的字段保留 flex 吸收行尾余量。 */
#grok-panel-model .field-row{flex-wrap:wrap;row-gap:8px}
#grok-panel-model .field{flex:0 0 auto;min-width:0}
#grok-panel-model .field>label{flex:none;overflow:visible;text-overflow:clip;white-space:nowrap}
#grok-panel-model .field>input,
#grok-panel-model .field>select{flex:0 0 auto;width:auto;field-sizing:content;min-height:24px}
/* 值本身可能很长的几列：不吃 field-sizing 的内容宽，改为撑满字段并吸收行尾余量 */
#grok-panel-model .gm-url{flex:1 1 190px}
#grok-panel-model .gm-up{flex:1 1 150px}
#grok-panel-model .gm-desc{flex:1 1 160px}
#grok-panel-model .gm-desc>input{flex:1 1 0;width:auto;min-width:0;max-width:100%;field-sizing:fixed}
/* API 密钥 / extra_headers：长串独占一行，同样撑满 */
#grok-panel-model .gm-full{flex:1 1 100%;min-width:100%}
#grok-panel-model .gm-full>input{flex:1 1 0;width:auto;min-width:0;max-width:100%;field-sizing:fixed}
/* 模型块键名（含「（默认）」后缀，实测 122px）/ [models] default 键名（实测 78px） */
#grok-panel-model .gm-blk>select{min-width:126px;max-width:210px}
#grok-panel-model .gm-def>select{min-width:104px;max-width:190px}
/* 新增块键 / 名称：自由文本，框给宽些便于输入（占位文本实测各 102px） */
#grok-panel-model .gm-new>input{min-width:140px;max-width:200px}
#grok-panel-model .gm-name>input{min-width:140px;max-width:220px}
/* 上下文长度：完整显示 1000000 七位数 + 步进器（实测 74px） */
#grok-panel-model .gm-ctx>input{min-width:82px;max-width:112px}
/* [models] web_search 键名 / API 后端（最长 responses，实测 87px） */
#grok-panel-model .gm-ws>select{min-width:106px;max-width:190px}
#grok-panel-model .gm-api>select{min-width:90px;max-width:124px}
/* 推理深度 / 后端搜索：「默认」「否」一类 2~4 字短枚举（实测 56 / 45px） */
#grok-panel-model .gm-eff>select{min-width:64px;max-width:86px}
#grok-panel-model .gm-bs>select{min-width:60px;max-width:84px}
/* 温度 / Top P（0.7、0.95 实测 45 / 53px）与最大输出 Tokens（8192 实测 55px） */
#grok-panel-model .gm-temp>input,#grok-panel-model .gm-top>input{min-width:58px;max-width:78px}
#grok-panel-model .gm-max>input{min-width:70px;max-width:104px}
.perm-inline.perm-bar .agy-box-opt{padding:0;gap:6px}
/* 胶囊高度贴合文本：内边距归零 + 行高收紧 + 单选圆圈定尺寸，防止撑高 */
.perm-inline.perm-bar .agy-box-opt input,.perm-inline.perm-bar .perm-mode-card input{width:12px;height:12px}
/* claude「执行命令」三项：与 agy 同款显示原生单选圆圈（.perm-inline 内原本隐藏成纯文字）。
   这里只恢复 display，圆圈配色仍交给 .agy-box-opt.cmd-* 的语义色（绿/琥珀/红） */
.perm-inline.perm-bar .agy-box-opt input{display:inline-block}
/* claude「权限模式」四档：原生是纯文字按钮卡片，这里同样给可见的单选圆圈，
   与「执行命令」三项、以及 agy 的胶囊保持同一种控件形态 */
.perm-inline.perm-bar .perm-mode-card input{accent-color:var(--accent);margin:0;flex:none;cursor:pointer}
.perm-mode-card{display:inline-flex;align-items:center;gap:6px;padding:3px 13px;text-align:center;background:transparent;border:1px solid transparent;border-radius:999px;cursor:pointer;font-family:inherit;transition:var(--transition)}
.perm-mode-card:hover{background:var(--s3)}
.perm-mode-card .pm-name{font-size:11px;font-weight:600;line-height:1.5;color:var(--text)}
.perm-mode-card .pm-desc{font-size:9px;line-height:1.3;color:var(--t3)}
.perm-mode-card.on{background:var(--accent);border-color:transparent}
.perm-mode-card.on .pm-name{color:var(--on-accent)}
.perm-mode-card.on .pm-desc{color:rgba(255,255,255,.78)}
/* 专属语义类只定义 --pmc（询问=琥珀 / 自动接受编辑=绿 / 不询问=红 / 只读规划=灰） */
.perm-mode-card.pm-ask{--pmc:var(--amber)}
.perm-mode-card.pm-accept{--pmc:var(--green)}
.perm-mode-card.pm-bypass{--pmc:var(--red)}
.perm-mode-card.pm-plan{--pmc:var(--t2)}
/* claude 权限行：去掉项级嵌套胶囊、圆点与一切描边（radio 圆点隐藏、前置语义点移除），
   组容器中性胶囊保留，组内为纯文字选项；选中态=仅文本变主色蓝，无背景、无边框 */
.perm-inline .perm-mode-card{padding:0 9px;line-height:1.2;gap:7px;background:transparent;border-color:transparent}
.perm-inline .perm-mode-card:hover{background:transparent;border-color:transparent}
.perm-inline .perm-mode-card .pm-name{line-height:1.2}
.perm-inline .perm-mode-card.on{background:transparent;border-color:transparent}
.perm-inline .perm-mode-card.on .pm-name{color:var(--accent)}
.perm-inline .perm-mode-card.on .pm-desc{color:var(--accent)}
.perm-inline .perm-mode-card::before{content:none}
.perm-inline .agy-perm-name{font-size:11px;line-height:1.2}
.perm-inline .agy-perm-box{gap:11px}
.perm-inline .agy-box-opt{line-height:1.2;padding:0;gap:7px;border:1px solid transparent;border-radius:999px;background:transparent}
.perm-inline .agy-box-opt:hover{border-color:transparent}
.perm-inline .agy-box-opt input{display:none}
.perm-inline .agy-box-opt input:checked + span{color:var(--accent);font-weight:700}
.perm-inline .agy-box-opt:has(input:checked){background:transparent;border-color:transparent}
.perm-inline .agy-box-opt.cmd-allow:has(input:checked){background:transparent;border-color:transparent}
.perm-inline .agy-box-opt.cmd-ask:has(input:checked){background:transparent;border-color:transparent}
.perm-inline .agy-box-opt.cmd-deny:has(input:checked){background:transparent;border-color:transparent}
.perm-inline .agy-box-opt.cmd-allow input:checked + span{color:var(--green)}
.perm-inline .agy-box-opt.cmd-ask input:checked + span{color:var(--amber)}
.perm-inline .agy-box-opt.cmd-deny input:checked + span{color:var(--red)}
.model-row{display:flex;gap:6px 10px;align-items:flex-end;flex-wrap:wrap;margin-bottom:3px}
/* claude 环境变量映射行改横排（fld-inline-row）：单行不换行、垂直居中 */
.model-row.fld-inline-row{flex-wrap:nowrap;align-items:center}
.row-del{color:var(--t3)}
/* 删除按钮悬停变红提示危险操作（对齐 pcl-del / perm-cap-x 的反馈）；
   带 .cfg-dialog 前缀提高优先级，压过下方 .cfg-dialog .row-del 的白色文字覆盖 */
.cfg-dialog .row-del:hover{color:var(--red);border-color:var(--red);background:var(--red-soft)}
/* 模型集合：每个模型一张独立卡片分割展示 */
.kmodel-wrap{display:flex;flex-direction:column;gap:6px}
.mimo-model-card{border:1px solid var(--border);border-radius:calc(var(--radius-sm) + 1px);background:var(--s3,var(--bg));padding:0;overflow:hidden}
.mimo-model-card:hover{border-color:var(--border-hover)}
.mimo-model-head{display:flex;align-items:center;gap:8px;padding:5px 9px;background:var(--s2,var(--surface));border-bottom:1px solid var(--border)}
.mimo-model-idx{font-size:10px;font-weight:700;color:var(--t3);letter-spacing:.5px}
.mimo-model-title{flex:1;min-width:0;font-weight:700;font-size:12px;color:var(--text);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.mimo-model-body{padding:6px 9px 8px}
/* MimoCode 权限面板：单行深色权限条，组间竖线分隔；禁止换行，隐藏滚动条 */
.perm-bar{display:flex;align-items:center;gap:12px;flex-wrap:nowrap;overflow-x:auto;scrollbar-width:none;background:var(--s2,var(--surface));border:1px solid var(--border);border-radius:8px;padding:7px 14px}
.perm-bar::-webkit-scrollbar{display:none}
.perm-bar-label{flex:none;font-size:11px;color:var(--t3);white-space:nowrap}
.perm-bar .perm-caps{display:flex;align-items:center;gap:6px;flex:none}
.perm-bar .perm-cap{flex:none;white-space:nowrap}
.perm-bar-sep{flex:none;width:1px;align-self:stretch;background:var(--border)}
/* MimoCode 模型集合：行内「标识」上拉候选需跨出卡片边界——允许溢出并以 focus-within
   抬高层级，避免被相邻模型卡遮挡 */
.kmodel-wrap.cp-lab-left .mimo-model-card{position:relative;overflow:visible;z-index:1}
.kmodel-wrap.cp-lab-left .mimo-model-card:focus-within{z-index:30}
.ro-url{display:flex;gap:6px;align-items:center}
.ro-url input{flex:1;min-height:26px;background:var(--s3);border:1px solid var(--border);border-radius:var(--radius-sm);padding:3px 8px;color:var(--t2);font-size:11px}
/* model-row 内未标注宽度的字段（模型选择 / 上下文长度）
   弹窗保持原 720px 宽，行内三列在固定总宽内分配：
   envKey 统一最长键名宽、上下文 96px 不收缩，模型 200px 为可收缩兜底列 */
.cfg-form .model-row > .field{flex:0 1 300px}
.cfg-form .model-row > .field.model-sel{flex:0 1 200px}
.cfg-form .model-row > .field.ctx-len{flex:0 0 auto}
/* 上下文长度输入框固定 96px：完整显示 200000 一类 6~7 位数并给数字步进按钮留位，
   行宽不足时由模型列收缩让位，本列不再被压缩裁字 */
.cfg-form .model-row > .field.ctx-len input{width:96px;flex:none}
/* 环境变量名只读（预填钥匙不再可改），视觉弱化表明不可编辑；
   等宽字体 + ch 宽度让输入框刚好完整显示变量名（14px = 左右内边距 + 边框）。
   选择器带 .field 提升优先级，压过 .cfg-form .field input 的 font-family:inherit */
.cfg-form .model-row > .field.env-key{flex:0 0 auto}
.cfg-form .field .env-ro{background:var(--s3,var(--bg));color:var(--t3);cursor:not-allowed;font-family:var(--mono);flex:none}
/* 配置项标签只做颜色提示：鼠标移入该项或控件聚焦时提亮，不做位移/缩放，避免半行跳动 */
.cfg-dialog .cfg-body .field>label{transition:color .12s var(--ease)}
.cfg-dialog .cfg-body .field:hover>label,
.cfg-dialog .cfg-body .field:focus-within>label{color:var(--text)}

/* ===== 权限规则编辑 ===== */
/* 能力/动作审批卡分组：中性分组面板（与各客户端权限容器一致） */
.perm-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(190px,1fr));gap:5px;padding:6px 10px;border-radius:var(--radius);background:var(--s2);border:1px solid var(--border)}
/* 自动审批操作：对齐 claude 权限条——「权限模式」四档卡片（询问/自动接受编辑/不询问/只读规划），
   每档映射到 autoApprovalSettings.actions 8 键的固定写盘值；样式完全复用 perm-bar/perm-mode 既有规则 */
.pcl-opts{display:inline-flex;gap:3px}
.pcl-opt{padding:2px 9px;font-size:9.5px;line-height:1.4;font-family:inherit;border:1px solid var(--border);border-radius:999px;background:var(--s2);color:var(--t2);cursor:pointer;transition:var(--transition)}
.pcl-opt:hover{border-color:var(--border-hover);color:var(--text)}
.pcl-opt.allow.on{background:var(--green-soft);border-color:var(--green);color:var(--green)}
.pcl-opt.deny.on{background:var(--red-soft);border-color:var(--red);color:var(--red)}
.pcl-opt.ask.on{background:var(--amber-soft);border-color:var(--amber);color:var(--amber)}
/* grok 权限：permission_mode 独占一行，allow/ask/deny 三列同一行等宽 */
.perm-grid.perm-3{grid-template-columns:repeat(3,minmax(0,1fr));align-items:start}
/* 权限规则点选列表样式见 public/style.css（全局，供 opencode 与各客户端共用） */
.perm-ta{font-family:var(--mono);resize:none;min-height:34px;max-height:110px;line-height:1.35}
/* agy 权限配置：工具权限 5 项 / 命令 3 项，两个单选组各被一个「淡绿胶囊背景」容器包裹归类。
   组内为原生 radio + 文字（圆圈可见、11px、互斥；不带 .field 包装，不受
   .cfg-form .field label{9px} 覆盖）；命令组 默认=中性灰 / 允许=绿 / 拒绝=红 的圆圈与选中文字配色。 */
.agy-perm-line{display:flex;gap:12px;flex-wrap:wrap;align-items:center;margin-top:6px}
/* 每组 = 「类名称」+ 淡绿胶囊背景容器；名称竖排于胶囊左侧，与容器中线对齐 */
.agy-perm-col{display:flex;align-items:center;gap:7px}
.agy-perm-name{font-size:10px;font-weight:800;letter-spacing:.3px;color:var(--t2);white-space:nowrap}
.agy-perm-box{display:flex;gap:14px;align-items:center;flex-wrap:wrap;padding:3px 14px;border-radius:var(--radius);background:var(--s2);border:1px solid var(--border)}
.agy-box-opt{display:inline-flex;align-items:center;gap:5px;font-size:11px;color:var(--t2);cursor:pointer;user-select:none;white-space:nowrap}
.agy-box-opt:hover{color:var(--text)}
.agy-box-opt input{accent-color:var(--accent);margin:0;cursor:pointer;flex:none}
.agy-box-opt input:checked + span{color:var(--text);font-weight:700}
.agy-box-opt.cmd-neutral input{accent-color:var(--t3)}
.agy-box-opt.cmd-allow input{accent-color:var(--green)}
.agy-box-opt.cmd-ask input{accent-color:var(--amber)}
.agy-box-opt.cmd-deny input{accent-color:var(--red)}
.agy-box-opt.cmd-allow input:checked + span{color:var(--green)}
.agy-box-opt.cmd-ask input:checked + span{color:var(--amber)}
.agy-box-opt.cmd-deny input:checked + span{color:var(--red)}
/* 双组（工具权限 5 项 / 执行所有命令 3 项）并入同一条淡绿胶囊：组名前置 + 竖线分隔。
   一条容器天然不存在「两枚胶囊一长一短」；强制单行——组内与组间均不折行、
   不出水平滚动条，常规宽度下内容完整放下 */
.agy-perm-line.agy-perm-bar{
  display:flex;flex-wrap:nowrap;align-items:center;gap:12px;
  padding:2px 12px;border-radius:var(--radius);
  background:var(--s2);border:1px solid var(--border);
}
.agy-perm-bar .agy-perm-seg{display:flex;align-items:center;gap:8px;flex:none}
.agy-perm-bar .agy-perm-name{flex:none}
.agy-perm-bar .agy-perm-opts{display:flex;flex-wrap:nowrap;align-items:center;gap:12px}
.agy-perm-bar .agy-box-opt{gap:6px}
.agy-perm-sep{flex:none;width:1px;height:13px;background:var(--border-hover)}
/* agy 权限配置中性分组结束 */
/* Claude 权限规则独立卡片：规则文本 + 允许/拒绝双态点选（预设常驻，自定义可删除） */
.perm-rule-card{display:flex;align-items:center;gap:6px;padding:4px 6px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);transition:var(--transition)}
.perm-rule-card:hover{border-color:var(--border-hover)}
.perm-rule-card.on{border-color:var(--accent);background:var(--accent-soft)}
.perm-rule-text{flex:1;min-width:0;font-size:10px;font-family:var(--mono);color:var(--t2);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
/* 能力卡片右侧状态标记（✓=开 / ＋=关） */
.perm-cap-mark{flex:none;font-size:10px;font-weight:800;color:var(--t3)}
.perm-rule-card.on .perm-cap-mark{color:var(--accent)}
/* commandcode 常用规则快选：紧凑胶囊双列浮层（向上展开防超窗），点击加入/移出上方文本框 */
.cc-quick{position:relative}
/* commandcode 胶囊开关（启动免登录）/ 胶囊删除按钮：与权限规则三态胶囊同款视觉 */
.cc-toggle{align-self:flex-start;padding:2px 14px;font-size:9.5px;line-height:1.6;border:1px solid var(--border);border-radius:999px;background:var(--surface);color:var(--t2);cursor:pointer;transition:var(--transition)}
/* 标签整合进胶囊的变体（启动免登录）：文字含字段名，适当加宽并水平居中 */
.cc-toggle-lab{padding:2px 16px;white-space:nowrap}
.cc-lab-left .field>.cc-toggle-lab{align-self:center;justify-self:start}
.cc-toggle.on{background:var(--accent-soft);border-color:var(--accent);color:var(--accent);font-weight:700}
.cc-del-pill{align-self:end;justify-self:center;margin-bottom:3px;padding:2px 14px;font-size:9.5px;line-height:1.6;border:1px solid var(--border);border-radius:999px;background:var(--surface);color:var(--t3);cursor:pointer;transition:var(--transition)}
.cc-del-pill:hover{background:var(--red-soft);border-color:var(--red);color:var(--red)}
/* 胶囊按钮危险态（清除类动作）：默认灰字，悬停红色高亮 */
.cc-toggle.danger:hover:not(:disabled){background:var(--red-soft);border-color:var(--red);color:var(--red)}
.cc-nologin-msg{font-size:9.5px;color:var(--t3)}
/* ---- agy 面板布局：卡片内分段标题 + 字段网格 + 映射表头 + 权限行 ---- */
.agy-sec{display:flex;align-items:center;gap:6px;margin:0 0 5px;font-size:10.5px;font-weight:700;letter-spacing:.3px;color:var(--t3)}
.agy-sec::before{content:'';flex:none;width:2px;height:10px;border-radius:1px;background:var(--border-hover)}
.agy-grid{display:grid;gap:8px 12px;align-items:start}
.agy-grid.c2{grid-template-columns:repeat(2,minmax(0,1fr))}
.agy-grid.c3{grid-template-columns:repeat(3,minmax(0,1fr))}
.agy-grid.c4{grid-template-columns:repeat(4,minmax(0,1fr))}
.agy-grid.c5{grid-template-columns:repeat(5,minmax(0,1fr))}
.agy-grid .field{margin-bottom:0}
/* 操作按钮行：单独一行、右对齐 */
.agy-actions{display:flex;justify-content:flex-end;gap:6px}
/* 映射表表头：列宽口径与下方行一致（内置 ID 150px + 目标模型撑满） */
.agy-map-main .agy-map-head{display:flex;align-items:center;gap:6px;padding:3px 6px;border-bottom:1px solid var(--border);background:var(--s3);border-radius:var(--radius-sm) var(--radius-sm) 0 0;font-size:9.5px;font-weight:700;letter-spacing:.3px;color:var(--t3)}
.agy-map-main .agy-map-head>span:first-child{flex:0 0 150px;min-width:0}
.agy-map-main .agy-map-head>span:last-child{flex:1;min-width:0}
/* 权限：每组一行，组名定宽、选项区等宽撑满 */
.agy-perm-rows{display:flex;flex-direction:column;gap:6px}
.agy-perm-rows>.agy-perm-col{display:flex;align-items:center;gap:8px}
.agy-perm-rows>.agy-perm-col>.agy-perm-name{flex:0 0 88px}
.agy-perm-rows>.agy-perm-col>.agy-perm-box{flex:1;min-width:0;padding:3px 12px}
/* ---- agy 模型映射面板：配对行 + aliases 行编辑器 ---- */
.agy-map-main{display:flex;flex-direction:column;gap:8px}
/* 注意不能加 overflow:hidden（即便为了圆角裁切）：会裁掉行内绝对定位的 .pi-sel-drop
   模型下拉；内层 .agy-map-rows 自带 padding，子元素碰不到边框圆角，无需裁切 */
.agy-map-main .agy-map-list{border:1px solid var(--border);border-radius:var(--radius-sm)}
.agy-map-main .agy-map-rows{padding:4px 6px}
.agy-map-main .agy-map-row{display:flex;align-items:center;gap:6px;margin-bottom:4px}
.agy-map-main .agy-map-row:last-child{margin-bottom:0}
/* 内置 ID 只读标签：等宽字体、固定宽度，右侧文本框为映射目标 */
.agy-map-main .agy-map-builtin{flex:0 0 150px;min-width:0;padding:2px 5px;background:var(--s2);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:10px;font-family:var(--mono);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.agy-map-main .agy-map-val{flex:1;min-width:0;padding:2px 5px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:10px;font-family:var(--mono)}
.agy-map-main .agy-map-val:focus{outline:none;border-color:var(--accent-ring)}
.agy-map-main .agy-map-eq{color:var(--t3);font-size:10px;font-family:var(--mono)}
/* deepcode 内置 ID 映射：ID 较长（deepseek-v4-flash-vision-exp），标签列单独加宽；
   该面板行内不画等号；说明文字用常显纯文本（.dc-note），不用问号浮层 */
.deepcode-map .agy-map-builtin{flex:0 0 196px}
.dc-note{margin:2px 0 0;font-size:9.5px;line-height:1.55;color:var(--t3)}

/* ===== pi/grok 模型下拉 ===== */
.pi-sel{position:relative}
.pi-sel > input{width:100%;min-height:26px;padding:3px 8px;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:11px;line-height:1.5;box-sizing:border-box;transition:border-color var(--transition),box-shadow var(--transition)}
.pi-sel > input:hover:not(:focus){border-color:var(--border-hover)}
.pi-sel > input:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 2px var(--accent-soft)}
.pi-sel-drop{position:absolute;z-index:50;top:calc(100% + 2px);left:0;right:0;max-height:150px;overflow:auto;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius-sm);box-shadow:0 8px 24px rgba(0,0,0,.25);padding:3px}
.pi-sel-letter{padding:1px 8px;font-size:9px;font-weight:800;color:var(--t3);text-transform:uppercase}
.pi-sel-group button{display:block;width:100%;text-align:left;padding:3px 10px;background:transparent;border:0;color:var(--text);font-size:11px;cursor:pointer}
.pi-sel-group button:hover{background:var(--accent-soft);color:var(--accent)}
.pi-sel-empty{color:var(--t3);font-size:10px;padding:6px;text-align:center}
/* 弹出入场：轻微浮入 + 淡入（v-show 每次展开重放一次） */
@keyframes piDropIn{from{opacity:0;transform:translateY(3px)}to{opacity:1;transform:none}}
@keyframes piDropInUp{from{opacity:0;transform:translateY(-3px)}to{opacity:1;transform:none}}
.pi-sel-drop{animation:piDropIn .14s ease}
.pi-sel-drop.pi-sel-up{animation-name:piDropInUp}
/* 三态等裸选项下拉（.pi-sel-drop 直接子 button，此前无任何样式命中导致裸文本挤在一起）：
   胶囊式选项 + 悬停高亮 + 当前值 ✓ 加粗 */
.pi-sel-drop > button{display:flex;align-items:center;justify-content:space-between;gap:8px;width:100%;text-align:left;padding:4px 9px;margin:1px 0;background:transparent;border:0;border-radius:6px;color:var(--t2);font-size:11px;font-family:inherit;cursor:pointer;transition:background .12s,color .12s}
.pi-sel-drop > button:hover{background:var(--accent-soft);color:var(--accent)}
.pi-sel-drop > button.on{background:var(--accent-soft);color:var(--accent);font-weight:600}
.pi-sel-drop > button.on::after{content:'✓';font-size:10px;font-weight:700}
/* 作用位置上拉候选（权限配置卡在页面底部，向下展开会溢出视野）：向上弹出 + 路径化美化 */
.pi-sel-drop.pi-sel-up{top:auto;bottom:calc(100% + 4px);max-height:190px;box-shadow:0 -8px 24px rgba(0,0,0,.3)}
.pi-sel-drop.loc-opt{padding:4px;border-radius:8px}
.pi-sel-drop.loc-opt button{display:flex;align-items:center;gap:7px;padding:5px 9px;margin:1px 0;border-radius:6px;font-size:11px;font-family:ui-monospace,Menlo,Consolas,monospace;color:var(--t2);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;transition:background .12s,color .12s}
.pi-sel-drop.loc-opt button::before{content:'';width:12px;height:12px;flex:0 0 auto;background:var(--t3);-webkit-mask:center/contain no-repeat url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'%3E%3Cpath d='M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z'/%3E%3C/svg%3E");mask:center/contain no-repeat url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'%3E%3Cpath d='M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z'/%3E%3C/svg%3E");transition:background .12s}
.pi-sel-drop.loc-opt button:hover,.pi-sel-drop.loc-opt button.sel{background:var(--accent-soft);color:var(--accent)}
.pi-sel-drop.loc-opt button:hover::before,.pi-sel-drop.loc-opt button.sel::before{background:var(--accent)}
/* 权限配置面板：作用位置并入胶囊条首列（图标按钮 + 上拉路径列表） */
.co-loc-col{flex:0 0 auto;min-width:0}
.co-loc-ctl{position:relative;display:flex;align-items:center;gap:7px;min-width:0}
.co-loc-btn{display:inline-flex;align-items:center;justify-content:center;width:26px;height:24px;flex:0 0 auto;border:1px solid var(--border);border-radius:6px;background:var(--surface);color:var(--t2);cursor:pointer;transition:border-color .12s,color .12s,background .12s}
.co-loc-btn:hover{border-color:var(--accent);color:var(--accent)}
.co-loc-btn.on{border-color:var(--accent);color:var(--accent);background:var(--accent-soft)}
.co-loc-cur{max-width:150px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:10.5px;color:var(--t3)}
.co-loc-pop{left:0;width:320px;max-height:220px}
/* Copilot 权限胶囊条：三组等宽平分一行 + 组间显眼分割线 */
.perm-inline.perm-bar.co-perm-bar{gap:0;white-space:nowrap}
.co-perm-bar>.agy-perm-col{flex:1 1 0;min-width:0}
/* 首列（作用位置）内容少，按内容自适应收窄，避免留出大片空区 */
.co-perm-bar>.co-loc-col{flex:0 0 auto}
/* 列内均分：名称固定，选项组占满剩余宽度均匀分布，保证三组视觉等距 */
.co-perm-bar>.agy-perm-col>.perm-mode,
.co-perm-bar>.agy-perm-col>.agy-perm-box{flex:1 1 auto;justify-content:space-evenly;gap:0;min-width:0}
.co-perm-bar>.agy-perm-col>.co-loc-ctl{flex:1 1 auto;justify-content:flex-start;min-width:0}
.co-perm-bar>.agy-perm-sep{flex:none;width:1px;height:18px;align-self:center;margin:0 2px;background:var(--border-hover)}
/* URL 白/黑名单紧凑按钮 + 上拉小提示框（permissions 面板同行排布） */
.url-pop-wrap{position:relative}
.url-pop-btn{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--border);background:var(--surface);color:var(--t2);border-radius:var(--radius-sm);padding:5px 10px;font-size:11px;cursor:pointer;font-family:inherit;transition:border-color .12s,color .12s,background .12s;width:100%;justify-content:space-between}
.url-pop-btn:hover{border-color:var(--border-hover);color:var(--text)}
.url-pop-btn.on{border-color:var(--accent);color:var(--accent);background:var(--accent-soft)}
.url-pop-caret{font-size:8px;opacity:.7}
.url-pop{position:absolute;z-index:60;top:auto;bottom:calc(100% + 6px);left:0;width:280px;background:var(--surface);border:1px solid var(--border);border-radius:8px;box-shadow:0 -8px 24px rgba(0,0,0,.3);padding:8px;display:flex;flex-direction:column;gap:5px}
.url-pop textarea{width:100%;background:var(--bg);color:var(--text);border:1px solid var(--border);border-radius:6px;padding:6px 8px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:11px;line-height:1.5;resize:vertical;box-sizing:border-box}
.url-pop textarea:focus{outline:none;border-color:var(--accent)}
.url-pop-tip{font-size:10px;color:var(--t3);line-height:1.5}

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
.pi-model-block{border:1px solid var(--border);border-radius:8px;padding:8px;margin-bottom:6px;background:var(--surface)}
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

/* ===== 配置窗口内文本：统一主文本色（比纯白柔和），层级由末尾的 label/hint 规则细分 ===== */
.cfg-dialog{color:var(--text)}
.cfg-dialog .cfg-title,
.cfg-dialog .cfg-form .field label,
.cfg-dialog .cfg-form .field .hint,
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
.cfg-dialog .cc-nologin-msg,
.cfg-dialog .perm-cap-mark,
.cfg-dialog .row-del,
.cfg-dialog .cfg-file-btn{color:var(--text)}
/* 保留状态/强调色，避免统一文本色破坏功能性高亮 */
.cfg-dialog .cfg-tabs .cfg-tab.on{color:var(--accent)}
/* 层级还原：字段标签 / 说明回到次级色，避免整窗一片白、小字号发虚 */
.cfg-dialog .cfg-form .field label{color:var(--t2)}
.cfg-dialog .cfg-form .field .hint{color:var(--t3)}

/* ===== 配置项表格：多列多行 =====
   卡片即表格：配置项按列自动排布（每列最小 200px），单元格之间以 1px 细线分隔，
   表格四周有完整外框；不再是一条到底的长列表。 */
.cfg-dialog .cfg-body .cfg-card{
  display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:0;
  padding:0;background:none;border:0;border-radius:0;
  border-top:1px solid var(--border);border-bottom:1px solid var(--border);border-left:1px solid var(--border);
}
.cfg-dialog .cfg-body .cfg-card:hover{border-color:var(--border)}

/* 行容器拆散为单元格（display:contents），所有配置项汇入同一张表。
   容器匹配一律用 :where（含 :has 排除守卫）：这些类只为命中，不参与优先级——
   否则下方单元格规则会以更高特异性压过末尾「标签内嵌」的同属性规则，
   内嵌标签整体失效（标签被顶回上方、单元格内距不被复合框覆盖） */
.cfg-dialog .cfg-body .cfg-card > :where(.field-row,.fld-inline-row,.claude-one-line,.claude-grid,.cfg-grid):not(.model-row),
.cfg-dialog .cfg-body .cfg-card > :where(.cc-grid4,.agy-grid):where(:not(:has(.row-del)):not(:has(.cc-del-pill))){display:contents}

/* 单元格 = 一个配置项：标签在上、控件在下 */
.cfg-dialog .cfg-body .cfg-card > .field,
.cfg-dialog .cfg-body .cfg-card > :where(.field-row,.fld-inline-row,.claude-one-line,.claude-grid,.cfg-grid) > .field,
.cfg-dialog .cfg-body .cfg-card > :where(.cc-grid4,.agy-grid):where(:not(:has(.row-del)):not(:has(.cc-del-pill))) > .field{
  display:flex;flex-direction:column;justify-content:flex-end;gap:4px;
  padding:10px 14px;margin:0;min-width:0;background:var(--surface);
  border-right:1px solid var(--border);border-bottom:1px solid var(--border);border-left:0;border-top:0;
}
/* 标签：次级色、单行省略 */
.cfg-dialog .cfg-body .cfg-card > .field > label,
.cfg-dialog .cfg-body .cfg-card > :where(.field-row,.fld-inline-row,.claude-one-line,.claude-grid,.cfg-grid) > .field > label,
.cfg-dialog .cfg-body .cfg-card > :where(.cc-grid4,.agy-grid):where(:not(:has(.row-del)):not(:has(.cc-del-pill))) > .field > label{
  margin:0;min-width:0;font-size:10.5px;font-weight:600;line-height:1.4;color:var(--t2);
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;
}
/* 控件：吃满单元格 */
.cfg-dialog .cfg-body .cfg-card > .field > :is(input,select,textarea,.pi-sel,.ro-url),
.cfg-dialog .cfg-body .cfg-card > :where(.field-row,.fld-inline-row,.claude-one-line,.claude-grid,.cfg-grid) > .field > :is(input,select,textarea,.pi-sel,.ro-url),
.cfg-dialog .cfg-body .cfg-card > :where(.cc-grid4,.agy-grid):where(:not(:has(.row-del)):not(:has(.cc-del-pill))) > .field > :is(input,select,textarea,.pi-sel,.ro-url){
  width:100%!important;max-width:100%!important;min-width:0;min-height:26px;
  field-sizing:normal!important;box-sizing:border-box;
}
/* 宽项（文本域 / 整行字段 / 映射表）跨满整行 */
.cfg-dialog .cfg-body .cfg-card > .field:has(> textarea),
.cfg-dialog .cfg-body .cfg-card > .field.fw-full,
.cfg-dialog .cfg-body .cfg-card > .field.gm-full,
.cfg-dialog .cfg-body .cfg-card > .field.dc-map-field,
.cfg-dialog .cfg-body .cfg-card > :is(.field-row,.fld-inline-row,.claude-one-line,.claude-grid,.cfg-grid) > .field:has(> textarea),
.cfg-dialog .cfg-body .cfg-card > :is(.field-row,.fld-inline-row,.claude-one-line,.claude-grid,.cfg-grid) > .field.fw-full,
.cfg-dialog .cfg-body .cfg-card > :is(.field-row,.fld-inline-row,.claude-one-line,.claude-grid,.cfg-grid) > .field.gm-full{grid-column:1/-1}

/* 卡内整块内容：记录行、按钮行、芯片——各占一整行 */
.cfg-dialog .cfg-body .cfg-card > :not(.field):not(:is(.field-row,.fld-inline-row,.claude-one-line,.claude-grid,.cfg-grid,.perm-inline,.perm-bar,.agy-map-main,.pi-model-block,.agy-map-list,.model-row)){
  grid-column:1/-1;padding:10px 14px;
  border-right:1px solid var(--border);border-bottom:1px solid var(--border);
}
/* 记录行（模型 / 推理映射的动态增删行）：整行 */
.cfg-dialog .cfg-body .cfg-card > .model-row{
  grid-column:1/-1;padding:10px 14px;
  border-right:1px solid var(--border);border-bottom:1px solid var(--border);
}
/* 自带样式的块（权限胶囊、映射表、记录块）保留自身内边距，只补表格边线 */
.cfg-dialog .cfg-body .cfg-card > :is(.perm-inline,.perm-bar,.agy-map-main,.pi-model-block,.agy-map-list){
  grid-column:1/-1;border-right:1px solid var(--border);border-bottom:1px solid var(--border);
}


/* 标签内嵌：配置项统一为「左侧标签在框内 + 右侧控件」的复合框。
   适用范围放宽到「有标签且标签后面还有任意控件」的字段——文本控件、模型下拉、
   只读地址组、按钮、胶囊、映射表一律同构，标签宽度按文字自适应、不再固定抢宽。 */
.cfg-dialog .cfg-body .cfg-card .field:has(> label):has(> :not(label)){
  flex-direction:row;align-items:stretch;justify-content:flex-start;gap:0;
  padding:10px 0;background:var(--input-bg);border-radius:0;
}
.cfg-dialog .cfg-body .cfg-card .field:has(> label):has(> :not(label)) > label{
  position:relative;z-index:2;flex:0 0 auto;max-width:100%;
  display:block;line-height:26px;margin:0;padding:0 8px 0 10px;
  font-size:10.5px;font-weight:600;color:var(--t2);
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;
  border-right:1px solid var(--border);cursor:help;
}
/* 文本控件：去掉自身描边与底色，框线由单元格承担 */
.cfg-dialog .cfg-body .cfg-card .field:has(> label):has(> :not(label)) > :is(input,select,textarea){
  flex:1 1 auto;min-width:0;width:auto!important;border:0;background-color:transparent;
  padding:3px 8px;min-height:26px;
}
/* 包裹型控件（模型下拉 / 只读地址组）整体吃满右侧 */
.cfg-dialog .cfg-body .cfg-card .field:has(> label):has(> :not(label)) > :is(.pi-sel,.ro-url){flex:1 1 auto;min-width:0}
.cfg-dialog .cfg-body .cfg-card .field:has(> label):has(> :not(label)) > .pi-sel > input,
.cfg-dialog .cfg-body .cfg-card .field:has(> label):has(> :not(label)) > .ro-url > input{
  width:100%;border:0;background-color:transparent;padding:3px 8px;min-height:26px;
}
/* 其余控件（映射表 / 胶囊 / 列表）占满右侧，按钮按内容宽度 */
.cfg-dialog .cfg-body .cfg-card .field:has(> label):has(> :not(label)) > :not(label){flex:1 1 auto;min-width:0}
.cfg-dialog .cfg-body .cfg-card .field:has(> label):has(> :not(label)) > button{flex:0 0 auto;align-self:center;margin-left:8px}
.cfg-dialog .cfg-body .cfg-card .field:has(> label):has(> :not(label)):focus-within > label{color:var(--text);border-right-color:var(--accent)}
/* 内嵌标签下的 select 同样给全局自定义箭头留位（基础规则的内距被复合框覆盖） */
.cfg-dialog .cfg-body .cfg-card .field:has(> label):has(> :not(label)) > select{padding-right:28px}
/* 多控件字段（如 OpenCode「模型集合」内含多条映射行 + 添加按钮）：保持纵向堆叠，
   标签退化为框内首行分隔头，不再与各记录行横向挤在一行 */
.cfg-dialog .cfg-body .cfg-card .field:has(> .model-row){
  flex-direction:column;align-items:stretch;gap:6px;
}
.cfg-dialog .cfg-body .cfg-card .field:has(> .model-row) > label{
  border-right:0;border-bottom:1px solid var(--border);line-height:1.4;padding:0 0 6px;
}
/* 字段行（display:contents）里的独立按钮（如 Copilot 写入/清除）：作为网格单元
   自适应内容宽并垂直居中，避免拉伸成整格宽的巨型按钮 */
.cfg-dialog .cfg-body .cfg-card .field-row > .btn{justify-self:start;align-self:center;margin-left:6px}
/* grok 模型卡旧的「逐字段测宽」规则带 #id 特异性，会压过表格控件规则导致同一行
   控件宽窄不一；表格布局下单元格即框，控件统一吃满标签右侧（与其它客户端一致） */
.cfg-dialog .cfg-body .cfg-card #grok-panel-model .field > :is(input,select){
  flex:1 1 auto;min-width:24px;max-width:none;
}
</style>
