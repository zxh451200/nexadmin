import { defineConfig, devices } from '@playwright/test'

/**
 * UI 几何回归闸：只跑「拖拽落位线」这类肉眼看不出错、改一行就静默破的几何断言。
 * 本地跑：cd web && npm run e2e        （会自己起 vite dev，复用已开的 5199）
 * 浏览器用系统里已装的 chromium 缓存，无需 playwright install。
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: 'http://localhost:5199',
    viewport: { width: 1440, height: 900 },
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'npx vite --port 5199 --strictPort',
    url: 'http://localhost:5199/admin/',
    reuseExistingServer: true,
    timeout: 120_000,
  },
})
