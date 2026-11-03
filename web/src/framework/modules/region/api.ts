import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'

/** 当前生效的省市区数据版本 */
export interface RegionDatasetInfo {
  builtin: boolean
  sourceUrl: string
  dataDate: string
  license: string
  sha256: string
  provinces: number
  prefectures: number
  counties: number
  syncedBy: string
  syncedAt: string
}

export interface RegionStatus extends RegionDatasetInfo {
  defaultSource: string
}

/** 一次同步的变更统计 */
export interface RegionSyncResult {
  info: RegionDatasetInfo
  added: number
  removed: number
  renamed: number
}

/** 一条变更样本（每类最多 20 条） */
export interface RegionDiffSample {
  code: string
  name: string
  /** 只有改名样本有：原名称 */
  old?: string
}

/** dry-run 预览结果：数据未生效 */
export interface RegionPreviewResult {
  preview: true
  added: number
  removed: number
  renamed: number
  addedSamples?: RegionDiffSample[]
  removedSamples?: RegionDiffSample[]
  renamedSamples?: RegionDiffSample[]
}

const KEY = ['region', 'status'] as const

export function useRegionStatus() {
  return useQuery({ queryKey: KEY, queryFn: () => api.get<RegionStatus>('/regions/status') })
}

/** 同步成功后同时失效省市区树缓存，选择器不必重新登录就能看到新数据 */
function useInvalidateRegions() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: KEY })
    void qc.invalidateQueries({ queryKey: ['regions-tree'] })
  }
}

export function useSyncRegion() {
  const invalidate = useInvalidateRegions()
  return useMutation({
    // silent：错误提示由面板自己弹（带固定 id，重复失败不会叠成一摞）。
    // 同步是后台任务：POST 只代表受理，结果从 status 轮询取。
    mutationFn: (source: string) => api.post<{ started: boolean }>('/regions/sync', { source }, { silent: true }),
    onSuccess: invalidate,
  })
}

/** 后台同步任务的阶段与结果 */
export interface RegionSyncStatus {
  running: boolean
  stage?: 'fetching' | 'validating'
  result?: RegionSyncResult
  error?: string
}

export function useSyncRegionStatus(enabled: boolean) {
  return useQuery({
    queryKey: [...KEY, 'sync-status'],
    queryFn: () => api.get<RegionSyncStatus>('/regions/sync/status'),
    // 任务进行中才轮询；结束时保留最后一次数据供展示
    refetchInterval: (q) => (q.state.data?.running ? 1200 : false),
    enabled,
  })
}

/** dry-run：同一条校验链路但不落库不生效，先看会改什么再决定同步 */
export function usePreviewRegion() {
  return useMutation({
    mutationFn: (source: string) => api.post<RegionPreviewResult>('/regions/preview', { source }, { silent: true }),
  })
}

export function useResetRegion() {
  const invalidate = useInvalidateRegions()
  return useMutation({
    mutationFn: () => api.post<RegionStatus>('/regions/reset', undefined, { silent: true }),
    onSuccess: invalidate,
  })
}

/** 手动上传数据文件：apply=false 只预览差异（dry-run），true 直接生效 */
export function useImportRegionFile() {
  const invalidate = useInvalidateRegions()
  return useMutation({
    mutationFn: ({ file, apply }: { file: File; apply: boolean }) => {
      const fd = new FormData()
      fd.append('file', file)
      return http
        .post<RegionPreviewResult>(`/regions/import${apply ? '?apply=1' : ''}`, fd, { silent: true, timeout: 60_000 })
        .then((r) => r.data)
    },
    onSuccess: (_d, v) => {
      if (v.apply) invalidate()
    },
  })
}
