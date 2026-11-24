import {
  contributionSource,
  discoverContributions,
  mergeContributions,
  reportContributionConflicts,
  type ActivePluginIds,
} from '@/framework/contributions'
import type { DashboardCard } from './types'

/** 卡片 id 规则：与后端 PUT /dashboard/layout 的校验一致 */
export const CARD_ID_RE = /^[a-z0-9][a-z0-9\-_.]*$/
export const CARD_ID_MAX = 64

export function isValidCardId(id: unknown): id is string {
  return typeof id === 'string' && id.length <= CARD_ID_MAX && CARD_ID_RE.test(id)
}

/** 卡片与路由、设置分区共用来源优先级。 */
export function cardRank(file: string): number {
  return contributionSource(file).priority
}

/**
 * 兼容旧调用：按来源目录剔除未启用插件的卡片模块。
 * 新调用应直接使用 mergeCards(files, active)，它还能识别逐卡片的 requiresPlugin。
 */
export function dropInactivePluginCards<T>(files: Record<string, T>, activeIds: string[] | null): Record<string, T> {
  if (activeIds === null) return files
  const allowed = new Set(activeIds)
  return Object.fromEntries(
    Object.entries(files).filter(([file]) => {
      const plugin = contributionSource(file).plugin
      return !plugin || allowed.has(plugin)
    }),
  )
}

/** 先过滤停用插件，再按 framework < plugin < pages 合并；同 id 冲突保留诊断。 */
export function mergeCards(files: Record<string, { cards?: DashboardCard[] }>, active: ActivePluginIds = null): DashboardCard[] {
  const discovered = discoverContributions(
    files,
    (module) => module.cards?.filter((card) => isValidCardId(card.id)),
    { requiresPlugin: (card) => card.requiresPlugin },
  )
  const { items, conflicts } = mergeContributions(discovered, (card) => card.id, 'card', active)
  reportContributionConflicts(conflicts)
  return items.map((item) => item.value)
}
