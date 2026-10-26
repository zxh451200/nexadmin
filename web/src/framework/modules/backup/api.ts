import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export type BackupSource = 'manual' | 'schedule'
export type BackupStatus = 'running' | 'success' | 'failed'

export interface BackupRecord {
  id: string
  fileName: string
  engine: string
  version: string
  size: number
  sha256: string
  source: BackupSource
  durationMs: number
  status: BackupStatus
  error: string
  createdBy: string
  createdAt: string
}

/** GET /backups/status；available=false 时 reason 是页面要显示的原因（如本机没有 mysqldump） */
export interface BackupStatusInfo {
  available: boolean
  tool?: string
  reason?: string
  engine: string
  version: string
  dir: string
  /** 读不到为 null */
  diskFreeBytes: number | null
  /** HH:MM，空 = 未开启定时 */
  schedule: string
  nextRunAt: string | null
  running: boolean
  lastSuccessAt: string | null
}

export interface BackupConfig {
  schedule: string
  keepDays: number
  keepCount: number
}

const KEY = ['backups'] as const
const STATUS_KEY = ['backups', 'status'] as const
const CONFIG_KEY = ['backups', 'config'] as const

const POLL_MS = 3000

/** 列表里有“进行中”的备份且页面可见（live）时每 3 秒刷新，其余时候不轮询 */
export function useBackupList(page: number, pageSize: number, live: boolean) {
  return useQuery({
    queryKey: [...KEY, 'list', page, pageSize],
    queryFn: () => api.get<Page<BackupRecord>>('/backups', { page, pageSize }),
    refetchInterval: (q) => (live && q.state.data?.list.some((r) => r.status === 'running') ? POLL_MS : false),
  })
}

/** 状态里 running=true 且页面可见（live）时每 3 秒刷新 */
export function useBackupStatus(live: boolean) {
  return useQuery({
    queryKey: STATUS_KEY,
    queryFn: () => api.get<BackupStatusInfo>('/backups/status'),
    refetchInterval: (q) => (live && q.state.data?.running ? POLL_MS : false),
  })
}

export function useBackupConfig() {
  return useQuery({ queryKey: CONFIG_KEY, queryFn: () => api.get<BackupConfig>('/backups/config') })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => qc.invalidateQueries({ queryKey: KEY })
}

export function useCreateBackup() {
  const inv = useInvalidate()
  return useMutation({ mutationFn: () => api.post<BackupRecord>('/backups'), onSuccess: inv })
}

export function useDeleteBackup() {
  const inv = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.del<void>(`/backups/${id}`), onSuccess: inv })
}

export function useSaveBackupConfig() {
  const inv = useInvalidate()
  return useMutation({ mutationFn: (body: BackupConfig) => api.put<BackupConfig>('/backups/config', body), onSuccess: inv })
}

/** 下载走带令牌的 XHR（浏览器直接打开链接不会带 Authorization）；大文件放宽超时 */
export async function downloadBackup(id: string): Promise<Blob> {
  const res = await http.get<unknown>(`/backups/${id}/download`, { responseType: 'blob', timeout: 30 * 60 * 1000 })
  return res.data instanceof Blob ? res.data : new Blob([String(res.data)], { type: 'application/gzip' })
}
