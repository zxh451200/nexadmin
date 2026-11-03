import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { ApiError, api, http, tokenStore } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'
import { useWs } from '@/framework/hooks/useWs'

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
  /** 报告人邮箱（通知客户用）；为空时后端对邮箱形态的 reporterId 兜底。 */
  reporterEmail?: string
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
  /** 自定义字段的值（key = 字段 code）；后端未定义/已停用的 code 不会出现。 */
  customValues?: Record<string, unknown>
  /**
   * 本单被合并进了哪张主单（列表行上的「已合并到 TK…」标记用）。
   * 真后端的工单列表投影（model.go 的 Ticket）目前不带这一列，所以是可选字段：
   * 没有就什么都不显示，关联面板仍会从 /tickets/:no/links 读到同一事实。
   * mock 里会填上，用来验证列表行的标记与跳转。
   */
  mergedIntoNo?: string
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
  /** 排序列键，取值由后端白名单决定；未知键后端退回默认列 */
  sort?: string
  /** asc / desc，缺省 desc */
  order?: string
}

export interface RepliesResult {
  ticket: Ticket
  replies: TicketReply[]
  attachments?: TicketAttachment[]
  /** 工单动态（审计轨迹）。坐席看到全部，客户自助页只有提交与状态变化。 */
  activities?: TicketActivity[]
  /** 附件配额与可收类型由后端下发（个数、总字节、文件选择器 accept），前端只呈现不复制常量 */
  quota?: { maxCount: number; maxBytes: number; accept: string }
}

export type TicketActivityAction =
  | 'created'
  | 'status_changed'
  | 'priority_changed'
  | 'assignee_changed'
  | 'group_changed'
  | 'tags_changed'
  | 'custom_fields_changed'
  | 'attachment_added'
  | 'attachment_removed'
  | 'rating_added'
  | 'rule_applied'
  | 'merged_into'
  | 'merged_tickets'
  | 'linked'
  | 'unlinked'

export interface TicketActivity {
  id: string
  ticketNo: string
  actorType: string
  actorName: string
  action: TicketActivityAction | string
  fromValue: string
  toValue: string
  detail: string
  createdAt: string
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
  /** 报告人邮箱（可选）：坐席回复 / 解决时按设置给客户发通知邮件。 */
  reporterEmail?: string
  bizType?: string
  bizNo?: string
  /** 建单时一并提交的自定义字段值。 */
  customValues?: Record<string, unknown>
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
  /** 该队列绑定的 SLA 策略编码；空串 = 默认策略。 */
  slaCode?: string
  members?: GroupMember[]
}

export interface GroupInput {
  name: string
  description: string
  autoAssign: boolean
  enabled: boolean
  sort: number
  slaCode?: string
}

export interface TicketRule {
  id: string
  name: string
  enabled: boolean
  sort: number
  matchPriority: string
  matchBizType: string
  matchKeyword: string
  /** 命中条件里的标签：多个标签是「与」语义，要全部命中 */
  matchTags: string[]
  setPriority: string
  setGroupId: string
  note: string
  /** 动作里追加的标签：最多 10 个、每个不超过 20 字（与工单标签同一规范） */
  addTags: string[]
}

export interface RuleInput {
  name: string
  enabled: boolean
  sort: number
  matchPriority: string
  matchBizType: string
  matchKeyword: string
  matchTags: string[]
  setPriority: string
  setGroupId: number
  note: string
  addTags: string[]
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

/** 删除附件（坐席端）。失败原因（配额、删除失败）由后端按请求语言返回文案，前端直接展示。 */
export function useDeleteAttachment() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ticketNo, token }: { ticketNo: string; token: string }) =>
      api.del<{ ok: boolean }>(`${BASE}/tickets/${ticketNo}/attachments/${token}`),
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

/** 命中后端「超过导出行数上限」错误（reason=TICKET_EXPORT_TOO_MANY）。 */
export function isExportTooMany(e: unknown): boolean {
  return e instanceof ApiError && e.reason === 'TICKET_EXPORT_TOO_MANY'
}

/**
 * 按当前筛选导出工单：服务端写 xlsx（最多 5000 行），需要 ticket:list 权限。
 * mock 阶段没有二进制能力，退回带 BOM 的 CSV 文本，文件名跟着换扩展名。
 */
export async function exportTickets(query: Omit<TicketQueryParams, 'page' | 'pageSize'>): Promise<void> {
  const res = await http.get<unknown>(`${BASE}/tickets-export`, {
    params: { ...query },
    responseType: 'blob',
    silent: true,
  })
  const data = res.data
  const isBlob = data instanceof Blob
  const blob = isBlob ? data : new Blob(['\uFEFF', String(data)], { type: 'text/csv;charset=utf-8' })
  const stamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '')
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `tickets-${stamp}.${isBlob ? 'xlsx' : 'csv'}`
  document.body.appendChild(a)
  a.click()
  a.remove()
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

/**
 * 合并与关联单（后端 plugins/ticket/merge.go）。
 * 失败原因（不能合并自己、已被合并、目标已结束、会成环、关联重复/超上限/不存在）
 * 后端已按请求语言返回整句，前端只显示 e.message，不再自己拼一套措辞。
 */
export type LinkedTicketKind = 'link' | 'merged_into' | 'merged_from'

export interface LinkedTicket {
  ticketNo: string
  title: string
  status: string
  priority: string
  kind: LinkedTicketKind
  linkedAt: string
}

export interface MergeSummary {
  /** 本单被并进了哪张；没被合并过就为空 */
  mergedInto?: LinkedTicket
  mergedFrom: LinkedTicket[]
  mergedCount: number
}

export interface LinksResult {
  links: LinkedTicket[]
  merge: MergeSummary
}

export interface MergeResult {
  source: string
  /** 链解析后实际生效的主单，可能与提交的目标不同 */
  target: string
  mergedCount: number
}

/** 一次读全「双向关联 + 合并痕迹」：关联面板与「已合并到 TK…」提示同源。 */
export function useTicketLinks(ticketNo?: string) {
  return useQuery({
    queryKey: [...KEY, 'links', ticketNo],
    queryFn: () => api.get<LinksResult>(`${BASE}/tickets/${ticketNo}/links`),
    enabled: !!ticketNo,
  })
}

export function useMergeTicket() {
  const invalidate = useInvalidate()
  return useMutation({
    // silent：错误整句由调用方展示，避免拦截器与页面各弹一次
    mutationFn: ({ ticketNo, intoNo }: { ticketNo: string; intoNo: string }) =>
      api.post<MergeResult>(`${BASE}/tickets/${ticketNo}/merge`, { intoNo }, { silent: true }),
    onSuccess: invalidate,
  })
}

export function useAddLink() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ticketNo, otherNo }: { ticketNo: string; otherNo: string }) =>
      api.post<unknown>(`${BASE}/tickets/${ticketNo}/links`, { ticketNo: otherNo }, { silent: true }),
    onSuccess: invalidate,
  })
}

export function useRemoveLink() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ticketNo, otherNo }: { ticketNo: string; otherNo: string }) =>
      api.del<{ ok: boolean }>(`${BASE}/tickets/${ticketNo}/links/${otherNo}`, undefined, { silent: true }),
    onSuccess: invalidate,
  })
}

const MERGE_TARGET_STATUSES = ['open', 'processing', 'pending'] as const
const candidateQuery = (keyword: string, status?: string) => ({
  page: 1,
  pageSize: 10,
  keyword: keyword || undefined,
  status,
})

/**
 * 合并候选：只有未完结（open/processing/pending）的单能当主单，后端是硬拒绝的。
 * status 支持逗号分隔多值，所以一次请求就是那个「未完结」池子——从前这里发三次，
 * 每次一页，拼完还去重，等于用三个请求换一个筛选条件。
 */
export function useMergeCandidates(keyword: string, enabled: boolean) {
  const query = useQuery({
    queryKey: [...KEY, 'tickets', candidateQuery(keyword, MERGE_TARGET_STATUSES.join(','))],
    queryFn: () => api.get<Page<Ticket>>(`${BASE}/tickets`, candidateQuery(keyword, MERGE_TARGET_STATUSES.join(','))),
    enabled,
    placeholderData: keepPreviousData,
  })
  return { list: query.data?.list ?? [], isLoading: query.isLoading }
}

/** 关联候选：关联允许指向终态单（恰恰是「这几张是同一件事」最需要留痕的场景），所以不加状态筛选。 */
export function useLinkCandidates(keyword: string, enabled: boolean) {
  return useQuery({
    queryKey: [...KEY, 'tickets', candidateQuery(keyword)],
    queryFn: () => api.get<Page<Ticket>>(`${BASE}/tickets`, candidateQuery(keyword)),
    enabled,
    placeholderData: keepPreviousData,
  })
}

/** SLA 策略（与后端 /plugins/ticket/sla 一一对应） */
export interface SLATarget {
  firstMinutes: number
  resolutionMinutes: number
}
export interface SLASetting {
  /** 策略编码；空串是默认策略（GET/PUT /sla 那一份）。 */
  code?: string
  label?: string
  workdays: number[]
  startMinute: number
  endMinute: number
  targets: Record<string, SLATarget>
  /** 跳过的节假日（YYYY-MM-DD，后端去重升序）。 */
  holidays?: string[]
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

/** 全部命名策略（默认策略 code 为空串，也在列表里）。 */
export function useSLAPolicies() {
  return useQuery({
    queryKey: [...KEY, 'sla-policies'],
    queryFn: () => api.get<SLASetting[]>(`${BASE}/sla/policies`),
  })
}

export function useSaveSLAPolicy() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: SLASetting) => api.post<SLASetting>(`${BASE}/sla/policy`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteSLAPolicy() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (code: string) => api.del<{ code: string }>(`${BASE}/sla/policy/${encodeURIComponent(code)}`, undefined, { silent: true }),
    onSuccess: invalidate,
  })
}

/** 自定义字段：定义（管理员配置）与值（挂在工单上）。 */
export type TicketFieldType = 'text' | 'number' | 'select' | 'date' | 'bool'

export interface TicketField {
  id?: string
  code: string
  label: string
  type: TicketFieldType
  options: string[]
  required: boolean
  enabled: boolean
  sort: number
  groupId?: string
}

export function useTicketFields() {
  return useQuery({
    queryKey: [...KEY, 'fields'],
    queryFn: () => api.get<TicketField[]>(`${BASE}/fields`),
  })
}

export function useSaveField() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: TicketField) =>
      input.id
        ? api.put<TicketField>(`${BASE}/fields/${input.id}`, input)
        : api.post<TicketField>(`${BASE}/fields`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteField() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del<{ id: string }>(`${BASE}/fields/${id}`, undefined, { silent: true }),
    onSuccess: invalidate,
  })
}

export function useSetCustomValues() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ticketNo, values }: { ticketNo: string; values: Record<string, unknown> }) =>
      api.post<Ticket>(`${BASE}/tickets/${ticketNo}/custom-values`, { values }),
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

// ---- 实时推送（WS）与侧栏面板（后端 ws.go / badge.go / notify.go）----

/** WS 帧：后端 wsFrame。字段刻意最小，详情数据由前端命中帧后拉增量。 */
export interface TicketWsFrame {
  type: 'created' | 'replied' | 'status' | 'assigned' | 'rated'
  ticketNo: string
  status?: string
  preview?: string
}

/** 列表级主题：任何工单变化都会发一帧，队列页订阅它刷新列表。 */
export const TICKET_WS_FEED = 'ticket:feed'

export const ticketWsTopic = (ticketNo: string) => `ticket:${ticketNo}`

/** 订阅 WS 主题的通用壳：handler 走 ref 免得订阅随闭包重建。 */
function useTicketTopic(topic: string | undefined, onFrame: (f: TicketWsFrame) => void) {
  const { subscribe } = useWs()
  const handler = useRef(onFrame)
  handler.current = onFrame
  useEffect(() => {
    if (!topic) return
    return subscribe(topic, (data) => {
      const f = data as TicketWsFrame
      if (f?.type && f.ticketNo) handler.current(f)
    })
  }, [topic, subscribe])
}

/** 选中工单的实时帧：详情会话页订阅，收到即刷回复与动态（轮询保留作兜底）。 */
export function useTicketStream(ticketNo: string | undefined, onFrame: (f: TicketWsFrame) => void) {
  useTicketTopic(ticketNo ? ticketWsTopic(ticketNo) : undefined, onFrame)
}

/** 全列表实时帧：收到即刷列表查询（8s 轮询保留作兜底）。 */
export function useTicketFeed(onFrame: (f: TicketWsFrame) => void) {
  useTicketTopic(TICKET_WS_FEED, onFrame)
}

/** 侧栏菜单徽标：待我注意的未完结数（未认领 open + 认领给我的未完结）。 */
export function useBadgeCount() {
  return useQuery({
    queryKey: [...KEY, 'badge'],
    queryFn: () => api.get<{ count: number }>(`${BASE}/tickets/badge`, undefined, { silent: true }),
    refetchInterval: 60_000,
  })
}

/** 插件设置：客户邮件通知开关 + 客服门户地址（邮件里的「查看工单」链接）。 */
export interface TicketSettings {
  emailNotifyEnabled: boolean
  portalUrl: string
}

export function useTicketSettings() {
  return useQuery({
    queryKey: [...KEY, 'settings'],
    queryFn: () => api.get<TicketSettings>(`${BASE}/settings`),
  })
}

export function useSaveTicketSettings() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: TicketSettings) => api.put<TicketSettings>(`${BASE}/settings`, input, { silent: true }),
    onSuccess: invalidate,
  })
}

/** 报告人历史工单：详情侧栏「这人之前提过什么」，防重复建单。 */
export interface ReporterHistoryItem {
  ticketNo: string
  title: string
  status: TicketStatus
  priority: TicketPriority
  reporterName: string
  createdAt: string
}

export function useReporterHistory(ticketNo?: string) {
  return useQuery({
    queryKey: [...KEY, 'reporter-history', ticketNo],
    queryFn: () => api.get<ReporterHistoryItem[]>(`${BASE}/tickets/${ticketNo}/reporter-tickets`),
    enabled: !!ticketNo,
  })
}
