import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type ConfigValueType = 'string' | 'number' | 'boolean' | 'json'
export const CONFIG_VALUE_TYPES: ConfigValueType[] = ['string', 'number', 'boolean', 'json']

export interface ConfigRow {
  id: string
  name: string
  key: string
  value: string
  type: ConfigValueType
  isPublic: boolean
  remark?: string
  createdAt: string
}

export type ConfigInput = Omit<ConfigRow, 'id' | 'createdAt'>

/** 0 表示不限制 */
export interface UploadSettings {
  maxImageMb: number
  maxVideoMb: number
  maxFileMb: number
  allowedTypes: string[]
  imageWebp: boolean
}

const LIST_KEY = ['sysConfig', 'list'] as const
const UPLOAD_KEY = ['sysConfig', 'upload'] as const

export function useConfigs(query: PageQuery) {
  return useQuery({
    queryKey: [...LIST_KEY, query],
    queryFn: () => api.get<Page<ConfigRow>>('/configs', query),
    placeholderData: keepPreviousData,
  })
}

export function useSaveConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body }: { id?: string; body: ConfigInput }) =>
      id ? api.put<ConfigRow>(`/configs/${id}`, body) : api.post<ConfigRow>('/configs', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sysConfig'] }),
  })
}

export function useDeleteConfigs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<null>('/configs', { ids: ids.join(',') }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sysConfig'] }),
  })
}

export function useUploadSettings() {
  return useQuery({ queryKey: UPLOAD_KEY, queryFn: () => api.get<UploadSettings>('/configs/upload') })
}

export function useSaveUploadSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UploadSettings) => api.put<UploadSettings>('/configs/upload', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sysConfig'] }),
  })
}

export function useRefreshConfigCache() {
  return useMutation({ mutationFn: () => api.post<null>('/configs/refresh-cache') })
}
