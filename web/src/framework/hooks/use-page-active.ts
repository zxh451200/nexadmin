import { createContext, useContext, useEffect, useRef, useSyncExternalStore } from 'react'

/** AppLayout 给每个缓存页包一层：当前标签为 true，被 hidden 隐藏的缓存页为 false */
export const PageActiveContext = createContext<boolean>(true)

/** 页面是否处于当前标签；没有 Provider（如移动端、独立页）时恒为 true */
export function usePageActive(): boolean {
  return useContext(PageActiveContext)
}

function subscribeVisibility(cb: () => void) {
  document.addEventListener('visibilitychange', cb)
  return () => document.removeEventListener('visibilitychange', cb)
}

/** 页面既是当前标签、浏览器标签页也在前台时为 true；轮询应以此为开关 */
export function usePageLive(): boolean {
  const active = usePageActive()
  const visible = useSyncExternalStore(
    subscribeVisibility,
    () => !document.hidden,
    () => true,
  )
  return active && visible
}

/** 从暂停恢复为 live 时立即调用一次 refresh（首次挂载不触发） */
export function useRefreshOnResume(live: boolean, refresh: () => unknown, enabled = true) {
  const prev = useRef(live)
  const fn = useRef(refresh)
  fn.current = refresh
  useEffect(() => {
    if (enabled && live && !prev.current) void fn.current()
    prev.current = live
  }, [live, enabled])
}
