import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export type TagVariant = 'default' | 'success' | 'warning' | 'destructive' | 'info'
export const TAG_VARIANTS: TagVariant[] = ['default', 'success', 'warning', 'destructive', 'info']

export interface DictTypeRow {
  id: string
  name: string
  code: string
  sort: number
  status: string
  remark?: string
  createdAt: string
  /** 出厂内置字典（后端由 seedTypes 派生，不入库）：不可删除、不可改编码 */
  builtin?: boolean
}

export interface DictDataRow {
  id: string
  typeId: string
  label: string
  value: string
  tag: TagVariant
  sort: number
  status: string
  remark?: string
}

export type DictTypeInput = Omit<DictTypeRow, 'id' | 'createdAt' | 'builtin'>
export type DictDataInput = Omit<DictDataRow, 'id'>

const TYPES_KEY = ['dict', 'types'] as const
const DATA_KEY = ['dict', 'data'] as const

export function useDictTypes(keyword: string) {
  return useQuery({
    queryKey: [...TYPES_KEY, keyword],
    queryFn: () => api.get<Page<DictTypeRow>>('/dict/types', { keyword: keyword || undefined, page: 1, pageSize: 500 }),
    select: (p) => p.list,
  })
}

export function useDictData(typeId: string | null) {
  return useQuery({
    queryKey: [...DATA_KEY, typeId],
    enabled: !!typeId,
    queryFn: () => api.get<Page<DictDataRow>>('/dict/data', { typeId, page: 1, pageSize: 500 }),
    select: (p) => p.list,
  })
}

function useInvalidate(key: readonly string[]) {
  const qc = useQueryClient()
  return () => qc.invalidateQueries({ queryKey: key })
}

export function useSaveDictType() {
  const invalidate = useInvalidate(TYPES_KEY)
  return useMutation({
    mutationFn: ({ id, body }: { id?: string; body: DictTypeInput }) =>
      id ? api.put<DictTypeRow>(`/dict/types/${id}`, body) : api.post<DictTypeRow>('/dict/types', body),
    onSuccess: invalidate,
  })
}

export function useDeleteDictType() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/dict/types/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dict'] }),
  })
}

export function useSortDictTypes() {
  const invalidate = useInvalidate(TYPES_KEY)
  return useMutation({
    mutationFn: (ids: string[]) => api.put<null>('/dict/types/sort', { ids }),
    onSuccess: invalidate,
  })
}

export function useSaveDictData() {
  const invalidate = useInvalidate(DATA_KEY)
  return useMutation({
    mutationFn: ({ id, body }: { id?: string; body: DictDataInput }) =>
      id ? api.put<DictDataRow>(`/dict/data/${id}`, body) : api.post<DictDataRow>('/dict/data', body),
    onSuccess: invalidate,
  })
}

export function useDeleteDictData() {
  const invalidate = useInvalidate(DATA_KEY)
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/dict/data/${id}`),
    onSuccess: invalidate,
  })
}

export function useSortDictData() {
  const invalidate = useInvalidate(DATA_KEY)
  return useMutation({
    mutationFn: (ids: string[]) => api.put<null>('/dict/data/sort', { ids }),
    onSuccess: invalidate,
  })
}
