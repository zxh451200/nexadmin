import { useCallback, useEffect, useSyncExternalStore } from 'react'
import i18n from 'i18next'
import { toast } from 'sonner'
import { MOCK_ENABLED } from '@/framework/env'

/** 仅生产构建且非 mock 模式启用（开发/演示模式下 SW 会缓存过期资源，干扰热更新与 mock） */
export function pwaEnabled(): boolean {
  return import.meta.env.PROD && !MOCK_ENABLED && typeof navigator !== 'undefined' && 'serviceWorker' in navigator
}

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

let deferred: BeforeInstallPromptEvent | null = null
const listeners = new Set<() => void>()
const emit = () => listeners.forEach((l) => l())

/** 尽早监听 beforeinstallprompt（React 挂载前就可能触发），由 registerServiceWorker 调用 */
export function initInstallPrompt(): void {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    deferred = e as BeforeInstallPromptEvent
    emit()
  })
  window.addEventListener('appinstalled', () => {
    deferred = null
    emit()
  })
}

/** 菜单里的“安装应用”：仅浏览器认为可安装且尚未安装时 canInstall 为 true */
export function usePwaInstall() {
  const canInstall = useSyncExternalStore(
    (cb) => {
      listeners.add(cb)
      return () => listeners.delete(cb)
    },
    () => deferred !== null,
    () => false,
  )
  const install = useCallback(async () => {
    const ev = deferred
    if (!ev) return
    deferred = null // 一个事件只能 prompt 一次
    emit()
    await ev.prompt()
    await ev.userChoice.catch(() => undefined)
  }, [])
  return { canInstall, install }
}

const IOS_HINT_KEY = 'nex-pwa-ios-hint'

function isIosSafariNotInstalled(): boolean {
  const ua = navigator.userAgent
  const ios = /iphone|ipad|ipod/i.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1) // iPadOS 伪装成 Mac
  if (!ios || /CriOS|FxiOS|EdgiOS|OPiOS|GSA\//.test(ua)) return false
  const standalone = (navigator as Navigator & { standalone?: boolean }).standalone === true || window.matchMedia('(display-mode: standalone)').matches
  return !standalone
}

/** iOS Safari 没有 beforeinstallprompt：提示一次“分享 → 添加到主屏幕”，关闭后不再出现 */
export function useIosInstallHint(): void {
  useEffect(() => {
    if (!pwaEnabled() || !isIosSafariNotInstalled()) return
    try {
      if (localStorage.getItem(IOS_HINT_KEY) === '1') return
    } catch {
      return
    }
    const remember = () => {
      try {
        localStorage.setItem(IOS_HINT_KEY, '1')
      } catch {
        // 隐私模式下写不进去，只是下次还会提示
      }
    }
    const timer = window.setTimeout(() => {
      toast(i18n.t('pwa.iosHint'), {
        id: 'pwa-ios-hint',
        duration: Infinity,
        closeButton: true,
        onDismiss: remember,
      })
    }, 3000)
    return () => window.clearTimeout(timer)
  }, [])
}
