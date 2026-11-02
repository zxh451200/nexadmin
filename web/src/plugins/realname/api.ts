import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

const BASE = '/plugins/realname'
const KEY = ['plugin_realname'] as const

// 核验状态：格式结论（valid/invalid）与在线结论（match/mismatch/error）分开。
export const STATUSES = ['valid', 'invalid', 'match', 'mismatch', 'error'] as const
export type VerifyStatus = (typeof STATUSES)[number]

export interface ParseResult {
  status: VerifyStatus
  reason?: string
  numberMasked: string
  legacy?: boolean
  regionCode?: string
  regionNames?: string[]
  regionHistorical?: boolean
  birthday?: string
  sex?: string
  age?: number
}

export interface Verification {
  id: string
  nameMasked: string
  idType: string
  numberMasked: string
  status: VerifyStatus
  reason?: string
  provider?: string
  providerMsg?: string
  latencyMs: number
  operatorName: string
  remark?: string
  createdAt: string
}

export interface VerificationQuery extends PageQuery {
  status?: VerifyStatus
}

export function useVerifications(query: VerificationQuery) {
  return useQuery({
    queryKey: [...KEY, 'verifications', query],
    queryFn: () => api.get<Page<Verification>>(`${BASE}/verifications`, query),
    placeholderData: keepPreviousData,
  })
}

export function useParse() {
  return useMutation({ mutationFn: (data: { idType?: string; number: string }) => api.post<ParseResult>(`${BASE}/parse`, data) })
}

export interface VerifyResult {
  verification: Verification
  parse: ParseResult
  online: boolean
}

export function useVerify() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (data: { idType?: string; name: string; number: string; remark?: string }) =>
      api.post<VerifyResult>(`${BASE}/verify`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

export interface VerifyConfig {
  enabled: boolean
  provider: string
  endpoint: string
  appKey: string
  hasSecret: boolean
  timeoutMs: number
  dailyLimit: number
}

// 配置只在有 realname:config 权限、且设置弹窗打开时才拉取：无权限用户进页面不该吃一发 403。
export function useConfig(enabled: boolean) {
  return useQuery({ queryKey: [...KEY, 'config'], queryFn: () => api.get<VerifyConfig>(`${BASE}/config`), enabled })
}

export function useSaveConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (data: VerifyConfig & { appSecret?: string }) => api.put<VerifyConfig>(`${BASE}/config`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'config'] }),
  })
}
