// 下拉/单选按钮枚举值 → i18n 键 映射表。
// option 的 :value / value 保持写盘枚举不变,仅显示文本经此表查词典(zh/en)。
// 未命中(模型名、品牌、协议标识、shell 名、数字等)回退原文。
export const OPT_KEYS = {
  // 思考/努力等级
  minimal: 'sync.opt.minimal',
  low: 'sync.opt.low',
  medium: 'sync.opt.medium',
  high: 'sync.opt.high',
  xhigh: 'sync.opt.xhigh',
  max: 'sync.opt.max',
  none: 'sync.opt.none',
  // 布尔
  true: 'sync.opt.true',
  false: 'sync.opt.false',
  // 主题/配色
  auto: 'sync.opt.auto',
  dark: 'sync.opt.dark',
  light: 'sync.opt.light',
  terminal: 'sync.opt.terminal',
  // 通用状态
  default: 'sync.opt.default',
  always: 'sync.opt.always',
  never: 'sync.opt.never',
  enabled: 'sync.opt.enabled',
  disabled: 'sync.opt.disabled',
  all: 'sync.opt.all',
  off: 'sync.opt.off',
  fast: 'sync.opt.fast',
  slow: 'sync.opt.slow',
  // 权限/批准策略
  strict: 'sync.opt.strict',
  'request-review': 'sync.opt.requestReview',
  'always-proceed': 'sync.opt.alwaysProceed',
  'proceed-in-sandbox': 'sync.opt.proceedInSandbox',
  ask: 'sync.opt.ask',
  // 来源/模式
  manual: 'sync.opt.manual',
  migration: 'sync.opt.migration',
  env: 'sync.opt.env',
  yolo: 'sync.opt.yolo',
  disable: 'sync.opt.disable',
  'allow-auto-only': 'sync.opt.allowAutoOnly',
  act: 'sync.opt.act',
  plan: 'sync.opt.plan',
  rich: 'sync.opt.rich',
  compact: 'sync.opt.compact',
  // 终端执行方式
  backgroundExec: 'sync.opt.backgroundExec',
  foreground: 'sync.opt.foreground',
  // Gemini CLI 认证方式
  'gemini-api-key': 'sync.opt.geminiApiKey',
  'vertex-ai': 'sync.opt.vertexAi',
  'oauth-personal': 'sync.opt.oauthPersonal',
  'cloud-shell': 'sync.opt.cloudShell',
}
