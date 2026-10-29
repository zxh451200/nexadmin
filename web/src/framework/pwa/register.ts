import i18n from 'i18next'
import { toast } from 'sonner'
import { ADMIN_BASE } from '@/framework/router/base'
import { initInstallPrompt, pwaEnabled } from './install'

const SW_URL = '/sw.js'
const UPDATE_TOAST_ID = 'pwa-update'
const UPDATE_CHECK_MS = 60 * 60_000

/** 登出/会话失效时清理运行时缓存，避免共用设备上泄露上一位用户可见的资源 */
export function clearPwaRuntimeCache(): void {
  if (!pwaEnabled()) return
  navigator.serviceWorker.controller?.postMessage({ type: 'CLEAR_RUNTIME' })
  if ('caches' in window) {
    void caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith('nex-runtime-')).map((k) => caches.delete(k))))
      .catch(() => undefined)
  }
}

export function registerServiceWorker(): void {
  if (!pwaEnabled()) return
  initInstallPrompt()

  let reloadRequested = false
  // 只在用户点击“刷新”之后才随新 SW 接管而重载；首次安装时 clients.claim 触发的 controllerchange 不处理
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (reloadRequested) window.location.reload()
  })

  const promptUpdate = (reg: ServiceWorkerRegistration) => {
    toast(i18n.t('pwa.updateAvailable'), {
      id: UPDATE_TOAST_ID,
      duration: Infinity,
      action: {
        label: i18n.t('pwa.refresh'),
        onClick: () => {
          if (reg.waiting) {
            reloadRequested = true
            reg.waiting.postMessage({ type: 'SKIP_WAITING' })
          } else {
            window.location.reload()
          }
        },
      },
    })
  }

  window.addEventListener('load', () => {
    void (async () => {
      try {
        // 迁移：早期版本把 SW 注册在 / 作用域；后台收敛到 /admin 后注销根作用域注册，
        // 避免旧 worker 继续控制官网页面并覆盖离线回退。
        const regs = await navigator.serviceWorker.getRegistrations()
        await Promise.all(regs.filter((reg) => new URL(reg.scope).pathname === '/').map((reg) => reg.unregister()))
      } catch {
        // 迁移失败不阻塞新作用域注册
      }
      try {
        const reg = await navigator.serviceWorker.register(SW_URL, { scope: `${ADMIN_BASE}/` })
        // 已有等待中的新版本（上次访问时下载完成但未激活）
        if (reg.waiting && navigator.serviceWorker.controller) promptUpdate(reg)
        reg.addEventListener('updatefound', () => {
          const next = reg.installing
          next?.addEventListener('statechange', () => {
            if (next.state === 'installed' && navigator.serviceWorker.controller) promptUpdate(reg)
          })
        })
        navigator.serviceWorker.addEventListener('message', (e: MessageEvent<{ type?: string }>) => {
          if (e.data?.type === 'SW_UPDATE_READY' && reg.waiting) promptUpdate(reg)
        })
        // 长期不关闭的标签页也能发现新版本
        let last = Date.now()
        document.addEventListener('visibilitychange', () => {
          if (document.visibilityState === 'visible' && Date.now() - last > UPDATE_CHECK_MS) {
            last = Date.now()
            void reg.update().catch(() => undefined)
          }
        })
      } catch {
        // 注册失败不影响应用本身
      }
    })()
  })
}
