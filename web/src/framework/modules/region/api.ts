import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

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
    // silent：错误提示由面板自己弹（带固定 id，重复失败不会叠成一摞）
    mutationFn: (source: string) => api.post<RegionSyncResult>('/regions/sync', { source }, { silent: true }),
    onSuccess: invalidate,
  })
}

export function useResetRegion() {
  const invalidate = useInvalidateRegions()
  return useMutation({
    mutationFn: () => api.post<RegionStatus>('/regions/reset', undefined, { silent: true }),
    onSuccess: invalidate,
  })
}
