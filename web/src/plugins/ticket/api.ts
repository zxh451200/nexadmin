import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type TicketStatus = 'open' | 'processing' | 'resolved' | 'closed'
export type TicketPriority = 'low' | 'normal' | 'high' | 'urgent'
export type ReplierType = 'reporter' | 'agent' | 'system'

export interface Ticket {
  id: string
  ticketNo: string
  bizType: string
  bizNo: string
  title: string
  status: TicketStatus
  priority: TicketPriority
  reporterType: string
  reporterId: string
  reporterName: string
  assigneeId: string
  assigneeName: string
  firstReplyAt?: string | null
  lastReplyAt?: string | null
  lastReplierType: string
  lastPreview: string
  dueAt?: string | null
  overdue: boolean
  createdAt: string
}

export interface TicketReply {
  id: string
  seq: number
  replierType: ReplierType
  replierId: string
  replierName: string
  content: string
  createdAt: string
}

export interface TicketQueryParams extends PageQuery {
  status?: string
  priority?: string
  bizType?: string
  overdue?: string
}

export interface RepliesResult {
  ticket: Ticket
  replies: TicketReply[]
}

const BASE = '/plugins/ticket'
const KEY = ['plugin_ticket'] as const

export function useTickets(query: TicketQueryParams) {
  return useQuery({
    queryKey: [...KEY, 'tickets', query],
    queryFn: () => api.get<Page<Ticket>>(`${BASE}/tickets`, query),
    placeholderData: keepPreviousData,
    refetchInterval: 8000,
  })
}

export function useReplies(ticketNo?: string) {
  return useQuery({
    queryKey: [...KEY, 'replies', ticketNo],
    queryFn: () => api.get<RepliesResult>(`${BASE}/tickets/${ticketNo}/replies`, { since: 0, limit: 200 }),
    enabled: !!ticketNo,
    refetchInterval: 3000,
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useReplyTicket() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ticketNo, content }: { ticketNo: string; content: string }) =>
      api.post<{ reply: TicketReply; ticket: Ticket }>(`${BASE}/tickets/${ticketNo}/replies`, { content }),
    onSuccess: invalidate,
  })
}

export function useSetTicketStatus() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ticketNo, status }: { ticketNo: string; status: string }) =>
      api.post<Ticket>(`${BASE}/tickets/${ticketNo}/status`, { status }),
    onSuccess: invalidate,
  })
}

export function useAssignTicket() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ticketNo, assigneeId, assigneeName }: { ticketNo: string; assigneeId: number; assigneeName: string }) =>
      api.post<Ticket>(`${BASE}/tickets/${ticketNo}/assign`, { assigneeId, assigneeName }),
    onSuccess: invalidate,
  })
}
