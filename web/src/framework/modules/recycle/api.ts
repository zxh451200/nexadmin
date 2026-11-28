import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

// 与后端 recbin.Tables 的登记保持一致：只列真正会进回收站的类型，
// 多列的筛选项永远查不到数据，只会让人以为那种数据丢了。
export const RECYCLE_TYPES = ['user', 'notice', 'role', 'dictType', 'dictData', 'post', 'customer'] as const
export type RecycleType = (typeof RECYCLE_TYPES)[number]

export interface RecycleBatchItemResult {
  id: string
  result: 'restored' | 'purged' | 'conflict' | 'gone' | 'forbidden'
}

/** 批量还原 / 批量彻底删除的回执：保留汇总计数，并可用 items 定位未处理条目。 */
export interface RecycleBatchResult {
  restored?: number
  purged?: number
  conflict?: number
  gone?: number
  items?: RecycleBatchItemResult[]
}

export interface RecycleItem {
  id: string
  type: RecycleType
  /** 展示名（用户昵称、公告标题等） */
  name: string
  deletedBy: string
  deletedAt: string
}

export interface RecycleQuery {
  page: number
  pageSize: number
  keyword?: string
  /** 逗号分隔多值：?type=user,role */
  type?: string
  /** 删除日期范围（YYYY-MM-DD，含当天），任一可省 */
  from?: string
  to?: string
  /** 表头排序字段（后端白名单见 recycle Service.List） */
  sort?: string
  desc?: boolean
}

export function useRecycleList(query: RecycleQuery) {
  return useQuery({
    queryKey: ['recycle', 'list', query],
    queryFn: () => api.get<Page<RecycleItem>>('/recycle', { ...query }),
    placeholderData: keepPreviousData,
  })
}

// 还原会让源模块（用户、公告…）的列表变化，这里直接让所有查询失效
export function useRestore() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<null>(`/recycle/${id}/restore`),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useRestoreBatch() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.post<RecycleBatchResult>('/recycle/restore', { ids }),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function usePurge() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/recycle/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['recycle'] }),
  })
}

export function usePurgeBatch() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<RecycleBatchResult>('/recycle', { ids: ids.join(',') }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['recycle'] }),
  })
}
