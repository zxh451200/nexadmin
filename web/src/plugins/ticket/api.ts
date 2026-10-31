import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, tokenStore } from '@/framework/request/http'
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
  groupId?: string
  groupName?: string
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
  resolutionDueAt?: string | null
  resolutionOverdue?: boolean
  rating?: number | null
  ratingComment?: string
  ratedAt?: string | null
  tags?: string[]
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
  groupId?: string
  tag?: string
}

export interface RepliesResult {
  ticket: Ticket
  replies: TicketReply[]
  attachments?: TicketAttachment[]
}

export interface TicketAttachment {
  id: string
  token: string
  uploaderType: string
  uploaderName: string
  internal: boolean
  name: string
  mime: string
  size: number
  createdAt: string
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

export interface GroupMember {
  id?: string
  groupId: string
  userId: string
  userName: string
}

export interface TicketGroup {
  id: string
  name: string
  description: string
  autoAssign: boolean
  enabled: boolean
  sort: number
  members?: GroupMember[]
}

export interface GroupInput {
  name: string
  description: string
  autoAssign: boolean
  enabled: boolean
  sort: number
}

export interface TicketRule {
  id: string
  name: string
  enabled: boolean
  sort: number
  matchPriority: string
  matchBizType: string
  matchKeyword: string
  setPriority: string
  setGroupId: string
  note: string
}

export interface RuleInput {
  name: string
  enabled: boolean
  sort: number
  matchPriority: string
  matchBizType: string
  matchKeyword: string
  setPriority: string
  setGroupId: number
  note: string
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

export function useUploadAttachment() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ticketNo, file, internal }: { ticketNo: string; file: File; internal?: boolean }) => {
      const fd = new FormData()
      fd.append('file', file)
      if (internal) fd.append('internal', '1')
      return api.post<TicketAttachment>(`${BASE}/tickets/${ticketNo}/attachments`, fd)
    },
    onSuccess: invalidate,
  })
}

/** 附件下载要带登录头，不能直接用 <a href>；取回 blob 后触发浏览器下载。 */
export async function downloadAttachment(ticketNo: string, att: TicketAttachment) {
  const res = await fetch(`${BASE}/tickets/${ticketNo}/attachments/${att.token}`, {
    headers: tokenStore.access ? { Authorization: `Bearer ${tokenStore.access}` } : undefined,
  })
  if (!res.ok) throw new Error('download failed')
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = att.name
  a.click()
  URL.revokeObjectURL(url)
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

export function useGroups() {
  return useQuery({
    queryKey: [...KEY, 'groups'],
    queryFn: () => api.get<TicketGroup[]>(`${BASE}/groups`),
  })
}

export function useCreateGroup() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: GroupInput) => api.post<TicketGroup>(`${BASE}/groups`, input),
    onSuccess: invalidate,
  })
}

export function useUpdateGroup() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, ...input }: GroupInput & { id: string }) => api.put<TicketGroup>(`${BASE}/groups/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteGroup() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del<unknown>(`${BASE}/groups/${id}`),
    onSuccess: invalidate,
  })
}

export function useSetGroupMembers() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, userIds }: { id: string; userIds: string[] }) =>
      api.put<TicketGroup>(`${BASE}/groups/${id}/members`, { userIds: userIds.map(Number) }),
    onSuccess: invalidate,
  })
}

export function useAssignGroup() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ticketNo, groupId }: { ticketNo: string; groupId: number }) =>
      api.post<Ticket>(`${BASE}/tickets/${ticketNo}/group`, { groupId }),
    onSuccess: invalidate,
  })
}

export function useRules() {
  return useQuery({
    queryKey: [...KEY, 'rules'],
    queryFn: () => api.get<TicketRule[]>(`${BASE}/rules`),
  })
}

/** 规则条件可用的业务类型候选：已有工单用过的 + 既有规则写过的，去重排序 */
export function useBizTypes() {
  return useQuery({
    queryKey: [...KEY, 'biz-types'],
    queryFn: () => api.get<string[]>(`${BASE}/biz-types`),
  })
}

export function useCreateRule() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: RuleInput) => api.post<TicketRule>(`${BASE}/rules`, input),
    onSuccess: invalidate,
  })
}

export function useUpdateRule() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, ...input }: RuleInput & { id: string }) => api.put<TicketRule>(`${BASE}/rules/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteRule() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del<unknown>(`${BASE}/rules/${id}`),
    onSuccess: invalidate,
  })
}

export function useTicketTags() {
  return useQuery({
    queryKey: [...KEY, 'tags'],
    queryFn: () => api.get<string[]>(`${BASE}/tags`),
  })
}

export function useSetTicketTags() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ticketNo, tags }: { ticketNo: string; tags: string[] }) =>
      api.put<Ticket>(`${BASE}/tickets/${ticketNo}/tags`, { tags }),
    onSuccess: invalidate,
  })
}

/** SLA 策略（与后端 /plugins/ticket/sla 一一对应） */
export interface SLATarget {
  firstMinutes: number
  resolutionMinutes: number
}
export interface SLASetting {
  workdays: number[]
  startMinute: number
  endMinute: number
  targets: Record<string, SLATarget>
}

export function useSLA() {
  return useQuery({
    queryKey: [...KEY, 'sla'],
    queryFn: () => api.get<SLASetting>(`${BASE}/sla`),
  })
}

export function useSaveSLA() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: SLASetting) => api.put<SLASetting>(`${BASE}/sla`, input),
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
  resolutionTargetMinutes: number
  resolutionBreached: number
  resolutionBreachRate: number
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
