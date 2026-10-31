import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type TicketStatus = 'open' | 'processing' | 'pending' | 'resolved' | 'closed'
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
  creatorId?: string
  creatorName?: string
  assigneeId: string
  assigneeName: string
  firstReplyAt?: string | null
  lastReplyAt?: string | null
  lastReplierType: string
  lastPreview: string
  dueAt?: string | null
  overdue: boolean
  slaPausedAt?: string | null
  resolvedAt?: string | null
  closedAt?: string | null
  rating?: number | null
  ratingComment?: string
  ratedAt?: string | null
  createdAt: string
}

export interface TicketReply {
  id: string
  seq: number
  replierType: ReplierType
  replierId: string
  replierName: string
  content: string
  /** 内部备注：仅坐席可见 */
  internal?: boolean
  createdAt: string
}

export interface TicketQueryParams extends PageQuery {
  status?: string
  priority?: string
  bizType?: string
  keyword?: string
  overdue?: string
  assigneeId?: string
  unassigned?: string
  mine?: string
}

export interface RepliesResult {
  ticket: Ticket
  replies: TicketReply[]
}

export interface CreateTicketInput {
  title: string
  content: string
  priority?: TicketPriority
  reporterName?: string
  reporterId?: string
  bizType?: string
  bizNo?: string
}

export interface BulkTicketsInput {
  ticketNos: string[]
  action: 'assign' | 'status' | 'priority'
  status?: string
  priority?: string
  assigneeId?: number
  assigneeName?: string
}

export interface BulkResult {
  updated: number
  skipped: number
}

export interface CannedReply {
  id: string
  title: string
  content: string
  sort: number
}

export interface CannedInput {
  title: string
  content: string
  sort?: number
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
    mutationFn: ({ ticketNo, content, internal }: { ticketNo: string; content: string; internal?: boolean }) =>
      api.post<{ reply: TicketReply; ticket: Ticket }>(`${BASE}/tickets/${ticketNo}/replies`, { content, internal }),
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

/** 后台代客建单 */
export function useCreateTicket() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: CreateTicketInput) => api.post<Ticket>(`${BASE}/tickets`, input),
    onSuccess: invalidate,
  })
}

/** 批量认领/改派、改状态、改优先级 */
export function useBulkTickets() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: BulkTicketsInput) => api.post<BulkResult>(`${BASE}/tickets/bulk`, input),
    onSuccess: invalidate,
  })
}

export function useCannedReplies() {
  return useQuery({
    queryKey: [...KEY, 'canned'],
    queryFn: () => api.get<CannedReply[]>(`${BASE}/canned`),
  })
}

export function useCreateCanned() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: CannedInput) => api.post<CannedReply>(`${BASE}/canned`, input),
    onSuccess: invalidate,
  })
}

export function useUpdateCanned() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, ...input }: CannedInput & { id: string }) =>
      api.put<CannedReply>(`${BASE}/canned/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteCanned() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del<unknown>(`${BASE}/canned/${id}`),
    onSuccess: invalidate,
  })
}

/** 工单统计（与后端 /plugins/ticket/tickets/stats 一一对应） */
export interface StatsNameValue { name: string; value: number }
export interface StatsDay { date: string; created: number; resolved: number }
export interface PrioritySla {
  priority: string
  targetMinutes: number
  total: number
  breached: number
  breachRate: number
}
export interface FirstResponseStats {
  responded: number
  noResponse: number
  avgMinutes: number
  p50Minutes: number
  p90Minutes: number
}
export interface ResolutionStats {
  resolved: number
  avgMinutes: number
  p50Minutes: number
  p90Minutes: number
}
export interface SatisfactionStats {
  rated: number
  avgScore: number
  byScore: StatsNameValue[]
}
export interface TicketStats {
  from: string
  to: string
  total: number
  open: number
  processing: number
  pending: number
  resolved: number
  closed: number
  unassigned: number
  overdue: number
  sampled: boolean
  days: StatsDay[]
  byStatus: StatsNameValue[]
  byPriority: StatsNameValue[]
  byBizType: StatsNameValue[]
  byAssignee: StatsNameValue[]
  firstResponse: FirstResponseStats
  resolution: ResolutionStats
  satisfaction: SatisfactionStats
  slaByPriority: PrioritySla[]
}

export interface TicketStatsFilter {
  status?: string
  priority?: string
  bizType?: string
  keyword?: string
  overdue?: string
  startDate?: string
  endDate?: string
}

/** 统计与列表共用筛选条件，两边的数才对得上 */
export function useTicketStats(filter: TicketStatsFilter) {
  return useQuery({
    queryKey: ['ticket', 'stats', filter],
    queryFn: () => api.get<TicketStats>(`${BASE}/tickets/stats`, filter),
    placeholderData: keepPreviousData,
  })
}
