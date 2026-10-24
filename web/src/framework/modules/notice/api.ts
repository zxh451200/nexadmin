import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'
import type { UploadedFile } from '@/framework/upload/types'

export type NoticeScope = 'all' | 'role' | 'dept' | 'user'
export type NoticeStatus = 'draft' | 'published' | 'revoked'

export interface Notice {
  id: string
  title: string
  type: string
  level: string
  status: NoticeStatus
  content: string
  scope: NoticeScope
  targetIds: string[]
  pinned: boolean
  popup: boolean
  /** 平台公告（全部租户） */
  platform: boolean
  publishAt?: string | null
  expireAt?: string | null
  publishedAt?: string | null
  attachments: UploadedFile[]
  readCount: number
  totalCount: number
  createdAt: string
  updatedAt: string
}

export interface NoticeForm {
  title: string
  type: string
  level: string
  content: string
  scope: NoticeScope
  targetIds: string[]
  pinned: boolean
  popup: boolean
  platform: boolean
  publishAt: string | null
  expireAt: string | null
  attachments: UploadedFile[]
}

export interface NoticeReadStats {
  readCount: number
  unreadCount: number
  unreadUsers: { id: string; username: string; nickname: string; deptName?: string }[]
}

export interface NoticeQuery extends PageQuery {
  type?: string
  level?: string
  status?: string
}

const KEY = ['notice'] as const

export function useNoticeList(query: NoticeQuery) {
  return useQuery({
    queryKey: [...KEY, 'list', query],
    queryFn: () => api.get<Page<Notice>>('/notices', query),
    placeholderData: keepPreviousData,
  })
}

export function useNoticeDetail(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'detail', id],
    queryFn: () => api.get<Notice>(`/notices/${id}`),
    enabled: !!id,
  })
}

export function useNoticeReadStats(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'stats', id],
    queryFn: () => api.get<NoticeReadStats>(`/notices/${id}/read-stats`),
    enabled: !!id,
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: KEY })
    void qc.invalidateQueries({ queryKey: ['message'] })
    void qc.invalidateQueries({ queryKey: ['notify'] })
  }
}

export function useSaveNotice() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: NoticeForm }) =>
      id ? api.put<Notice>(`/notices/${id}`, data) : api.post<Notice>('/notices', data),
    onSuccess: invalidate,
  })
}

export function usePublishNotice() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.post<Notice>(`/notices/${id}/publish`),
    onSuccess: invalidate,
  })
}

export function useRevokeNotice() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.post<Notice>(`/notices/${id}/revoke`),
    onSuccess: invalidate,
  })
}

export function useDeleteNotices() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ids: string[]) =>
      ids.length === 1 ? api.del(`/notices/${ids[0]}`) : api.del('/notices', { ids: ids.join(',') }),
    onSuccess: invalidate,
  })
}
