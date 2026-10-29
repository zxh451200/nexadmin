import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { SavedLayout } from './types'

export interface StatValue {
  value: number
  delta?: number
  /** 上期基线太小时后端返回 false：百分比没有意义，隐藏涨跌标签。 */
  comparable?: boolean
}

export interface DashboardStats {
  users: StatValue
  todayLogin: StatValue
  unreadMessages: StatValue
  storage: { usedGb: number; totalGb: number; percent: number; delta?: number; comparable?: boolean }
  loginTrend: Array<{ date: string; logins: number; failures: number }>
  requestTrend: Array<{ date: string; requests: number; errors: number }>
  operTypes: Array<{ type: string; value: number }>
  todos: Array<{ id: string; kind: 'message' | 'notice'; title: string; time: string }>
  recentNotices: Array<{ id: string; title: string; type: string; level: string; publishAt: string }>
}

export function useDashboardStats() {
  return useQuery({ queryKey: ['dashboard', 'stats'], queryFn: () => api.get<DashboardStats>('/dashboard/stats') })
}

const layoutKey = ['dashboard', 'layout'] as const

/** 当前用户保存的工作台布局；没有记录时是空数组 */
export function useDashboardLayout() {
  return useQuery({ queryKey: layoutKey, queryFn: () => api.get<SavedLayout>('/dashboard/layout'), staleTime: 60_000 })
}

export function useSaveDashboardLayout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (layout: SavedLayout) => api.put<SavedLayout>('/dashboard/layout', layout),
    onSuccess: (_data, layout) => qc.setQueryData(layoutKey, layout),
  })
}

export function useResetDashboardLayout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.del<null>('/dashboard/layout'),
    onSuccess: () => qc.setQueryData<SavedLayout>(layoutKey, { order: [], hidden: [] }),
  })
}
