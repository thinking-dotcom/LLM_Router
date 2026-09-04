import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// base 保持默认 '/'：Wails 的 assetserver 以嵌入的 dist 根目录提供服务，
// 绝对路径（/assets/...、/style.css、/fonts/...）都能正确解析。
export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    host: '127.0.0.1',
  },
})
