// 临时本地预览配置：dev 代理指向本机已初始化的后端 18089（不入库）
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

export default defineConfig({
  plugins: [react()],
  define: { __APP_VERSION__: JSON.stringify('dev-preview') },
  resolve: { alias: { '@': path.resolve(__dirname, 'src') } },
  server: {
    port: 5199,
    strictPort: true,
    proxy: { '/api': { target: 'http://127.0.0.1:18089', changeOrigin: true } },
  },
})
