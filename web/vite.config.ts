import { createLogger, defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

const here = import.meta.dirname

const logger = createLogger()
const postcssFromWarning = 'A PostCSS plugin did not pass the `from` option to `postcss.parse`'
const warn = logger.warn.bind(logger)
const warnOnce = logger.warnOnce.bind(logger)
logger.warn = (msg, options) => {
  if (!msg.includes(postcssFromWarning)) warn(msg, options)
}
logger.warnOnce = (msg, options) => {
  if (!msg.includes(postcssFromWarning)) warnOnce(msg, options)
}

export default defineConfig({
  customLogger: logger,
  plugins: [react()],
  define: { __APP_VERSION__: JSON.stringify(process.env.npm_package_version ?? 'dev') },
  resolve: {
    alias: { '@': path.resolve(here, 'src') },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true },
      // 官网插件资源（上传媒体 /website-assets/media/<id>、主题静态资源）注册在后端根路径，
      // 不带 /api 前缀；不转发的话管理端里 <img> 在 dev 下全部裂图（生产单二进制同域无此问题）。
      '/website-assets': { target: 'http://127.0.0.1:8080', changeOrigin: true },
    },
  },
  build: {
    sourcemap: false,
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined
          if (/[\\/]node_modules[\\/](react|react-dom|react-router|react-router-dom|i18next|react-i18next)[\\/]/.test(id)) {
            return 'framework-vendor'
          }
          if (/[\\/]node_modules[\\/](recharts|d3-[^/\\]+)[\\/]/.test(id)) return 'charts-vendor'
          if (/[\\/]node_modules[\\/](xlsx|exceljs)[\\/]/.test(id)) return 'data-vendor'
          if (/[\\/]node_modules[\\/](@radix-ui|lucide-react|sonner|cmdk|tailwind-merge|class-variance-authority)[\\/]/.test(id)) {
            return 'ui-vendor'
          }
          return undefined
        },
      },
    },
  },
})
