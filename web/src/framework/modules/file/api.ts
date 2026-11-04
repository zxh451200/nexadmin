import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export type FileCategory = 'image' | 'video' | 'doc' | 'other'
export type FileVisibility = 'public' | 'private'

export interface FileItem {
  id: string
  name: string
  /** 视频 url 可能为空，此时只显示封面占位 */
  url: string
  posterUrl?: string
  type: string
  category: FileCategory
  size: number
  /** 图片像素尺寸（0 = 非图片或未提取） */
  width?: number
  height?: number
  visibility: FileVisibility
  /** 空字符串 = 未分组 */
  groupId: string
  uploader: string
  createdAt: string
}

export interface FileGroup {
  id: string
  name: string
  count?: number
}

export type FileSort = 'created_at' | 'name' | 'size'

export interface FileQuery {
  page: number
  pageSize: number
  keyword?: string
  category?: string
  groupId?: string
  from?: string
  to?: string
  /** 排序字段（白名单外的值后端回退默认）；默认 created_at */
  sort?: FileSort
  /** 默认倒序（最新/最大在前） */
  desc?: boolean
}

export interface RegisterFilesBody {
  files: Array<{ name: string; url: string; posterUrl?: string; type: string; size: number }>
  groupId: string
  visibility: FileVisibility
}

const KEY = ['files'] as const
const GROUP_KEY = ['file-groups'] as const

export function useFileList(query: FileQuery) {
  return useQuery({
    queryKey: [...KEY, query],
    queryFn: () => api.get<Page<FileItem>>('/files', query),
    placeholderData: keepPreviousData,
  })
}

export interface FileUsage {
  usedBytes: number
  /** 套餐存储额度（MB）；0 = 不限 */
  quotaMB: number
}

/** 侧栏用量（已用 + 套餐额度）：后端 30s 缓存，这里 30s 轮询同口径。 */
export function useFileUsage() {
  return useQuery({
    queryKey: [...KEY, 'usage'],
    queryFn: () => api.get<FileUsage>('/files/usage'),
    refetchInterval: 30_000,
  })
}

export function useFileGroups() {
  return useQuery({ queryKey: GROUP_KEY, queryFn: () => api.get<FileGroup[]>('/file-groups') })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: KEY })
    void qc.invalidateQueries({ queryKey: GROUP_KEY })
  }
}

export function useCreateGroup() {
  const inv = useInvalidate()
  return useMutation({ mutationFn: (name: string) => api.post<FileGroup>('/file-groups', { name }), onSuccess: inv })
}

export function useRenameGroup() {
  const inv = useInvalidate()
  return useMutation({ mutationFn: (v: { id: string; name: string }) => api.put<FileGroup>(`/file-groups/${v.id}`, { name: v.name }), onSuccess: inv })
}

export function useDeleteGroup() {
  const inv = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.del(`/file-groups/${id}`), onSuccess: inv })
}

export function useRegisterFiles() {
  const inv = useInvalidate()
  return useMutation({ mutationFn: (body: RegisterFilesBody) => api.post<FileItem[]>('/files', body), onSuccess: inv })
}

export function useUpdateFile() {
  const inv = useInvalidate()
  return useMutation({ mutationFn: (v: { id: string; patch: Partial<Pick<FileItem, 'visibility' | 'name' | 'groupId'>> }) => api.put<FileItem>(`/files/${v.id}`, v.patch), onSuccess: inv })
}

export function useMoveFiles() {
  const inv = useInvalidate()
  return useMutation({ mutationFn: (v: { ids: string[]; groupId: string }) => api.put('/files/move', v), onSuccess: inv })
}

export function useDeleteFiles() {
  const inv = useInvalidate()
  return useMutation({ mutationFn: (ids: string[]) => api.del('/files', { ids: ids.join(',') }), onSuccess: inv })
}
