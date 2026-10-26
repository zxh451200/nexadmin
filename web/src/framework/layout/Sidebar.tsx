import { useEffect, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { ChevronDown, PanelLeftClose, PanelLeftOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/framework/ui/button'
import { BrandMark } from '@/framework/components/BrandMark'
import { ScrollArea } from '@/framework/ui/scroll-area'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/framework/ui/tooltip'
import type { MenuNode } from '@/framework/router/types'
import { DynamicIcon } from './DynamicIcon'
import { usePublicSettings } from './Watermark'

const GROUPS_KEY = 'nex-sidebar-groups' // 记住哪些一级目录是收起的

function loadClosed(): string[] {
  try {
    return JSON.parse(localStorage.getItem(GROUPS_KEY) ?? '[]') as string[]
  } catch {
    return []
  }
}

interface SidebarProps {
  menu: MenuNode[]
  collapsed?: boolean
  /** 点击菜单项后的回调（手机抽屉用来关闭） */
  onNavigate?: () => void
  /** 提供后在底部显示「收起 / 展开侧边栏」按钮（手机抽屉不需要） */
  onToggleCollapse?: () => void
}

export function Sidebar({ menu, collapsed = false, onNavigate, onToggleCollapse }: SidebarProps) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const [closed, setClosed] = useState<string[]>(loadClosed)
  const { data: settings } = usePublicSettings()

  const toggleGroup = (key: string) =>
    setClosed((prev) => {
      const next = prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]
      try {
        localStorage.setItem(GROUPS_KEY, JSON.stringify(next))
      } catch {
        // 存储不可用时仅本次生效
      }
      return next
    })

  // 当前页面所在的一级目录自动展开，避免选中项藏在收起的目录里
  useEffect(() => {
    const g = menu.find((m) => m.children?.some((c) => c.path && (pathname === c.path || pathname.startsWith(c.path + '/'))))
    if (g) setClosed((prev) => (prev.includes(g.key) ? prev.filter((k) => k !== g.key) : prev))
  }, [pathname, menu])

  const renderItem = (item: MenuNode, nested: boolean) => {
    const link = (
      <NavLink
        to={item.path!}
        onClick={onNavigate}
        aria-label={collapsed ? t(item.title) : undefined}
        className={({ isActive }) =>
          cn(
            'relative flex min-h-11 items-center gap-3 rounded-md px-3 text-sm transition-colors md:min-h-9',
            'hover:bg-sidebar-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
            isActive
              ? 'bg-sidebar-accent font-medium text-sidebar-accent-foreground before:absolute before:inset-y-2 before:start-0 before:w-0.5 before:rounded-full before:bg-brand-gradient'
              : nested
                ? 'text-sidebar-foreground/80'
                : 'text-sidebar-foreground',
            // 收起成图标轨时：整行等宽保证图标水平居中；隐藏激活项左侧竖条（那是展开列表的指示器，轨内显得突兀）
            collapsed && 'w-full justify-center px-0 before:hidden',
          )
        }
      >
        <DynamicIcon name={item.icon} className={collapsed ? 'size-5 shrink-0' : 'size-5 shrink-0 md:size-4'} />
        {!collapsed && <span className="truncate">{t(item.title)}</span>}
      </NavLink>
    )
    return (
      <li key={item.key}>
        {collapsed ? (
          <Tooltip>
            <TooltipTrigger asChild>{link}</TooltipTrigger>
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
        to="/"
        onClick={onNavigate}
        aria-label={settings?.siteName || t('layout.appName')}
        className={cn(
          'flex h-14 shrink-0 items-center gap-2 border-b border-border px-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring',
          collapsed && 'justify-center px-0',
        )}
      >
        <BrandMark name={settings?.siteName || t('layout.appName')} logoUrl={settings?.logoUrl} className="size-8" iconClassName="size-5" />
        {!collapsed && <span className="truncate text-base font-semibold tracking-tight">{settings?.siteName || t('layout.appName')}</span>}
      </NavLink>

      <ScrollArea className="flex-1">
        <nav aria-label={t('layout.navigation')} className="space-y-1 p-2">
          {menu.map((group, gi) => {
            const bare = !group.title // 顶级单项（如工作台）：没有分组标题，直接是一级菜单
            const open = !closed.includes(group.key)

            // 图标模式：所有菜单只显示图标，分组之间用细线隔开
            if (collapsed) {
              return (
                <div key={group.key}>
                  {!bare && gi > 0 && <div className="mx-2 my-2 h-px bg-sidebar-border" role="separator" />}
                  <ul className="space-y-1">{group.children?.map((item) => renderItem(item, false))}</ul>
                </div>
              )
            }

            if (bare) return <ul key={group.key} className="space-y-1">{group.children?.map((item) => renderItem(item, false))}</ul>

            // 一级目录：图标 + 加粗标题 + 折叠箭头；二级菜单缩进并带一条竖向引导线
            return (
              <div key={group.key} className="pt-1">
                <button
                  type="button"
                  onClick={() => toggleGroup(group.key)}
                  aria-expanded={open}
                  className="flex min-h-11 w-full items-center gap-3 rounded-md px-3 text-sm font-semibold text-foreground transition-colors hover:bg-sidebar-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:min-h-9"
                >
                  <DynamicIcon name={group.icon} className="size-5 shrink-0 md:size-4" />
                  <span className="flex-1 truncate text-start">{t(group.title)}</span>
                  <ChevronDown
                    className={cn('size-4 shrink-0 text-muted-foreground transition-transform duration-200 motion-reduce:transition-none', !open && '-rotate-90 rtl:rotate-90')}
                    aria-hidden="true"
                  />
                </button>
                {open && <ul className="ms-5 mt-1 space-y-1 border-s border-sidebar-border ps-2">{group.children?.map((item) => renderItem(item, true))}</ul>}
              </div>
            )
          })}
        </nav>
      </ScrollArea>

      {onToggleCollapse && (
        <div className="shrink-0 border-t border-sidebar-border p-2">
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
