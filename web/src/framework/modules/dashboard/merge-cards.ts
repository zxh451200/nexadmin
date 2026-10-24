import type { DashboardCard } from './types'

/** 卡片 id 规则：与后端 PUT /dashboard/layout 的校验一致 */
export const CARD_ID_RE = /^[a-z0-9][a-z0-9\-_.]*$/
export const CARD_ID_MAX = 64

export function isValidCardId(id: unknown): id is string {
  return typeof id === 'string' && id.length <= CARD_ID_MAX && CARD_ID_RE.test(id)
}

/** 卡片来源优先级：框架模块 < 插件 < 用户页面（web/src/pages），与路由（router/merge.ts）一致 */
export function cardRank(file: string): number {
  return file.startsWith('/src/pages') ? 2 : file.startsWith('/src/plugins') ? 1 : 0
}

/**
 * 合并各 cards.tsx 导出的卡片：按来源优先级升序处理，同 id 后者覆盖前者。
 * 因此用户页面里声明与内置卡片相同的 id，就能替换该卡片，无需改框架文件。id 不合法的卡片被忽略。
 * 纯函数，不依赖 Vite，便于单测。
 */
export function mergeCards(files: Record<string, { cards?: DashboardCard[] }>): DashboardCard[] {
  const map = new Map<string, DashboardCard>()
  const entries = Object.entries(files).sort(([a], [b]) => cardRank(a) - cardRank(b) || a.localeCompare(b))
  for (const [, mod] of entries) {
    for (const c of mod.cards ?? []) {
      if (isValidCardId(c.id)) map.set(c.id, c)
    }
  }
  return [...map.values()]
}
