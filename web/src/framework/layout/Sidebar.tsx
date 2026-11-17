import { useEffect, useMemo } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { ChevronDown, ChevronsDownUp, ChevronsUpDown, PanelLeftClose, PanelLeftOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/framework/ui/button'
import { BrandMark } from '@/framework/components/BrandMark'
import { Popover, PopoverContent, PopoverTrigger } from '@/framework/ui/popover'
import { ScrollArea } from '@/framework/ui/scroll-area'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/framework/ui/tooltip'
import type { MenuNode } from '@/framework/router/types'
import { usePersistedSet } from '@/framework/hooks/use-persisted-set'
import { DynamicIcon } from './DynamicIcon'
import { useMenuBadges } from './menuBadge'
import { HOME_PATH } from './tabs-store'
import { usePublicSettings } from './Watermark'

const GROUPS_KEY = 'nex-sidebar-groups' // 记住哪些一级目录是收起的

interface SidebarProps {
  menu: MenuNode[]
  /** Logo 的落点（菜单里第一个能进的页面）；缺省回落到工作台 */
  homePath?: string
  collapsed?: boolean
  /** 点击菜单项后的回调（手机抽屉用来关闭） */
  onNavigate?: () => void
  /** 提供后在底部显示「收起 / 展开侧边栏」按钮（手机抽屉不需要） */
  onToggleCollapse?: () => void
}

/** 收起态（icon rail）的一级目录：图标触发浮出子菜单（shadcn sidebar collapsible="icon" 的形态）。
    旧实现把所有叶子平铺成超长图标列，菜单多的站点一屏放不下；浮出后轨上只留每目录一枚图标。 */
function GroupFlyout({ group, onNavigate }: { group: MenuNode; onNavigate?: () => void }) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const childPaths = useMemo(() => (group.children ?? []).filter((c) => c.path).map((c) => c.path as string), [group])
  const badges = useMenuBadges(childPaths)
  const active = childPaths.some((p) => pathname === p || pathname.startsWith(p + '/'))
  const badgeTotal = childPaths.reduce((n, p) => n + (badges[p] ?? 0), 0)

  return (
    <Popover>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger
            className={cn(
              'relative flex h-9 w-full items-center justify-center rounded-md transition-colors',
              'hover:bg-sidebar-accent/60 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring',
              active && 'bg-sidebar-accent/60 text-sidebar-accent-foreground',
            )}
            aria-label={t(group.title!)}
            aria-haspopup="menu"
          >
            <DynamicIcon name={group.icon} className="size-4" />
            {badgeTotal > 0 && <span aria-hidden="true" className="absolute inset-e-2.5 top-1.5 size-2 rounded-full bg-primary" />}
          </PopoverTrigger>
        </TooltipTrigger>
        {/* Tooltip 与 Popover 同用一枚触发器：hover 给目录名，点击弹子菜单（Tooltip 的 asChild 桥接见上） */}
        <TooltipContent side="right">{t(group.title!)}</TooltipContent>
      </Tooltip>
      <PopoverContent side="right" align="start" sideOffset={6} className="w-56 p-2">
        <p className="px-2 pb-1.5 text-xs font-medium text-muted-foreground">{t(group.title!)}</p>
        <ul className="space-y-0.5">
          {group.children?.map((item) => {
            const badge = item.path ? (badges[item.path] ?? 0) : 0
            return (
              <li key={item.key}>
                <NavLink
                  to={item.path!}
                  onClick={onNavigate}
                  className={({ isActive }) =>
                    cn(
                      'flex min-h-9 items-center gap-2.5 rounded-md px-2.5 text-sm transition-colors',
                      'hover:bg-sidebar-accent/60 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring',
                      isActive ? 'bg-primary/10 font-medium text-primary' : 'text-sidebar-foreground/90',
                    )
                  }
                >
                  <DynamicIcon name={item.icon} className="size-4 shrink-0" />
                  <span className="min-w-0 flex-1 truncate">{t(item.title)}</span>
                  {badge > 0 && (
                    <span className="inline-flex min-w-5 items-center justify-center rounded-full bg-primary px-1.5 py-0.5 text-[11px] leading-none font-medium text-primary-foreground">
                      {badge > 99 ? '99+' : badge}
                    </span>
                  )}
                </NavLink>
              </li>
            )
          })}
        </ul>
      </PopoverContent>
    </Popover>
  )
}

export function Sidebar({ menu, homePath = HOME_PATH, collapsed = false, onNavigate, onToggleCollapse }: SidebarProps) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  // 收起的目录记在 localStorage，刷新、切页、重进后台都保持
  const { has: isClosed, toggle: toggleGroup, remove: openGroup, replace: setClosed } = usePersistedSet(GROUPS_KEY)
  const { data: settings } = usePublicSettings()

  // 当前页面所在的一级目录自动展开（并写回记忆），避免选中项藏在收起的目录里
  useEffect(() => {
    const g = menu.find((m) => m.children?.some((c) => c.path && (pathname === c.path || pathname.startsWith(c.path + '/'))))
    if (g) openGroup(g.key)
  }, [pathname, menu, openGroup])

  // 有可折叠的一级目录时，提供「全部展开 / 全部收起」（跟随所有目录的当前状态切换）
  const groupKeys = menu.filter((g) => g.title && (g.children?.length ?? 0) > 0).map((g) => g.key)
  const allClosed = groupKeys.length > 0 && groupKeys.every((k) => isClosed(k))
  const toggleAllGroups = () => setClosed(allClosed ? [] : groupKeys)

  // 菜单徽标：只对当前可见的叶子菜单项轮询（源由各模块/插件注册，见 menuBadge.ts）
  const leafPaths = useMemo(
    () => menu.flatMap((g) => (g.children ?? []).filter((c) => c.path).map((c) => c.path as string)),
    [menu],
  )
  const badges = useMenuBadges(leafPaths)

  const renderItem = (item: MenuNode, nested: boolean) => {
    const badge = item.path ? (badges[item.path] ?? 0) : 0
    // 外链菜单（如官网插件的「打开官网」）：不走路由、不开标签页，新标签页打开目标地址。
    // 开发环境前端跑在 Vite，同源根路径不是官网——按 vite.config 代理目标拼后端地址。
    const itemHref = item.external && item.path
      ? import.meta.env.DEV && item.path.startsWith('/')
        ? `${location.protocol}//${location.hostname}:8080${item.path}`
        : item.path
      : undefined
    const shared = cn(
      'relative flex min-h-11 items-center gap-3 rounded-md px-3 text-sm transition-colors md:min-h-9',
      'hover:bg-sidebar-accent/60 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring',
      // 一级行（含单项与目录标题）统一字重，层级差交给缩进与引导线，不用字重表达
      !nested && 'font-medium',
      nested ? 'text-sidebar-foreground/80' : 'text-sidebar-foreground',
      // 收起成图标轨时：整行等宽保证图标水平居中；隐藏激活项左侧竖条（那是展开列表的指示器，轨内显得突兀）
      collapsed && 'w-full justify-center px-0 before:hidden',
    )
    const content = (
      <>
        {/* 图标与文字同色：侧栏不上分组色调，层级只靠缩进、引导线与激活态表达 */}
        <DynamicIcon name={item.icon} className={cn('size-5 shrink-0', !collapsed && 'md:size-4')} />
        {!collapsed && <span className="truncate">{t(item.title)}</span>}
        {/* 徽标：展开态是行尾计数，收起态是图标上的圆点；0 时不渲染 */}
        {badge > 0 &&
          (collapsed ? (
            <span aria-hidden="true" className="absolute inset-e-2 top-2 size-2 rounded-full bg-primary" />
          ) : (
            <span className="ms-auto inline-flex min-w-5 items-center justify-center rounded-full bg-primary px-1.5 py-0.5 text-[11px] leading-none font-medium text-primary-foreground">
              {badge > 99 ? '99+' : badge}
            </span>
          ))}
      </>
    )
    const link = item.external ? (
      <a
        href={itemHref}
        target="_blank"
        rel="noreferrer"
        aria-label={collapsed ? t(item.title) : undefined}
        className={shared}
      >
        {content}
      </a>
    ) : (
      <NavLink
        to={item.path!}
        onClick={onNavigate}
        aria-label={collapsed ? t(item.title) : undefined}
        className={({ isActive }) =>
          cn(
            shared,
            isActive
              ? cn(
                  // 激活态整体走品牌色：底色/文字/左条同源，和 hover 的中性 accent 拉开对比——
                  // 之前的 sidebar-accent 底色与 hover 几乎同调，选中看不出来
                  'bg-primary/10 font-medium text-primary hover:bg-primary/15',
                  'before:absolute before:inset-y-2 before:w-0.5 before:rounded-full before:bg-primary',
                  // 二级的激活竖条压在引导线上，免得同一条轨道上出现两条竖线
                  nested ? 'before:inset-s-[-9px]' : 'before:inset-s-0',
                )
              : nested
                ? 'text-sidebar-foreground/80'
                : 'text-sidebar-foreground',
          )
        }
      >
        {/* 图标与文字同色：侧栏不上分组色调，层级只靠缩进、引导线与激活态表达 */}
        <DynamicIcon name={item.icon} className={cn('size-5 shrink-0', !collapsed && 'md:size-4')} />
        {!collapsed && <span className="truncate">{t(item.title)}</span>}
        {/* 徽标：展开态是行尾计数，收起态是图标上的圆点；0 时不渲染 */}
        {badge > 0 &&
          (collapsed ? (
            <span aria-hidden="true" className="absolute inset-e-2 top-2 size-2 rounded-full bg-primary" />
          ) : (
            <span className="ms-auto inline-flex min-w-5 items-center justify-center rounded-full bg-primary px-1.5 py-0.5 text-[11px] leading-none font-medium text-primary-foreground">
              {badge > 99 ? '99+' : badge}
            </span>
          ))}
      </NavLink>
    )
    return (
      <li key={item.key}>
        {collapsed ? (
          <Tooltip>
            {/* NavLink 的 className 是函数：直接当 Slot 子节点会被 Radix 合并成一段字符串，
                结果链接失去全部样式（收起态图标变成浏览器默认链接色）。用 span 承接 Tooltip。 */}
            <TooltipTrigger asChild>
              <span className="flex w-full justify-center">{link}</span>
            </TooltipTrigger>
            <TooltipContent side="right">{t(item.title)}</TooltipContent>
          </Tooltip>
        ) : (
          link
        )}
      </li>
    )
  }

  return (
    <div className="flex h-full flex-col bg-sidebar text-sidebar-foreground">
      <NavLink
        to={homePath}
        onClick={onNavigate}
        aria-label={settings?.siteName || t('layout.appName')}
        className={cn(
          'flex h-14 shrink-0 items-center gap-2 border-b border-border px-4 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring',
          collapsed && 'justify-center px-0',
        )}
      >
        <BrandMark name={settings?.siteName || t('layout.appName')} logoUrl={settings?.logoUrl} className="size-8" iconClassName="size-5" />
        {!collapsed && <span className="truncate text-base font-semibold tracking-tight">{settings?.siteName || t('layout.appName')}</span>}
      </NavLink>

      <ScrollArea className="flex-1">
        <nav aria-label={t('layout.navigation')} className="space-y-1 p-2">
          {menu.map((group) => {
            const bare = !group.title // 顶级单项（如工作台）：没有分组标题，直接是一级菜单
            const open = !isClosed(group.key)

            // 图标模式：顶级单项直接是图标链接；一级目录收成图标 + 浮出子菜单。
            // 分组靠间距区分（目录行多 py-0.5），不放分隔线——深色下线要么糊成噪点要么太扎眼。
            if (collapsed) {
              return (
                <div key={group.key}>
                  {bare ? (
                    <ul className="space-y-1">{group.children?.map((item) => renderItem(item, false))}</ul>
                  ) : (
                    <div className="py-0.5">
                      <GroupFlyout group={group} onNavigate={onNavigate} />
                    </div>
                  )}
                </div>
              )
            }

            if (bare) return <ul key={group.key} className="space-y-1">{group.children?.map((item) => renderItem(item, false))}</ul>

            // 一级目录：图标 + 标题 + 折叠箭头（箭头是唯一的分组标记，字重与单项一致）；二级菜单缩进并带一条竖向引导线
            return (
              <div key={group.key}>
                <button
                  type="button"
                  onClick={() => toggleGroup(group.key)}
                  aria-expanded={open}
                  className="flex min-h-11 w-full items-center gap-3 rounded-md px-3 text-sm font-medium text-sidebar-foreground transition-colors hover:bg-sidebar-accent/60 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring md:min-h-9"
                >
                  <DynamicIcon name={group.icon} className="size-5 shrink-0 md:size-4" />
                  <span className="flex-1 truncate text-start">{t(group.title)}</span>
                  <ChevronDown
                    className={cn('size-4 shrink-0 text-muted-foreground transition-transform duration-(--motion-base) ease-(--ease-standard) motion-reduce:transition-none', !open && '-rotate-90 rtl:rotate-90')}
                    aria-hidden="true"
                  />
                </button>
                {/* 引导线用绝对定位画，不占布局：这样二级图标正好落在一级标题文字的下一条竖线上 */}
                {open && (
                  <ul className="relative ms-5 mt-1 space-y-1 ps-2 before:absolute before:inset-y-0 before:inset-s-0 before:w-px before:bg-sidebar-border">
                    {group.children?.map((item) => renderItem(item, true))}
                  </ul>
                )}
              </div>
            )
          })}
        </nav>
      </ScrollArea>

      {onToggleCollapse && (
        <div className="flex shrink-0 flex-col gap-1 border-t border-sidebar-border p-2">
          {!collapsed && groupKeys.length > 1 ? (
            <Button
              type="button"
              variant="ghost"
              size="default"
              className="w-full justify-start gap-3 px-3 text-sidebar-foreground hover:bg-sidebar-accent/60"
              onClick={toggleAllGroups}
            >
              {allClosed ? <ChevronsUpDown aria-hidden="true" /> : <ChevronsDownUp aria-hidden="true" />}
              <span>{allClosed ? t('layout.expandAllGroups') : t('layout.collapseAllGroups')}</span>
            </Button>
          ) : null}
          <Button
            type="button"
            variant="ghost"
            size={collapsed ? 'icon' : 'default'}
            className={cn(
              'text-sidebar-foreground hover:bg-sidebar-accent/60',
              collapsed ? 'mx-auto flex' : 'w-full justify-start gap-3 px-3',
            )}
            aria-label={collapsed ? t('layout.expandSidebar') : t('layout.collapseSidebar')}
            onClick={onToggleCollapse}
          >
            {collapsed ? <PanelLeftOpen aria-hidden="true" /> : <PanelLeftClose aria-hidden="true" />}
            {!collapsed && <span>{t('layout.collapseSidebar')}</span>}
          </Button>
        </div>
      )}
    </div>
  )
}
