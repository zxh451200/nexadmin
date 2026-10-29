import { checkPerm } from '@/framework/auth/usePermission'
import { dropInactivePluginCards, mergeCards } from './merge-cards'
import type { DashboardCard } from './types'

const modules = import.meta.glob<{ cards?: DashboardCard[] }>(
  ['/src/framework/modules/*/cards.tsx', '/src/plugins/*/cards.tsx', '/src/pages/**/cards.tsx'],
  { eager: true },
)

/** 所有已发现的卡片（去重）：框架模块 → 插件 → 用户页面，同 id 后者覆盖前者（规则见 merge-cards.ts） */
export const allCards: DashboardCard[] = mergeCards(modules)

/**
 * 按「已启用插件」取卡片：
 * activeIds = null 问不到（mock / 旧后端）保持原样；undefined 还没问到，先不挂任何插件卡片。
 */
export function discoverCards(activeIds: string[] | null | undefined): DashboardCard[] {
  if (activeIds === null) return allCards
  return mergeCards(dropInactivePluginCards(modules, activeIds ?? []))
}

/** 按权限过滤；没有 perm 的卡片所有登录用户可见（判定方式与路由一致） */
export function filterCards(perms: string[], cards: DashboardCard[] = allCards): DashboardCard[] {
  return cards.filter((c) => checkPerm(perms, c.perm))
}
