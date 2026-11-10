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
  /** 号码本身分辨出的证件类型 */
  kind?: 'resident_id' | 'hmt_permit' | 'foreigner_prp'
  /** 仅五星卡有值：国籍 ISO 3166-1 alpha-2 */
  country?: string
  reason?: string
  numberMasked: string
  legacy?: boolean
  regionCode?: string
  regionNames?: string[]
  /** 1 省 / 2 地级 / 3 县级：小于 3 说明区划数据粒度或撤并导致只认到上级 */
  regionLevel?: number
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
  /** '1' = 显示全部留痕；缺省按号码折叠成最新一次 */
  all?: '1'
  /** 核验时间窗（yyyy-MM-dd，含两端）；缺省不限 */
  from?: string
  to?: string
}

export function useVerifications(query: VerificationQuery) {
  return useQuery({
    queryKey: [...KEY, 'verifications', query],
    queryFn: () => api.get<Page<Verification>>(`${BASE}/verifications`, query),
    placeholderData: keepPreviousData,
  })
}

/** 台账导出：真后端回 xlsx Blob。失败由拦截器解码 Blob 里的 JSON 错误体并弹提示。 */
export async function exportVerifications(query: Pick<VerificationQuery, 'keyword' | 'status' | 'from' | 'to' | 'all'>): Promise<void> {
  const { http } = await import('@/framework/request/http')
  const res = await http.get<unknown>(`${BASE}/verifications/export`, { params: { ...query }, responseType: 'blob' })
  const blob = res.data instanceof Blob ? res.data : new Blob(['\uFEFF', String(res.data)], { type: 'text/csv;charset=utf-8' })
  const stamp = new Date().toISOString().slice(0, 19).replace(/-|:|T/g, '')
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `realname-${stamp}.xlsx`
  a.click()
  URL.revokeObjectURL(url)
}

export function useParse() {
  return useMutation({ mutationFn: (data: { idType?: string; number: string }) => api.post<ParseResult>(`${BASE}/parse`, data) })
}

export function useDeleteVerifications() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>(`${BASE}/verifications`, { ids: ids.join(',') }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
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
