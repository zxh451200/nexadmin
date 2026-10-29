import { Fragment, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link, useLocation, useNavigate, useOutlet } from 'react-router-dom'
import { ChevronsUpDown, Download, LogOut, Maximize, Menu, Minimize, PanelLeftClose, PanelLeftOpen, RotateCw, Search, UserRound } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useAuth } from '@/framework/auth/useAuth'
import { LanguageSwitch } from '@/framework/i18n/LanguageSwitch'
import { api } from '@/framework/request/http'
import { useActivePlugins } from '@/framework/modules/plugin/active'
import { buildMenuFromServer, buildMenuTree, findMenuTrail, type ServerMenu } from '@/framework/router/discover'
import { ThemeToggle } from '@/framework/theme/ThemeToggle'
import { Avatar, AvatarFallback, AvatarImage } from '@/framework/ui/avatar'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/framework/ui/breadcrumb'
import { Button } from '@/framework/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/framework/ui/dropdown-menu'
import { Sheet, SheetContent, SheetDescription, SheetTitle } from '@/framework/ui/sheet'
import { Skeleton } from '@/framework/ui/skeleton'
import { AnnouncementPopup } from './AnnouncementPopup'
import { CommandMenu } from './CommandMenu'
import { JobIndicator } from './JobIndicator'
import { NotificationBell } from './NotificationBell'
import { Sidebar } from './Sidebar'
import { TabsBar } from './TabsBar'
import { TenantExpireBanner } from './TenantExpireBanner'
import { MAX_CACHED_PAGES, resolveTab, useTabs } from './tabs-store'
import { Watermark } from './Watermark'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { PageActiveContext } from '@/framework/hooks/use-page-active'
import { useIosInstallHint, usePwaInstall } from '@/framework/pwa/install'

const COLLAPSE_KEY = 'nex-sidebar-collapsed'

/** 目录节点没有自己的页面，取它下面第一个有路径的页面 */
function firstLeafPath(node: { path?: string; children?: { path?: string; children?: unknown[] }[] }): string | undefined {
  if (node.path) return node.path
  for (const c of node.children ?? []) {
    const p = firstLeafPath(c as Parameters<typeof firstLeafPath>[0])
    if (p) return p
  }
  return undefined
}

const pageFallback = (
  <div className="space-y-4 p-4 md:p-6">
    <Skeleton className="h-8 w-48" />
    <Skeleton className="h-64 w-full" />
  </div>
)

function UserMenu() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { user, logout } = useAuth()
  const { canInstall, install } = usePwaInstall()
  const name = user?.nickname || user?.username || ''

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" className="h-11 gap-2 px-2 md:h-9" aria-label={t('layout.userMenu')}>
          <Avatar className="size-7">
            {user?.avatar && <AvatarImage src={user.avatar} alt="" />}
            <AvatarFallback className="text-xs">{name.slice(0, 1).toUpperCase()}</AvatarFallback>
          </Avatar>
          <span className="hidden max-w-28 truncate text-sm md:inline">{name}</span>
          <ChevronsUpDown className="hidden size-3.5 text-muted-foreground md:inline" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel className="font-normal">
          <p className="truncate text-sm font-medium">{name}</p>
          <p className="truncate text-xs text-muted-foreground">{user?.username}</p>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem className="min-h-11 md:min-h-8" onSelect={() => navigate('/profile')}>
          <UserRound className="me-2 size-4" aria-hidden="true" />
          {t('layout.profile')}
        </DropdownMenuItem>
        {canInstall && (
          <DropdownMenuItem className="min-h-11 md:min-h-8" onSelect={() => void install()}>
            <Download className="me-2 size-4" aria-hidden="true" />
            {t('pwa.install')}
          </DropdownMenuItem>
        )}
        <DropdownMenuItem
          className="min-h-11 md:min-h-8"
          onSelect={async () => {
            await logout()
            navigate('/login', { replace: true })
          }}
        >
          <LogOut className="me-2 size-4" aria-hidden="true" />
          {t('layout.logout')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function AppLayout() {
  const { t } = useTranslation()
  const location = useLocation()
  const perms = useAuth((s) => s.perms)
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSE_KEY) === '1')
  const [mobileOpen, setMobileOpen] = useState(false)
  const [cmdOpen, setCmdOpen] = useState(false)
  const [refreshes, setRefreshes] = useState<Record<string, number>>({})
  const [cache, setCache] = useState<Map<string, ReactNode>>(() => new Map())
  const scrollTops = useRef(new Map<string, number>())
  const mainRef = useRef<HTMLElement>(null)
  const outlet = useOutlet()
  const tabs = useTabs((s) => s.tabs)
  const [isFullscreen, setIsFullscreen] = useState(() => !!document.fullscreenElement)
  const isMobile = useIsMobile()
  const isMac = /mac|iphone|ipad/i.test(navigator.platform)
  useIosInstallHint()

  const toggleFullscreen = () => {
    if (document.fullscreenElement) void document.exitFullscreen()
    else void document.documentElement.requestFullscreen?.()
  }
  useEffect(() => {
    const onChange = () => setIsFullscreen(!!document.fullscreenElement)
    document.addEventListener('fullscreenchange', onChange)
    return () => document.removeEventListener('fullscreenchange', onChange)
  }, [])

  // 侧边栏来自后端菜单（菜单管理里改了会立即生效，键以 sysMenu 开头以便一起刷新）；后端取不到时退回到按路由生成
  const { data: serverMenu, isError } = useQuery({
    queryKey: ['sysMenu', 'mine'],
    queryFn: () => api.get<ServerMenu[]>('/menus/mine', undefined, { silent: true }),
    staleTime: 60_000,
    retry: false,
  })
  // 插件失效（过期 / 吊销 / 未授权）时，它的菜单项也一并隐藏，不留点了就报错的入口
  const { data: activePlugins } = useActivePlugins()
  const activeSet = useMemo(() => (activePlugins ? new Set(activePlugins) : null), [activePlugins])
  const menu = useMemo(
    () => (serverMenu ? buildMenuFromServer(serverMenu, perms, undefined, activeSet) : isError ? buildMenuTree(perms, undefined, activeSet) : []),
    [serverMenu, isError, perms, activeSet],
  )
  const activeKey = resolveTab(menu, location.pathname).path
  const refresh = useCallback(() => setRefreshes((r) => ({ ...r, [activeKey]: (r[activeKey] ?? 0) + 1 })), [activeKey])

  // keep-alive：记录每个标签最近一次渲染的 outlet 元素；LRU 上限 MAX_CACHED_PAGES，已关闭的标签随即卸载
  useEffect(() => {
    if (isMobile) return
    const open = new Set(tabs.map((x) => x.path))
    setCache((prev) => {
      const next = new Map(prev)
      next.delete(activeKey)
      next.set(activeKey, outlet)
      for (const k of [...next.keys()]) if (k !== activeKey && !open.has(k)) next.delete(k)
      while (next.size > MAX_CACHED_PAGES) next.delete(next.keys().next().value as string)
      return next
    })
  }, [activeKey, location.pathname, tabs, isMobile]) // eslint-disable-line react-hooks/exhaustive-deps

  // 切换标签后恢复该页的滚动位置
  useLayoutEffect(() => {
    if (mainRef.current) mainRef.current.scrollTop = scrollTops.current.get(activeKey) ?? 0
  }, [activeKey])

  // 按标签顺序排列，切换时 DOM 不会被重排；当前页若还没进 tabs 则排在最后
  const tabOrder = tabs.map((x) => x.path)
  const pageKeys = tabOrder.filter((k) => k === activeKey || cache.has(k))
  if (!pageKeys.includes(activeKey)) pageKeys.push(activeKey)
  const trail = findMenuTrail(menu, location.pathname).filter((n) => n.title) // 顶级单项的分组没有标题，不进面包屑

  const toggleCollapsed = () => {
    setCollapsed((c) => {
      localStorage.setItem(COLLAPSE_KEY, c ? '0' : '1')
      return !c
    })
  }

  return (
    <div className="flex h-dvh bg-background text-foreground">
      <a
        href="#main-content"
        className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:absolute focus:start-2 focus:top-2"
      >
        {t('layout.skipToContent')}
      </a>

      <aside className={cn('hidden shrink-0 border-e border-border transition-[width] duration-200 motion-reduce:transition-none md:block', collapsed ? 'w-16' : 'w-64')}>
        <Sidebar menu={menu} collapsed={collapsed} onToggleCollapse={toggleCollapsed} />
      </aside>

      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent side="start" className="w-72 p-0" closeLabel={t('common.close')}>
          <SheetTitle className="sr-only">{t('layout.navigation')}</SheetTitle>
          <SheetDescription className="sr-only">{t('layout.navigation')}</SheetDescription>
          <Sidebar menu={menu} onNavigate={() => setMobileOpen(false)} />
        </SheetContent>
      </Sheet>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center gap-1 border-b border-border bg-background/80 px-2 backdrop-blur md:gap-2 md:px-4">
          <Button variant="ghost" size="icon" className="size-11 md:hidden" aria-label={t('layout.openMenu')} onClick={() => setMobileOpen(true)}>
            <Menu className="size-5" aria-hidden="true" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="hidden size-9 md:inline-flex"
            aria-label={collapsed ? t('layout.expandSidebar') : t('layout.collapseSidebar')}
            onClick={toggleCollapsed}
          >
            {collapsed ? <PanelLeftOpen className="size-4" aria-hidden="true" /> : <PanelLeftClose className="size-4" aria-hidden="true" />}
          </Button>

          <Breadcrumb className="min-w-0 flex-1">
            <BreadcrumbList className="flex-nowrap">
              <BreadcrumbItem className="hidden md:inline-flex">
                <BreadcrumbLink asChild>
                  <Link to="/">{t('layout.home')}</Link>
                </BreadcrumbLink>
              </BreadcrumbItem>
              {trail.map((node, i) => (
                <Fragment key={node.key}>
                  <BreadcrumbSeparator className="hidden md:list-item" />
                  <BreadcrumbItem className={cn('min-w-0', i === 0 && 'hidden md:inline-flex')}>
                    {i === trail.length - 1 ? (
                      <BreadcrumbPage className="truncate">{t(node.title)}</BreadcrumbPage>
                    ) : firstLeafPath(node) ? (
                      // 目录本身没有页面，点击跳到它下面第一个页面
                      <BreadcrumbLink asChild>
                        <Link to={firstLeafPath(node)!} className="truncate">
                          {t(node.title)}
                        </Link>
                      </BreadcrumbLink>
                    ) : (
                      <span className="truncate text-muted-foreground">{t(node.title)}</span>
                    )}
                  </BreadcrumbItem>
                </Fragment>
              ))}
            </BreadcrumbList>
          </Breadcrumb>

          <Button
            variant="outline"
            className="hidden h-9 w-56 justify-start gap-2 px-3 text-sm font-normal text-muted-foreground md:inline-flex"
            aria-label={t('cmdk.open')}
            onClick={() => setCmdOpen(true)}
          >
            <Search className="size-4" aria-hidden="true" />
            <span className="flex-1 truncate text-start">{t('cmdk.placeholder')}</span>
            <kbd className="rounded border border-border bg-muted px-1.5 text-xs">{isMac ? '⌘K' : 'Ctrl K'}</kbd>
          </Button>
          <Button variant="ghost" size="icon" className="size-11 md:hidden" aria-label={t('cmdk.open')} onClick={() => setCmdOpen(true)}>
            <Search className="size-5" aria-hidden="true" />
          </Button>
          <Button variant="ghost" size="icon" className="hidden size-9 md:inline-flex" aria-label={t('tabs.refreshPage')} onClick={refresh}>
            <RotateCw className="size-4" aria-hidden="true" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="hidden size-9 md:inline-flex"
            aria-label={isFullscreen ? t('tabs.exitFullscreen') : t('tabs.fullscreen')}
            onClick={toggleFullscreen}
          >
            {isFullscreen ? <Minimize className="size-4" aria-hidden="true" /> : <Maximize className="size-4" aria-hidden="true" />}
          </Button>
          <LanguageSwitch />
          <ThemeToggle />
          <JobIndicator />
          <NotificationBell />
          <UserMenu />
        </header>

        <TenantExpireBanner />

        {!isMobile && <TabsBar menu={menu} onRefresh={refresh} />}

        <main
          id="main-content"
          ref={mainRef}
          className="min-h-0 flex-1 overflow-y-auto pb-[env(safe-area-inset-bottom)]"
          tabIndex={-1}
          onScroll={(e) => scrollTops.current.set(activeKey, e.currentTarget.scrollTop)}
        >
          {isMobile ? (
            <Suspense fallback={pageFallback}>{outlet}</Suspense>
          ) : (
            // 每个标签一个容器，非当前的用 hidden 隐藏而不卸载，状态（筛选、分页等）得以保留
            pageKeys.map((k) => (
              <div key={`${k}#${refreshes[k] ?? 0}`} className={k === activeKey ? undefined : 'hidden'}>
                <PageActiveContext.Provider value={k === activeKey}>
                  <Suspense fallback={pageFallback}>{k === activeKey ? outlet : cache.get(k)}</Suspense>
                </PageActiveContext.Provider>
              </div>
            ))
          )}
        </main>
      </div>

      <AnnouncementPopup />
      <CommandMenu menu={menu} open={cmdOpen} onOpenChange={setCmdOpen} />
      <Watermark />
    </div>

  )
}

export default AppLayout
