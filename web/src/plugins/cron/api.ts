import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import { usePageLive, useRefreshOnResume } from '@/framework/hooks/use-page-active'
import type { Page, PageQuery } from '@/framework/request/types'

export type RunStatus = 'running' | 'success' | 'failed'
export type Trigger = 'schedule' | 'manual'

export interface CronJob {
  id: string
  name: string
  cronExpr: string
  handler: string
  params: string
  enabled: boolean
  retryCount: number
  /** 单次尝试的超时（秒）；0 = 用全局默认（5 分钟） */
  timeoutSeconds?: number
  lastRunAt: string | null
  lastStatus: RunStatus | ''
  nextRunAt: string | null
  createdAt: string
  updatedAt: string
}

export interface CronJobForm {
  name: string
  cronExpr: string
  handler: string
  params: string
  enabled: boolean
  retryCount: number
  timeoutSeconds?: number
}

export interface CronHandler {
  name: string
  description: string
}

export interface CronLog {
  id: string
  jobId: string
  jobName: string
  handler: string
  trigger: Trigger
  status: RunStatus
  attempts: number
  startedAt: string
  finishedAt: string | null
  costMs: number
  output: string
  error: string
}

export interface JobQuery extends PageQuery {
  handler?: string
  enabled?: string
}

export interface LogQuery extends PageQuery {
  jobId?: string
  status?: string
}

const BASE = '/plugins/cron'
const KEY = ['plugin_cron'] as const

/**
 * 创建执行日志导出任务：筛选与列表同源，提交到导出中心，立即返回任务 ID；
 * 进度与下载见导出中心，创建失败由 http 拦截器统一弹出原因。
 */
export function createLogExportJob(query: Pick<LogQuery, 'jobId' | 'status'>) {
  return api.post<{ id: string }>(`${BASE}/logs/export-async`, null, { params: { jobId: query.jobId, status: query.status } })
}

export function useHandlers() {
  return useQuery({
    queryKey: [...KEY, 'handlers'],
    queryFn: () => api.get<CronHandler[]>(`${BASE}/handlers`),
    staleTime: 5 * 60_000,
  })
}

export function useJobs(query: JobQuery, autoRefresh = true) {
  const live = usePageLive()
  return useQuery({
    queryKey: [...KEY, 'jobs', query],
    queryFn: () => api.get<Page<CronJob>>(`${BASE}/jobs`, query),
    placeholderData: keepPreviousData,
    // 运行中的行 3s 紧盯，平时 10s 慢刷；开关关掉即不轮询，页面不在前台时一律暂停
    refetchInterval: (q) => {
      if (!autoRefresh || !live) return false
      return q.state.data?.list.some((j) => j.lastStatus === 'running') ? 3000 : 10_000
    },
  })
}

export function useLogs(query: LogQuery, autoRefresh = true) {
  const live = usePageLive()
  const q = useQuery({
    queryKey: [...KEY, 'logs', query],
    queryFn: () => api.get<Page<CronLog>>(`${BASE}/logs`, query),
    placeholderData: keepPreviousData,
    // 有任务在运行时 3s 紧盯；平时随开关 10s 慢刷；页面不在前台时暂停
    refetchInterval: (q) => {
      if (!live) return false
      if (q.state.data?.list.some((l) => l.status === 'running')) return 3000
      return autoRefresh ? 10_000 : false
    },
  })
  useRefreshOnResume(live, q.refetch, autoRefresh && !!q.data?.list.some((l) => l.status === 'running'))
  return q
}

export function useLogDetail(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'log', id],
    queryFn: () => api.get<CronLog>(`${BASE}/logs/${id}`),
    enabled: !!id,
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useSaveJob() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: CronJobForm }) =>
      id ? api.put<CronJob>(`${BASE}/jobs/${id}`, data) : api.post<CronJob>(`${BASE}/jobs`, data),
    onSuccess: invalidate,
  })
}

export function useSetEnabled() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.put<CronJob>(`${BASE}/jobs/${id}/enabled`, { enabled }),
    onSuccess: invalidate,
  })
}

export function useDeleteJob() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/jobs/${id}`),
    onSuccess: invalidate,
  })
}

/** 批量删除任务：deleted 是后端实际删除条数（越权或已被删的 id 不计入，一条都没删到时报 404）。
 *  路径写静态字面量：contract_check 靠静态解析这里的第一个实参对照后端路由。 */
export function useDeleteJobs() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>('/plugins/cron/jobs', { ids: ids.join(',') }),
    onSuccess: invalidate,
  })
}

export function useRunJob() {
  const invalidate = useInvalidate()
  return useMutation({
    // params 可选：带上时仅本次执行覆盖任务保存的参数（不写回任务行）
    mutationFn: ({ id, params }: { id: string; params?: string }) =>
      api.post<CronLog>(`${BASE}/jobs/${id}/run`, params ? { params } : undefined),
    onSuccess: invalidate,
  })
}

/** 批量启停：updated 是后端实际命中的行数（越权或已删除的 id 不计入）。 */
export function useSetJobsEnabled() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (p: { ids: string[]; enabled: boolean }) =>
      api.post<{ updated: number }>('/plugins/cron/jobs/enabled', p),
    onSuccess: invalidate,
  })
}

export function useCleanLogs() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (params: { days: number; jobId?: string }) => api.del<{ deleted: number }>(`${BASE}/logs`, params),
    onSuccess: invalidate,
  })
}
