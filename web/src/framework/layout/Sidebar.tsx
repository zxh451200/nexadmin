import { useEffect, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { ChevronDown, ChevronsDownUp, ChevronsUpDown, PanelLeftClose, PanelLeftOpen } from 'lucide-react'
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

/** 一级分组 → 色调的确定性映射：纯函数取 group.key（与标题译文无关），渲染与语言切换间稳定。
    只用 chart/success/info/warning，不用 destructive（红色留给失败/危险语义）。 */
const GROUP_TONES = ['chart-1', 'chart-2', 'chart-3', 'chart-4', 'chart-5', 'success', 'info', 'warning'] as const
function groupTone(key: string) {
  let h = 2166136261 // FNV-1a 32 位
  for (let i = 0; i < key.length; i++) {
    h = Math.imul(h ^ key.charCodeAt(i), 16777619) >>> 0
  }
  return GROUP_TONES[h % GROUP_TONES.length]
}


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

  // 有可折叠的一级目录时，提供「全部展开 / 全部收起」（跟随所有目录的当前状态切换）
  const groupKeys = menu.filter((g) => g.title && (g.children?.length ?? 0) > 0).map((g) => g.key)
  const allClosed = groupKeys.length > 0 && groupKeys.every((k) => closed.includes(k))
  const toggleAllGroups = () => {
    const next = allClosed ? [] : groupKeys
    setClosed(next)
    try {
      localStorage.setItem(GROUPS_KEY, JSON.stringify(next))
    } catch {
      // 存储不可用时仅本次生效
    }
  }

  const renderItem = (item: MenuNode, nested: boolean, tone?: string) => {
    const link = (
      <NavLink
        to={item.path!}
        onClick={onNavigate}
        aria-label={collapsed ? t(item.title) : undefined}
        data-tone={tone}
        className={({ isActive }) =>
          cn(
            'relative flex min-h-11 items-center gap-3 rounded-md px-3 text-sm transition-colors md:min-h-9',
            'hover:bg-sidebar-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
            // 一级行（含单项与目录标题）统一字重，层级差交给缩进与引导线，不用字重表达
            !nested && 'font-medium',
            isActive
              ? cn(
                  'bg-sidebar-accent font-medium text-sidebar-accent-foreground',
                  'before:absolute before:inset-y-2 before:w-0.5 before:rounded-full before:bg-brand-gradient',
                  // 二级的激活竖条压在引导线上，免得同一条轨道上出现两条竖线
                  nested ? 'before:start-[-9px]' : 'before:start-0',
                )
              : nested
                ? 'text-sidebar-foreground/80'
                : 'text-sidebar-foreground',
            // 收起成图标轨时：整行等宽保证图标水平居中；隐藏激活项左侧竖条（那是展开列表的指示器，轨内显得突兀）
            collapsed && 'w-full justify-center px-0 before:hidden',
          )
        }
      >
        {/* 图标按分组色调着色（.tone-text 读 data-tone 的 --tone）；文字与激活底色仍走 sidebar-accent 令牌，不替换 */}
        <DynamicIcon name={item.icon} className={cn('size-5 shrink-0', !collapsed && 'md:size-4', tone && 'tone-text')} />
        {!collapsed && <span className="truncate">{t(item.title)}</span>}
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

            // 图标模式：所有菜单只显示图标，分组之间用细线隔开（图标轨也按分组着色，收展开两套观感一致）
            if (collapsed) {
              return (
                <div key={group.key}>
                  {!bare && gi > 0 && <div className="mx-2 my-2 h-px bg-sidebar-border" role="separator" />}
                  <ul className="space-y-1">{group.children?.map((item) => renderItem(item, false, groupTone(group.key)))}</ul>
                </div>
              )
            }

            if (bare) return <ul key={group.key} className="space-y-1">{group.children?.map((item) => renderItem(item, false, groupTone(group.key)))}</ul>

            // 一级目录：图标 + 标题 + 折叠箭头（箭头是唯一的分组标记，字重与单项一致）；二级菜单缩进并带一条竖向引导线
            return (
              <div key={group.key}>
                <button
                  type="button"
                  onClick={() => toggleGroup(group.key)}
                  aria-expanded={open}
                  data-tone={groupTone(group.key)}
                  className="flex min-h-11 w-full items-center gap-3 rounded-md px-3 text-sm font-medium text-sidebar-foreground transition-colors hover:bg-sidebar-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:min-h-9"
                >
                  {/* 一级目录图标按分组色调着色：--tone 由 button 上的 data-tone 提供，仅颜色层 */}
                  <DynamicIcon name={group.icon} className="tone-text size-5 shrink-0 md:size-4" />
                  <span className="flex-1 truncate text-start">{t(group.title)}</span>
                  <ChevronDown
                    className={cn('size-4 shrink-0 text-muted-foreground transition-transform duration-200 motion-reduce:transition-none', !open && '-rotate-90 rtl:rotate-90')}
                    aria-hidden="true"
                  />
                </button>
                {/* 引导线用绝对定位画，不占布局：这样二级图标正好落在一级标题文字的下一条竖线上 */}
                {open && (
                  <ul className="relative ms-5 mt-1 space-y-1 ps-2 before:absolute before:inset-y-0 before:start-0 before:w-px before:bg-sidebar-border">
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
