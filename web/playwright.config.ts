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
  // 产物目录默认落在 web/ 之外：Vite 会 watch 整个项目目录，测试进行中原样往 web/test-results
  // 写视频与 trace 会触发整页 reload，几何用例随即假红（这台机器上踩过）。
  // CI 用 --output 覆盖成 $RUNNER_TEMP 下的目录，本地默认到这里。
  outputDir: process.env.PW_OUTPUT_DIR ?? '../.e2e-artifacts',
  use: {
    baseURL: 'http://localhost:5199',
    viewport: { width: 1440, height: 900 },
    // 只在失败时留证据：本地全绿不落盘，CI 失败产物不至于只剩一个 .last-run.json。
    // 这条闸看的就是像素几何，没有截图的话红票只能靠猜。
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'npx vite --port 5199 --strictPort',
    url: 'http://localhost:5199/admin/',
    reuseExistingServer: true,
    timeout: 120_000,
  },
})
