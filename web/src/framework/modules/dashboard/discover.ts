import { checkPerm } from '@/framework/auth/usePermission'
import { mergeCards } from './merge-cards'
import type { DashboardCard } from './types'

const modules = import.meta.glob<{ cards?: DashboardCard[] }>(
  ['/src/framework/modules/*/cards.tsx', '/src/plugins/*/cards.tsx', '/src/pages/**/cards.tsx'],
  { eager: true },
)

/** 所有已发现的卡片（去重）：框架模块 → 插件 → 用户页面，同 id 后者覆盖前者（规则见 merge-cards.ts） */
export const allCards: DashboardCard[] = mergeCards(modules)

/** 按权限过滤；没有 perm 的卡片所有登录用户可见（判定方式与路由一致） */
export function filterCards(perms: string[], cards: DashboardCard[] = allCards): DashboardCard[] {
  return cards.filter((c) => checkPerm(perms, c.perm))
}
