import { toast } from 'sonner'

import i18n from '@/framework/i18n/init'
import { ADMIN_BASE } from '@/framework/router/base'

/**
 * 发版后用户手上还是旧 bundle 的兜底：
 * 页面重新获得焦点时（以及每 10 分钟）拉一次 /admin/ 的 HTML，比对当前入口 chunk 名；
 * 不一致说明服务端已经换包，提示刷新，而不是让用户对着旧界面猜“到底改没改”。
 */
const CHECK_MS = 10 * 60_000
let lastChecked = 0

function currentEntry(): string {
  const s = document.querySelector<HTMLScriptElement>('script[type="module"][src]')
  return s?.src ?? ''
}

async function checkStale(force = false) {
  if (!force && Date.now() - lastChecked < CHECK_MS) return
  lastChecked = Date.now()
  try {
    const res = await fetch(`${ADMIN_BASE}/`, { cache: 'no-store', credentials: 'same-origin' })
    if (!res.ok) return
    const html = await res.text()
    const served = /assets\/index-[A-Za-z0-9_-]+\.js/.exec(html)?.[0]
    const running = currentEntry()
    if (!served || !running || running.includes(served)) return
    toast(i18n.t('pwa.staleBuild'), {
      id: 'stale-build',
      duration: Infinity,
      action: { label: i18n.t('pwa.refresh'), onClick: () => window.location.reload() },
    })
  } catch {
    // 离线或接口异常：不打扰
  }
}

export function watchBuildFreshness(): void {
  window.addEventListener('focus', () => void checkStale())
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') void checkStale()
  })
  window.setInterval(() => void checkStale(true), CHECK_MS)
}
