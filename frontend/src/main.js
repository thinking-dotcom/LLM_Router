import { createApp } from 'vue'
import App from './App.vue'
import { initTheme } from './store'
import api from './api'
import './selectMenu'
import './tooltip'

initTheme()
// 独立窗口实例（--read 阅读器 / --term 内嵌终端）：Wails 固定加载根路径
// index.html，这里按后端标志分流挂载对应窗口页。窗口页动态 import，
// 主界面 bundle 不含它们。
async function boot() {
  let page = App
  try {
    if (api.IsReaderWindow && await api.IsReaderWindow()) {
      page = (await import('./components/ReaderWindow.vue')).default
    } else if (api.IsTermWindow && await api.IsTermWindow()) {
      page = (await import('./components/TermWindow.vue')).default
    }
  } catch (e) {}
  try {
    createApp(page).mount('#app')
  } catch (e) {
    console.error('window page mount failed:', e)
  }
}
boot()
