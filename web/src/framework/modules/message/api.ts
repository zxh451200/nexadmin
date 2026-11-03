import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { collectAllPages } from '@/framework/components/export/ExportCsvButton'
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
  keyword?: string
  /** 'yyyy-MM-dd'，含当天 */
  from?: string
  /** 'yyyy-MM-dd'，含当天 */
  to?: string
}

export interface NoticeQuery extends PageQuery {
  /** '1' 仅未读 */
  unread?: string
  type?: string
  keyword?: string
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

export function useMyNotices(query: NoticeQuery) {
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

export function useMarkRead() {  const invalidate = useInvalidate()
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

export function useClearMessages() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: () => api.del('/messages'),
    onSuccess: invalidate,
  })
}

/** 批量删除选中的消息；只删自己的行，返回实际删除条数 */
export function useDeleteMessages() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>('/messages', { ids: ids.join(',') }),
    onSuccess: invalidate,
  })
}

export function useClearNotices() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: () => api.del('/notices/mine'),
    onSuccess: invalidate,
  })
}

/** 只把这条公告从我的消息中心移除，公告本身与其他人不受影响 */
export function useDeleteNotice() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`/notices/mine/${id}`),
    onSuccess: invalidate,
  })
}

/** 批量移除选中的公告；返回实际移除条数（越权或已移除过的不计入） */
export function useDismissNotices() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ dismissed: number }>('/notices/mine', { ids: ids.join(',') }),
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

/**
 * 导出用的全量取数：与列表同一套筛选条件，只是不分页（逐页捞齐）。
 * 这两个接口本来就是「只返回当前用户自己的数据」，所以导出不会绕过数据范围。
 */
export function fetchMessagesForExport(query: Omit<MessageQuery, 'page' | 'pageSize'>) {
  return collectAllPages<Message>((page, pageSize) => api.get<Page<Message>>('/messages', { ...query, page, pageSize }))
}

export function fetchNoticesForExport(query: Omit<NoticeQuery, 'page' | 'pageSize'>) {
  return collectAllPages<MyNotice>((page, pageSize) => api.get<Page<MyNotice>>('/notices/mine', { ...query, page, pageSize }))
}
