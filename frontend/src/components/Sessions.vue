<script setup>
import { ref, computed, reactive, onMounted, nextTick } from 'vue'
import { t } from '../i18n'
import { toast } from '../toast'
import api from '../api'
import { targetLogo } from './sync/shared'
import { copyText } from '../utils'
import { PhoneIcon, BookOpenIcon } from '@heroicons/vue/24/outline'

// 会话管理：浏览 / 删除各 AI CLI 客户端落在本机磁盘上的历史会话记录。
// 主体为当前客户端的会话条目表，每条会话最左侧以该客户端 logo 标识归属；
// 客户端切换收进右下角浮动按钮（chatbot 图标 → 从右往左展开竖向客户端
// 图标栏），选中后收起。支持批量删除与终端恢复（电话图标在标题左侧：
// 点击直接用自带终端窗口恢复该会话，不再提供宿主选择）。
// 悬停会话 ID 在光标处浮现命令速查：该会话恢复命令 + 常用管理命令及解释。
// 客户端图标导航栏按最近点击（选中）时间自新到旧排列，点击即置顶并持久化
// （localStorage）；进入页面默认显示列表最上方客户端的会话记录。

const clients = ref([])      // 客户端概览（后端注册表驱动）
const activeId = ref('')     // 当前选中的客户端
const items = ref([])        // 当前客户端的会话条目
const loading = ref(false)
const navOpen = ref(false)      // 右下角浮动图标栏是否展开
const tip = ref(null)           // 会话 ID 悬停命令速查浮层：{entries,x,y}（null=收起）
const tipEl = ref(null)         // 命令速查浮层根节点（视口收敛用实测宽高）

// 客户端图标导航栏排序依据：各客户端最近一次被点击（选中）的时间戳（MRU），
// 持久化到 localStorage 跨启动保持；从未点过的不记录，保留后端顺序垫底。
const RECENT_KEY = 'llmrouter.sessions.recent'
const recent = reactive(loadRecent())
function loadRecent() {
  try { return JSON.parse(localStorage.getItem(RECENT_KEY) || '{}') || {} } catch (e) { return {} }
}
function saveRecent() {
  try { localStorage.setItem(RECENT_KEY, JSON.stringify(recent)) } catch (e) {}
}
// 清理已不存在客户端的过期记录（配置下线后不残留占位）
function pruneRecent() {
  const alive = new Set(clients.value.map(c => c.id))
  let dirty = false
  for (const k of Object.keys(recent)) {
    if (!alive.has(k)) { delete recent[k]; dirty = true }
  }
  if (dirty) saveRecent()
}
function markRecent(id) {
  recent[id] = Date.now()
  saveRecent()
}

const activeClient = computed(() => clients.value.find(c => c.id === activeId.value) || null)
// 行首客户端图标：单客户端视图下所有条目均属当前客户端
const rowLogo = computed(() => (activeClient.value && targetLogo(activeClient.value.id)) || '')

// 标题显示：有真实标题用原文（常规字体字重）；无标题回退会话 ID 时中间省略
// （等宽 + 次要色，避免一长串哈希主导整行视觉），完整 ID 保留在悬停提示
function titleOf(it) {
  if (it.title) return { text: it.title, isId: false }
  const id = it.id || it.name || ''
  return { text: id.length > 18 ? id.slice(0, 8) + '…' + id.slice(-6) : id, isId: true }
}
// 单元格悬停提示：标题会话给「ID · 路径」，ID 会话同样补全完整 ID；
// 库型存储的会话（it.cli）没有磁盘路径，Path 是会话 ID，改给「ID · 原始工作目录」
function tipOf(it) {
  const id = it.title ? it.id : (it.id || it.name)
  if (it.cli) return id ? id + (it.group ? ' · ' + it.group : '') : it.path
  return id ? id + ' · ' + it.path : it.path
}
// 修改时间分级显示：今天只给时分秒、当年给月日时分、更早给完整年月日；
// 完整时间串保留在悬停提示（title）
function fmtMtime(m) {
  const d = new Date(String(m || '').replace(/-/g, '/'))
  if (isNaN(d.getTime())) return m || ''
  const p = n => String(n).padStart(2, '0')
  const hm = p(d.getHours()) + ':' + p(d.getMinutes())
  const now = new Date()
  if (d.toDateString() === now.toDateString()) return hm + ':' + p(d.getSeconds())
  if (d.getFullYear() === now.getFullYear()) return p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + hm
  return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate())
}
// token 数压缩显示（列宽有限）：1234 → 1.2k，1234567 → 1.2M
// 进位边界：本档四舍五入满 1000 时升到上一档单位，避免出现「1000k / 1000M」
function fmtTokens(n) {
  const v = Number(n) || 0
  if (v >= 999.5e6) return (v / 1e9).toFixed(1) + 'B'
  if (v >= 999.5e3) return (v / 1e6).toFixed(v >= 1e7 ? 0 : 1) + 'M'
  if (v >= 1e3) return (v / 1e3).toFixed(v >= 1e5 ? 0 : 1) + 'k'
  return String(v)
}
// 图标栏顺序：最近点击过的按时间新→旧排最前，其余保持后端顺序垫底
const navClients = computed(() => {
  const used = clients.value.filter(c => recent[c.id])
  const rest = clients.value.filter(c => !recent[c.id])
  used.sort((a, b) => (recent[b.id] || 0) - (recent[a.id] || 0))
  return used.concat(rest)
})

// 对外状态：驱动页面顶部工具条（.page-head 位于本组件之外，由 App.vue 渲染）

function fmtBytes(n) {
  if (!n) return '0 B'
  // 进位边界：本档四舍五入满 1024 时升到上一档单位，避免出现「1024.0 KB」这类越界值
  if (n >= 1023.5 * 1048576) return (n / 1073741824).toFixed(1) + ' GB'
  if (n >= 1023.5 * 1024) return (n / 1048576).toFixed(1) + ' MB'
  if (n >= 1024) return (n / 1024).toFixed(1) + ' KB'
  return n + ' B'
}

async function loadClients(pick) {
  try { clients.value = (await api.GetSessionClients()) || [] } catch (e) { clients.value = [] }
  if (!clients.value.length) { activeId.value = ''; items.value = []; return }
  pruneRecent()
  const ids = clients.value.map(c => c.id)
  let id = pick && ids.includes(pick) ? pick : ''
  if (!id) {
    // 默认显示导航栏最上方客户端：MRU 命中时=最近点击；首次无记录=后端顺序第一
    const top = navClients.value.find(c => c.managed)
    id = top ? top.id : ids[0]
  }
  await selectClient(id)
}

async function selectClient(id, mark = false) {
  if (loading.value) return false
  activeId.value = id
  items.value = []
  loading.value = true
  try {
    const r = await api.GetSessionItems(id)
    if (r && r.ok) {
      items.value = r.items || []
      if (mark) markRecent(id)
      return true
    }
    toast(t('common.fail', { msg: r?.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
  finally { loading.value = false }
  return false
}

// 客户端图标点击：已接入（managed）才进入会话条目；未接入置灰，点击提示原因。
// 成功选中即刷新 MRU（该客户端置顶），随后收起浮动图标栏。
function pickClient(c) {
  if (!c.managed) {
    toast(c.note || t('sessions.unmanaged'))
    return
  }
  navOpen.value = false
  // 加载中被吞掉的切换不应提前改动 MRU，否则界面与导航顺序不一致
  selectClient(c.id, true)
}

// 客户端胶囊悬停提示：名称 + 会话统计（总大小）或缺失根目录说明。
function chipTip(c) {
  if (!c.managed) return [c.name, c.note || t('sessions.unmanaged')].join('\n')
  const lines = [c.name]
  if (c.rootExists) {
    lines.push(t('sessions.total', { n: c.count || 0, size: fmtBytes(c.totalSize) }))
  } else {
    lines.push(t('sessions.noRoot'))
    if (c.root) lines.push(c.root)
  }
  return lines.join('\n')
}

// 在系统文件管理器中打开（目录型会话选中目录本身）
async function openLocation(p) {
  try {
    const r = await api.OpenFileLocation(p)
    if (r && r.ok === false) toast(t('common.fail', { msg: r.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}

// 顶部工具条：在系统文件管理器中打开「当前客户端」的会话目录
//（原来是每行一个「打开所在目录」按钮，行多时既占地方又难找，统一挪到工具条）
function openClientDir() {
  const c = activeClient.value
  if (!c || !c.root) {
    toast(t('sessions.noRoot'))
    return
  }
  openLocation(c.root)
}

// 用自带的终端窗口恢复该会话（claude --resume <id> 等命令由后端按客户端组装）。
// 客户端不支持按会话恢复（Copilot/pi）时 toast 说明原因并返回 false。
async function openTerminal(p) {
  if (!activeClient.value?.resumeSupported) {
    toast(activeClient.value?.resumeNote || t('sessions.noResume'))
    return false
  }
  try {
    // 第三个参数仅为兼容旧后端签名（早期外部终端要选 cmd / powershell 宿主），
    // 自带终端窗口不再区分宿主，固定传 cmd。
    const r = await api.OpenSessionTerminal(activeId.value, p, 'cmd')
    if (r && r.ok === false) { toast(t('common.fail', { msg: r.error || '' })); return false }
    return true
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })); return false }
}

// 点击行首电话图标：直接用自带终端打开该会话（不再弹 cmd / PowerShell 选择浮层）。
function resumeInTerminal(it) {
  if (!it) return
  openTerminal(it.path)
}

// 各客户端「会话管理常用命令」速查（悬停会话 ID 时展示）。cmd 为可读命令原型，
// {id} 在悬停某条会话时替换为该会话 ID；命令与解释均经本机 CLI --help 校准。
// 「恢复该会话」命令始终由后端 item.cmd 首条提供（真实可执行），此处只列补充命令。
const CMD_HELP = {
  'claude-code': [
    { cmd: 'claude --continue', k: 'sessions.hlp.cont' },
    { cmd: 'claude --resume', k: 'sessions.hlp.pick' },
  ],
  codebuddy: [
    { cmd: 'codebuddy --continue', k: 'sessions.hlp.cont' },
    { cmd: 'codebuddy --resume', k: 'sessions.hlp.pick' },
  ],
  opencode: [
    { cmd: 'opencode --continue', k: 'sessions.hlp.cont' },
    { cmd: 'opencode session', k: 'sessions.hlp.sess' },
    { cmd: 'opencode export {id}', k: 'sessions.hlp.export' },
  ],
  'grok-build': [
    { cmd: 'grok --continue', k: 'sessions.hlp.cont' },
    { cmd: 'grok --resume', k: 'sessions.hlp.omit' },
  ],
  codex: [
    { cmd: 'codex resume', k: 'sessions.hlp.pick' },
    { cmd: 'codex archive {id}', k: 'sessions.hlp.arch' },
    { cmd: 'codex delete {id}', k: 'sessions.hlp.del' },
  ],
  deepcode: [
    { cmd: 'deepcode -l', k: 'sessions.hlp.cont' },
    { cmd: 'deepcode -r', k: 'sessions.hlp.pick' },
  ],
  mimocode: [
    { cmd: 'mimo -c', k: 'sessions.hlp.cont' },
    { cmd: 'mimo session list', k: 'sessions.hlp.hist' },
    { cmd: 'mimo export {id}', k: 'sessions.hlp.export' },
  ],
  agy: [
    { cmd: 'agy -c', k: 'sessions.hlp.cont' },
    { cmd: 'agy --conversation=', k: 'sessions.hlp.pick' },
  ],
  copilot: [
    { cmd: 'github-copilot --continue', k: 'sessions.hlp.cont' },
  ],
  commandcode: [
    { cmd: 'commandcode --continue', k: 'sessions.hlp.cont' },
    { cmd: 'commandcode --resume', k: 'sessions.hlp.pick' },
  ],
  kimi: [
    { cmd: 'kimi --continue', k: 'sessions.hlp.cont' },
    { cmd: 'kimi export {id}', k: 'sessions.hlp.exportZip' },
  ],
  // pi：未安装且未登记会话管理命令 → 悬停不弹速查（原生路径提示兜底）
}

// 悬停条目列表：首条 = 该会话真实恢复命令（后端 cmd 字段，与行内复制一致），
// 其后为该客户端的常用管理命令（{id} 占位替换为该会话 ID）。无内容返回 null。
function tipEntries(it) {
  const lines = []
  if (it.cmd) lines.push({ cmd: it.cmd, k: 'sessions.openTerminal' })
  const id = it.id || it.name || ''
  for (const x of CMD_HELP[activeId.value] || []) {
    lines.push({ cmd: x.cmd.split('{id}').join(id), k: x.k })
  }
  return lines.length ? lines : null
}

// ---- 会话 ID 悬停：命令速查浮层跟随光标，始终位于光标正下方 ----
// 划入会话 ID 列时先按光标原位显示，随后随鼠标在列内移动持续平移；
// x 对齐光标横坐标、y 在光标下方错开 16px。收敛仅按视口右/下边缘向内
// 钳制（用实测宽高），不做上下/左右翻转，避免贴边时来回跳变。
function placeCmdTip(e) {
  const t = tip.value
  if (!t) return
  const pad = 12
  const el = tipEl.value
  const w = el ? el.offsetWidth : 0
  const h = el ? el.offsetHeight : 0
  t.x = Math.min(Math.max(e.clientX, pad), Math.max(pad, innerWidth - w - pad))
  t.y = Math.min(Math.max(e.clientY + 16, pad), Math.max(pad, innerHeight - h - pad))
}
async function showCmdTip(it, e) {
  const entries = tipEntries(it)
  tip.value = entries ? { entries, x: e.clientX, y: e.clientY + 16 } : null
  if (entries) {
    await nextTick() // 等浮层挂载，随后用实测宽高把溢出部分收敛回视口内
    placeCmdTip(e)
  }
}
function moveCmdTip(e) { placeCmdTip(e) }
function hideCmdTip() { tip.value = null }

// 点击会话 ID 左侧复制按钮：复制该会话的恢复命令行（不支持恢复时提示原因）。
function copyCmd(it, e) {
  if (!it.cmd) {
    toast(activeClient.value?.resumeNote || t('sessions.noResume'))
    return
  }
  copyText(it.cmd, e.currentTarget)
}

// 「阅读」：开独立窗口渲染该会话的对话内容（--read 精简实例，后端解析 jsonl）
async function openReader(it) {
  try {
    const r = await api.OpenSessionReader(activeId.value, it.path, titleOf(it).text)
    if (r && r.ok === false) toast(t('common.fail', { msg: r.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}

// 每行行尾垃圾桶图标删除：确认对话框 → 删除该单条（目录型整棵移除，不可恢复）
const confirming = ref(false)
const delTarget = ref(null) // 待删除的会话条目（null=收起）
function askDeleteOne(it) { delTarget.value = it; confirming.value = true }
async function doDelete() {
  const it = delTarget.value
  confirming.value = false
  delTarget.value = null
  if (!it || !activeId.value) return
  try {
    const r = await api.DeleteSessionItems(activeId.value, [it.path])
    if (r && r.ok) {
      toast(t('sessions.deleted', { n: 1 }))
      await loadClients(activeId.value) // 删除后刷新概览与条目
    } else toast(t('common.fail', { msg: r?.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}

// 小导航栏底部「一键清空」：清空当前客户端的全部会话（逐路径仍由后端
// 校验位于该客户端会话根目录内且层级相符，越界一律拒绝）。
const clearOpen = ref(false)
const clearCount = ref(0)
function askClearAll() {
  if (!activeId.value) return
  if (!items.value.length) {
    toast(t('sessions.nothingToClear'))
    return
  }
  clearCount.value = items.value.length
  clearOpen.value = true
}
async function doClearAll() {
  clearOpen.value = false
  const paths = items.value.map(it => it.path)
  if (!paths.length || !activeId.value) return
  try {
    const r = await api.DeleteSessionItems(activeId.value, paths)
    if (r && r.ok) {
      toast(t('sessions.cleared', { n: paths.length }))
      await loadClients(activeId.value)
    } else toast(t('common.fail', { msg: r?.error || '' }))
  } catch (e) { toast(t('common.fail', { msg: e?.message || '' })) }
}

// ---- 暴露给 App.vue 顶部工具条（.page-head）的动作 ----
// 刷新：重载客户端概览并保持当前选中
function refresh() { return loadClients(activeId.value) }

onMounted(() => loadClients())
defineExpose({ loadClients, refresh, openClientDir, askClearAll })
</script>

<template>
  <div class="sessions-wrap">
    <!-- 会话条目表（客户端切换在右下角浮动图标栏） -->
    <!-- 面板整体的大背景：当前客户端的 logo 居中淡显，切换客户端时自动跟随 -->
    <div class="recent-card sess-card" :style="{ '--sess-logo': rowLogo ? 'url(' + rowLogo + ')' : 'none' }">
      <div class="logs-scroll">
        <table v-if="items.length" class="logs-table sess-table">
          <colgroup>
            <col class="c-name"><col class="c-msgs"><col class="c-tokens"><col class="c-time"><col class="c-logo">
          </colgroup>
          <thead>
            <tr>
              <th class="th-name">{{ t('sessions.colName') }}</th>
              <th class="th-num">{{ t('sessions.colMsgs') }}</th>
              <th class="th-num">{{ t('sessions.colTokens') }}</th>
              <th>{{ t('sessions.colTime') }}</th>
              <!-- 「恢复会话」图标列在最右：末尾空表头与它一一对应 -->
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="it in items" :key="it.path">
              <td
                class="sr-name"
                :title="tipEntries(it) ? undefined : tipOf(it)"
                @mouseenter="showCmdTip(it, $event)" @mousemove="moveCmdTip" @mouseleave="hideCmdTip"
              >
                <span class="sr-inner">
                  <!-- 阅读：书本图标，置于「恢复会话」电话图标左侧；
                       库型存储的会话（it.cli，如 MiMo Code）没有可解析的转写文件，不显示 -->
                  <span
                    v-if="!it.cli"
                    class="sr-logo-ico"
                    :title="t('sessions.read')"
                    :aria-label="t('sessions.read')"
                    role="button" tabindex="0"
                    @click.stop="openReader(it)" @keydown.enter.prevent="openReader(it)"
                  >
                    <BookOpenIcon class="ico" style="width:11px;height:11px;stroke-width:2.2" />
                  </span>
                  <!-- 恢复会话：电话图标，置于标题左侧 -->
                  <PhoneIcon
                    class="ico sr-logo-ico"
                    :title="activeClient?.resumeSupported ? t('sessions.openTerminal') : (activeClient?.resumeNote || t('sessions.noResume'))"
                    :aria-label="t('sessions.openTerminal')"
                    role="button" tabindex="0"
                    @click.stop="resumeInTerminal(it)" @keydown.enter.prevent="resumeInTerminal(it)"
                  />
                  <span class="sr-id" :class="{ 'is-id': titleOf(it).isId }">{{ titleOf(it).text }}</span>
                </span>
              </td>
              <td class="sr-msgs" :class="{ 'sr-nil': !it.stats }">{{ it.stats ? it.msgs : '—' }}</td>
              <td class="sr-tokens" :class="{ 'sr-nil': !(it.stats && it.tokensIn + it.tokensOut > 0) }" :title="(it.stats && it.tokensIn + it.tokensOut > 0) ? t('sessions.tokensTip', { in: fmtTokens(it.tokensIn), out: fmtTokens(it.tokensOut) }) : ''">{{ (it.stats && it.tokensIn + it.tokensOut > 0) ? fmtTokens(it.tokensIn + it.tokensOut) : '—' }}</td>
              <td class="sr-time">
                <!-- 客户端图标不再逐行占位：只在面板整体背景上淡显一次（见 .sess-card::before） -->
                <span class="st-cell"><span class="st-mtime" :title="it.mtime">{{ fmtMtime(it.mtime) }}</span></span>
              </td>
              <td class="sr-logo">
                <!-- 复制恢复命令：原在标题胶囊右侧，与「恢复会话」电话图标互换位置 -->
                <button
                  class="sr-copy" :class="{ off: !it.cmd }"
                  :title="it.cmd ? undefined : (activeClient?.resumeNote || t('sessions.noResume'))"
                  :aria-label="it.cmd ? t('sessions.copyCmd') : (activeClient?.resumeNote || t('sessions.noResume'))"
                  @click.stop="copyCmd(it, $event)"
                >
                  <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                </button>
                <!-- 删除该会话：确认后单条删除（目录型整棵移除，不可恢复） -->
                <button
                  class="sr-copy sr-del"
                  :title="t('sessions.deleteItem')" :aria-label="t('sessions.deleteItem')"
                  @click.stop="askDeleteOne(it)"
                >
                  <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty-tip">
          <template v-if="loading">{{ t('sessions.loading') }}</template>
          <template v-else-if="activeClient && !activeClient.rootExists">{{ t('sessions.noRoot') }}<br><span class="sr-path">{{ activeClient.root }}</span></template>
          <template v-else-if="activeClient">{{ t('sessions.empty') }}</template>
        </div>
      </div>
    </div>

    <!-- 右下角浮动：chatbot 图标 → 展开/收起客户端图标导航栏 -->
    <div class="sess-float">
      <transition name="sess-nav">
        <div v-if="navOpen" class="sess-nav">
          <div class="sess-nav-list">
            <div v-if="!clients.length" class="empty-tip" style="padding:6px 4px">{{ t('sessions.noClients') }}</div>
            <!-- 图标自上而下：最近点击(选中)的在前，未点过的按后端顺序垫底 -->
            <button
              v-for="c in navClients" :key="c.id"
              class="sess-chip" :class="{ on: c.id === activeId, empty: c.managed && !c.rootExists, unmanaged: !c.managed }"
              :title="chipTip(c)"
              @click="pickClient(c)"
            >
              <img v-if="targetLogo(c.id)" class="sess-chip-logo" :src="targetLogo(c.id)" alt="" draggable="false" />
            </button>
          </div>
        </div>
      </transition>
      <button
        class="sess-fab" :class="{ on: navOpen }"
        :title="t('sessions.chooseClient')"
        @click="navOpen = !navOpen"
      >
        <img src="/logos/chatbot.svg" alt="agents" draggable="false" />
      </button>
    </div>

    <!-- 会话 ID 悬停：命令速查浮层跟随光标、位于其正下方（恢复该会话 + 常用管理命令）
         Teleport 到 body：脱离 .panel(深色主题含 backdrop-filter)的包含块约束，
         保证 position:fixed 相对视口、与 clientX/Y 坐标一致 -->
    <Teleport to="body">
      <div v-if="tip" ref="tipEl" class="cmd-tip" :style="{ left: tip.x + 'px', top: tip.y + 'px' }">
        <div v-for="(n, i) in tip.entries" :key="i" class="ct-line">
          <code class="ct-cmd">{{ n.cmd }}</code>
          <span class="ct-note">{{ t(n.k) }}</span>
        </div>
      </div>
    </Teleport>

    <!-- 以下弹层 Teleport 到 body：深色主题 .panel 带 backdrop-filter，会成为 fixed 子元素
         的包含块，留在面板内时遮罩只覆盖面板、弹窗按面板居中；脱离面板才覆盖整个窗口 -->
    <Teleport to="body">

    <!-- 删除确认（应用内对话框，删除不可恢复） -->
    <div class="dialog-overlay" :class="{ open: confirming }" @click.self="confirming=false">
      <div class="dialog" style="width:min(420px, calc(100% - 24px))">
        <h3>{{ t('sessions.deleteTitle') }}</h3>
        <p class="delete-desc">{{ t('sessions.confirmDeleteOne', { name: delTarget ? (delTarget.title || delTarget.id || delTarget.name || '') : '' }) }}</p>
        <div class="dialog-foot">
          <span></span>
          <div class="right">
            <button class="btn btn-ghost btn-sm" type="button" @click="confirming=false">{{ t('dlg.cancel') }}</button>
            <button class="btn btn-danger btn-sm" type="button" @click="doDelete">{{ t('dlg.confirmDeleteBtn') }}</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 一键清空确认（当前客户端全部会话，删除不可恢复） -->
    <div class="dialog-overlay" :class="{ open: clearOpen }" @click.self="clearOpen=false">
      <div class="dialog" style="width:min(440px, calc(100% - 24px))">
        <h3>{{ t('sessions.clearTitle') }}</h3>
        <p class="delete-desc">{{ t('sessions.confirmClearAll', { name: activeClient ? activeClient.name : '', n: clearCount }) }}</p>
        <div class="dialog-foot">
          <span></span>
          <div class="right">
            <button class="btn btn-ghost btn-sm" type="button" @click="clearOpen=false">{{ t('dlg.cancel') }}</button>
            <button class="btn btn-danger btn-sm" type="button" @click="doClearAll">{{ t('dlg.confirmDeleteBtn') }}</button>
          </div>
        </div>
      </div>
    </div>
    </Teleport>
  </div>
</template>
