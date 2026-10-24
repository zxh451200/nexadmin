import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export interface OnlineUser {
  id: string
  username: string
  nickname: string
  deptName?: string
  device: 'web' | 'mobile' | 'tablet' | 'other'
  browser?: string
  os?: string
  ip: string
  loginAt: string
  lastActiveAt: string
}

export interface OnlineQuery {
  page: number
  pageSize: number
  keyword?: string
}

export function useOnlineList(query: OnlineQuery) {
  return useQuery({
    queryKey: ['online', 'list', query],
    queryFn: () => api.get<Page<OnlineUser>>('/online-users', { ...query }),
    placeholderData: keepPreviousData,
  })
}

export function useForceLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/online-users/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['online'] }),
  })
}
