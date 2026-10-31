import { useCallback, useEffect, useRef, useState } from 'react'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type ConvStatus = 'open' | 'closed'
export type SenderType = 'visitor' | 'agent' | 'system'
export type MessageKind = 'text' | 'system'

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
  visitorReadSeq: number
  closedAt?: string | null
  closedBy?: string
  assigneeId?: string
  assigneeName?: string
  rating: number
  ratingComment?: string
  ratedAt?: string | null
  tags?: string
  visitorIp?: string
  visitorUa?: string
  visitorRef?: string
  /** 内部备注：只给坐席看，不会发给访客 */
  note?: string
}

export interface ImMessage {
  id: string
  seq: number
  senderType: SenderType
  senderId: string
  senderName: string
  content: string
  kind: MessageKind
  /** 系统消息的机器可读载荷，如 {"code":"conv_closed"} */
  meta?: string
  /** 撤回时间；撤回后 content 为空、kind 为 system */
  recalledAt?: string | null
  createdAt: string
}

export interface ConvQuery extends PageQuery {
  status?: string
  bizType?: string
  assignee?: 'me' | 'unassigned' | string
  tag?: string
}

export interface MessagesResult {
  conversation: ImConversation
  messages: ImMessage[]
  hasMore: boolean
}

export interface ImAttachment {
  token: string
  name: string
  mime: string
  size: number
}

export interface ImAgent {
  id: string
  username: string
  nickname: string
  /** 心跳在线（90 秒内收到过心跳） */
  online: boolean
}

export interface ImSettings {
  businessHours: boolean
  hoursStart: string
  hoursEnd: string
  workdays: number[]
  timezone: string
  offlineText: string
  inviteDelay: number
  inviteText: string
  /** 访客首次打开会话时自动发出的第一条消息；空 = 不发欢迎语 */
  welcomeText: string
  /** 已结束会话的保留天数；0 = 永久保留 */
  retentionDays: number
  /** 敏感词：访客消息命中后替换为 *** */
  bannedWords: string[]
  /** 新会话自动分给在线且最闲的坐席；没人在线时排队，坐席上线后按先后领走 */
  autoAssign: boolean
}

/** 会话统计：窗口内的总量、首响、满意度、按天趋势与坐席工作量 */
export interface ImStats {
  days: number
  total: number
  closed: number
  open: number
  avgRating: number
  ratingCount: number
  avgFirstResponseSec: number
  firstResponseCount: number
  truncated: boolean
  daily: { date: string; created: number; closed: number }[]
  agents: { id: string; name: string; online: boolean; assigned: number; closed: number; avgRating: number }[]
}

/** 删除会话的结果：删除数与因「进行中/不存在」被跳过的会话号 */
export interface DeleteConvResult {
  deleted: number
  messages: number
  skipped: string[]
}

export interface ImQuickReply {
  id: string
  title: string
  content: string
  sort: number
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

/**
 * useChatSession 管理单个会话的消息流：
 * - 首屏取最近 50 条（不再被"最早的 200 条"卡住）；
 * - refresh 用 since 增量拉取；
 * - loadOlder 用 before 向上翻页。
 * 与 react-query 解耦，因为聊天窗口需要"追加/前插"而不是整表替换。
 */
export interface ChatSession {
  conv?: ImConversation
  messages: ImMessage[]
  hasMore: boolean
  loading: boolean
  refresh: () => Promise<void>
  loadOlder: () => Promise<void>
}

export function useChatSession(convNo: string | undefined, pollMs = 5000): ChatSession {
  const [conv, setConv] = useState<ImConversation>()
  const [messages, setMessages] = useState<ImMessage[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(false)
  const lastSeq = useRef(0)
  const inFlight = useRef(false)
  const current = useRef<string | undefined>(convNo)

  const apply = useCallback((no: string, r: MessagesResult, mode: 'replace' | 'append' | 'prepend') => {
    if (current.current !== no) return
    setConv(r.conversation)
    setMessages((prev) => {
      const rows = mode === 'prepend' ? [...r.messages, ...prev] : mode === 'append' ? [...prev, ...r.messages] : r.messages
      const seen = new Set<number>()
      const uniq = rows.filter((m) => {
        if (seen.has(m.seq)) return false
        seen.add(m.seq)
        return true
      })
      uniq.sort((a, b) => a.seq - b.seq)
      return uniq
    })
    if (mode !== 'prepend') {
      for (const m of r.messages) if (m.seq > lastSeq.current) lastSeq.current = m.seq
    }
    if (mode !== 'append') setHasMore(r.hasMore)
  }, [])

  useEffect(() => {
    current.current = convNo
    setConv(undefined)
    setMessages([])
    setHasMore(false)
    lastSeq.current = 0
    if (!convNo) return
    let cancelled = false
    setLoading(true)
    api
      .get<MessagesResult>(`${BASE}/conversations/${convNo}/messages`, { limit: 50 })
      .then((r) => {
        if (!cancelled) apply(convNo, r, 'replace')
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [convNo, apply])

  const refresh = useCallback(async () => {
    const no = current.current
    if (!no || inFlight.current) return
    inFlight.current = true
    try {
      const since = lastSeq.current
      const r = await api.get<MessagesResult>(`${BASE}/conversations/${no}/messages`, since > 0 ? { since, limit: 200 } : { limit: 50 })
      apply(no, r, since > 0 ? 'append' : 'replace')
    } catch {
      // 轮询失败静默，下一轮再试
    } finally {
      inFlight.current = false
    }
  }, [apply])

  const loadOlder = useCallback(async () => {
    const no = current.current
    if (!no) return
    const first = messages[0]
    if (!first) return
    try {
      const r = await api.get<MessagesResult>(`${BASE}/conversations/${no}/messages`, { before: first.seq, limit: 50 })
      apply(no, r, 'prepend')
      setHasMore(r.hasMore)
    } catch {
      // 忽略：用户可以再点一次
    }
  }, [messages, apply])

  useEffect(() => {
    if (!convNo || pollMs <= 0) return
    const timer = window.setInterval(() => void refresh(), pollMs)
    return () => window.clearInterval(timer)
  }, [convNo, pollMs, refresh])

  return { conv, messages, hasMore, loading, refresh, loadOlder }
}

const API_BASE = import.meta.env.VITE_API_BASE || '/api/v1'

/**
 * 打开某会话的 SSE 实时流（先认证换一次性流票）。返回关闭函数。
 * 事件既可能是消息（含 seq），也可能是控制帧（type: typing / closed / read / assigned）。
 * 断流时通过 onError 通知调用方回落到轮询；组件重挂/切换会话时调用关闭函数。
 */
export async function openImStream(
  convNo: string,
  onEvent: (evt: unknown) => void,
  onError: () => void,
): Promise<() => void> {
  try {
    const { ticket } = await api.post<{ ticket: string }>(`${BASE}/stream-ticket`)
    const es = new EventSource(`${API_BASE}/plugins/im/stream/${encodeURIComponent(convNo)}?ticket=${encodeURIComponent(ticket)}`)
    let closed = false
    es.onmessage = (e) => {
      if (closed) return
      try {
        onEvent(JSON.parse(e.data))
      } catch {
        onEvent(null)
      }
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
  return useMutation({
    mutationFn: ({ convNo, content, attachmentToken }: { convNo: string; content: string; attachmentToken?: string }) =>
      api.post<ImMessage>(`${BASE}/conversations/${convNo}/messages`, { content, attachmentToken }),
  })
}

/** 附件的展示地址：token 不可枚举，直接给 img/链接用。 */
export function imAttachmentUrl(token: string): string {
  return `${API_BASE}/plugins/im/visitor/attachments/${encodeURIComponent(token)}`
}

/** 从消息 meta 里取附件信息。 */
export function messageAttachment(m: ImMessage): ImAttachment | undefined {
  if (!m.meta) return undefined
  try {
    const data = JSON.parse(m.meta) as { attachment?: ImAttachment }
    return data.attachment
  } catch {
    return undefined
  }
}

/** 上传附件（multipart），返回 token 等元信息；随后发消息时带 attachmentToken。 */
export async function uploadAttachment(convNo: string, file: File): Promise<ImAttachment> {
  const form = new FormData()
  form.append('file', file)
  return api.post<ImAttachment>(`${BASE}/conversations/${convNo}/attachments`, form)
}

export function useAgents(enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'agents'],
    queryFn: () => api.get<ImAgent[]>(`${BASE}/agents`),
    enabled,
    staleTime: 60_000,
  })
}

export function useImSettings(enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'settings'],
    queryFn: () => api.get<ImSettings>(`${BASE}/settings`),
    enabled,
    staleTime: 60_000,
  })
}

export function useSaveImSettings() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (settings: ImSettings) => api.put<ImSettings>(`${BASE}/settings`, settings),
    onSuccess: invalidate,
  })
}

export function useTransferConv() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ convNo, assigneeId, assigneeName }: { convNo: string; assigneeId: string; assigneeName: string }) =>
      api.post<ImConversation>(`${BASE}/conversations/${convNo}/transfer`, { assigneeId, assigneeName }),
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

export function useClaimConv() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ convNo, release }: { convNo: string; release?: boolean }) =>
      api.post<ImConversation>(`${BASE}/conversations/${convNo}/${release ? 'release' : 'claim'}`),
    onSuccess: invalidate,
  })
}

export function useSetTags() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ convNo, tags }: { convNo: string; tags: string }) =>
      api.put<ImConversation>(`${BASE}/conversations/${convNo}/tags`, { tags }),
    onSuccess: invalidate,
  })
}

export function useSetNote() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ convNo, note }: { convNo: string; note: string }) =>
      api.put<ImConversation>(`${BASE}/conversations/${convNo}/note`, { note }),
    onSuccess: invalidate,
  })
}

/** 超管删除单个会话（仅已结束的）；后端会级联删除消息、附件与存储文件。 */
export function useDeleteConv() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (convNo: string) => api.del<DeleteConvResult>(`${BASE}/conversations/${convNo}`),
    onSuccess: invalidate,
  })
}

/** 按当前筛选条件导出会话记录（xlsx，最多 5000 条）。 */
export async function exportConversations(query: ConvQuery): Promise<void> {
  const res = await http.get<unknown>(`${BASE}/conversations-export`, { params: query, responseType: 'blob' })
  const blob =
    res.data instanceof Blob
      ? res.data
      : new Blob([String(res.data)], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  const stamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '')
  a.href = url
  a.download = `im-conversations-${stamp}.xlsx`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

/** 输入中提示：不等待结果、失败静默。 */
export function sendTyping(convNo: string) {
  void api.post(`${BASE}/conversations/${convNo}/typing`, undefined, { silent: true }).catch(() => undefined)
}

export function useQuickReplies(enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'quick-replies'],
    queryFn: () => api.get<ImQuickReply[]>(`${BASE}/quick-replies`),
    enabled,
    staleTime: 60_000,
  })
}

export function useSaveQuickReply() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (q: { id?: string; title: string; content: string; sort?: number }) =>
      q.id
        ? api.put<ImQuickReply>(`${BASE}/quick-replies/${q.id}`, { title: q.title, content: q.content, sort: q.sort ?? 0 })
        : api.post<ImQuickReply>(`${BASE}/quick-replies`, { title: q.title, content: q.content, sort: q.sort ?? 0 }),
    onSuccess: invalidate,
  })
}

export function useDeleteQuickReply() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/quick-replies/${id}`),
    onSuccess: invalidate,
  })
}

/** 坐席心跳：记录在线（90 秒 TTL），开启自动分配时后端会顺带领走一个排队会话。 */
export function heartbeatPresence(): Promise<{ ok: boolean; assigned: string }> {
  return api.post<{ ok: boolean; assigned: string }>(`${BASE}/presence/heartbeat`, undefined, { silent: true })
}

export function useImStats(days: number, enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'stats', days],
    queryFn: () => api.get<ImStats>(`${BASE}/stats`, { days }),
    enabled,
    staleTime: 30_000,
  })
}

/** 撤回本人 2 分钟内发出的消息。 */
export function useRecallMessage() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ convNo, seq }: { convNo: string; seq: number }) =>
      api.post<ImMessage>(`${BASE}/conversations/${convNo}/messages/${seq}/recall`),
    onSuccess: invalidate,
  })
}

/** 把当前会话合并进目标会话（目标必须是进行中）。 */
export function useMergeConv() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ convNo, target }: { convNo: string; target: string }) =>
      api.post<{ target: string; moved: number }>(`${BASE}/conversations/${convNo}/merge`, { target }),
    onSuccess: invalidate,
  })
}
