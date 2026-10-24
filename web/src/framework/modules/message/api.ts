import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'
import type { UploadedFile } from '@/framework/upload/types'

export interface Message {
  id: string
  category: string
  title: string
  content: string
  link?: string
  readAt?: string | null
  createdAt: string
}

export interface MyNotice {
  id: string
  title: string
  type: string
  level: string
  content: string
  pinned: boolean
  read: boolean
  publishedAt?: string | null
  attachments: UploadedFile[]
}

export interface UnreadCount {
  messages: number
  notices: number
}

export interface MessageQuery extends PageQuery {
  /** '1' 仅未读 */
  unread?: string
  category?: string
}

const KEY = ['message'] as const

export function useMessageList(query: MessageQuery) {
  return useQuery({
    queryKey: [...KEY, 'list', query],
    queryFn: () => api.get<Page<Message>>('/messages', query),
    placeholderData: keepPreviousData,
  })
}

export function useUnreadCount() {
  return useQuery({
    queryKey: [...KEY, 'unread-count'],
    queryFn: () => api.get<UnreadCount>('/messages/unread-count', undefined, { silent: true }),
  })
}

export function useMyNotices(query: PageQuery) {
  return useQuery({
    queryKey: [...KEY, 'notices', query],
    queryFn: () => api.get<Page<MyNotice>>('/notices/mine', query),
    placeholderData: keepPreviousData,
  })
}

/** 同时刷新消息中心与顶栏铃铛（['notify']）的数据 */
function useInvalidate() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: KEY })
    void qc.invalidateQueries({ queryKey: ['notify'] })
  }
}

export function useMarkRead() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.put(`/messages/${id}/read`, undefined),
    onSuccess: invalidate,
  })
}

export function useReadAll() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: () => api.put('/messages/read-all', undefined),
    onSuccess: invalidate,
  })
}

export function useDeleteMessage() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`/messages/${id}`),
    onSuccess: invalidate,
  })
}

export function useReadNotice() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.post(`/notices/${id}/read`, undefined),
    onSuccess: invalidate,
  })
}
