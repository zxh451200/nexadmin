import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export const RECYCLE_TYPES = ['user', 'role', 'dict', 'notice', 'post'] as const
export type RecycleType = (typeof RECYCLE_TYPES)[number]

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
  type?: RecycleType
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

export function usePurge() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/recycle/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['recycle'] }),
  })
}
