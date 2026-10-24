import type { DashboardCard, SavedLayout } from './types'

/** 没写 order 的卡片排在写了的后面 */
const NO_ORDER = Number.MAX_SAFE_INTEGER

/** 按 order（小的在前）稳定排序，返回新数组 */
export function sortByOrder(cards: DashboardCard[]): DashboardCard[] {
  return cards
    .map((c, i) => ({ c, i }))
    .sort((a, b) => (a.c.order ?? NO_ORDER) - (b.c.order ?? NO_ORDER) || a.i - b.i)
    .map((x) => x.c)
}

/**
 * 把「当前可见的卡片」与「用户保存的布局」合并为最终顺序与隐藏集合。
 * 规则：
 *  - 已保存顺序里仍存在的卡片，按保存顺序；
 *  - 已保存顺序里不再存在的 id 丢弃（重复 id 只保留第一个）；
 *  - 新出现的卡片（既不在 order 也不在 hidden，例如框架升级或新装插件带来的）追加到末尾，按 order；
 *  - hidden 里不存在的 id 丢弃；
 *  - 新卡片若 defaultHidden 则初始隐藏；
 *  - 只出现在 hidden、不在 order 的已知卡片保持隐藏，并和新卡片一起追加到末尾，保证可以恢复。
 * 返回的 order 包含全部卡片（含隐藏的），hidden 是其子集。
 */
export function mergeLayout(cards: DashboardCard[], saved?: SavedLayout | null): SavedLayout {
  const byId = new Map(cards.map((c) => [c.id, c]))
  const savedOrder = saved?.order ?? []
  const savedHidden = saved?.hidden ?? []

  const order: string[] = []
  const placed = new Set<string>()
  for (const id of savedOrder) {
    if (byId.has(id) && !placed.has(id)) {
      placed.add(id)
      order.push(id)
    }
  }

  const known = new Set([...savedOrder, ...savedHidden])
  const hidden = new Set<string>()
  for (const id of savedHidden) if (byId.has(id)) hidden.add(id)

  for (const c of sortByOrder(cards.filter((x) => !placed.has(x.id)))) {
    order.push(c.id)
    if (!known.has(c.id) && c.defaultHidden) hidden.add(c.id)
  }
  return { order, hidden: order.filter((id) => hidden.has(id)) }
}

/** 把 id 在列表里上移（-1）或下移（+1）一位；到头或不存在返回原数组 */
export function moveItem(ids: string[], id: string, delta: -1 | 1): string[] {
  const from = ids.indexOf(id)
  const to = from + delta
  if (from < 0 || to < 0 || to >= ids.length) return ids
  const next = [...ids]
  ;[next[from], next[to]] = [next[to], next[from]]
  return next
}

/**
 * 在「可见卡片」范围内移动：隐藏的卡片不参与上移/下移，位置保持不变。
 * 做法：取出可见 id 的位置槽，在可见序列里交换后再回填。
 */
export function moveVisible(order: string[], hidden: string[], id: string, delta: -1 | 1): string[] {
  const hide = new Set(hidden)
  const slots = order.map((x, i) => (hide.has(x) ? -1 : i)).filter((i) => i >= 0)
  const visible = slots.map((i) => order[i])
  const moved = moveItem(visible, id, delta)
  if (moved === visible) return order
  const next = [...order]
  slots.forEach((slot, k) => {
    next[slot] = moved[k]
  })
  return next
}

/** 两份布局是否完全一致（顺序敏感；hidden 视为集合） */
export function sameLayout(a: SavedLayout, b: SavedLayout): boolean {
  if (a.order.length !== b.order.length || a.hidden.length !== b.hidden.length) return false
  if (a.order.some((id, i) => id !== b.order[i])) return false
  const h = new Set(b.hidden)
  return a.hidden.every((id) => h.has(id))
}
