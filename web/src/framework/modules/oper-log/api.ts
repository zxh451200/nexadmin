import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, api, http } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export interface OperLog {
  id: string
  module: string
  type: string
  operator: string
  ip: string
  location?: string
  status: 'success' | 'failed'
  costMs: number
  method: string
  url: string
  params?: unknown
  response?: unknown
  errorStack?: string
  createdAt: string
}

export interface OperLogQuery {
  page: number
  pageSize: number
  keyword?: string
  status?: string
  type?: string
  startDate?: string
  endDate?: string
}

export function useOperLogList(query: OperLogQuery) {
  return useQuery({
    queryKey: ['operLog', 'list', query],
    queryFn: () => api.get<Page<OperLog>>('/oper-logs', { ...query }),
    placeholderData: keepPreviousData,
  })
}

/** 导出请求的公共配置：format=xlsx，响应是文件（Blob）；错误由页面按 isExportTooMany 等本地化，所以请求本身静默 */
export function exportRequest(query: object) {
  return { params: { ...query, format: 'xlsx' }, responseType: 'blob' as const, silent: true }
}

/** 真实后端返回文件（Blob），mock 阶段返回 CSV 文本 */
export function asBlob(data: unknown): Blob {
  return data instanceof Blob ? data : new Blob([String(data)], { type: 'text/csv;charset=utf-8' })
}

/** 服务端因匹配行数超过 log.export_max_rows 拒绝导出 */
export function isExportTooMany(e: unknown): boolean {
  return e instanceof ApiError && e.reason === 'EXPORT_TOO_MANY'
}

/**
 * 服务端导出 .xlsx：沿用列表的筛选条件与数据范围，需要 operLog:export 权限，行数受 log.export_max_rows 限制。
 * 出错（如超限）时抛出 ApiError。
 */
export async function exportOperLogs(query: Omit<OperLogQuery, 'page' | 'pageSize'>): Promise<Blob> {
  const res = await http.get<unknown>('/oper-logs/export', exportRequest(query))
  return asBlob(res.data)
}

export function useDeleteOperLogs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<null>('/oper-logs', { ids: ids.join(',') }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['operLog'] }),
  })
}

/** 清理 N 天前的日志；days=0 表示全部清空 */
export function useCleanOperLogs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (days: number) => api.post<{ removed: number }>('/oper-logs/clean', { days }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['operLog'] }),
  })
}

/** 日志统计（后端 /stats 接口的响应；分桶与分位都在服务端算好，前端只做呈现） */
export interface NameValue { name: string; value: number }
export interface StatsDay { date: string; total: number; failures: number }
export interface StatsHour { hour: number; value: number }
export interface StatsLatency { avg: number; p50: number; p95: number; p99: number; max: number }
export interface StatsUrl { url: string; method: string; avgMs: number; count: number }

export interface OperStats {
  from: string
  to: string
  total: number
  failures: number
  sampled: boolean
  days: StatsDay[]
  hours: StatsHour[]
  topModules: NameValue[]
  topTypes: NameValue[]
  methods: NameValue[]
  topOperators: NameValue[]
  latency: StatsLatency
  slowest: StatsUrl[]
}

/** 统计与列表共用筛选条件，保证「统计卡上的数」与「列表里看到的数」同源 */
export type StatsFilter = Pick<OperLogQuery, 'keyword' | 'status' | 'type' | 'startDate' | 'endDate'>

export function useOperStats(filter: StatsFilter) {
  return useQuery({
    queryKey: ['operLog', 'stats', filter],
    queryFn: () => api.get<OperStats>('/oper-logs/stats', { ...filter }),
    placeholderData: keepPreviousData,
  })
}
