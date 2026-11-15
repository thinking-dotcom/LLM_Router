// 配置面板共用的小工具与枚举常量（纯展示层，无组件状态）。
// 从 Clients.vue 抽出来供各客户端面板组件复用，避免面板之间复制粘贴。
import { t } from '../../i18n'
import { OPT_KEYS } from './optmap'
import { PERM_OPTIONS } from './shared'

// 下拉/单选选项显示本地化：命中 OPT_KEYS 走词典，未命中（模型名/品牌/协议标识等）回退原文
export const optTxt = v => {
  const k = v != null ? OPT_KEYS[v] : undefined
  return k ? t(k) : String(v ?? '')
}

// 三态（不写/是/否）下拉的显示文案：Copilot 沙箱、权限 URL 等共用
export const coSbTxt = v => (v === '' ? t('sync.notWrite') : v === 'true' ? t('sync.opt.true') : t('sync.opt.false'))

// 权限规则胶囊：每条规则一枚胶囊，点按循环 未设(灰虚线) → 允许(绿) → 询问(琥珀) → 拒绝(红)
// 写盘走各客户端既有模型，这里的 class/文案仅展示层
export const permCapCls = m => (m === 'allow' || m === 'ask' || m === 'deny') ? m : 'def'
export const permCapWord = (m, obj) => obj
  ? t('sync.permObjShort')
  : m === 'allow' ? t('sync.allow')
    : m === 'ask' ? t('sync.ask')
      : m === 'deny' ? t('sync.deny')
        : t('sync.opt.default')

// Claude 高级字段按类型排宽：bool/select/数字窄排并排，文本中等，list 独占一行
export const claudeFieldWidth = f =>
  f.type === 'list' ? 'fw-full'
    : (f.type === 'bool' || f.type === 'select' || f.type === 'number') ? 'fw-s'
      : 'fw-m'

// 权限模式四档配色：default(询问) / acceptEdits(自动接受编辑) / bypassPermissions(不询问) / plan(只读规划)
export const CLAUDE_PM_CLASS = { default: 'pm-ask', acceptEdits: 'pm-accept', bypassPermissions: 'pm-bypass', plan: 'pm-plan' }
export const pmModeCls = v => CLAUDE_PM_CLASS[v] || 'pm-plan'

// ---- agy ----
// 安全区四开关（settings.json 顶层布尔）
export const AGY_SAFE_KEYS = ['enableTerminalSandbox', 'allowNonWorkspaceAccess', 'useG1Credits', 'enableTelemetry']
// 工具权限五档：不写 / 严格 / 请求审查 / 始终放行 / 沙箱内放行（写盘枚举值不变）
export const AGY_TOOL_PERM_MODES = [
  { v: '', label: 'sync.notWrite' },
  { v: 'strict', label: 'sync.opt.strict' },
  { v: 'request-review', label: 'sync.opt.requestReview' },
  { v: 'always-proceed', label: 'sync.opt.alwaysProceed' },
  { v: 'proceed-in-sandbox', label: 'sync.opt.proceedInSandbox' },
]
// 「执行所有命令」三档：默认(不接管) / 允许 / 拒绝（command(*) 通配规则）
export const AGY_ALL_CMD_MODES = [
  { v: '', label: 'sync.opt.default', cls: 'neutral' },
  { v: 'allow', label: 'sync.allow', cls: 'allow' },
  { v: 'deny', label: 'sync.deny', cls: 'deny' },
]

// 统一权限面板：把各客户端形态各异的模式常量归一为 { v, label }
export const toModeOpts = list => (list || []).map(m => {
  const v = m.v !== undefined ? m.v : (m.value !== undefined ? m.value : '')
  const label = m.name ? m.name()
    : (m.label ? (String(m.label).startsWith('sync.') ? t(m.label) : String(m.label)) : String(v))
  return { v, label }
})

// ---- codebuddy ----
// 默认权限模式四档（沿用全客户端 PERM_OPTIONS 词条，agy 式单选胶囊呈现）
export const CB_DEFAULT_MODES = PERM_OPTIONS
// 「执行所有命令」三档：控制 Bash 通配规则落在哪一档位列表
export const CB_CMD_MODES = [
  { v: 'allow', cls: 'allow', label: 'sync.allow' },
  { v: 'ask', cls: 'ask', label: 'sync.ask' },
  { v: 'deny', cls: 'deny', label: 'sync.deny' },
]

// ---- commandcode ----
// 权限模式四档：写盘值为官方 default / auto-accept / bypass / plan
export const CC_PM_OPTIONS = [
  { value: 'default', cls: 'default', name: PERM_OPTIONS[0].name, desc: PERM_OPTIONS[0].desc },
  { value: 'auto-accept', cls: 'acceptEdits', name: PERM_OPTIONS[1].name, desc: PERM_OPTIONS[1].desc },
  { value: 'bypass', cls: 'bypassPermissions', name: PERM_OPTIONS[2].name, desc: PERM_OPTIONS[2].desc },
  { value: 'plan', cls: 'plan', name: PERM_OPTIONS[3].name, desc: PERM_OPTIONS[3].desc },
]

// ---- codex ----
// 审批策略四档：''=不写 / untrusted=仅不可信询问 / on-request=按需询问 / never=不询问
export const CX_PERM_MODES = [
  { v: '', name: () => t('sync.notWrite'), desc: () => t('sync.codex.pmDefaultDesc') },
  { v: 'untrusted', name: () => t('sync.codex.pmUntrustedName'), desc: () => t('sync.codex.pmUntrustedDesc') },
  { v: 'on-request', name: () => t('sync.codex.pmOnRequestName'), desc: () => t('sync.codex.pmOnRequestDesc') },
  { v: 'never', name: () => t('sync.codex.pmNeverName'), desc: () => t('sync.codex.pmNeverDesc') },
]
export const CX_PM_CLASS = { '': 'pm-plan', untrusted: 'pm-ask', 'on-request': 'pm-accept', never: 'pm-bypass' }

// ---- copilot ----
// 权限模式三档（官方仅支持持久化「批准」）：询问 / 自动接受编辑 / 只读
export const CO_PERM_MODES = [
  { v: 'ask', cls: 'default', name: () => t('sync.ask'), desc: () => t('sync.copilot.pmAskDesc') },
  { v: 'edit', cls: 'acceptEdits', name: PERM_OPTIONS[1].name, desc: () => t('sync.copilot.pmEditDesc') },
  { v: 'read', cls: 'plan', name: () => t('sync.permPlanName'), desc: () => t('sync.copilot.pmReadDesc') },
]
// 提权限制三态：''=不写（默认）/ disable=禁止 / allow-auto-only=仅自动批准
export const CO_BYPASS_MODES = [
  { v: '', label: 'sync.opt.default' },
  { v: 'disable', label: 'sync.copilot.bypassOff' },
  { v: 'allow-auto-only', label: 'sync.copilot.bypassAuto' },
]

// ---- claude ----
// 「命令」权限三态胶囊：只维护权限通配行 Bash（裸工具名 = 该工具全部终端命令）
export const CLAUDE_CMD_RULE = 'Bash'
export const CLAUDE_CMD_MODES = [
  { v: 'allow', label: 'sync.allow', cls: 'allow' },
  { v: 'ask', label: 'sync.ask', cls: 'ask' },
  { v: 'deny', label: 'sync.deny', cls: 'deny' },
]
