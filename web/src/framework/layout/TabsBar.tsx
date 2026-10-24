import { useEffect, useRef } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { MoreHorizontal, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { findMenuTrail } from '@/framework/router/discover'
import type { MenuNode } from '@/framework/router/types'
import { Button } from '@/framework/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/framework/ui/dropdown-menu'
import { HOME_PATH, resolveTab, useTabs, type TabItem } from './tabs-store'

interface TabsBarProps {
  menu: MenuNode[]
  /** 刷新当前页（重新挂载该页的缓存） */
  onRefresh: () => void
}

function TabActions({ path, onRefresh, onNavigate }: { path: string; onRefresh: () => void; onNavigate: (to: string) => void }) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const { tabs, closeOthers, closeRight, closeAll } = useTabs()
  const idx = tabs.findIndex((x) => x.path === path)
  const hasRight = tabs.slice(idx + 1).some((x) => x.path !== HOME_PATH)
  const hasOthers = tabs.some((x) => x.path !== HOME_PATH && x.path !== path)
  const hasClosable = tabs.some((x) => x.path !== HOME_PATH)

  return (
    <>
      <DropdownMenuItem
        onSelect={() => {
          if (path !== pathname) onNavigate(path)
          onRefresh()
        }}
      >
        {t('tabs.refresh')}
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem
        disabled={!hasOthers}
        onSelect={() => {
          closeOthers(path)
          if (path !== pathname) onNavigate(path)
        }}
      >
        {t('tabs.closeOthers')}
      </DropdownMenuItem>
      <DropdownMenuItem
        disabled={!hasRight}
        onSelect={() => {
          closeRight(path)
          // 当前页若在被关闭的右侧，回到该标签
          if (tabs.findIndex((x) => x.path === pathname) > idx) onNavigate(path)
        }}
      >
        {t('tabs.closeRight')}
      </DropdownMenuItem>
      <DropdownMenuItem
        disabled={!hasClosable}
        onSelect={() => {
          closeAll()
          onNavigate(HOME_PATH)
        }}
      >
        {t('tabs.closeAll')}
      </DropdownMenuItem>
    </>
  )
}

export function TabsBar({ menu, onRefresh }: TabsBarProps) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const { tabs, open, close } = useTabs()
  const listRef = useRef<HTMLDivElement>(null)
  const current = resolveTab(menu, pathname)

  // 路由变化时自动打开/激活；菜单尚未加载时先不记录，避免标题被写成 pathname
  useEffect(() => {
    if (menu.length === 0 && pathname !== HOME_PATH) return
    open(current)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current.path, current.titleKey, open])

  // 选中标签滚动到可见
  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>('[aria-current="page"]')?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }, [current.path, tabs.length])

  const closeTab = (path: string) => {
    const next = close(path)
    if (path === current.path) navigate(next)
  }

  const titleOf = (tab: TabItem) => {
    // 菜单标题在更新（如改名）后以菜单为准，否则用存储的 key；key 不存在时 t 会原样返回
    const fresh = findMenuTrail(menu, tab.path)[1]
    return t(fresh?.title ?? tab.titleKey)
  }

  return (
    <div className="shrink-0 border-b border-border bg-muted/30 px-2 py-1.5 md:px-4">
      <div ref={listRef} role="tablist" aria-label={t('tabs.bar')} className="flex items-center gap-1.5 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        {tabs.map((tab) => {
          const active = tab.path === current.path
          const pinned = tab.path === HOME_PATH
          return (
            <DropdownMenu key={tab.path}>
              {/* 右键：用 onContextMenu 触发同一个下拉；这里借助隐藏的受控方式较重，故右键走“⋯”触发器 */}
              <div
                role="presentation"
                className={cn(
                  'group/tab flex h-8 shrink-0 items-center rounded-full ps-3 text-sm transition-colors',
                  active
                    ? 'bg-background font-medium text-primary shadow-sm'
                    : 'text-muted-foreground hover:bg-background/60 hover:text-foreground',
                  pinned && 'pe-3',
                )}
                onContextMenu={(e) => {
                  e.preventDefault()
                  e.currentTarget.querySelector<HTMLButtonElement>('[data-tab-more]')?.click()
                }}
              >
                <button
                  type="button"
                  role="tab"
                  aria-selected={active}
                  aria-current={active ? 'page' : undefined}
                  onClick={() => navigate(tab.path)}
                  className="max-w-40 truncate rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {titleOf(tab)}
                </button>
                {!pinned && (
                  <>
                    <DropdownMenuTrigger asChild>
                      <button
                        type="button"
                        data-tab-more=""
                        aria-label={t('tabs.more')}
                        className="ms-1 flex size-5 items-center justify-center rounded-full hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      >
                        <MoreHorizontal className="size-3.5" aria-hidden="true" />
                      </button>
                    </DropdownMenuTrigger>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className="me-1 size-5 rounded-full p-0"
                      aria-label={t('tabs.close')}
                      onClick={() => closeTab(tab.path)}
                    >
                      <X className="size-3.5" aria-hidden="true" />
                    </Button>
                  </>
                )}
                {pinned && (
                  <DropdownMenuTrigger asChild>
                    <button type="button" data-tab-more="" tabIndex={-1} aria-hidden="true" className="sr-only" />
                  </DropdownMenuTrigger>
                )}
              </div>
              <DropdownMenuContent align="start" className="w-40">
                <TabActions path={tab.path} onRefresh={onRefresh} onNavigate={(to) => navigate(to)} />
              </DropdownMenuContent>
            </DropdownMenu>
          )
        })}
      </div>
    </div>
  )
}
