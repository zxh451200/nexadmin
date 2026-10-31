import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  ChevronLeft,
  ChevronRight,
  CircleCheck,
  Plus,
  RotateCcw,
  Send,
  Settings2,
  StickyNote,
  Trash2,
  UserRoundCheck,
  XCircle,
  Zap,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Checkbox } from '@/framework/ui/checkbox'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/framework/ui/dropdown-menu'
import { Input } from '@/framework/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { Textarea } from '@/framework/ui/textarea'
import { cn } from '@/lib/utils'
import {
  useAssignTicket,
  useBulkTickets,
  useCannedReplies,
  useCreateCanned,
  useCreateTicket,
  useDeleteCanned,
  useReplies,
  useReplyTicket,
  useSetTicketStatus,
  useTickets,
  useUpdateCanned,
  type CannedReply,
  type Ticket,
  type TicketPriority,
  type TicketReply,
} from './api'
import { formatTime, priorityVariant, statusVariant } from './format'
import { TicketStatsPanel } from './TicketStatsPanel'

const ALL = '__all'
const PAGE_SIZE = 20
const STATUSES = ['open', 'processing', 'pending', 'resolved', 'closed'] as const
const PRIORITIES = ['urgent', 'high', 'normal', 'low'] as const

type AssigneeFilter = 'all' | 'unassigned' | 'mine'

const emptyCreateForm = {
  title: '',
  content: '',
  priority: 'normal' as TicketPriority,
  reporterName: '',
  reporterId: '',
  bizType: '',
  bizNo: '',
}

export default function TicketPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [params, setParams] = useSearchParams()
  const view = params.get('view') === 'stats' ? 'stats' : 'logs'

  const [statusFilter, setStatusFilter] = useState<string | undefined>()
  const [priorityFilter, setPriorityFilter] = useState<string | undefined>()
  const [assigneeFilter, setAssigneeFilter] = useState<AssigneeFilter>('all')
  const [keyword, setKeyword] = useState('')
  const [appliedKeyword, setAppliedKeyword] = useState('')
  const [overdueOnly, setOverdueOnly] = useState(false)
  const [page, setPage] = useState(1)
  const [selectedNo, setSelectedNo] = useState<string | undefined>()
  const [checked, setChecked] = useState<string[]>([])

  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState(emptyCreateForm)
  const [cannedOpen, setCannedOpen] = useState(false)
  const [cannedForm, setCannedForm] = useState<{ id?: string; title: string; content: string }>({ title: '', content: '' })
  const [assignTarget, setAssignTarget] = useState<Ticket | null>(null)
  const [assignUserId, setAssignUserId] = useState('')

  // 搜索防抖：300ms 内连续输入只发一次请求
  useEffect(() => {
    const id = window.setTimeout(() => setAppliedKeyword(keyword.trim()), 300)
    return () => window.clearTimeout(id)
  }, [keyword])

  // 任一筛选变化都回到第一页
  useEffect(() => {
    setPage(1)
  }, [appliedKeyword, statusFilter, priorityFilter, assigneeFilter, overdueOnly])

  const tickets = useTickets({
    status: statusFilter,
    priority: priorityFilter,
    keyword: appliedKeyword || undefined,
    overdue: overdueOnly ? '1' : undefined,
    unassigned: assigneeFilter === 'unassigned' ? '1' : undefined,
    mine: assigneeFilter === 'mine' ? '1' : undefined,
    page,
    pageSize: PAGE_SIZE,
  })
  const list = tickets.data?.list ?? []
  const total = tickets.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  // 非首页删空后回退，避免停在空页
  useEffect(() => {
    if (page > pages) setPage(pages)
  }, [page, pages])

  // 站内信 / 分享链接直达某张工单
  const ticketParam = params.get('ticket') ?? ''
  useEffect(() => {
    if (ticketParam && ticketParam !== selectedNo) setSelectedNo(ticketParam)
  }, [ticketParam, selectedNo])

  const replies = useReplies(selectedNo)
  const reply = useReplyTicket()
  const setStatusMut = useSetTicketStatus()
  const assign = useAssignTicket()
  const createTicket = useCreateTicket()
  const bulk = useBulkTickets()
  const canned = useCannedReplies()
  const createCanned = useCreateCanned()
  const updateCanned = useUpdateCanned()
  const deleteCanned = useDeleteCanned()

  const setView = (v: string) => {
    const next = new URLSearchParams(params)
    if (v === 'stats') next.set('view', 'stats')
    else next.delete('view')
    setParams(next, { replace: true })
  }

  const selectTicket = (no: string) => {
    setSelectedNo(no)
    const next = new URLSearchParams(params)
    next.set('ticket', no)
    setParams(next, { replace: true })
  }

  const toggleChecked = (no: string, on: boolean) => {
    setChecked((prev) => (on ? (prev.includes(no) ? prev : [...prev, no]) : prev.filter((x) => x !== no)))
  }

  const onSend = async (content: string, internal: boolean) => {
    if (!selectedNo) return
    try {
      await reply.mutateAsync({ ticketNo: selectedNo, content, internal })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
      throw new Error('reply failed')
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

  const claim = async (ticketNo: string) => {
    try {
      await assign.mutateAsync({ ticketNo, assigneeId: 0, assigneeName: '' })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const openAssign = (ticket: Ticket) => {
    setAssignTarget(ticket)
    setAssignUserId('')
  }

  const submitAssign = async () => {
    if (!assignTarget || !assignUserId) return
    try {
      await assign.mutateAsync({ ticketNo: assignTarget.ticketNo, assigneeId: Number(assignUserId), assigneeName: '' })
      toast.success(t('common.operateSuccess'))
      setAssignTarget(null)
      setAssignUserId('')
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const runBulk = async (input: { action: 'assign' | 'status' | 'priority'; status?: string; priority?: string }) => {
    if (!checked.length) return
    try {
      const res = await bulk.mutateAsync({ ticketNos: checked, ...input })
      toast.success(t('plugin_ticket.bulk.done', { updated: res.updated, skipped: res.skipped }))
      setChecked([])
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const bulkClose = async () => {
    if (!(await confirm({ description: t('plugin_ticket.bulk.closeConfirm', { count: checked.length }) }))) return
    await runBulk({ action: 'status', status: 'closed' })
  }

  const openCreate = () => {
    setCreateForm(emptyCreateForm)
    setCreateOpen(true)
  }

  const submitCreate = async () => {
    if (!createForm.title.trim() || !createForm.content.trim()) {
      toast.error(t('plugin_ticket.create.required'))
      return
    }
    try {
      const created = await createTicket.mutateAsync({
        title: createForm.title.trim(),
        content: createForm.content.trim(),
        priority: createForm.priority,
        reporterName: createForm.reporterName.trim() || undefined,
        reporterId: createForm.reporterId.trim() || undefined,
        bizType: createForm.bizType.trim() || undefined,
        bizNo: createForm.bizNo.trim() || undefined,
      })
      toast.success(t('common.operateSuccess'))
      setCreateOpen(false)
      selectTicket(created.ticketNo)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const editCanned = (row: CannedReply) => {
    setCannedForm({ id: row.id, title: row.title, content: row.content })
  }

  const saveCanned = async () => {
    if (!cannedForm.title.trim() || !cannedForm.content.trim()) return
    try {
      if (cannedForm.id) {
        await updateCanned.mutateAsync({ id: cannedForm.id, title: cannedForm.title.trim(), content: cannedForm.content.trim() })
      } else {
        await createCanned.mutateAsync({ title: cannedForm.title.trim(), content: cannedForm.content.trim() })
      }
      setCannedForm({ title: '', content: '' })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const removeCanned = async (row: CannedReply) => {
    if (!(await confirm({ description: t('plugin_ticket.canned.deleteConfirm', { title: row.title }) }))) return
    try {
      await deleteCanned.mutateAsync(row.id)
      if (cannedForm.id === row.id) setCannedForm({ title: '', content: '' })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const filtered = Boolean(statusFilter || priorityFilter || appliedKeyword || overdueOnly || assigneeFilter !== 'all')

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_ticket.title')}
        description={t('plugin_ticket.description')}
        actions={
          <Can perm="ticket:create">
            <Button onClick={openCreate}>
              <Plus aria-hidden="true" />
              {t('plugin_ticket.create.title')}
            </Button>
          </Can>
        }
      />
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
                  aria-label={t('plugin_ticket.assigneeFilter')}
                  className="w-28"
                  value={assigneeFilter}
                  onChange={(v) => setAssigneeFilter(v as AssigneeFilter)}
                  options={[
                    { value: 'all', label: t('plugin_ticket.assigneeAll') },
                    { value: 'unassigned', label: t('plugin_ticket.assigneeUnassigned') },
                    { value: 'mine', label: t('plugin_ticket.assigneeMine') },
                  ]}
                />
                <SimpleSelect
                  aria-label={t('plugin_ticket.fields.status')}
                  className="w-24"
                  value={statusFilter ?? ALL}
                  onChange={(v) => setStatusFilter(v === ALL ? undefined : v)}
                  options={[
                    { value: ALL, label: t('plugin_ticket.all') },
                    ...STATUSES.map((s) => ({ value: s, label: t(`plugin_ticket.status.${s}`) })),
                  ]}
                />
                <SimpleSelect
                  aria-label={t('plugin_ticket.fields.priority')}
                  className="w-20"
                  value={priorityFilter ?? ALL}
                  onChange={(v) => setPriorityFilter(v === ALL ? undefined : v)}
                  options={[
                    { value: ALL, label: t('plugin_ticket.all') },
                    ...PRIORITIES.map((p) => ({ value: p, label: t(`plugin_ticket.priority.${p}`) })),
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

              {checked.length > 0 ? (
                <div className="flex flex-wrap items-center gap-2 rounded-md bg-muted px-2 py-1.5">
                  <span className="text-xs text-muted-foreground">{t('common.selected', { count: checked.length })}</span>
                  <Can perm="ticket:edit">
                    <Button size="sm" variant="outline" onClick={() => void runBulk({ action: 'assign' })}>
                      {t('plugin_ticket.actions.assignToMe')}
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => void runBulk({ action: 'status', status: 'resolved' })}>
                      {t('plugin_ticket.bulk.resolve')}
                    </Button>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button size="sm" variant="outline">
                          {t('plugin_ticket.actions.setPriority')}
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="start">
                        {PRIORITIES.map((p) => (
                          <DropdownMenuItem key={p} onSelect={() => void runBulk({ action: 'priority', priority: p })}>
                            {t(`plugin_ticket.priority.${p}`)}
                          </DropdownMenuItem>
                        ))}
                      </DropdownMenuContent>
                    </DropdownMenu>
                    <Button size="sm" variant="outline" className="text-destructive" onClick={() => void bulkClose()}>
                      {t('plugin_ticket.bulk.close')}
                    </Button>
                  </Can>
                  <Button size="sm" variant="ghost" onClick={() => setChecked([])}>
                    {t('common.clear')}
                  </Button>
                </div>
              ) : null}

              <div className="min-h-0 flex-1 overflow-y-auto">
                {list.map((tk) => (
                  <TicketListItem
                    key={tk.id}
                    ticket={tk}
                    active={tk.ticketNo === selectedNo}
                    checked={checked.includes(tk.ticketNo)}
                    onToggle={(on) => toggleChecked(tk.ticketNo, on)}
                    onSelect={() => selectTicket(tk.ticketNo)}
                  />
                ))}
                {!tickets.isLoading && list.length === 0 ? (
                  <EmptyState title={filtered ? t('plugin_ticket.emptyFiltered') : t('plugin_ticket.empty')} className="py-8" />
                ) : null}
                {tickets.isLoading && list.length === 0 ? (
                  <div className="py-8 text-center text-sm text-muted-foreground">{t('plugin_ticket.loading')}</div>
                ) : null}
              </div>

              <div className="flex items-center justify-between gap-2 border-t pt-2 text-xs text-muted-foreground">
                <span>{t('dataTable.totalCount', { count: total })}</span>
                <div className="flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={t('dataTable.prevPage')}
                    disabled={page <= 1}
                    onClick={() => setPage((p) => Math.max(1, p - 1))}
                  >
                    <ChevronLeft aria-hidden="true" />
                  </Button>
                  <span className="whitespace-nowrap">{t('dataTable.pageInfo', { page, pages })}</span>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={t('dataTable.nextPage')}
                    disabled={page >= pages}
                    onClick={() => setPage((p) => Math.min(pages, p + 1))}
                  >
                    <ChevronRight aria-hidden="true" />
                  </Button>
                </div>
              </div>
            </Card>

            <TicketThread
              ticket={replies.data?.ticket}
              replies={replies.data?.replies ?? []}
              loading={replies.isLoading}
              sending={reply.isPending}
              onSend={onSend}
              onSetStatus={onChangeStatus}
              onClaim={claim}
              onOpenAssign={openAssign}
              canned={canned.data ?? []}
              onManageCanned={() => setCannedOpen(true)}
            />
          </div>
        </TabsContent>
        <TabsContent value="stats">
          <TicketStatsPanel />
        </TabsContent>
      </Tabs>

      {/* 转派：候选人来自用户列表接口（需要 user:list），负责人姓名由后端补全 */}
      <ResponsiveDialog
        open={Boolean(assignTarget)}
        onOpenChange={(open) => {
          if (!open) {
            setAssignTarget(null)
            setAssignUserId('')
          }
        }}
        title={t('plugin_ticket.transfer.title')}
        description={t('plugin_ticket.transfer.description')}
        size="sm"
        footer={
          <>
            <Button variant="outline" onClick={() => setAssignTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={!assignUserId || assign.isPending} onClick={() => void submitAssign()}>
              {t('plugin_ticket.transfer.confirm')}
            </Button>
          </>
        }
      >
        <RemoteSelect
          url="/users"
          labelKey="nickname"
          value={assignUserId}
          onChange={(v) => setAssignUserId(Array.isArray(v) ? (v[0] ?? '') : v)}
          placeholder={t('plugin_ticket.transfer.placeholder')}
          initialOptions={
            assignTarget && assignTarget.assigneeName
              ? [{ value: assignTarget.assigneeId, label: assignTarget.assigneeName }]
              : undefined
          }
        />
      </ResponsiveDialog>

      {/* 代客建单 */}
      <ResponsiveDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        title={t('plugin_ticket.create.title')}
        description={t('plugin_ticket.create.description')}
        size="md"
        footer={
          <>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={createTicket.isPending} onClick={() => void submitCreate()}>
              {t('plugin_ticket.create.submit')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Field label={t('plugin_ticket.create.titleField')} required>
            <Input
              value={createForm.title}
              onChange={(e) => setCreateForm((f) => ({ ...f, title: e.target.value }))}
              maxLength={200}
            />
          </Field>
          <Field label={t('plugin_ticket.create.contentField')} required>
            <Textarea
              rows={4}
              value={createForm.content}
              onChange={(e) => setCreateForm((f) => ({ ...f, content: e.target.value }))}
              maxLength={2000}
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('plugin_ticket.fields.priority')}>
              <SimpleSelect
                value={createForm.priority}
                onChange={(v) => setCreateForm((f) => ({ ...f, priority: v as TicketPriority }))}
                options={PRIORITIES.map((p) => ({ value: p, label: t(`plugin_ticket.priority.${p}`) }))}
                aria-label={t('plugin_ticket.fields.priority')}
              />
            </Field>
            <Field label={t('plugin_ticket.create.reporterName')}>
              <Input
                value={createForm.reporterName}
                placeholder={t('plugin_ticket.create.reporterNamePlaceholder')}
                onChange={(e) => setCreateForm((f) => ({ ...f, reporterName: e.target.value }))}
                maxLength={64}
              />
            </Field>
            <Field label={t('plugin_ticket.create.reporterId')}>
              <Input
                value={createForm.reporterId}
                placeholder={t('plugin_ticket.create.reporterIdPlaceholder')}
                onChange={(e) => setCreateForm((f) => ({ ...f, reporterId: e.target.value }))}
                maxLength={64}
              />
            </Field>
            <Field label={t('plugin_ticket.create.bizType')}>
              <Input
                value={createForm.bizType}
                onChange={(e) => setCreateForm((f) => ({ ...f, bizType: e.target.value }))}
                maxLength={32}
              />
            </Field>
            <Field label={t('plugin_ticket.create.bizNo')}>
              <Input
                value={createForm.bizNo}
                onChange={(e) => setCreateForm((f) => ({ ...f, bizNo: e.target.value }))}
                maxLength={64}
              />
            </Field>
          </div>
        </div>
      </ResponsiveDialog>

      {/* 快捷回复管理 */}
      <ResponsiveDialog
        open={cannedOpen}
        onOpenChange={setCannedOpen}
        title={t('plugin_ticket.canned.manage')}
        size="lg"
        footer={
          <Button variant="outline" onClick={() => setCannedOpen(false)}>
            {t('common.close')}
          </Button>
        }
      >
        <div className="space-y-4">
          <div className="space-y-2">
            {canned.data?.length ? (
              canned.data.map((row) => (
                <div key={row.id} className="flex items-start gap-2 rounded-md border p-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{row.title}</p>
                    <p className="line-clamp-2 text-xs text-muted-foreground">{row.content}</p>
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => editCanned(row)}>
                    {t('common.edit')}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={t('common.delete')}
                    className="text-destructive"
                    onClick={() => void removeCanned(row)}
                  >
                    <Trash2 aria-hidden="true" />
                  </Button>
                </div>
              ))
            ) : (
              <p className="text-sm text-muted-foreground">{t('plugin_ticket.canned.empty')}</p>
            )}
          </div>
          <div className="space-y-2 rounded-md bg-muted/50 p-3">
            <Field label={t('plugin_ticket.canned.title')} required>
              <Input
                value={cannedForm.title}
                placeholder={t('plugin_ticket.canned.titlePlaceholder')}
                onChange={(e) => setCannedForm((f) => ({ ...f, title: e.target.value }))}
                maxLength={100}
              />
            </Field>
            <Field label={t('plugin_ticket.canned.content')} required>
              <Textarea
                rows={3}
                value={cannedForm.content}
                placeholder={t('plugin_ticket.canned.contentPlaceholder')}
                onChange={(e) => setCannedForm((f) => ({ ...f, content: e.target.value }))}
                maxLength={2000}
              />
            </Field>
            <div className="flex justify-end gap-2">
              {cannedForm.id ? (
                <Button variant="ghost" onClick={() => setCannedForm({ title: '', content: '' })}>
                  {t('plugin_ticket.canned.cancelEdit')}
                </Button>
              ) : null}
              <Button
                disabled={!cannedForm.title.trim() || !cannedForm.content.trim() || createCanned.isPending || updateCanned.isPending}
                onClick={() => void saveCanned()}
              >
                {cannedForm.id ? t('plugin_ticket.canned.save') : t('plugin_ticket.canned.add')}
              </Button>
            </div>
          </div>
        </div>
      </ResponsiveDialog>
    </PageContainer>
  )
}

function Field({ label, required, children }: { label: string; required?: boolean; children: ReactNode }) {
  return (
    <label className="block space-y-1">
      <span className="text-xs font-medium text-foreground">
        {label}
        {required ? <span className="text-destructive"> *</span> : null}
      </span>
      {children}
    </label>
  )
}

function TicketListItem({
  ticket,
  active,
  checked,
  onToggle,
  onSelect,
}: {
  ticket: Ticket
  active: boolean
  checked: boolean
  onToggle: (on: boolean) => void
  onSelect: () => void
}) {
  const { t, i18n } = useTranslation()
  return (
    <div
      className={cn(
        'flex items-start gap-2 rounded-md px-2 py-2 transition-colors hover:bg-accent',
        active && 'bg-accent',
      )}
    >
      <Checkbox
        checked={checked}
        onCheckedChange={(v) => onToggle(v === true)}
        aria-label={t('dataTable.selectRow')}
        className="mt-1"
      />
      <button type="button" onClick={onSelect} className="flex min-w-0 flex-1 flex-col gap-1 text-start text-sm">
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
    </div>
  )
}

function TicketThread({
  ticket,
  replies,
  loading,
  sending,
  onSend,
  onSetStatus,
  onClaim,
  onOpenAssign,
  canned,
  onManageCanned,
}: {
  ticket?: Ticket
  replies: TicketReply[]
  loading: boolean
  sending: boolean
  onSend: (content: string, internal: boolean) => Promise<void>
  onSetStatus: (ticketNo: string, to: string, confirmMsg?: string) => Promise<void>
  onClaim: (ticketNo: string) => Promise<void>
  onOpenAssign: (ticket: Ticket) => void
  canned: CannedReply[]
  onManageCanned: () => void
}) {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState('')
  const [internal, setInternal] = useState(false)
  const bottomRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [replies.length, ticket?.ticketNo])

  // 切工单时清空未发送的草稿，避免把上一张的回复发到下一张
  useEffect(() => {
    setDraft('')
    setInternal(false)
  }, [ticket?.ticketNo])

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
    void onSend(content, internal)
      .then(() => setDraft(''))
      .catch(() => undefined)
  }

  const insertCanned = (id: string) => {
    const row = canned.find((c) => c.id === id)
    if (row) setDraft(row.content)
  }

  return (
    <Card className="flex min-h-0 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2">
        <div className="min-w-0 flex-1 basis-48">
          <div className="truncate text-sm font-medium text-foreground">
            {ticket.title}
            <span className="text-muted-foreground"> · {ticket.ticketNo}</span>
          </div>
          <div className="truncate text-xs text-muted-foreground">
            {ticket.reporterName || t(`plugin_ticket.repliers.${ticket.reporterType}`)}
            {ticket.bizNo ? ` · ${ticket.bizType}/${ticket.bizNo}` : ''}
            {ticket.assigneeName || (ticket.assigneeId && ticket.assigneeId !== '0')
              ? ` · ${t('plugin_ticket.assignee')}: ${ticket.assigneeName || `#${ticket.assigneeId}`}`
              : ` · ${t('plugin_ticket.assignee')}: ${t('plugin_ticket.assigneeUnassigned')}`}
            {ticket.creatorName ? ` · ${t('plugin_ticket.createdBy')}: ${ticket.creatorName}` : ''}
          </div>
        </div>
        <Can perm="ticket:edit">
          {ticket.assigneeId === '0' || ticket.assigneeId === '' ? (
            <Button variant="outline" size="sm" onClick={() => void onClaim(ticket.ticketNo)}>
              <UserRoundCheck aria-hidden="true" />
              {t('plugin_ticket.assign')}
            </Button>
          ) : (
            <Button variant="outline" size="sm" onClick={() => onOpenAssign(ticket)}>
              <UserRoundCheck aria-hidden="true" />
              {t('plugin_ticket.actions.transfer')}
            </Button>
          )}
          {ticket.status === 'open' ? (
            <Button variant="outline" size="sm" onClick={() => void onSetStatus(ticket.ticketNo, 'processing')}>
              {t('plugin_ticket.actions.processing')}
            </Button>
          ) : null}
          {ticket.status === 'processing' ? (
            <Button variant="outline" size="sm" onClick={() => void onSetStatus(ticket.ticketNo, 'pending')}>
              {t('plugin_ticket.actions.pending')}
            </Button>
          ) : null}
          {ticket.status === 'pending' ? (
            <Button variant="outline" size="sm" onClick={() => void onSetStatus(ticket.ticketNo, 'processing')}>
              {t('plugin_ticket.actions.resume')}
            </Button>
          ) : null}
          {ticket.status === 'resolved' ? (
            <Button variant="outline" size="sm" onClick={() => void onSetStatus(ticket.ticketNo, 'processing')}>
              <RotateCcw aria-hidden="true" />
              {t('plugin_ticket.actions.reopen')}
            </Button>
          ) : null}
          {ticket.status !== 'closed' && ticket.status !== 'resolved' ? (
            <>
              <Button variant="outline" size="sm" onClick={() => void onSetStatus(ticket.ticketNo, 'resolved')}>
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
          {ticket.status === 'resolved' ? (
            <Button
              variant="outline"
              size="sm"
              className="text-destructive"
              onClick={() =>
                void onSetStatus(ticket.ticketNo, 'closed', t('plugin_ticket.closeConfirm', { no: ticket.ticketNo }))
              }
            >
              <XCircle aria-hidden="true" />
              {t('plugin_ticket.close')}
            </Button>
          ) : null}
        </Can>
        <Badge variant={statusVariant(ticket.status)}>{t(`plugin_ticket.status.${ticket.status}`)}</Badge>
        {ticket.rating ? (
          <Badge variant="success" title={ticket.ratingComment || undefined}>
            {t('plugin_ticket.rating.badge', { score: ticket.rating })}
          </Badge>
        ) : null}
      </div>

      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-4">
        {loading && replies.length === 0 ? (
          <div className="text-center text-sm text-muted-foreground">{t('plugin_ticket.loading')}</div>
        ) : null}
        {replies.map((r) => (
          <div
            key={r.id}
            className={cn('flex', r.internal ? 'justify-start' : r.replierType === 'agent' ? 'justify-end' : 'justify-start')}
          >
            <div
              className={cn(
                'max-w-[75%] rounded-lg px-3 py-2 text-sm',
                r.internal
                  ? 'border border-warning/40 bg-warning/10 text-foreground'
                  : r.replierType === 'agent'
                    ? 'bg-primary text-primary-foreground'
                    : r.replierType === 'system'
                      ? 'bg-muted text-muted-foreground'
                      : 'bg-muted text-foreground',
              )}
            >
              <div className="mb-0.5 flex items-center gap-1 text-[11px] opacity-70">
                {r.internal ? (
                  <Badge variant="warning" className="px-1 py-0 text-[10px]">
                    {t('plugin_ticket.internal.badge')}
                  </Badge>
                ) : null}
                <span>
                  {r.replierName || t(`plugin_ticket.repliers.${r.replierType}`)} ·{' '}
                  {formatTime(r.createdAt, i18n.language)}
                </span>
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
            className="space-y-2 border-t p-3"
            onSubmit={(e) => {
              e.preventDefault()
              submit()
            }}
          >
            <div className="flex flex-wrap items-center gap-2">
              <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Checkbox
                  checked={internal}
                  onCheckedChange={(v) => setInternal(v === true)}
                  aria-label={t('plugin_ticket.internal.label')}
                />
                {t('plugin_ticket.internal.label')}
              </label>
              <span className="hidden text-[11px] text-muted-foreground sm:inline">{t('plugin_ticket.internal.hint')}</span>
              <div className="ms-auto flex items-center gap-1">
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button type="button" variant="outline" size="sm" disabled={!canned.length}>
                      <Zap aria-hidden="true" />
                      {t('plugin_ticket.canned.label')}
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="max-h-72 overflow-y-auto">
                    {canned.length ? (
                      canned.map((row) => (
                        <DropdownMenuItem key={row.id} onSelect={() => insertCanned(row.id)}>
                          {row.title}
                        </DropdownMenuItem>
                      ))
                    ) : (
                      <DropdownMenuItem disabled>{t('plugin_ticket.canned.empty')}</DropdownMenuItem>
                    )}
                  </DropdownMenuContent>
                </DropdownMenu>
                <Can perm="ticket:edit">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={t('plugin_ticket.canned.manage')}
                    onClick={onManageCanned}
                  >
                    <Settings2 aria-hidden="true" />
                  </Button>
                </Can>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Input
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                placeholder={
                  internal
                    ? t('plugin_ticket.internal.label')
                    : t('plugin_ticket.replyPlaceholder')
                }
                aria-label={t('plugin_ticket.replyPlaceholder')}
              />
              <Button type="submit" variant={internal ? 'outline' : 'default'} disabled={sending || !draft.trim()}>
                {internal ? <StickyNote aria-hidden="true" /> : <Send aria-hidden="true" />}
                {internal ? t('plugin_ticket.internal.label') : t('plugin_ticket.send')}
              </Button>
            </div>
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
