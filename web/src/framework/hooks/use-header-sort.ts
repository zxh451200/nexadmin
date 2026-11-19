import { useState } from 'react'

/**
 * 表头排序的受控状态，与用户管理页同一模式（参考 UserPage）：
 * - 排序与筛选分开存，改排序不重置筛选条件；
 * - 排序变化回第一页（resetPage 由页面传 `() => setPage(1)`）：旧页码在新排序下
 *   经常越界，空列表会被读成「没有数据」；
 * - sortProp 直接喂给 DataTable 的 sort prop，列表 hook 把 sort/desc 并进查询参数。
 */
export function useHeaderSort(initialKey: string, initialDesc = true, resetPage?: () => void) {
  const [sort, setSort] = useState(initialKey)
  const [desc, setDesc] = useState(initialDesc)

  const onChange = (key: string, d: boolean) => {
    setSort(key)
    setDesc(d)
    resetPage?.()
  }

  return { sort, desc, sortProp: { key: sort, desc, onChange } }
}
