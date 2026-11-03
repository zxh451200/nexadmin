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
const VERSIONS_KEY = ['region', 'versions'] as const

/** 一个历史版本（列表项，不含整棵树正文） */
export interface RegionVersion {
  version: number
  sha256: string
  dataDate: string
  sourceUrl: string
  provinces: number
  prefectures: number
  counties: number
  createdBy: string
  createdAt: string
}

/** 单个历史版本的完整记录：正文是那一份整棵树，页面只用它算省级摘要 */
export interface RegionVersionDetail extends RegionVersion {
  payload: string
}

export function useRegionStatus() {
  return useQuery({ queryKey: KEY, queryFn: () => api.get<RegionStatus>('/regions/status') })
}

/** 同步成功后同时失效省市区树缓存，选择器不必重新登录就能看到新数据 */
function useInvalidateRegions() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: KEY })
    void qc.invalidateQueries({ queryKey: VERSIONS_KEY })
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

/** 版本历史列表（新→旧，不含整棵树正文）：对话框打开时才请求 */
export function useRegionVersions(enabled: boolean) {
  return useQuery({
    queryKey: VERSIONS_KEY,
    queryFn: () => api.get<RegionVersion[]>('/regions/versions'),
    enabled,
  })
}

/** 单个版本的完整记录（含整棵树，页面只取省级名单展示） */
export function useRegionVersion(version?: number) {
  return useQuery({
    // 路径写静态字面量：contract_check 靠静态解析这里的第一个实参对照后端路由
    queryFn: () => api.get<RegionVersionDetail>(`/regions/versions/${version}`, undefined, { silent: true }),
    queryKey: [...VERSIONS_KEY, version],
    enabled: !!version,
  })
}

/** 回滚到某个版本：那份快照重新走一次完整校验与落库，版本继续递增，还能再回滚 */
export function useRollbackRegionVersion() {
  const invalidate = useInvalidateRegions()
  return useMutation({
    mutationFn: (version: number) =>
      api.post<RegionSyncResult>(`/regions/versions/${version}/rollback`, {}, { silent: true }),
    onSuccess: invalidate,
  })
}
