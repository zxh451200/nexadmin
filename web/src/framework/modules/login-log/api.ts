import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'
import { asBlob, exportRequest } from '../oper-log/api'

export const LOGIN_TYPES = ['password', 'phone', 'wecom', 'dingtalk', 'feishu', 'google', 'github', 'oidc'] as const
export type LoginType = (typeof LOGIN_TYPES)[number]

/** 失败原因只给分类，不暴露细节（防账号枚举） */
export type FailReason = 'credentials' | 'captcha' | 'locked' | 'disabled' | 'blocked' | 'other'

export interface LoginLog {
  id: string
  username: string
  loginType: LoginType
  ip: string
  location?: string
  browser?: string
  os?: string
  status: 'success' | 'failed'
  failReason?: FailReason
  createdAt: string
}

export interface LoginLogQuery {
  page: number
  pageSize: number
  keyword?: string
  status?: string
  loginType?: string
  startDate?: string
  endDate?: string
}

export function useLoginLogList(query: LoginLogQuery) {
  return useQuery({
    queryKey: ['loginLog', 'list', query],
    queryFn: () => api.get<Page<LoginLog>>('/login-logs', { ...query }),
    placeholderData: keepPreviousData,
  })
}

/** 服务端导出 .xlsx（需要 loginLog:export 权限），筛选条件与数据范围同列表 */
export async function exportLoginLogs(query: Omit<LoginLogQuery, 'page' | 'pageSize'>): Promise<Blob> {
  const res = await http.get<unknown>('/login-logs/export', exportRequest(query))
  return asBlob(res.data)
}

export function useDeleteLoginLogs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<null>('/login-logs', { ids: ids.join(',') }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['loginLog'] }),
  })
}
