import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export const SEND_CHANNELS = ['sms', 'email', 'robot'] as const
export type SendChannel = (typeof SEND_CHANNELS)[number]

export const SEND_STATUSES = ['success', 'fail', 'skipped'] as const
export type SendStatus = (typeof SEND_STATUSES)[number]

/** 发送场景；后端可能出现未列出的值，界面上按原文显示 */
export const SEND_SCENES = ['notice', 'verify_code', 'bind_phone', 'account', 'security', 'test'] as const

/**
 * 发送日志。recipient 已在服务端脱敏（138****1234、a***@x.com，机器人为渠道名）；
 * 日志里没有验证码、正文和密钥，error 已脱敏并截断到 500 字。
 */
export interface SendLog {
  id: string
  channel: SendChannel
  scene: string
  recipient: string
  subject: string
  status: SendStatus
  error?: string
  durationMs: number
  createdAt: string
}

export interface SendLogQuery {
  page: number
  pageSize: number
  channel?: string
  status?: string
  recipient?: string
  startDate?: string
  endDate?: string
}

export function useSendLogList(query: SendLogQuery) {
  return useQuery({
    queryKey: ['sendlog', 'list', query],
    queryFn: () => api.get<Page<SendLog>>('/sendlogs', { ...query }),
    placeholderData: keepPreviousData,
  })
}

export function useSendLog(id: string | null) {
  return useQuery({
    queryKey: ['sendlog', 'detail', id],
    queryFn: () => api.get<SendLog>(`/sendlogs/${id}`),
    enabled: !!id,
  })
}

export function useDeleteSendLogs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ removed: number }>('/sendlogs', { ids: ids.join(',') }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sendlog'] }),
  })
}

/** 清理 N 天前的记录（N >= 1） */
export function useCleanSendLogs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (days: number) => api.post<{ removed: number }>('/sendlogs/clean', { days }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sendlog'] }),
  })
}
