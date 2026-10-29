import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Check, ChevronLeft, ChevronRight, List, MoreHorizontal, X } from 'lucide-react'
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

/** RTL 下 scrollLeft 为负，统一成「从起点滚过的距离」，箭头与滚轮都按这个口径算 */
function stripMetrics(el: HTMLElement) {
  const max = Math.max(0, el.scrollWidth - el.clientWidth)
  const rtl = getComputedStyle(el).direction === 'rtl'
  const fromStart = rtl ? -Math.min(0, el.scrollLeft) : Math.max(0, el.scrollLeft)
  return { max, rtl, fromStart }
}

function stripScrollTo(el: HTMLElement, fromStart: number) {
  const { max, rtl } = stripMetrics(el)
  const target = Math.min(max, Math.max(0, fromStart))
  const reduce = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  el.scrollTo({ left: rtl ? -target : target, behavior: reduce ? 'auto' : 'smooth' })
}

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
  const [overflow, setOverflow] = useState({ any: false, start: false, end: false })
  const current = resolveTab(menu, pathname)

  const measure = useCallback(() => {
    const el = listRef.current
    if (!el) return
    const { max, fromStart } = stripMetrics(el)
    const next = { any: max > 1, start: fromStart > 1, end: fromStart < max - 1 }
    setOverflow((p) => (p.any === next.any && p.start === next.start && p.end === next.end ? p : next))
  }, [])

  // 路由变化时自动打开/激活；菜单尚未加载时先不记录，避免标题被写成 pathname
  useEffect(() => {
    if (menu.length === 0 && pathname !== HOME_PATH) return
    open(current)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current.path, current.titleKey, open])

  // 标签增减或容器变窄时重新量两端是否还有内容
  useEffect(() => {
    const el = listRef.current
    if (!el) return
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [measure, tabs])

  // 纵向滚轮转成横向滚动：标签条高度只有一行，否则滚轮只会把 2px 的纵向溢出顶来顶去
  useEffect(() => {
    const el = listRef.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      if (e.ctrlKey || Math.abs(e.deltaY) <= Math.abs(e.deltaX)) return
      const { max, rtl, fromStart } = stripMetrics(el)
      if (max <= 1) return
      const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? el.clientWidth : 1
      const target = Math.min(max, Math.max(0, fromStart + e.deltaY * unit * (rtl ? -1 : 1)))
      if (Math.abs(target - fromStart) < 1) return // 已经在边界，滚轮交给外层
      e.preventDefault()
      el.scrollLeft = rtl ? -target : target
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [])

  // 选中标签滚动到可见（两端让出箭头的位置）；不用 scrollIntoView，它会连带纵向滚动
  useEffect(() => {
    const el = listRef.current
    const active = el?.querySelector<HTMLElement>('[aria-current="page"]')
    if (!el || !active) return
    const item = active.closest<HTMLElement>('[data-tab-item]') ?? active
    const box = el.getBoundingClientRect()
    const r = item.getBoundingClientRect()
    const pad = 28
    const delta = r.left < box.left + pad ? r.left - box.left - pad : r.right > box.right - pad ? r.right - box.right + pad : 0
    if (!delta) return
    stripScrollTo(el, stripMetrics(el).fromStart + delta)
  }, [current.path, tabs.length])

  const nudge = (dir: -1 | 1) => {
    const el = listRef.current
    if (!el) return
    stripScrollTo(el, stripMetrics(el).fromStart + dir * Math.max(160, Math.round(el.clientWidth * 0.6)))
  }

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
      <div className="flex min-w-0 items-center">
        <div className="relative min-w-0 flex-1">
          {overflow.start && (
            <Button
              type="button"
              variant="outline"
              size="icon"
              data-tabs-scroll="start"
              aria-label={t('tabs.scrollBack')}
              onClick={() => nudge(-1)}
              className="absolute start-0 top-1/2 z-10 size-6 -translate-y-1/2 rounded-full bg-background/95 shadow-sm backdrop-blur"
            >
              <ChevronLeft className="size-3.5 rtl:rotate-180" aria-hidden="true" />
            </Button>
          )}
          <div
            ref={listRef}
            role="tablist"
            aria-label={t('tabs.bar')}
            onScroll={measure}
            className="-my-1 flex items-center gap-1.5 overflow-x-auto overflow-y-hidden py-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
          >
            {tabs.map((tab) => {
              const active = tab.path === current.path
              const pinned = tab.path === HOME_PATH
              return (
                <DropdownMenu key={tab.path}>
                  {/* 右键：用 onContextMenu 触发同一个下拉；这里借助隐藏的受控方式较重，故右键走“⋯”触发器 */}
                  <div
                    role="presentation"
                    data-tab-item=""
                    className={cn(
                      'group/tab flex h-8 shrink-0 items-center rounded-md ps-3 text-sm transition-colors',
                      active
                        ? 'bg-primary font-medium text-primary-foreground shadow-sm'
                        : 'text-muted-foreground hover:bg-accent hover:text-foreground',
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
                            className={cn(
                              'ms-1 flex size-5 items-center justify-center rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                              active ? 'text-primary-foreground/85 hover:bg-white/15 hover:text-primary-foreground' : 'hover:bg-muted',
                            )}
                          >
                            <MoreHorizontal className="size-3.5" aria-hidden="true" />
                          </button>
                        </DropdownMenuTrigger>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className={cn(
                            'me-1 size-5 rounded-full p-0',
                            active ? 'text-primary-foreground/85 hover:bg-white/15 hover:text-primary-foreground' : 'hover:bg-muted',
                          )}
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
          {overflow.end && (
            <Button
              type="button"
              variant="outline"
              size="icon"
              data-tabs-scroll="end"
              aria-label={t('tabs.scrollForward')}
              onClick={() => nudge(1)}
              className="absolute end-0 top-1/2 z-10 size-6 -translate-y-1/2 rounded-full bg-background/95 shadow-sm backdrop-blur"
            >
              <ChevronRight className="size-3.5 rtl:rotate-180" aria-hidden="true" />
            </Button>
          )}
        </div>
        {overflow.any && (
          <div className="ms-1.5 flex shrink-0 items-center border-s border-border ps-1.5">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button type="button" variant="ghost" size="icon" aria-label={t('tabs.list')} className="size-7 text-muted-foreground">
                  <List className="size-4" aria-hidden="true" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="max-h-72 w-56 overflow-y-auto">
                {tabs.map((tab) => (
                  <DropdownMenuItem key={tab.path} onSelect={() => navigate(tab.path)} className="gap-2">
                    <span className={cn('flex-1 truncate', tab.path === current.path && 'font-medium')}>{titleOf(tab)}</span>
                    {tab.path === current.path && <Check className="size-3.5 shrink-0 text-primary" aria-hidden="true" />}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        )}
      </div>
    </div>
  )
}
