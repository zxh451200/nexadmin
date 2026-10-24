import { keepPreviousData, useMutation, useQuery, useQueryClient, type Query } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export const JOB_STATUSES = ['queued', 'running', 'success', 'failed', 'canceled'] as const
export type JobStatus = (typeof JOB_STATUSES)[number]

/** 已知任务类型；其它业务模块新增类型时，在 jobs.type.<type> 里补文案即可（缺省显示原始类型名） */
export const JOB_TYPES = ['user.export'] as const

export interface Job {
  id: string
  type: string
  status: JobStatus
  /** 0-100 */
  progress: number
  message: string
  error: string
  ownerId: string
  ownerName: string
  fileName: string
  fileSize: number
  downloadable: boolean
  createdAt: string
  startedAt: string | null
  finishedAt: string | null
}

export interface JobQuery {
  page: number
  pageSize: number
  type?: string
  status?: string
  /** 管理员（job:all）查看全部任务 */
  all?: boolean
}

export const isActive = (s: JobStatus) => s === 'queued' || s === 'running'

export const jobKeys = {
  all: ['jobs'] as const,
  list: (q: JobQuery) => ['jobs', 'list', q] as const,
  active: ['jobs', 'active'] as const,
  one: (id: string) => ['jobs', 'one', id] as const,
}

/** 列表；refetchInterval 由页面按「有进行中任务且页面可见」决定 */
export function useJobList(query: JobQuery, refetchInterval: number | false | ((q: Query<Page<Job>>) => number | false) = false) {
  return useQuery({
    queryKey: jobKeys.list(query),
    queryFn: () => api.get<Page<Job>>('/jobs', { ...query, all: query.all ? 1 : undefined }),
    placeholderData: keepPreviousData,
    refetchInterval,
  })
}

/** 进行中（排队 + 执行）任务数，顶栏徽标用；静默请求（导出中心模块未启用时不弹错） */
export function useActiveJobCount(refetchInterval: number | false | ((q: Query<number>) => number | false)) {
  return useQuery({
    queryKey: jobKeys.active,
    queryFn: () =>
      api.get<Page<Job>>('/jobs', { status: 'active', page: 1, pageSize: 1 }, { silent: true }).then((p) => p.total),
    refetchInterval,
    retry: false,
  })
}

export function fetchJob(id: string, silent = false) {
  return api.get<Job>(`/jobs/${id}`, undefined, { silent })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => qc.invalidateQueries({ queryKey: jobKeys.all })
}

export function useCancelJob() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.post<null>(`/jobs/${id}/cancel`), onSuccess: invalidate })
}

export function useRetryJob() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.post<null>(`/jobs/${id}/retry`), onSuccess: invalidate })
}

export function useDeleteJob() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.del<null>(`/jobs/${id}`), onSuccess: invalidate })
}

/** 下载结果文件（后端校验所有者；Content-Disposition 带文件名，这里优先用列表里的名字） */
export async function downloadJobFile(job: Pick<Job, 'id' | 'fileName'>): Promise<void> {
  const res = await http.get<unknown>(`/jobs/${job.id}/download`, { responseType: 'blob' })
  const blob = res.data instanceof Blob ? res.data : new Blob([String(res.data)], { type: 'application/octet-stream' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = job.fileName || 'download'
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}
