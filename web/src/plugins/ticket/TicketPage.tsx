import { useEffect, useRef, useState } from 'react'
import { CircleCheck, Send, UserRoundCheck, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { cn } from '@/lib/utils'
import {
  useAssignTicket,
  useReplies,
  useReplyTicket,
  useSetTicketStatus,
  useTickets,
  type Ticket,
  type TicketReply,
} from './api'
import { formatTime, priorityVariant, statusVariant } from './format'
import { TicketStatsPanel } from './TicketStatsPanel'

const ALL = '__all'

export default function TicketPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [status, setStatus] = useState<string | undefined>()
  const [priority, setPriority] = useState<string | undefined>()
  const [keyword, setKeyword] = useState('')
  const [overdueOnly, setOverdueOnly] = useState(false)
  const [selected, setSelected] = useState<string | undefined>()
  // 视图写进 URL：?view=stats 可以直接分享给同事
  const [params, setParams] = useSearchParams()
  const view = params.get('view') === 'stats' ? 'stats' : 'logs'
  const setView = (v: string) => setParams(v === 'stats' ? { view: v } : {}, { replace: true })

  const tickets = useTickets({
    status, priority, keyword: keyword || undefined, overdue: overdueOnly ? '1' : undefined,
    page: 1, pageSize: 50,
  })
  const replies = useReplies(selected)
  const reply = useReplyTicket()
  const setStatusMut = useSetTicketStatus()
  const assign = useAssignTicket()

  const onReply = async (content: string) => {
    if (!selected) return
    try {
      await reply.mutateAsync({ ticketNo: selected, content })
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onChangeStatus = async (ticketNo: string, to: string, confirmMsg?: string) => {
    if (confirmMsg && !(await confirm({ description: confirmMsg }))) return
    try {
      await setStatusMut.mutateAsync({ ticketNo, status: to })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onAssign = async (ticket: Ticket) => {
    try {
      // assigneeId=0 表示认领给自己，后端从登录态解析。
      await assign.mutateAsync({ ticketNo: ticket.ticketNo, assigneeId: 0, assigneeName: '' })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <PageContainer>
      <PageHeader title={t('plugin_ticket.title')} description={t('plugin_ticket.description')} />
      {/* 列表与统计分屏：处理工单的人要先看到队列，做复盘的人也不该被双栏挤走 */}
      <Tabs value={view} onValueChange={setView}>
        <TabsList className="w-fit">
          <TabsTrigger value="logs">{t('plugin_ticket.view.logs')}</TabsTrigger>
          <TabsTrigger value="stats">{t('plugin_ticket.view.stats')}</TabsTrigger>
        </TabsList>
        <TabsContent value="logs">
          {/* 队列与会话是一体两面：选中哪张单就看哪段对话，整体算「列表」视图，不拆散 */}
          <div className="grid h-[calc(100vh-14rem)] min-h-96 grid-cols-1 gap-4 md:grid-cols-[24rem_1fr]">
        <Card className="flex min-h-0 flex-col gap-2 p-2">
          <div className="flex flex-wrap items-center gap-2">
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder={t('plugin_ticket.searchPlaceholder')}
              aria-label={t('common.keyword')}
              className="min-w-32 flex-1"
            />
            <SimpleSelect
              aria-label={t('plugin_ticket.fields.status')}
              className="w-20"
              value={status ?? ALL}
              onChange={(v) => setStatus(v === ALL ? undefined : v)}
              options={[
                { value: ALL, label: t('plugin_ticket.all') },
                ...(['open', 'processing', 'resolved', 'closed'] as const).map((s) => ({ value: s, label: t(`plugin_ticket.status.${s}`) })),
              ]}
            />
            <SimpleSelect
              aria-label={t('plugin_ticket.fields.priority')}
              className="w-20"
              value={priority ?? ALL}
              onChange={(v) => setPriority(v === ALL ? undefined : v)}
              options={[
                { value: ALL, label: t('plugin_ticket.all') },
                ...(['urgent', 'high', 'normal', 'low'] as const).map((p) => ({ value: p, label: t(`plugin_ticket.priority.${p}`) })),
              ]}
            />
            {/* 和两个下拉同高同边框：窄面板里它会被挤到第二行，看起来仍是一个筛选控件，而不是孤零零一个勾选框 */}
            <label className="flex h-10 items-center gap-2 whitespace-nowrap rounded-md border border-input bg-background px-3 text-base shadow-sm md:h-9 md:text-sm">
              <Checkbox
                checked={overdueOnly}
                onCheckedChange={(v) => setOverdueOnly(v === true)}
                aria-label={t('plugin_ticket.overdue')}
              />
              {t('plugin_ticket.overdue')}
            </label>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            {(tickets.data?.list ?? []).map((tk) => (
              <TicketListItem
                key={tk.id}
                ticket={tk}
                active={tk.ticketNo === selected}
                onSelect={() => setSelected(tk.ticketNo)}
              />
            ))}
            {!tickets.isLoading && (tickets.data?.list ?? []).length === 0 ? (
              <EmptyState title={t('plugin_ticket.empty')} className="py-8" />
            ) : null}
          </div>
        </Card>

        <TicketThread
          ticket={replies.data?.ticket}
          replies={replies.data?.replies ?? []}
          loading={replies.isLoading}
          sending={reply.isPending}
          onSend={onReply}
          onSetStatus={onChangeStatus}
          onAssign={(tk) => void onAssign(tk)}
        />
          </div>
        </TabsContent>
        <TabsContent value="stats">
          <TicketStatsPanel />
        </TabsContent>
      </Tabs>
    </PageContainer>
  )
}

function TicketListItem({
  ticket,
  active,
  onSelect,
}: {
  ticket: Ticket
  active: boolean
  onSelect: () => void
}) {
  const { t, i18n } = useTranslation()
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        'flex w-full flex-col gap-1 rounded-md px-3 py-2 text-start text-sm transition-colors hover:bg-accent',
        active && 'bg-accent',
      )}
    >
      <div className="flex w-full items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-medium text-foreground">{ticket.title}</span>
        {ticket.overdue ? <Badge variant="destructive">{t('plugin_ticket.overdue')}</Badge> : null}
        <Badge variant={statusVariant(ticket.status)}>{t(`plugin_ticket.status.${ticket.status}`)}</Badge>
      </div>
      <div className="flex w-full items-center gap-2 text-xs text-muted-foreground">
        <Badge variant={priorityVariant(ticket.priority)}>{t(`plugin_ticket.priority.${ticket.priority}`)}</Badge>
        <span className="min-w-0 flex-1 truncate">
          {ticket.ticketNo}
          {ticket.bizNo ? ` · ${ticket.bizNo}` : ''}
        </span>
        <span>{formatTime(ticket.lastReplyAt ?? ticket.createdAt, i18n.language)}</span>
      </div>
    </button>
  )
}

function TicketThread({
  ticket,
  replies,
  loading,
  sending,
  onSend,
  onSetStatus,
  onAssign,
}: {
  ticket?: Ticket
  replies: TicketReply[]
  loading: boolean
  sending: boolean
  onSend: (content: string) => void
  onSetStatus: (ticketNo: string, to: string, confirmMsg?: string) => Promise<void>
  onAssign: (ticket: Ticket) => void
}) {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState('')
  const bottomRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [replies.length, ticket?.ticketNo])

  if (!ticket) {
    return (
      <Card className="flex items-center justify-center text-sm text-muted-foreground">
        {t('plugin_ticket.pickTicket')}
      </Card>
    )
  }

  const submit = () => {
    const content = draft.trim()
    if (!content) return
    onSend(content)
    setDraft('')
  }

  return (
    <Card className="flex min-h-0 flex-col">
      <div className="flex items-center gap-2 border-b px-4 py-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium text-foreground">
            {ticket.title}
            <span className="text-muted-foreground"> · {ticket.ticketNo}</span>
          </div>
          <div className="truncate text-xs text-muted-foreground">
            {ticket.reporterName || t(`plugin_ticket.repliers.${ticket.reporterType}`)}
            {ticket.bizNo ? ` · ${ticket.bizType}/${ticket.bizNo}` : ''}
            {ticket.assigneeName ? ` · ${t('plugin_ticket.assignee')}: ${ticket.assigneeName}` : ''}
          </div>
        </div>
        <Can perm="ticket:edit">
          {ticket.assigneeId === '0' || ticket.assigneeId === '' ? (
            <Button variant="outline" size="sm" onClick={() => onAssign(ticket)}>
              <UserRoundCheck aria-hidden="true" />
              {t('plugin_ticket.assign')}
            </Button>
          ) : null}
          {ticket.status === 'open' ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => void onSetStatus(ticket.ticketNo, 'processing')}
            >
              {t('plugin_ticket.claim')}
            </Button>
          ) : null}
          {ticket.status === 'open' || ticket.status === 'processing' ? (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void onSetStatus(ticket.ticketNo, 'resolved')}
              >
                <CircleCheck aria-hidden="true" />
                {t('plugin_ticket.resolve')}
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="text-destructive"
                onClick={() =>
                  void onSetStatus(
                    ticket.ticketNo,
                    'closed',
                    t('plugin_ticket.closeConfirm', { no: ticket.ticketNo }),
                  )
                }
              >
                <XCircle aria-hidden="true" />
                {t('plugin_ticket.close')}
              </Button>
            </>
          ) : null}
        </Can>
        <Badge variant={statusVariant(ticket.status)}>{t(`plugin_ticket.status.${ticket.status}`)}</Badge>
      </div>

      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-4">
        {loading && replies.length === 0 ? (
          <div className="text-center text-sm text-muted-foreground">{t('plugin_ticket.loading')}</div>
        ) : null}
        {replies.map((r) => (
          <div key={r.id} className={cn('flex', r.replierType === 'agent' ? 'justify-end' : 'justify-start')}>
            <div
              className={cn(
                'max-w-[75%] rounded-lg px-3 py-2 text-sm',
                r.replierType === 'agent'
                  ? 'bg-primary text-primary-foreground'
                  : r.replierType === 'system'
                    ? 'bg-muted text-muted-foreground'
                    : 'bg-muted text-foreground',
              )}
            >
              <div className="mb-0.5 text-[11px] opacity-70">
                {r.replierName || t(`plugin_ticket.repliers.${r.replierType}`)} ·{' '}
                {formatTime(r.createdAt, i18n.language)}
              </div>
              <div className="whitespace-pre-wrap break-words">{r.content}</div>
            </div>
          </div>
        ))}
        <div ref={bottomRef} />
      </div>

      {ticket.status !== 'closed' ? (
        <Can perm="ticket:reply">
          <form
            className="flex items-center gap-2 border-t p-3"
            onSubmit={(e) => {
              e.preventDefault()
              submit()
            }}
          >
            <Input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder={t('plugin_ticket.replyPlaceholder')}
              aria-label={t('plugin_ticket.replyPlaceholder')}
            />
            <Button type="submit" disabled={sending || !draft.trim()}>
              <Send aria-hidden="true" />
              {t('plugin_ticket.send')}
            </Button>
          </form>
        </Can>
      ) : (
        <div className="border-t p-3 text-center text-xs text-muted-foreground">
          {t('plugin_ticket.closedNotice')}
        </div>
      )}
    </Card>
  )
}
