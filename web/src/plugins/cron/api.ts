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
 * 导出执行日志 CSV（带令牌的 XHR 拉取 blob——浏览器直接打开链接不会带 Authorization）。
 * 筛选与列表同源；上限见后端 logExportMax。
 */
export async function downloadLogsCsv(query: Pick<LogQuery, 'jobId' | 'status'>): Promise<void> {
  const { http } = await import('@/framework/request/http')
  const res = await http.get<unknown>(`${BASE}/logs/export.csv`, { params: { ...query }, responseType: 'blob', timeout: 60_000 })
  // mock 阶段的响应体是字符串而不是 Blob（与用户导出同一兜底写法），否则 createObjectURL 直接抛。
  const blob = res.data instanceof Blob ? res.data : new Blob([String(res.data)], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `cron-logs-${new Date().toISOString().slice(0, 10).replace(/-/g, '')}.csv`
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

export function useHandlers() {
  return useQuery({
    queryKey: [...KEY, 'handlers'],
    queryFn: () => api.get<CronHandler[]>(`${BASE}/handlers`),
    staleTime: 5 * 60_000,
  })
}

export function useJobs(query: JobQuery) {
  return useQuery({
    queryKey: [...KEY, 'jobs', query],
    queryFn: () => api.get<Page<CronJob>>(`${BASE}/jobs`, query),
    placeholderData: keepPreviousData,
  })
}

export function useLogs(query: LogQuery) {
  const live = usePageLive()
  const q = useQuery({
    queryKey: [...KEY, 'logs', query],
    queryFn: () => api.get<Page<CronLog>>(`${BASE}/logs`, query),
    placeholderData: keepPreviousData,
    // 有任务在运行时轮询，结束后自动停止；页面不在前台时暂停
    refetchInterval: (q) => (live && q.state.data?.list.some((l) => l.status === 'running') ? 3000 : false),
  })
  useRefreshOnResume(live, q.refetch, !!q.data?.list.some((l) => l.status === 'running'))
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
    mutationFn: (id: string) => api.post<CronLog>(`${BASE}/jobs/${id}/run`),
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
