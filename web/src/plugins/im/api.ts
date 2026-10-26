import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type ConvStatus = 'open' | 'closed'
export type SenderType = 'visitor' | 'agent' | 'system'

export interface ImConversation {
  id: string
  convNo: string
  bizType: string
  bizNo: string
  visitorToken: string
  visitorName: string
  subject: string
  status: ConvStatus
  lastPreview: string
  lastMsgAt?: string | null
  createdAt: string
  unread: number
}

export interface ImMessage {
  id: string
  seq: number
  senderType: SenderType
  senderId: string
  senderName: string
  content: string
  createdAt: string
}

export interface ConvQuery extends PageQuery {
  status?: string
  bizType?: string
}

export interface MessagesResult {
  conversation: ImConversation
  messages: ImMessage[]
}

const BASE = '/plugins/im'
const KEY = ['plugin_im'] as const

export function useConversations(query: ConvQuery) {
  return useQuery({
    queryKey: [...KEY, 'convs', query],
    queryFn: () => api.get<Page<ImConversation>>(`${BASE}/conversations`, query),
    placeholderData: keepPreviousData,
    refetchInterval: 5000,
  })
}

export function useMessages(convNo?: string, refetchMs = 2000) {
  return useQuery({
    queryKey: [...KEY, 'messages', convNo],
    queryFn: () => api.get<MessagesResult>(`${BASE}/conversations/${convNo}/messages`, { since: 0, limit: 200 }),
    enabled: !!convNo,
    refetchInterval: refetchMs,
  })
}

const API_BASE = import.meta.env.VITE_API_BASE || '/api/v1'

/**
 * 打开某会话的 SSE 实时流（先认证换一次性流票）。返回关闭函数。
 * 断流时通过 onError 通知调用方回落到轮询；组件重挂/切换会话时调用关闭函数。
 */
export async function openImStream(
  convNo: string,
  onEvent: () => void,
  onError: () => void,
): Promise<() => void> {
  try {
    const { ticket } = await api.post<{ ticket: string }>(`${BASE}/stream-ticket`)
    const es = new EventSource(`${API_BASE}/plugins/im/stream/${encodeURIComponent(convNo)}?ticket=${encodeURIComponent(ticket)}`)
    let closed = false
    es.onmessage = () => {
      if (!closed) onEvent()
    }
    es.onerror = () => {
      if (!closed) {
        closed = true
        es.close()
        onError()
      }
    }
    return () => {
      if (!closed) {
        closed = true
        es.close()
      }
    }
  } catch {
    onError()
    return () => {}
  }
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useSendReply() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ convNo, content }: { convNo: string; content: string }) =>
      api.post<ImMessage>(`${BASE}/conversations/${convNo}/messages`, { content }),
    onSuccess: invalidate,
  })
}

export function useCloseConv() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (convNo: string) => api.post<ImConversation>(`${BASE}/conversations/${convNo}/close`),
    onSuccess: invalidate,
  })
}
