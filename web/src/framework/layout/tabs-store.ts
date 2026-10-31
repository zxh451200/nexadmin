import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import { SESSION_CLEARED_EVENT } from '@/framework/auth/session-events'
import { findMenuTrail } from '@/framework/router/discover'
import type { MenuNode } from '@/framework/router/types'

export interface TabItem {
  path: string
  /** i18n key；找不到菜单时为 pathname 本身 */
  titleKey: string
}

export const HOME_PATH = '/dashboard' // 工作台

interface TabsState {
  tabs: TabItem[]
  /** 打开（或更新标题）一个标签 */
  open: (tab: TabItem) => void
  /** 关闭一个标签，返回关闭后应激活的相邻标签 path（关闭的不是当前时由调用方忽略） */
  close: (path: string) => string
  closeOthers: (path: string) => void
  closeRight: (path: string) => void
  closeAll: () => void
}

/** keep-alive 最多同时缓存的页面数（含当前页） */
export const MAX_CACHED_PAGES = 10

/** 在菜单里找 pathname 对应的叶子，返回其 path 与标题 key；找不到时退回 pathname 本身 */
export function resolveTab(menu: MenuNode[], pathname: string): TabItem {
  const hit = findMenuTrail(menu, pathname)[1]
  return hit?.path ? { path: hit.path, titleKey: hit.title } : { path: pathname, titleKey: pathname }
}

const HOME_TAB: TabItem = { path: HOME_PATH, titleKey: 'dashboard.menu' }

export const useTabs = create<TabsState>()(
  persist(
    (set, get) => ({
      tabs: [HOME_TAB],
      open: (tab) =>
        set((s) => {
          const idx = s.tabs.findIndex((x) => x.path === tab.path)
          if (idx < 0) return { tabs: [...s.tabs, tab] }
          if (s.tabs[idx].titleKey === tab.titleKey) return s
          const next = [...s.tabs]
          next[idx] = tab
          return { tabs: next }
        }),
      close: (path) => {
        const { tabs } = get()
        if (path === HOME_PATH) return HOME_PATH
        const idx = tabs.findIndex((x) => x.path === path)
        if (idx < 0) return HOME_PATH
        const next = tabs.filter((x) => x.path !== path)
        set({ tabs: next })
        return (next[idx] ?? next[idx - 1] ?? HOME_TAB).path
      },
      closeOthers: (path) => set((s) => ({ tabs: s.tabs.filter((x) => x.path === HOME_PATH || x.path === path) })),
      closeRight: (path) =>
        set((s) => {
          const idx = s.tabs.findIndex((x) => x.path === path)
          return idx < 0 ? s : { tabs: s.tabs.filter((x, i) => i <= idx || x.path === HOME_PATH) }
        }),
      closeAll: () => set({ tabs: [HOME_TAB] }),
    }),
    { name: 'nex-tabs', storage: createJSONStorage(() => sessionStorage), version: 1 },
  ),
)

// 登出/会话失效时收起所有标签，避免换账号后残留上一账号的页签。
if (typeof window !== 'undefined') {
  window.addEventListener(SESSION_CLEARED_EVENT, () => useTabs.getState().closeAll())
}
