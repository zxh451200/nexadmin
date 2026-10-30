// 临时校验用配置（与本机 preview-local 相同，额外忽略构建产物目录，避免文件锁把 watcher 打崩）
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

export default defineConfig({
  plugins: [react()],
  define: { __APP_VERSION__: JSON.stringify('dev-preview') },
  resolve: { alias: { '@': path.resolve(__dirname, 'src') } },
  server: {
    port: 5200,
    strictPort: true,
    proxy: { '/api': { target: 'http://127.0.0.1:18089', changeOrigin: true } },
    watch: { ignored: ['**/dist*/**', '**/playwright-report/**', '**/test-results/**'] },
  },
})
