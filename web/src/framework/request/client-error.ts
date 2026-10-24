import { tokenStore, useMock } from './http'
import {
  buildPayload,
  createThrottle,
  fingerprint,
  isIgnorable,
  type ErrorPayload,
} from './client-error-core'

/** 构建时注入（vite.config.ts define），取不到时为 dev */
const VERSION = typeof __APP_VERSION__ === 'string' ? __APP_VERSION__ : 'dev'
const ENDPOINT = `${import.meta.env.VITE_API_BASE || '/api/v1'}/client-errors`

/** 开发模式与 mock 模式不上报：此时没有后端接收，报错也只是噪声 */
const disabled = () => import.meta.env.DEV || useMock

// 每分钟最多 5 条；同一类错误 60 秒内只报一次（服务端按指纹聚合计数）
const throttle = createThrottle({ maxPerMinute: 5, dedupeWindowMs: 60_000 })

function describe(value: unknown): { message: string; stack?: string } {
  if (value instanceof Error) return { message: value.message || value.name, stack: value.stack }
  if (typeof value === 'string') return { message: value }
  try {
    return { message: JSON.stringify(value) ?? String(value) }
  } catch {
    return { message: String(value) }
  }
}

function send(payload: ErrorPayload) {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const token = tokenStore.access
  if (token) headers.Authorization = `Bearer ${token}`
  // 直接用 fetch：不经过 axios 拦截器（401 刷新、错误 toast），上报失败也不能再触发上报
  fetch(ENDPOINT, { method: 'POST', headers, body: JSON.stringify(payload), keepalive: true, credentials: 'omit' }).catch(
    () => undefined,
  )
}

/** 上报一个错误（脱敏、节流、去重都在这里）；永远不会抛出 */
export function reportClientError(error: unknown, extra?: { componentStack?: string }) {
  try {
    if (disabled()) return
    const { message, stack } = describe(error)
    if (isIgnorable(message)) return
    const payload = buildPayload({
      message,
      stack: stack || extra?.componentStack,
      route: window.location.pathname,
      ua: navigator.userAgent,
      version: VERSION,
    })
    if (!payload || !throttle.allow(fingerprint(payload))) return
    send(payload)
  } catch {
    // 上报本身出错只能吞掉
  }
}

let installed = false

/** 挂 window 的 error 与 unhandledrejection 监听（幂等）；React 渲染错误由 ErrorBoundary 上报 */
export function installClientErrorReporter() {
  if (installed || disabled()) return
  installed = true
  window.addEventListener('error', (e: ErrorEvent) => {
    // 资源加载失败的 error 事件目标是元素，不是 window，这里只关心脚本错误
    if (e.target !== window) return
    reportClientError(e.error ?? e.message)
  })
  window.addEventListener('unhandledrejection', (e: PromiseRejectionEvent) => {
    reportClientError(e.reason)
  })
}
