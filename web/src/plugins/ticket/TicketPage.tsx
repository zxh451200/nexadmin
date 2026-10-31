import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  ChevronLeft,
  ChevronRight,
  CircleCheck,
  Paperclip,
  Plus,
  RotateCcw,
  Send,
  Settings2,
  StickyNote,
  Tag,
  Trash2,
  Timer,
  UserRoundCheck,
  Users,
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
  downloadAttachment,
  useAssignGroup,
  useAssignTicket,
  useBizTypes,
  useBulkTickets,
  useCannedReplies,
  useCreateCanned,
  useCreateGroup,
  useCreateRule,
  useCreateTicket,
  useDeleteCanned,
  useDeleteGroup,
  useDeleteRule,
  useGroups,
  useReplies,
  useReplyTicket,
  useRules,
  useSLA,
  useSaveSLA,
  useSetGroupMembers,
  useSetTicketStatus,
  useSetTicketTags,
  useTicketTags,
  useTickets,
  useUpdateCanned,
  useUpdateGroup,
  useUpdateRule,
  useUploadAttachment,
  type CannedReply,
  type SLASetting,
  type Ticket,
  type TicketAttachment,
  type TicketGroup,
  type TicketPriority,
  type TicketReply,
  type TicketRule,
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

const WEEKDAYS = [1, 2, 3, 4, 5, 6, 7] as const

const defaultSLA: SLASetting = {
  workdays: [1, 2, 3, 4, 5, 6, 7],
  startMinute: 0,
  endMinute: 1440,
  targets: {
    urgent: { firstMinutes: 120, resolutionMinutes: 480 },
    high: { firstMinutes: 480, resolutionMinutes: 1440 },
    normal: { firstMinutes: 1440, resolutionMinutes: 4320 },
    low: { firstMinutes: 4320, resolutionMinutes: 10080 },
  },
}

const cloneSLA = (v: SLASetting): SLASetting => ({
  workdays: [...v.workdays],
  startMinute: v.startMinute,
  endMinute: v.endMinute,
  targets: Object.fromEntries(Object.entries(v.targets).map(([k, t]) => [k, { ...t }])),
})

const minutesToHours = (m: number) => String(Math.round((m / 60) * 100) / 100)
const hoursToMinutes = (v: string) => Math.max(1, Math.round(Number(v || 0) * 60))
const hoursToClock = (v: string) => Math.max(0, Math.min(24, Math.round(Number(v || 0) * 60)))

const formatSize = (n: number) =>
  n >= 1024 * 1024 ? `${(n / 1024 / 1024).toFixed(1)}MB` : n >= 1024 ? `${Math.round(n / 1024)}KB` : `${n}B`

const emptyGroupForm = {
  id: undefined as string | undefined,
  name: '',
  description: '',
  autoAssign: false,
  enabled: true,
  sort: 0,
  userIds: [] as string[],
}

const ANY = '__any'
const NONE = '__none'

const emptyRuleForm = {
  id: undefined as string | undefined,
  name: '',
  enabled: true,
  sort: 0,
  matchPriority: '',
  matchBizType: '',
  matchKeyword: '',
  setPriority: '',
  setGroupId: '0',
  note: '',
}

export default function TicketPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [params, setParams] = useSearchParams()
  const view = params.get('view') === 'stats' ? 'stats' : 'logs'

  const [statusFilter, setStatusFilter] = useState<string | undefined>()
  const [priorityFilter, setPriorityFilter] = useState<string | undefined>()
  const [assigneeFilter, setAssigneeFilter] = useState<AssigneeFilter>('all')
  const [groupFilter, setGroupFilter] = useState('0')
  const [tagFilter, setTagFilter] = useState('0')
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
  const [slaOpen, setSlaOpen] = useState(false)
  const [slaForm, setSlaForm] = useState<SLASetting>(defaultSLA)
  const [groupOpen, setGroupOpen] = useState(false)
  const [groupForm, setGroupForm] = useState(emptyGroupForm)
  const [groupMemberOptions, setGroupMemberOptions] = useState<{ value: string; label: string }[]>([])
  const [moveTarget, setMoveTarget] = useState<Ticket | null>(null)
  const [moveGroupId, setMoveGroupId] = useState('0')
  const [ruleOpen, setRuleOpen] = useState(false)
  const [ruleForm, setRuleForm] = useState(emptyRuleForm)
  const [tagOpen, setTagOpen] = useState(false)
  const [tagTarget, setTagTarget] = useState<Ticket | null>(null)
  const [tagDraft, setTagDraft] = useState('')

  // 搜索防抖：300ms 内连续输入只发一次请求
  useEffect(() => {
    const id = window.setTimeout(() => setAppliedKeyword(keyword.trim()), 300)
    return () => window.clearTimeout(id)
  }, [keyword])

  // 任一筛选变化都回到第一页
  useEffect(() => {
    setPage(1)
  }, [appliedKeyword, statusFilter, priorityFilter, assigneeFilter, overdueOnly, groupFilter, tagFilter])

  const tickets = useTickets({
    status: statusFilter,
    priority: priorityFilter,
    keyword: appliedKeyword || undefined,
    overdue: overdueOnly ? '1' : undefined,
    unassigned: assigneeFilter === 'unassigned' ? '1' : undefined,
    mine: assigneeFilter === 'mine' ? '1' : undefined,
    groupId: groupFilter !== '0' ? groupFilter : undefined,
    tag: tagFilter !== '0' ? tagFilter : undefined,
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
  const slaQuery = useSLA()
  const saveSLA = useSaveSLA()
  const groups = useGroups()
  const createGroup = useCreateGroup()
  const updateGroup = useUpdateGroup()
  const deleteGroup = useDeleteGroup()
  const setGroupMembers = useSetGroupMembers()
  const assignGroupMut = useAssignGroup()
  const rules = useRules()
  const bizTypes = useBizTypes()
  const createRule = useCreateRule()
  const updateRule = useUpdateRule()
  const deleteRule = useDeleteRule()
  const uploadAttachmentMut = useUploadAttachment()
  const allTags = useTicketTags()
  const setTagsMut = useSetTicketTags()

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

  const filtered = Boolean(statusFilter || priorityFilter || appliedKeyword || overdueOnly || assigneeFilter !== 'all' || groupFilter !== '0' || tagFilter !== '0')

  const openTags = (tk: Ticket) => {
    setTagTarget(tk)
    setTagDraft((tk.tags ?? []).join(', '))
    setTagOpen(true)
  }

  const toggleTagSuggestion = (tag: string) => {
    const current = tagDraft.split(/[,，]/).map((x) => x.trim()).filter(Boolean)
    setTagDraft(
      current.includes(tag) ? current.filter((x) => x !== tag).join(', ') : [...current, tag].join(', '),
    )
  }

  const submitTags = async () => {
    if (!tagTarget) return
    const tags = [...new Set(tagDraft.split(/[,，]/).map((x) => x.trim()).filter(Boolean))]
    if (tags.length > 10 || tags.some((x) => [...x].length > 20)) {
      toast.error(t('plugin_ticket.tags.invalid'))
      return
    }
    try {
      await setTagsMut.mutateAsync({ ticketNo: tagTarget.ticketNo, tags })
      toast.success(t('common.operateSuccess'))
      setTagOpen(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const uploadAtt = async (file: File, internal: boolean) => {
    if (!selectedNo) return
    try {
      await uploadAttachmentMut.mutateAsync({ ticketNo: selectedNo, file, internal })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('plugin_ticket.attachments.failed'))
    }
  }

  const openSLA = () => {
    setSlaForm(cloneSLA(slaQuery.data ?? defaultSLA))
    setSlaOpen(true)
  }

  const toggleWorkday = (d: number) => {
    setSlaForm((f) => ({
      ...f,
      workdays: f.workdays.includes(d) ? f.workdays.filter((x) => x !== d) : [...f.workdays, d].sort((a, b) => a - b),
    }))
  }

  const setTarget = (p: TicketPriority, field: 'firstMinutes' | 'resolutionMinutes', minutes: number) => {
    setSlaForm((f) => ({
      ...f,
      targets: { ...f.targets, [p]: { ...(f.targets[p] ?? defaultSLA.targets[p]), [field]: minutes } },
    }))
  }

  const submitSLA = async () => {
    if (!slaForm.workdays.length || slaForm.endMinute <= slaForm.startMinute) {
      toast.error(t('plugin_ticket.sla.invalid'))
      return
    }
    try {
      await saveSLA.mutateAsync(slaForm)
      toast.success(t('common.operateSuccess'))
      setSlaOpen(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const openGroups = () => {
    setGroupForm(emptyGroupForm)
    setGroupMemberOptions([])
    setGroupOpen(true)
  }

  const editGroup = (g: TicketGroup) => {
    setGroupForm({
      id: g.id,
      name: g.name,
      description: g.description,
      autoAssign: g.autoAssign,
      enabled: g.enabled,
      sort: g.sort,
      userIds: (g.members ?? []).map((m) => m.userId),
    })
    // 组员回显用接口返回的姓名，避免不在用户列表首页的成员显示成数字 ID
    setGroupMemberOptions((g.members ?? []).map((m) => ({ value: m.userId, label: m.userName || m.userId })))
  }

  const submitGroup = async () => {
    if (!groupForm.name.trim()) {
      toast.error(t('plugin_ticket.groups.invalid'))
      return
    }
    try {
      const input = {
        name: groupForm.name.trim(),
        description: groupForm.description.trim(),
        autoAssign: groupForm.autoAssign,
        enabled: groupForm.enabled,
        sort: groupForm.sort,
      }
      const saved = groupForm.id
        ? await updateGroup.mutateAsync({ id: groupForm.id, ...input })
        : await createGroup.mutateAsync(input)
      await setGroupMembers.mutateAsync({ id: saved.id, userIds: groupForm.userIds })
      setGroupForm(emptyGroupForm)
      setGroupMemberOptions([])
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const removeGroup = async (g: TicketGroup) => {
    if (!(await confirm({ description: t('plugin_ticket.groups.deleteConfirm', { name: g.name }) }))) return
    try {
      await deleteGroup.mutateAsync(g.id)
      if (groupForm.id === g.id) {
        setGroupForm(emptyGroupForm)
        setGroupMemberOptions([])
      }
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const openMove = (tk: Ticket) => {
    setMoveTarget(tk)
    setMoveGroupId(tk.groupId && tk.groupId !== '0' ? tk.groupId : '0')
  }

  const submitMove = async () => {
    if (!moveTarget) return
    // 没换组就直接关窗：所在组已停用时下拉会保留该组，避免提交后被后端拒绝
    if (moveGroupId === (moveTarget.groupId && moveTarget.groupId !== '0' ? moveTarget.groupId : '0')) {
      setMoveTarget(null)
      return
    }
    try {
      await assignGroupMut.mutateAsync({ ticketNo: moveTarget.ticketNo, groupId: Number(moveGroupId) })
      toast.success(t('common.operateSuccess'))
      setMoveTarget(null)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const openRules = () => {
    setRuleForm(emptyRuleForm)
    setRuleOpen(true)
  }

  const editRule = (r: TicketRule) => {
    setRuleForm({
      id: r.id,
      name: r.name,
      enabled: r.enabled,
      sort: r.sort,
      matchPriority: r.matchPriority,
      matchBizType: r.matchBizType,
      matchKeyword: r.matchKeyword,
      setPriority: r.setPriority,
      setGroupId: r.setGroupId && r.setGroupId !== '0' ? r.setGroupId : '0',
      note: r.note,
    })
  }

  const submitRule = async () => {
    const hasAction = Boolean(ruleForm.setPriority || (ruleForm.setGroupId && ruleForm.setGroupId !== '0') || ruleForm.note.trim())
    if (!ruleForm.name.trim() || !hasAction) {
      toast.error(t('plugin_ticket.rules.invalid'))
      return
    }
    try {
      const input = {
        name: ruleForm.name.trim(),
        enabled: ruleForm.enabled,
        sort: ruleForm.sort,
        matchPriority: ruleForm.matchPriority,
        matchBizType: ruleForm.matchBizType.trim(),
        matchKeyword: ruleForm.matchKeyword.trim(),
        setPriority: ruleForm.setPriority,
        setGroupId: Number(ruleForm.setGroupId || 0),
        note: ruleForm.note.trim(),
      }
      if (ruleForm.id) await updateRule.mutateAsync({ id: ruleForm.id, ...input })
      else await createRule.mutateAsync(input)
      setRuleForm(emptyRuleForm)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const removeRule = async (r: TicketRule) => {
    if (!(await confirm({ description: t('plugin_ticket.rules.deleteConfirm', { name: r.name }) }))) return
    try {
      await deleteRule.mutateAsync(r.id)
      if (ruleForm.id === r.id) setRuleForm(emptyRuleForm)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const ruleSummary = (r: TicketRule) => {    const condition = [
      r.matchPriority ? t(`plugin_ticket.priority.${r.matchPriority}`) : t('plugin_ticket.rules.any'),
      r.matchBizType || '-',
      r.matchKeyword || '-',
    ].join(' · ')
    const action = [
      r.setPriority ? t(`plugin_ticket.priority.${r.setPriority}`) : '',
      r.setGroupId && r.setGroupId !== '0'
        ? (groups.data?.find((g) => g.id === r.setGroupId)?.name ?? `#${r.setGroupId}`)
        : '',
      r.note,
    ]
      .filter(Boolean)
      .join(' / ')
    return `${condition} → ${action}`
  }

  // 停用组里可能还有存量工单：筛选保留并标注；转组只列启用的，当前所在的停用组除外。
  const groupLabel = (g: TicketGroup) =>
    g.enabled ? g.name : `${g.name} · ${t('plugin_ticket.groups.disabled')}`

  // 业务类型填错只会静默不命中，所以把已有值摆出来点选；仍允许输入新的类型
  const ruleBizTypeHints = (bizTypes.data ?? []).filter((v) => v && v !== ruleForm.matchBizType).slice(0, 6)
  const ruleBizTypePlaceholder = ruleBizTypeHints[0] ?? ''

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_ticket.title')}
        description={t('plugin_ticket.description')}
        actions={
          <>
            <Can perm="ticket:edit">
              <Button variant="outline" onClick={openRules}>
                <Zap aria-hidden="true" />
                {t('plugin_ticket.rules.title')}
              </Button>
            </Can>
            <Can perm="ticket:edit">
              <Button variant="outline" onClick={openGroups}>
                <Users aria-hidden="true" />
                {t('plugin_ticket.groups.title')}
              </Button>
            </Can>
            <Can perm="ticket:edit">
              <Button variant="outline" onClick={openSLA}>
                <Timer aria-hidden="true" />
                {t('plugin_ticket.sla.title')}
              </Button>
            </Can>
            <Can perm="ticket:create">
              <Button onClick={openCreate}>
                <Plus aria-hidden="true" />
                {t('plugin_ticket.create.title')}
              </Button>
            </Can>
          </>
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
                  aria-label={t('plugin_ticket.groups.label')}
                  className="w-28"
                  value={groupFilter}
                  onChange={setGroupFilter}
                  options={[
                    { value: '0', label: t('plugin_ticket.groups.all') },
                    ...(groups.data ?? []).map((g) => ({ value: g.id, label: groupLabel(g) })),
                  ]}
                />
                <SimpleSelect
                  aria-label={t('plugin_ticket.tags.title')}
                  className="w-24"
                  value={tagFilter}
                  onChange={setTagFilter}
                  options={[
                    { value: '0', label: t('plugin_ticket.tags.all') },
                    ...(allTags.data ?? []).map((tag) => ({ value: tag, label: tag })),
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
              onOpenMove={openMove}
              onEditTags={openTags}
              canned={canned.data ?? []}
              onManageCanned={() => setCannedOpen(true)}
              attachments={replies.data?.attachments ?? []}
              onUpload={uploadAtt}
              uploading={uploadAttachmentMut.isPending}
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

      {/* SLA 策略：营业时间 + 按优先级的首响/解决时限 */}
      <ResponsiveDialog
        open={slaOpen}
        onOpenChange={setSlaOpen}
        title={t('plugin_ticket.sla.title')}
        description={t('plugin_ticket.sla.description')}
        size="lg"
        footer={
          <>
            <Button variant="outline" onClick={() => setSlaOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={saveSLA.isPending} onClick={() => void submitSLA()}>
              {t('plugin_ticket.sla.save')}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label={t('plugin_ticket.sla.workdays')}>
            <div className="flex flex-wrap gap-x-4 gap-y-2">
              {WEEKDAYS.map((d) => (
                <label key={d} className="flex items-center gap-1.5 text-sm">
                  <Checkbox
                    checked={slaForm.workdays.includes(d)}
                    onCheckedChange={() => toggleWorkday(d)}
                    aria-label={t(`plugin_ticket.weekdays.${d}`)}
                  />
                  {t(`plugin_ticket.weekdays.${d}`)}
                </label>
              ))}
            </div>
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('plugin_ticket.sla.start')}>
              <Input
                type="number"
                min={0}
                max={24}
                step={0.5}
                value={minutesToHours(slaForm.startMinute)}
                onChange={(e) => setSlaForm((f) => ({ ...f, startMinute: hoursToClock(e.target.value) }))}
              />
            </Field>
            <Field label={t('plugin_ticket.sla.end')}>
              <Input
                type="number"
                min={0}
                max={24}
                step={0.5}
                value={minutesToHours(slaForm.endMinute)}
                onChange={(e) => setSlaForm((f) => ({ ...f, endMinute: hoursToClock(e.target.value) }))}
              />
            </Field>
          </div>
          <div className="space-y-2">
            <div className="grid grid-cols-[4rem_1fr_1fr] gap-2 text-xs text-muted-foreground">
              <span />
              <span>{t('plugin_ticket.sla.first')}</span>
              <span>{t('plugin_ticket.sla.resolution')}</span>
            </div>
            {PRIORITIES.map((p) => {
              const target = slaForm.targets[p] ?? defaultSLA.targets[p]
              return (
                <div key={p} className="grid grid-cols-[4rem_1fr_1fr] items-center gap-2">
                  <span className="text-sm">{t(`plugin_ticket.priority.${p}`)}</span>
                  <Input
                    type="number"
                    min={0.5}
                    step={0.5}
                    value={minutesToHours(target.firstMinutes)}
                    aria-label={`${t(`plugin_ticket.priority.${p}`)} ${t('plugin_ticket.sla.first')}`}
                    onChange={(e) => setTarget(p, 'firstMinutes', hoursToMinutes(e.target.value))}
                  />
                  <Input
                    type="number"
                    min={0.5}
                    step={0.5}
                    value={minutesToHours(target.resolutionMinutes)}
                    aria-label={`${t(`plugin_ticket.priority.${p}`)} ${t('plugin_ticket.sla.resolution')}`}
                    onChange={(e) => setTarget(p, 'resolutionMinutes', hoursToMinutes(e.target.value))}
                  />
                </div>
              )
            })}
          </div>
          <p className="text-xs text-muted-foreground">{t('plugin_ticket.sla.hint')}</p>
        </div>
      </ResponsiveDialog>

      {/* 分组管理：组队列 + 组员 + 自动分派开关 */}
      <ResponsiveDialog
        open={groupOpen}
        onOpenChange={setGroupOpen}
        title={t('plugin_ticket.groups.manage')}
        size="lg"
        footer={
          <Button variant="outline" onClick={() => setGroupOpen(false)}>
            {t('common.close')}
          </Button>
        }
      >
        <div className="space-y-4">
          <div className="space-y-2">
            {groups.data?.length ? (
              groups.data.map((g) => (
                <div key={g.id} className="flex items-start gap-2 rounded-md border p-2">
                  <div className="min-w-0 flex-1">
                    <p className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
                      {g.name}
                      {g.autoAssign ? <Badge variant="info">{t('plugin_ticket.groups.autoShort')}</Badge> : null}
                      {!g.enabled ? <Badge variant="secondary">{t('plugin_ticket.groups.disabled')}</Badge> : null}
                    </p>
                    <p className="truncate text-xs text-muted-foreground">
                      {g.description || '-'} · {t('plugin_ticket.groups.memberCount', { count: g.members?.length ?? 0 })}
                    </p>
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => editGroup(g)}>
                    {t('common.edit')}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={t('common.delete')}
                    className="text-destructive"
                    onClick={() => void removeGroup(g)}
                  >
                    <Trash2 aria-hidden="true" />
                  </Button>
                </div>
              ))
            ) : (
              <p className="text-sm text-muted-foreground">{t('plugin_ticket.groups.empty')}</p>
            )}
          </div>
          <div className="space-y-2 rounded-md bg-muted/50 p-3">
            <p className="text-xs font-medium">
              {groupForm.id ? t('plugin_ticket.groups.edit') : t('plugin_ticket.groups.add')}
            </p>
            <Field label={t('plugin_ticket.groups.name')} required>
              <Input
                value={groupForm.name}
                maxLength={64}
                onChange={(e) => setGroupForm((f) => ({ ...f, name: e.target.value }))}
              />
            </Field>
            <Field label={t('plugin_ticket.groups.description')}>
              <Input
                value={groupForm.description}
                maxLength={200}
                onChange={(e) => setGroupForm((f) => ({ ...f, description: e.target.value }))}
              />
            </Field>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label={t('plugin_ticket.groups.sort')}>
                <Input
                  type="number"
                  min={0}
                  value={String(groupForm.sort)}
                  onChange={(e) => setGroupForm((f) => ({ ...f, sort: Math.max(0, Number(e.target.value) || 0) }))}
                />
              </Field>
              <Field label={t('plugin_ticket.groups.members')}>
                <RemoteSelect
                  url="/users"
                  labelKey="nickname"
                  multiple
                  value={groupForm.userIds}
                  onChange={(v) => setGroupForm((f) => ({ ...f, userIds: Array.isArray(v) ? v : [] }))}
                  initialOptions={groupMemberOptions}
                  placeholder={t('plugin_ticket.groups.membersPlaceholder')}
                />
              </Field>
            </div>
            <div className="flex flex-wrap gap-4">
              <label className="flex items-center gap-1.5 text-sm">
                <Checkbox
                  checked={groupForm.autoAssign}
                  onCheckedChange={(v) => setGroupForm((f) => ({ ...f, autoAssign: v === true }))}
                />
                {t('plugin_ticket.groups.autoAssign')}
              </label>
              <label className="flex items-center gap-1.5 text-sm">
                <Checkbox
                  checked={groupForm.enabled}
                  onCheckedChange={(v) => setGroupForm((f) => ({ ...f, enabled: v === true }))}
                />
                {t('plugin_ticket.groups.enabled')}
              </label>
            </div>
            <div className="flex justify-end gap-2">
              {groupForm.id ? (
                <Button variant="ghost" onClick={() => setGroupForm(emptyGroupForm)}>
                  {t('common.cancel')}
                </Button>
              ) : null}
              <Button
                disabled={
                  !groupForm.name.trim() ||
                  createGroup.isPending ||
                  updateGroup.isPending ||
                  setGroupMembers.isPending
                }
                onClick={() => void submitGroup()}
              >
                {t('plugin_ticket.groups.save')}
              </Button>
            </div>
          </div>
        </div>
      </ResponsiveDialog>

      {/* 转组：工单移到另一个队列 */}
      <ResponsiveDialog
        open={Boolean(moveTarget)}
        onOpenChange={(open) => {
          if (!open) setMoveTarget(null)
        }}
        title={t('plugin_ticket.groups.move')}
        description={t('plugin_ticket.groups.moveDesc')}
        size="sm"
        footer={
          <>
            <Button variant="outline" onClick={() => setMoveTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={assignGroupMut.isPending} onClick={() => void submitMove()}>
              {t('plugin_ticket.groups.move')}
            </Button>
          </>
        }
      >
        <SimpleSelect
          aria-label={t('plugin_ticket.groups.label')}
          value={moveGroupId}
          onChange={setMoveGroupId}
          options={[
            { value: '0', label: t('plugin_ticket.groups.none') },
            ...(groups.data ?? [])
              .filter((g) => g.enabled || g.id === moveTarget?.groupId)
              .map((g) => ({ value: g.id, label: groupLabel(g) })),
          ]}
        />
      </ResponsiveDialog>

      {/* 自动化规则：新建工单时按条件改优先级/转组/加内部备注 */}
      <ResponsiveDialog
        open={ruleOpen}
        onOpenChange={setRuleOpen}
        title={t('plugin_ticket.rules.manage')}
        size="lg"
        footer={
          <Button variant="outline" onClick={() => setRuleOpen(false)}>
            {t('common.close')}
          </Button>
        }
      >
        <div className="space-y-4">
          <div className="space-y-2">
            {rules.data?.length ? (
              rules.data.map((r) => (
                <div key={r.id} className="flex items-start gap-2 rounded-md border p-2">
                  <div className="min-w-0 flex-1">
                    <p className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
                      {r.name}
                      {!r.enabled ? <Badge variant="secondary">{t('plugin_ticket.rules.disabled')}</Badge> : null}
                    </p>
                    <p className="line-clamp-2 text-xs text-muted-foreground">{ruleSummary(r)}</p>
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => editRule(r)}>
                    {t('common.edit')}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={t('common.delete')}
                    className="text-destructive"
                    onClick={() => void removeRule(r)}
                  >
                    <Trash2 aria-hidden="true" />
                  </Button>
                </div>
              ))
            ) : (
              <p className="text-sm text-muted-foreground">{t('plugin_ticket.rules.empty')}</p>
            )}
          </div>
          <div className="space-y-2 rounded-md bg-muted/50 p-3">
            <p className="text-xs font-medium">
              {ruleForm.id ? t('plugin_ticket.rules.edit') : t('plugin_ticket.rules.add')}
            </p>
            <Field label={t('plugin_ticket.rules.name')} required>
              <Input
                value={ruleForm.name}
                maxLength={64}
                onChange={(e) => setRuleForm((f) => ({ ...f, name: e.target.value }))}
              />
            </Field>
            <p className="pt-1 text-xs font-medium text-muted-foreground">{t('plugin_ticket.rules.match')}</p>
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label={t('plugin_ticket.rules.matchPriority')}>
                <SimpleSelect
                  aria-label={t('plugin_ticket.rules.matchPriority')}
                  value={ruleForm.matchPriority || ANY}
                  onChange={(v) => setRuleForm((f) => ({ ...f, matchPriority: v === ANY ? '' : v }))}
                  options={[
                    { value: ANY, label: t('plugin_ticket.rules.any') },
                    ...PRIORITIES.map((p) => ({ value: p, label: t(`plugin_ticket.priority.${p}`) })),
                  ]}
                />
              </Field>
              <Field label={t('plugin_ticket.rules.matchBizType')}>
                <div className="space-y-1.5">
                  <Input
                    value={ruleForm.matchBizType}
                    maxLength={32}
                    placeholder={ruleBizTypePlaceholder}
                    onChange={(e) => setRuleForm((f) => ({ ...f, matchBizType: e.target.value }))}
                  />
                  {ruleBizTypeHints.length ? (
                    <div className="flex flex-wrap items-center gap-1">
                      <span className="text-[11px] text-muted-foreground">{t('plugin_ticket.rules.bizTypesHint')}</span>
                      {ruleBizTypeHints.map((v) => (
                        <Button
                          key={v}
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-6 px-2 text-xs"
                          onClick={() => setRuleForm((f) => ({ ...f, matchBizType: v }))}
                        >
                          {v}
                        </Button>
                      ))}
                    </div>
                  ) : null}
                </div>
              </Field>
              <Field label={t('plugin_ticket.rules.matchKeyword')}>
                <Input
                  value={ruleForm.matchKeyword}
                  maxLength={64}
                  onChange={(e) => setRuleForm((f) => ({ ...f, matchKeyword: e.target.value }))}
                />
              </Field>
            </div>
            <p className="pt-1 text-xs font-medium text-muted-foreground">{t('plugin_ticket.rules.action')}</p>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label={t('plugin_ticket.rules.setPriority')}>
                <SimpleSelect
                  aria-label={t('plugin_ticket.rules.setPriority')}
                  value={ruleForm.setPriority || NONE}
                  onChange={(v) => setRuleForm((f) => ({ ...f, setPriority: v === NONE ? '' : v }))}
                  options={[
                    { value: NONE, label: t('plugin_ticket.rules.none') },
                    ...PRIORITIES.map((p) => ({ value: p, label: t(`plugin_ticket.priority.${p}`) })),
                  ]}
                />
              </Field>
              <Field label={t('plugin_ticket.rules.setGroup')}>
                <SimpleSelect
                  aria-label={t('plugin_ticket.rules.setGroup')}
                  value={ruleForm.setGroupId}
                  onChange={(v) => setRuleForm((f) => ({ ...f, setGroupId: v }))}
                  options={[
                    { value: '0', label: t('plugin_ticket.rules.none') },
                    // 后端已拒绝指向停用组，这里只列启用的（当前值例外，避免编辑时被迫改）
                    ...(groups.data ?? [])
                      .filter((g) => g.enabled || g.id === ruleForm.setGroupId)
                      .map((g) => ({ value: g.id, label: groupLabel(g) })),
                  ]}
                />
              </Field>
            </div>
            <Field label={t('plugin_ticket.rules.note')}>
              <Textarea
                rows={2}
                maxLength={500}
                value={ruleForm.note}
                onChange={(e) => setRuleForm((f) => ({ ...f, note: e.target.value }))}
              />
            </Field>
            <div className="flex flex-wrap items-center gap-4">
              <label className="flex items-center gap-1.5 text-sm">
                <Checkbox
                  checked={ruleForm.enabled}
                  onCheckedChange={(v) => setRuleForm((f) => ({ ...f, enabled: v === true }))}
                />
                {t('plugin_ticket.rules.enabled')}
              </label>
              <label className="flex items-center gap-1.5 text-sm">
                {t('plugin_ticket.rules.sort')}
                <Input
                  type="number"
                  min={0}
                  className="w-20"
                  value={String(ruleForm.sort)}
                  onChange={(e) => setRuleForm((f) => ({ ...f, sort: Math.max(0, Number(e.target.value) || 0) }))}
                />
              </label>
            </div>
            <div className="flex justify-end gap-2">
              {ruleForm.id ? (
                <Button variant="ghost" onClick={() => setRuleForm(emptyRuleForm)}>
                  {t('common.cancel')}
                </Button>
              ) : null}
              <Button
                disabled={!ruleForm.name.trim() || createRule.isPending || updateRule.isPending}
                onClick={() => void submitRule()}
              >
                {t('plugin_ticket.rules.save')}
              </Button>
            </div>
          </div>
        </div>
      </ResponsiveDialog>

      {/* 标签：逗号分隔，支持点选已有标签 */}
      <ResponsiveDialog
        open={tagOpen}
        onOpenChange={setTagOpen}
        title={t('plugin_ticket.tags.edit')}
        size="sm"
        footer={
          <>
            <Button variant="outline" onClick={() => setTagOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={setTagsMut.isPending} onClick={() => void submitTags()}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Field label={t('plugin_ticket.tags.title')}>
            <Input
              value={tagDraft}
              placeholder={t('plugin_ticket.tags.placeholder')}
              onChange={(e) => setTagDraft(e.target.value)}
              maxLength={220}
            />
          </Field>
          {allTags.data?.length ? (
            <div>
              <p className="mb-1 text-xs text-muted-foreground">{t('plugin_ticket.tags.suggestions')}</p>
              <div className="flex flex-wrap gap-1.5">
                {allTags.data.map((tag) => {
                  const active = tagDraft.split(/[,，]/).map((x) => x.trim()).includes(tag)
                  return (
                    <Button
                      key={tag}
                      type="button"
                      size="sm"
                      variant={active ? 'default' : 'outline'}
                      onClick={() => toggleTagSuggestion(tag)}
                    >
                      {tag}
                    </Button>
                  )
                })}
              </div>
            </div>
          ) : null}
          <p className="text-xs text-muted-foreground">{t('plugin_ticket.tags.invalid')}</p>
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
  onOpenMove,
  onEditTags,
  canned,
  onManageCanned,
  attachments,
  onUpload,
  uploading,
}: {
  ticket?: Ticket
  replies: TicketReply[]
  loading: boolean
  sending: boolean
  onSend: (content: string, internal: boolean) => Promise<void>
  onSetStatus: (ticketNo: string, to: string, confirmMsg?: string) => Promise<void>
  onClaim: (ticketNo: string) => Promise<void>
  onOpenAssign: (ticket: Ticket) => void
  onOpenMove: (ticket: Ticket) => void
  onEditTags: (ticket: Ticket) => void
  canned: CannedReply[]
  onManageCanned: () => void
  attachments: TicketAttachment[]
  onUpload: (file: File, internal: boolean) => Promise<void>
  uploading: boolean
}) {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState('')
  const [internal, setInternal] = useState(false)
  const bottomRef = useRef<HTMLDivElement | null>(null)
  const fileRef = useRef<HTMLInputElement | null>(null)

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
            {ticket.groupName ? ` · ${t('plugin_ticket.groups.label')}: ${ticket.groupName}` : ''}
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
          <Button variant="outline" size="sm" onClick={() => onOpenMove(ticket)}>
            <Users aria-hidden="true" />
            {t('plugin_ticket.groups.move')}
          </Button>
          <Button variant="outline" size="sm" onClick={() => onEditTags(ticket)}>
            <Tag aria-hidden="true" />
            {t('plugin_ticket.tags.edit')}
          </Button>
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
        {(ticket.tags ?? []).map((tag) => (
          <Badge key={tag} variant="secondary">
            #{tag}
          </Badge>
        ))}
        {ticket.rating ? (
          <Badge variant="success" title={ticket.ratingComment || undefined}>
            {t('plugin_ticket.rating.badge', { score: ticket.rating })}
          </Badge>
        ) : null}
      </div>

      {attachments.length ? (
        <div className="flex flex-wrap gap-2 border-b px-4 py-2">
          {attachments.map((a) => (
            <Button
              key={a.id}
              type="button"
              variant="outline"
              size="sm"
              onClick={() => void downloadAttachment(ticket.ticketNo, a)}
            >
              <Paperclip aria-hidden="true" />
              {a.internal ? (
                <Badge variant="warning" className="me-1">
                  {t('plugin_ticket.attachments.internal')}
                </Badge>
              ) : null}
              <span className="max-w-48 truncate">{a.name}</span>
              <span className="ms-1 text-[11px] text-muted-foreground">{formatSize(a.size)}</span>
            </Button>
          ))}
        </div>
      ) : null}

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
              <input
                ref={fileRef}
                type="file"
                className="hidden"
                accept="image/png,image/jpeg,image/gif,image/webp,application/pdf,text/plain,.zip,.docx,.xlsx,.pptx"
                onChange={(e) => {
                  const file = e.target.files?.[0]
                  if (file) void onUpload(file, internal)
                  e.target.value = ''
                }}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('plugin_ticket.attachments.add')}
                disabled={uploading}
                onClick={() => fileRef.current?.click()}
              >
                <Paperclip aria-hidden="true" />
              </Button>
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
