import { useState } from 'react'

export interface ClientFilterValues {
  keyword: string
  status: string
  type: string
}

const EMPTY: ClientFilterValues = { keyword: '', status: '', type: '' }

/** 不分页的小列表（角色、岗位、菜单、部门）用的前端筛选：草稿 → 点「搜索」才生效 */
export function useClientFilter() {
  const [draft, setDraft] = useState<ClientFilterValues>(EMPTY)
  const [applied, setApplied] = useState<ClientFilterValues>(EMPTY)
  const active = !!(applied.keyword.trim() || applied.status || applied.type)
  return {
    draft,
    setDraft,
    applied,
    active,
    search: () => setApplied(draft),
    reset: () => {
      setDraft(EMPTY)
      setApplied(EMPTY)
    },
  }
}

export type ClientFilter = ReturnType<typeof useClientFilter>

/** 关键词为空视为匹配；否则任一字段包含关键词（不分大小写）即匹配 */
export function matchText(keyword: string, ...fields: (string | null | undefined)[]) {
  const k = keyword.trim().toLowerCase()
  return !k || fields.some((f) => (f ?? '').toLowerCase().includes(k))
}

/** 树形筛选：自身命中保留（子级有命中只留命中的，没有则保留原子级）；子孙命中则保留祖先链 */
export function filterTree<T extends { children?: T[] }>(nodes: T[], pred: (n: T) => boolean): T[] {
  const out: T[] = []
  for (const n of nodes) {
    const kids = n.children ? filterTree(n.children, pred) : []
    const self = pred(n)
    if (self || kids.length) out.push({ ...n, children: kids.length ? kids : self ? n.children : undefined })
  }
  return out
}
