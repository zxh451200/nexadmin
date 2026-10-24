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
