// Wails Go↔JS 绑定。桌面环境中由 Wails 注入 window.go.main.App；
// 纯浏览器环境下不存在，调用方需自行容错（与原版行为一致）。
const api = window.go?.main?.App || {}

export default api
