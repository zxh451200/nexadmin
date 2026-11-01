import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  ArrowUpRight,
  ChevronLeft,
  ChevronRight,
  CircleCheck,
  Copy,
  Download,
  Ellipsis,
  GitMerge,
  Link2,
  ListChecks,
  Paperclip,
  Plus,
  RotateCcw,
  Send,
  Settings2,
  SlidersHorizontal,
  StickyNote,
  Tag,
  Trash2,
  Unlink,
  Timer,
  UserRoundCheck,
  Users,
  X,
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
import { Popover, PopoverContent, PopoverTrigger } from '@/framework/ui/popover'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { Textarea } from '@/framework/ui/textarea'
import { cn } from '@/lib/utils'
import {
  downloadAttachment,
  exportTickets,
  isExportTooMany,
  useAddLink,
  useAssignGroup,
  useAssignTicket,
  useBizTypes,
  useBulkTickets,
  useCannedReplies,
  useCreateCanned,
  useCreateGroup,
  useCreateRule,
  useCreateTicket,
  useDeleteAttachment,
  useDeleteCanned,
  useDeleteField,
  useDeleteGroup,
  useDeleteRule,
  useDeleteSLAPolicy,
  useGroups,
  useLinkCandidates,
  useMergeCandidates,
  useMergeTicket,
  useRemoveLink,
  useReplies,
  useReplyTicket,
  useRules,
  useSLA,
  useSLAPolicies,
  useSaveField,
  useSaveSLA,
  useSaveSLAPolicy,
  useSetCustomValues,
  useSetGroupMembers,
  useSetTicketStatus,
  useSetTicketTags,
  useTicketFields,
  useTicketLinks,
  useTicketTags,
  useTickets,
  useUpdateCanned,
  useUpdateGroup,
  useUpdateRule,
  useUploadAttachment,
  type CannedReply,
  type LinksResult,
  type SLASetting,
  type Ticket,
  type TicketActivity,
  type TicketAttachment,
  type TicketField,
  type TicketFieldType,
  type TicketGroup,
  type TicketPriority,
  type TicketQueryParams,
  type TicketReply,
  type TicketRule,
} from './api'
import { formatTime, priorityVariant, statusVariant } from './format'
import { TicketStatsPanel } from './TicketStatsPanel'

const ALL = '__all'

/** 线程里的一条：客户/坐席说的话，或系统里发生的事。 */
type ThreadItem =
  | { kind: 'reply'; at: string; reply: TicketReply }
  | { kind: 'act'; at: string; act: TicketActivity }
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
  code: v.code ?? '',
  label: v.label ?? '',
  workdays: [...v.workdays],
  startMinute: v.startMinute,
  endMinute: v.endMinute,
  holidays: [...(v.holidays ?? [])],
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
  slaCode: '',
  userIds: [] as string[],
}

const ANY = '__any'
const NONE = '__none'

const FIELD_TYPES: TicketFieldType[] = ['text', 'number', 'select', 'date', 'bool']

const emptyFieldForm = {
  id: undefined as string | undefined,
  code: '',
  label: '',
  type: 'text' as TicketFieldType,
  // 候选项按行/逗号输入都可以（存进后端是数组），编辑态用整段文本最省事。
  optionsText: '',
  required: false,
  enabled: true,
  sort: 0,
}

/** 自定义字段的只读显示形态：未填、布尔与其余类型各走各的词。 */
const fieldValueText = (type: TicketFieldType, v: unknown, unset: string, yes: string, no: string) => {
  if (v === undefined || v === null || v === '') return unset
  if (type === 'bool') return v === true ? yes : no
  return String(v)
}

/**
 * 自定义字段的控件组：建单与「改附加字段」两处复用，渲染口径只有一份。
 * 单选的空值用哨兵而不是空串——Radix 的 SelectItem 不接受空 value。
 */
function FieldInputs({
  defs,
  values,
  onChange,
  disabled,
}: {
  defs: TicketField[]
  values: Record<string, unknown>
  onChange: (code: string, value: unknown) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  if (defs.length === 0) return null
  return (
    <>
      {defs.map((f) => (
        <Field key={f.code} label={f.label} required={f.required}>
          {f.type === 'select' ? (
            <SimpleSelect
              aria-label={f.label}
              value={String(values[f.code] ?? '') || NONE}
              onChange={(v) => onChange(f.code, v === NONE ? '' : v)}
              options={[{ value: NONE, label: t('plugin_ticket.fields.unset') }, ...f.options.map((o) => ({ value: o, label: o }))]}
              disabled={disabled}
            />
          ) : f.type === 'bool' ? (
            <label className="flex h-9 items-center gap-2 text-sm">
              <Checkbox
                checked={values[f.code] === true}
                onCheckedChange={(v) => onChange(f.code, v === true)}
                aria-label={f.label}
              />
              {t('plugin_ticket.fields.yes')}
            </label>
          ) : (
            <Input
              type={f.type === 'number' ? 'number' : f.type === 'date' ? 'date' : 'text'}
              value={String(values[f.code] ?? '')}
              onChange={(e) =>
                onChange(f.code, f.type === 'number' && e.target.value !== '' ? Number(e.target.value) : e.target.value)
              }
              aria-label={f.label}
              disabled={disabled}
            />
          )}
        </Field>
      ))}
    </>
  )
}

const emptyRuleForm = {
  id: undefined as string | undefined,
  name: '',
  enabled: true,
  sort: 0,
  matchPriority: '',
  matchBizType: '',
  matchKeyword: '',
  matchTags: [] as string[],
  setPriority: '',
  setGroupId: '0',
  note: '',
  addTags: [] as string[],
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
  const [filtersOpen, setFiltersOpen] = useState(false)
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
  const [exporting, setExporting] = useState(false)
  // 合并：只选未完结的主单；关联：可选任何状态的单。两个入口都在详情头部按钮组里。
  const [mergeOpen, setMergeOpen] = useState(false)
  const [mergeKeyword, setMergeKeyword] = useState('')
  const [mergeChoice, setMergeChoice] = useState('')
  const [linkOpen, setLinkOpen] = useState(false)
  const [linkKeyword, setLinkKeyword] = useState('')
  const [linkChoice, setLinkChoice] = useState('')
  // 自定义字段：定义管理 + 建单时填值 + 详情里改值。
  const [fieldsOpen, setFieldsOpen] = useState(false)
  const [fieldForm, setFieldForm] = useState(emptyFieldForm)
  const [createCustom, setCreateCustom] = useState<Record<string, unknown>>({})
  const [valuesTarget, setValuesTarget] = useState<Ticket | null>(null)
  const [valuesDraft, setValuesDraft] = useState<Record<string, unknown>>({})
  // SLA 对话框当前是在「新建一套策略」还是「改已有的一套」：编码只在新建时可填，
  // 改已有策略的编码等于另起一套、留下孤儿。
  const [policyNew, setPolicyNew] = useState(false)

  // 搜索防抖：300ms 内连续输入只发一次请求
  useEffect(() => {
    const id = window.setTimeout(() => setAppliedKeyword(keyword.trim()), 300)
    return () => window.clearTimeout(id)
  }, [keyword])

  // 任一筛选变化都回到第一页
  useEffect(() => {
    setPage(1)
  }, [appliedKeyword, statusFilter, priorityFilter, assigneeFilter, overdueOnly, groupFilter, tagFilter])

  const listFilters: Omit<TicketQueryParams, 'page' | 'pageSize'> = {
    status: statusFilter,
    priority: priorityFilter,
    keyword: appliedKeyword || undefined,
    overdue: overdueOnly ? '1' : undefined,
    unassigned: assigneeFilter === 'unassigned' ? '1' : undefined,
    mine: assigneeFilter === 'mine' ? '1' : undefined,
    groupId: groupFilter !== '0' ? groupFilter : undefined,
    tag: tagFilter !== '0' ? tagFilter : undefined,
  }

  const tickets = useTickets({ ...listFilters, page, pageSize: PAGE_SIZE })
  const list = tickets.data?.list ?? []
  const total = tickets.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  // 非首页删空后回退，避免停在空页
  useEffect(() => {
    if (page > pages) setPage(pages)
  }, [page, pages])

  // 导出复用列表当前的筛选对象：看到的和导出的必须同源，最多 5000 行由服务端把关。
  const onExport = async () => {
    setExporting(true)
    try {
      await exportTickets(listFilters)
    } catch (e) {
      toast.error(isExportTooMany(e) ? t('plugin_ticket.export.tooMany') : t('plugin_ticket.export.failed'))
    } finally {
      setExporting(false)
    }
  }

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
  const deleteAttachmentMut = useDeleteAttachment()
  const allTags = useTicketTags()
  const setTagsMut = useSetTicketTags()
  const fields = useTicketFields()
  const saveFieldMut = useSaveField()
  const deleteFieldMut = useDeleteField()
  const setValuesMut = useSetCustomValues()
  const slaPolicies = useSLAPolicies()
  const savePolicyMut = useSaveSLAPolicy()
  const deletePolicyMut = useDeleteSLAPolicy()
  const links = useTicketLinks(selectedNo)
  const mergeMut = useMergeTicket()
  const addLinkMut = useAddLink()
  const removeLinkMut = useRemoveLink()
  const mergeCandidates = useMergeCandidates(mergeKeyword.trim(), mergeOpen)
  const linkCandidates = useLinkCandidates(linkKeyword.trim(), linkOpen)

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
    setCreateCustom({})
    setCreateOpen(true)
  }

  const submitCreate = async () => {
    if (!createForm.title.trim() || !createForm.content.trim()) {
      toast.error(t('plugin_ticket.create.required'))
      return
    }
    // 空着的字段不提交：后端把「没填」和「填了空串」当成两件事（必填项会因此报错）。
    const customValues = Object.fromEntries(
      Object.entries(createCustom).filter(([, v]) => v !== '' && v !== undefined && v !== null),
    )
    try {
      const created = await createTicket.mutateAsync({
        title: createForm.title.trim(),
        content: createForm.content.trim(),
        priority: createForm.priority,
        reporterName: createForm.reporterName.trim() || undefined,
        reporterId: createForm.reporterId.trim() || undefined,
        bizType: createForm.bizType.trim() || undefined,
        bizNo: createForm.bizNo.trim() || undefined,
        customValues: Object.keys(customValues).length ? customValues : undefined,
      })
      toast.success(t('common.operateSuccess'))
      setCreateOpen(false)
      selectTicket(created.ticketNo)
    } catch (e) {
      // 必填缺失、类型不符都由后端按请求语言给句子，前端不重造措辞。
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const enabledFields = (fields.data ?? []).filter((f) => f.enabled)

  const openFields = () => {
    setFieldForm(emptyFieldForm)
    setFieldsOpen(true)
  }

  const editField = (row: TicketField) => {
    setFieldForm({
      id: row.id,
      code: row.code,
      label: row.label,
      type: row.type,
      optionsText: row.options.join(', '),
      required: row.required,
      enabled: row.enabled,
      sort: row.sort,
    })
  }

  const submitField = async () => {
    if (!fieldForm.code.trim() || !fieldForm.label.trim()) {
      toast.error(t('plugin_ticket.fields.requiredError'))
      return
    }
    const options = [...new Set(fieldForm.optionsText.split(/[,，\n]/).map((s) => s.trim()).filter(Boolean))]
    if (fieldForm.type === 'select' && options.length === 0) {
      toast.error(t('plugin_ticket.fields.optionsRequired'))
      return
    }
    try {
      await saveFieldMut.mutateAsync({
        id: fieldForm.id,
        code: fieldForm.code.trim(),
        label: fieldForm.label.trim(),
        type: fieldForm.type,
        options: fieldForm.type === 'select' ? options : [],
        required: fieldForm.required,
        enabled: fieldForm.enabled,
        sort: fieldForm.sort,
      })
      setFieldForm(emptyFieldForm)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const removeField = async (row: TicketField) => {
    if (!row.id) return
    if (!(await confirm({ description: t('plugin_ticket.fields.deleteConfirm', { label: row.label }) }))) return
    try {
      await deleteFieldMut.mutateAsync(row.id)
      if (fieldForm.id === row.id) setFieldForm(emptyFieldForm)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const openValues = (tk: Ticket) => {
    setValuesDraft({ ...(tk.customValues ?? {}) })
    setValuesTarget(tk)
  }

  const submitValues = async () => {
    if (!valuesTarget) return
    // 清空的项发 null：后端的合并语义里 null 才是「删掉这个字段」。
    const patch = Object.fromEntries(
      Object.entries(valuesDraft).map(([code, v]) => [code, v === '' || v === undefined ? null : v]),
    )
    try {
      await setValuesMut.mutateAsync({ ticketNo: valuesTarget.ticketNo, values: patch })
      setValuesTarget(null)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
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

  const removeAtt = async (att: TicketAttachment) => {
    if (!selectedNo) return
    if (!(await confirm({ description: t('plugin_ticket.attachments.deleteConfirm', { name: att.name }) }))) return
    try {
      await deleteAttachmentMut.mutateAsync({ ticketNo: selectedNo, token: att.token })
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      // 配额与删除失败都由后端按请求语言给句子（含「当前 N / 上限 M」），前端不重造措辞。
      toast.error(e instanceof Error && e.message ? e.message : t('plugin_ticket.attachments.deleteFailed'))
    }
  }

  // ---- 合并与关联 ----
  // 失败原因（自合并、已合并、目标已结束、成环、重复/超上限/不存在）后端已按请求语言给整句，
  // 这里只显示 e.message，不再拼一套自己的措辞。
  const openMerge = () => {
    setMergeKeyword('')
    setMergeChoice('')
    setMergeOpen(true)
  }

  const submitMerge = async () => {
    if (!selectedNo || !mergeChoice) return
    const ok = await confirm({
      title: t('plugin_ticket.merge.confirmTitle'),
      description: t('plugin_ticket.merge.confirm', { source: selectedNo, target: mergeChoice }),
      confirmText: t('plugin_ticket.merge.confirmButton'),
      destructive: true,
    })
    if (!ok) return
    try {
      const res = await mergeMut.mutateAsync({ ticketNo: selectedNo, intoNo: mergeChoice })
      toast.success(t('plugin_ticket.merge.done', { no: res.target, count: res.mergedCount }))
      setMergeOpen(false)
      // 合并痕迹要看的是主单：直接切过去，省得人去列表里找
      selectTicket(res.target)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('plugin_ticket.merge.failed'))
    }
  }

  const openLink = () => {
    setLinkKeyword('')
    setLinkChoice('')
    setLinkOpen(true)
  }

  const submitLink = async () => {
    if (!selectedNo || !linkChoice) return
    try {
      await addLinkMut.mutateAsync({ ticketNo: selectedNo, otherNo: linkChoice })
      toast.success(t('plugin_ticket.link.done', { no: linkChoice }))
      setLinkOpen(false)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('plugin_ticket.link.failed'))
    }
  }

  const removeLink = async (otherNo: string) => {
    if (!selectedNo) return
    const ok = await confirm({ description: t('plugin_ticket.link.removeConfirm', { no: otherNo }), destructive: true })
    if (!ok) return
    try {
      await removeLinkMut.mutateAsync({ ticketNo: selectedNo, otherNo })
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('plugin_ticket.link.removeFailed'))
    }
  }

  const openSLA = () => {
    setSlaForm(cloneSLA(slaQuery.data ?? defaultSLA))
    setPolicyNew(false)
    setSlaOpen(true)
  }

  // 切到哪一套策略就把哪一套装进表单；列表里没有的那套（刚删掉）回落默认。
  const pickPolicy = (code: string) => {
    const row = (slaPolicies.data ?? []).find((p) => (p.code ?? '') === code)
    setSlaForm(cloneSLA(row ?? slaQuery.data ?? defaultSLA))
    setPolicyNew(false)
  }

  const newPolicy = () => {
    setSlaForm({ ...cloneSLA(slaQuery.data ?? defaultSLA), code: '', label: '', holidays: [] })
    setPolicyNew(true)
  }

  const removePolicy = async (code: string) => {
    if (!(await confirm({ description: t('plugin_ticket.sla.deletePolicyConfirm', { code }) }))) return
    try {
      await deletePolicyMut.mutateAsync(code)
      pickPolicy('')
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const addHoliday = () => setSlaForm((f) => ({ ...f, holidays: [...(f.holidays ?? []), ''] }))
  const setHoliday = (i: number, v: string) =>
    setSlaForm((f) => ({ ...f, holidays: (f.holidays ?? []).map((h, k) => (k === i ? v : h)) }))
  const removeHoliday = (i: number) =>
    setSlaForm((f) => ({ ...f, holidays: (f.holidays ?? []).filter((_, k) => k !== i) }))

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
    const code = (slaForm.code ?? '').trim()
    if (code && !/^[a-z0-9_-]{1,32}$/.test(code)) {
      toast.error(t('plugin_ticket.sla.codeInvalid'))
      return
    }
    const days = (slaForm.holidays ?? []).map((h) => h.trim()).filter(Boolean)
    if (days.some((h) => !/^\d{4}-\d{2}-\d{2}$/.test(h))) {
      toast.error(t('plugin_ticket.sla.holidayInvalid'))
      return
    }
    const payload: SLASetting = { ...slaForm, code, label: (slaForm.label ?? '').trim(), holidays: [...new Set(days)].sort() }
    try {
      // 有编码就是命名策略（POST /sla/policy）；空编码仍是整份默认策略（PUT /sla）。
      if (payload.code) await savePolicyMut.mutateAsync(payload)
      else await saveSLA.mutateAsync(payload)
      toast.success(t('common.operateSuccess'))
      setSlaOpen(false)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
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
      slaCode: g.slaCode ?? '',
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
        slaCode: groupForm.slaCode,
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
      matchTags: [...(r.matchTags ?? [])],
      setPriority: r.setPriority,
      setGroupId: r.setGroupId && r.setGroupId !== '0' ? r.setGroupId : '0',
      note: r.note,
      addTags: [...(r.addTags ?? [])],
    })
  }

  /** 与后端 normalizeTags 同一口径：不含逗号、每个 ≤20 字、总量 ≤10；不合规则当场拦住 */
  const tagsValid = (tags: string[]) =>
    tags.length <= 10 && tags.every((tag) => tag.trim() && !tag.includes(',') && [...tag.trim()].length <= 20)

  const submitRule = async () => {
    // addTags 也是动作：只打标的规则同样合法（与后端 normalize 一致）
    const hasAction =
      Boolean(ruleForm.setPriority || (ruleForm.setGroupId && ruleForm.setGroupId !== '0') || ruleForm.note.trim()) ||
      ruleForm.addTags.length > 0
    if (!ruleForm.name.trim() || !hasAction) {
      toast.error(t('plugin_ticket.rules.invalid'))
      return
    }
    if (!tagsValid(ruleForm.matchTags) || !tagsValid(ruleForm.addTags)) {
      toast.error(t('plugin_ticket.tags.invalid'))
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
        matchTags: ruleForm.matchTags,
        setPriority: ruleForm.setPriority,
        setGroupId: Number(ruleForm.setGroupId || 0),
        note: ruleForm.note.trim(),
        addTags: ruleForm.addTags,
      }
      if (ruleForm.id) await updateRule.mutateAsync({ id: ruleForm.id, ...input })
      else await createRule.mutateAsync(input)
      setRuleForm(emptyRuleForm)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
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

  const ruleSummary = (r: TicketRule): string => {
    const condition = [
      r.matchPriority ? t(`plugin_ticket.priority.${r.matchPriority}`) : t('plugin_ticket.rules.any'),
      r.matchBizType || '-',
      r.matchKeyword || '-',
      // 标签条件是 AND，摘要里把「全部命中」写出来，别让人猜
      r.matchTags?.length ? `${t('plugin_ticket.rules.matchTags')}: ${r.matchTags.map((x) => `#${x}`).join(' + ')}` : '-',
    ].join(' · ')
    const action = [
      r.setPriority ? t(`plugin_ticket.priority.${r.setPriority}`) : '',
      r.setGroupId && r.setGroupId !== '0'
        ? (groups.data?.find((g) => g.id === r.setGroupId)?.name ?? `#${r.setGroupId}`)
        : '',
      r.note,
      r.addTags?.length ? `${t('plugin_ticket.rules.addTags')}: ${r.addTags.map((x) => `#${x}`).join(' ')}` : '',
    ]
      .filter(Boolean)
      .join(' / ')
    return `${condition} → ${action}`
  }

  // 停用组里可能还有存量工单：筛选保留并标注；转组只列启用的，当前所在的停用组除外。
  const groupLabel = (g: TicketGroup) =>
    g.enabled ? g.name : `${g.name} · ${t('plugin_ticket.groups.disabled')}`

  // 下拉收进弹出面板后，屏上要仍看得出「筛了什么、怎么取消」：生效条件一枚 chip，点一下就撤掉该条件。
  const activeFilters: { key: string; label: string; value?: string; clear: () => void }[] = []
  if (statusFilter) {
    activeFilters.push({
      key: 'status',
      label: t('plugin_ticket.fields.status'),
      value: t(`plugin_ticket.status.${statusFilter}`),
      clear: () => setStatusFilter(undefined),
    })
  }
  if (priorityFilter) {
    activeFilters.push({
      key: 'priority',
      label: t('plugin_ticket.fields.priority'),
      value: t(`plugin_ticket.priority.${priorityFilter}`),
      clear: () => setPriorityFilter(undefined),
    })
  }
  if (assigneeFilter !== 'all') {
    activeFilters.push({
      key: 'assignee',
      label: t('plugin_ticket.assignee'),
      value: t(assigneeFilter === 'mine' ? 'plugin_ticket.assigneeMine' : 'plugin_ticket.assigneeUnassigned'),
      clear: () => setAssigneeFilter('all'),
    })
  }
  if (groupFilter !== '0') {
    const g = (groups.data ?? []).find((x) => x.id === groupFilter)
    activeFilters.push({
      key: 'group',
      label: t('plugin_ticket.groups.label'),
      value: g ? groupLabel(g) : groupFilter,
      clear: () => setGroupFilter('0'),
    })
  }
  if (tagFilter !== '0') {
    activeFilters.push({
      key: 'tag',
      label: t('plugin_ticket.tags.title'),
      value: `#${tagFilter}`,
      clear: () => setTagFilter('0'),
    })
  }
  if (overdueOnly) {
    activeFilters.push({ key: 'overdue', label: t('plugin_ticket.overdue'), clear: () => setOverdueOnly(false) })
  }

  const resetFilters = () => {
    setStatusFilter(undefined)
    setPriorityFilter(undefined)
    setAssigneeFilter('all')
    setGroupFilter('0')
    setTagFilter('0')
    setOverdueOnly(false)
  }

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
            <Button variant="outline" onClick={() => void onExport()} disabled={exporting}>
              <Download aria-hidden="true" />
              <span className="max-sm:sr-only">{t('plugin_ticket.export.action')}</span>
            </Button>
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
              <Button variant="outline" onClick={openFields}>
                <ListChecks aria-hidden="true" />
                {t('plugin_ticket.fields.title')}
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
                {/* 六个维度平铺在 24rem 窄栏里要占三四行，队列被顶出视口；收成一枚按钮 + 已选 chips，
                    列表高度与筛选是否生效都仍然一眼可见 */}
                <Popover open={filtersOpen} onOpenChange={setFiltersOpen}>
                  <PopoverTrigger asChild>
                    <Button variant="outline" className="h-10 shrink-0 md:h-9">
                      <SlidersHorizontal aria-hidden="true" />
                      {t('plugin_ticket.filter.title')}
                      {activeFilters.length > 0 ? (
                        <>
                          <Badge variant="secondary" aria-hidden="true" className="ms-1 min-w-5 justify-center">
                            {activeFilters.length}
                          </Badge>
                          {/* 屏下只念「筛选 1」听不出 1 是什么，补一句可读的计数 */}
                          <span className="sr-only">
                            {t('plugin_ticket.filter.activeCount', { count: activeFilters.length })}
                          </span>
                        </>
                      ) : null}
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent align="end" className="w-72 space-y-2">
                    <SimpleSelect
                      aria-label={t('plugin_ticket.assigneeFilter')}
                      label={t('plugin_ticket.assignee')}
                      className="w-full"
                      value={assigneeFilter}
                      onChange={(v) => setAssigneeFilter(v as AssigneeFilter)}
                      options={[
                        { value: 'all', label: t('plugin_ticket.all') },
                        { value: 'unassigned', label: t('plugin_ticket.assigneeUnassigned') },
                        { value: 'mine', label: t('plugin_ticket.assigneeMine') },
                      ]}
                    />
                    <SimpleSelect
                      aria-label={t('plugin_ticket.groups.label')}
                      label={t('plugin_ticket.groups.label')}
                      className="w-full"
                      value={groupFilter}
                      onChange={setGroupFilter}
                      options={[
                        { value: '0', label: t('plugin_ticket.all') },
                        ...(groups.data ?? []).map((g) => ({ value: g.id, label: groupLabel(g) })),
                      ]}
                    />
                    <SimpleSelect
                      aria-label={t('plugin_ticket.tags.title')}
                      label={t('plugin_ticket.tags.title')}
                      className="w-full"
                      value={tagFilter}
                      onChange={setTagFilter}
                      options={[
                        { value: '0', label: t('plugin_ticket.all') },
                        ...(allTags.data ?? []).map((tag) => ({ value: tag, label: tag })),
                      ]}
                    />
                    <SimpleSelect
                      aria-label={t('plugin_ticket.fields.status')}
                      label={t('plugin_ticket.fields.status')}
                      className="w-full"
                      value={statusFilter ?? ALL}
                      onChange={(v) => setStatusFilter(v === ALL ? undefined : v)}
                      options={[
                        { value: ALL, label: t('plugin_ticket.all') },
                        ...STATUSES.map((s) => ({ value: s, label: t(`plugin_ticket.status.${s}`) })),
                      ]}
                    />
                    <SimpleSelect
                      aria-label={t('plugin_ticket.fields.priority')}
                      label={t('plugin_ticket.fields.priority')}
                      className="w-full"
                      value={priorityFilter ?? ALL}
                      onChange={(v) => setPriorityFilter(v === ALL ? undefined : v)}
                      options={[
                        { value: ALL, label: t('plugin_ticket.all') },
                        ...PRIORITIES.map((p) => ({ value: p, label: t(`plugin_ticket.priority.${p}`) })),
                      ]}
                    />
                    <label className="flex h-9 items-center gap-2 rounded-md border border-input px-3 text-sm">
                      <Checkbox
                        checked={overdueOnly}
                        onCheckedChange={(v) => setOverdueOnly(v === true)}
                        aria-label={t('plugin_ticket.overdue')}
                      />
                      {t('plugin_ticket.overdue')}
                    </label>
                    <div className="flex items-center justify-between border-t pt-2 text-sm">
                      <span className="text-muted-foreground">
                        {t('plugin_ticket.filter.activeCount', { count: activeFilters.length })}
                      </span>
                      <Button variant="ghost" size="sm" onClick={resetFilters} disabled={activeFilters.length === 0}>
                        {t('common.reset')}
                      </Button>
                    </div>
                  </PopoverContent>
                </Popover>
              </div>

              {activeFilters.length > 0 ? (
                <div className="flex flex-wrap items-center gap-1.5">
                  {activeFilters.map((f) => (
                    <button
                      key={f.key}
                      type="button"
                      onClick={f.clear}
                      aria-label={`${t('plugin_ticket.filter.remove')} ${f.label}${f.value ? ` ${f.value}` : ''}`}
                      className="flex h-7 max-w-full items-center gap-1 rounded-full border border-input bg-muted/60 px-2.5 text-xs hover:bg-muted"
                    >
                      <span className="shrink-0 text-muted-foreground">{f.label}</span>
                      {f.value ? <span className="truncate">{f.value}</span> : null}
                      <X aria-hidden="true" className="size-3 shrink-0" />
                    </button>
                  ))}
                </div>
              ) : null}

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
                    onOpenTarget={(no) => selectTicket(no)}
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
              fields={enabledFields}
              onEditValues={openValues}
              canned={canned.data ?? []}
              onManageCanned={() => setCannedOpen(true)}
              attachments={replies.data?.attachments ?? []}
              activities={replies.data?.activities ?? []}
              quota={replies.data?.quota}
              onUpload={uploadAtt}
              onDeleteAttachment={removeAtt}
              uploading={uploadAttachmentMut.isPending}
              links={links.data}
              onOpenMerge={openMerge}
              onOpenLink={openLink}
              onUnlink={(otherNo) => void removeLink(otherNo)}
              onOpenTicket={(no) => selectTicket(no)}
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
          {/* 自定义字段：管理员定义的附加信息，建单时一并填；必填与类型由后端把关。 */}
          {enabledFields.length > 0 ? (
            <div className="grid gap-3 sm:grid-cols-2">
              <FieldInputs
                defs={enabledFields}
                values={createCustom}
                onChange={(code, v) => setCreateCustom((c) => ({ ...c, [code]: v }))}
              />
            </div>
          ) : null}
        </div>
      </ResponsiveDialog>

      {/* 自定义字段定义：加字段不动表结构，值统一存在工单的 custom_values 里。 */}
      <ResponsiveDialog
        open={fieldsOpen}
        onOpenChange={setFieldsOpen}
        title={t('plugin_ticket.fields.title')}
        description={t('plugin_ticket.fields.description')}
        size="lg"
        footer={
          <Button variant="outline" onClick={() => setFieldsOpen(false)}>
            {t('common.close')}
          </Button>
        }
      >
        <div className="space-y-4">
          {(fields.data ?? []).length === 0 ? (
            <EmptyState title={t('plugin_ticket.fields.empty')} className="py-6" />
          ) : (
            <div className="divide-y">
              {(fields.data ?? []).map((f) => (
                <div key={f.id ?? f.code} className="flex flex-wrap items-center gap-2 py-2">
                  <span className={cn('font-medium', !f.enabled && 'text-muted-foreground')}>{f.label}</span>
                  <span className="font-mono text-xs text-muted-foreground">{f.code}</span>
                  <Badge variant="secondary">{t(`plugin_ticket.fields.types.${f.type}`)}</Badge>
                  {f.required ? <Badge variant="outline">{t('plugin_ticket.fields.required')}</Badge> : null}
                  {!f.enabled ? <Badge variant="outline">{t('plugin_ticket.fields.disabled')}</Badge> : null}
                  {f.options.length > 0 ? (
                    <span className="text-xs text-muted-foreground">{f.options.join(' / ')}</span>
                  ) : null}
                  <span className="ms-auto flex items-center gap-1">
                    <Button variant="ghost" size="sm" onClick={() => editField(f)}>
                      {t('common.edit')}
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => void removeField(f)} aria-label={t('common.delete')}>
                      <Trash2 aria-hidden="true" />
                    </Button>
                  </span>
                </div>
              ))}
            </div>
          )}

          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('plugin_ticket.fields.code')}>
              <Input
                value={fieldForm.code}
                onChange={(e) => setFieldForm((f) => ({ ...f, code: e.target.value }))}
                maxLength={32}
                disabled={!!fieldForm.id}
                placeholder="order_channel"
              />
            </Field>
            <Field label={t('plugin_ticket.fields.label')}>
              <Input value={fieldForm.label} onChange={(e) => setFieldForm((f) => ({ ...f, label: e.target.value }))} maxLength={64} />
            </Field>
            <Field label={t('plugin_ticket.fields.type')}>
              <SimpleSelect
                aria-label={t('plugin_ticket.fields.type')}
                value={fieldForm.type}
                onChange={(v) => setFieldForm((f) => ({ ...f, type: v as TicketFieldType }))}
                options={FIELD_TYPES.map((tp) => ({ value: tp, label: t(`plugin_ticket.fields.types.${tp}`) }))}
              />
            </Field>
            <Field label={t('plugin_ticket.fields.sort')}>
              <Input
                type="number"
                min={0}
                value={String(fieldForm.sort)}
                onChange={(e) => setFieldForm((f) => ({ ...f, sort: Math.max(0, Number(e.target.value) || 0) }))}
              />
            </Field>
          </div>
          {fieldForm.type === 'select' ? (
            <Field label={t('plugin_ticket.fields.options')}>
              <Input
                value={fieldForm.optionsText}
                onChange={(e) => setFieldForm((f) => ({ ...f, optionsText: e.target.value }))}
                placeholder={t('plugin_ticket.fields.optionsPlaceholder')}
              />
            </Field>
          ) : null}
          <div className="flex flex-wrap items-center gap-4">
            <label className="flex items-center gap-1.5 text-sm">
              <Checkbox
                checked={fieldForm.required}
                onCheckedChange={(v) => setFieldForm((f) => ({ ...f, required: v === true }))}
              />
              {t('plugin_ticket.fields.required')}
            </label>
            <label className="flex items-center gap-1.5 text-sm">
              <Checkbox
                checked={fieldForm.enabled}
                onCheckedChange={(v) => setFieldForm((f) => ({ ...f, enabled: v === true }))}
              />
              {t('plugin_ticket.fields.enabled')}
            </label>
            <Button size="sm" disabled={saveFieldMut.isPending} onClick={() => void submitField()}>
              {fieldForm.id ? t('common.save') : t('plugin_ticket.fields.add')}
            </Button>
            {fieldForm.id ? (
              <Button size="sm" variant="ghost" onClick={() => setFieldForm(emptyFieldForm)}>
                {t('common.cancel')}
              </Button>
            ) : null}
          </div>
        </div>
      </ResponsiveDialog>

      {/* 改附加字段：清空某项再保存就是删掉它（后端的合并语义里 null 才是删除）。 */}
      <ResponsiveDialog
        open={!!valuesTarget}
        onOpenChange={(o) => {
          if (!o) setValuesTarget(null)
        }}
        title={t('plugin_ticket.fields.editValues')}
        description={t('plugin_ticket.fields.editValuesFor', { no: valuesTarget?.ticketNo ?? '' })}
        footer={
          <>
            <Button variant="outline" onClick={() => setValuesTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={setValuesMut.isPending} onClick={() => void submitValues()}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <FieldInputs
            defs={enabledFields}
            values={valuesDraft}
            onChange={(code, v) => setValuesDraft((d) => ({ ...d, [code]: v }))}
          />
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
          {/* 多套策略：默认策略就是原来那一份；命名策略绑给队列，各队列时限与节假日可以不同。 */}
          <div className="flex flex-wrap items-center gap-2">
            <SimpleSelect
              aria-label={t('plugin_ticket.sla.policy')}
              label={t('plugin_ticket.sla.policy')}
              value={slaForm.code ? slaForm.code : NONE}
              onChange={(v) => pickPolicy(v === NONE ? '' : v)}
              options={[
                { value: NONE, label: t('plugin_ticket.sla.policyDefault') },
                ...(slaPolicies.data ?? [])
                  .filter((p) => p.code)
                  .map((p) => ({ value: p.code ?? '', label: p.label || (p.code ?? '') })),
              ]}
            />
            <Button variant="outline" size="sm" onClick={newPolicy}>
              <Plus aria-hidden="true" />
              {t('plugin_ticket.sla.newPolicy')}
            </Button>
            {slaForm.code ? (
              <Button
                variant="outline"
                size="sm"
                disabled={deletePolicyMut.isPending}
                onClick={() => void removePolicy(slaForm.code ?? '')}
              >
                <Trash2 aria-hidden="true" />
                {t('plugin_ticket.sla.deletePolicy')}
              </Button>
            ) : null}
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('plugin_ticket.sla.code')}>
              <Input
                value={slaForm.code ?? ''}
                onChange={(e) => setSlaForm((f) => ({ ...f, code: e.target.value }))}
                maxLength={32}
                disabled={!policyNew}
                placeholder="vip-desk"
              />
            </Field>
            <Field label={t('plugin_ticket.sla.label')}>
              <Input
                value={slaForm.label ?? ''}
                onChange={(e) => setSlaForm((f) => ({ ...f, label: e.target.value }))}
                maxLength={64}
              />
            </Field>
          </div>
          <Field label={t('plugin_ticket.sla.holidays')}>
            <div className="space-y-2">
              {(slaForm.holidays ?? []).map((h, i) => (
                <div key={`holiday-${i}`} className="flex items-center gap-2">
                  <Input
                    type="date"
                    value={h}
                    onChange={(e) => setHoliday(i, e.target.value)}
                    aria-label={`${t('plugin_ticket.sla.holidays')} ${i + 1}`}
                  />
                  <Button variant="ghost" size="icon" onClick={() => removeHoliday(i)} aria-label={t('common.delete')}>
                    <Trash2 aria-hidden="true" />
                  </Button>
                </div>
              ))}
              <Button variant="outline" size="sm" onClick={addHoliday}>
                <Plus aria-hidden="true" />
                {t('plugin_ticket.sla.addHoliday')}
              </Button>
              <p className="text-xs text-muted-foreground">{t('plugin_ticket.sla.holidaysHint')}</p>
            </div>
          </Field>
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
            <Field label={t('plugin_ticket.groups.slaPolicy')}>
              <SimpleSelect
                aria-label={t('plugin_ticket.groups.slaPolicy')}
                value={groupForm.slaCode || NONE}
                onChange={(v) => setGroupForm((f) => ({ ...f, slaCode: v === NONE ? '' : v }))}
                options={[
                  { value: NONE, label: t('plugin_ticket.sla.policyDefault') },
                  ...(slaPolicies.data ?? [])
                    .filter((p) => p.code)
                    .map((p) => ({ value: p.code ?? '', label: p.label || (p.code ?? '') })),
                ]}
              />
              <p className="mt-1 text-xs text-muted-foreground">{t('plugin_ticket.groups.slaPolicyHint')}</p>
            </Field>
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
            {/* 标签条件：用 div 而不是 Field，避免 label 里套可点击控件 */}
            <div className="space-y-1">
              <span className="text-xs font-medium text-foreground">{t('plugin_ticket.rules.matchTags')}</span>
              <TagMultiSelect
                value={ruleForm.matchTags}
                onChange={(tags) => setRuleForm((f) => ({ ...f, matchTags: tags }))}
                options={allTags.data ?? []}
                hint={t('plugin_ticket.rules.matchTagsHint')}
              />
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
            <div className="space-y-1">
              <span className="text-xs font-medium text-foreground">{t('plugin_ticket.rules.addTags')}</span>
              <TagMultiSelect
                value={ruleForm.addTags}
                onChange={(tags) => setRuleForm((f) => ({ ...f, addTags: tags }))}
                options={allTags.data ?? []}
                allowCreate
                hint={t('plugin_ticket.rules.addTagsHint')}
              />
            </div>
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

      {/* 合并重复工单：只列未完结的主单，提交前二次确认，且不可撤销 */}
      <ResponsiveDialog
        open={mergeOpen}
        onOpenChange={setMergeOpen}
        title={t('plugin_ticket.merge.dialogTitle')}
        description={t('plugin_ticket.merge.description', { no: selectedNo ?? '' })}
        size="md"
        footer={
          <>
            <Button variant="outline" onClick={() => setMergeOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={!mergeChoice || mergeMut.isPending} onClick={() => void submitMerge()}>
              <GitMerge aria-hidden="true" />
              {t('plugin_ticket.merge.confirmButton')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <p className="rounded-md bg-muted px-2.5 py-2 text-xs text-muted-foreground">{t('plugin_ticket.merge.warning')}</p>
          <Input
            value={mergeKeyword}
            onChange={(e) => setMergeKeyword(e.target.value)}
            placeholder={t('plugin_ticket.merge.searchPlaceholder')}
            aria-label={t('plugin_ticket.merge.searchPlaceholder')}
          />
          <p className="text-xs font-medium text-muted-foreground">{t('plugin_ticket.merge.candidates')}</p>
          <div className="max-h-72 divide-y divide-border/60 overflow-y-auto">
            {mergeCandidates.list
              .filter((tk) => tk.ticketNo !== selectedNo && !tk.mergedIntoNo)
              .map((tk) => (
                <button
                  key={tk.id}
                  type="button"
                  onClick={() => setMergeChoice(tk.ticketNo)}
                  className={cn(
                    'flex w-full min-w-0 flex-wrap items-center gap-2 px-2 py-2 text-start text-xs hover:bg-accent',
                    mergeChoice === tk.ticketNo && 'bg-accent',
                  )}
                >
                  <span className="font-mono">{tk.ticketNo}</span>
                  <span className="min-w-24 max-w-full flex-1 truncate">{tk.title}</span>
                  <Badge variant={statusVariant(tk.status)}>{t(`plugin_ticket.status.${tk.status}`)}</Badge>
                </button>
              ))}
            {!mergeCandidates.isLoading && mergeCandidates.list.filter((tk) => tk.ticketNo !== selectedNo).length === 0 ? (
              <p className="px-2 py-3 text-xs text-muted-foreground">{t('plugin_ticket.merge.noCandidates')}</p>
            ) : null}
          </div>
          {mergeChoice ? (
            <p className="text-xs text-muted-foreground">{t('plugin_ticket.merge.picked', { no: mergeChoice })}</p>
          ) : null}
        </div>
      </ResponsiveDialog>

      {/* 关联工单：双向、可解除，任何状态都能关联（终态单正是最需要留痕的场景） */}
      <ResponsiveDialog
        open={linkOpen}
        onOpenChange={setLinkOpen}
        title={t('plugin_ticket.link.dialogTitle')}
        description={t('plugin_ticket.link.description')}
        size="md"
        footer={
          <>
            <Button variant="outline" onClick={() => setLinkOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={!linkChoice || addLinkMut.isPending} onClick={() => void submitLink()}>
              <Link2 aria-hidden="true" />
              {t('plugin_ticket.link.confirmButton')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Input
            value={linkKeyword}
            onChange={(e) => setLinkKeyword(e.target.value)}
            placeholder={t('plugin_ticket.link.searchPlaceholder')}
            aria-label={t('plugin_ticket.link.searchPlaceholder')}
          />
          <p className="text-xs font-medium text-muted-foreground">{t('plugin_ticket.link.results')}</p>
          <div className="max-h-72 divide-y divide-border/60 overflow-y-auto">
            {(linkCandidates.data?.list ?? [])
              .filter((tk) => tk.ticketNo !== selectedNo)
              .map((tk) => (
                <button
                  key={tk.id}
                  type="button"
                  onClick={() => setLinkChoice(tk.ticketNo)}
                  className={cn(
                    'flex w-full min-w-0 flex-wrap items-center gap-2 px-2 py-2 text-start text-xs hover:bg-accent',
                    linkChoice === tk.ticketNo && 'bg-accent',
                  )}
                >
                  <span className="font-mono">{tk.ticketNo}</span>
                  <span className="min-w-24 max-w-full flex-1 truncate">{tk.title}</span>
                  <Badge variant={statusVariant(tk.status)}>{t(`plugin_ticket.status.${tk.status}`)}</Badge>
                </button>
              ))}
            {!linkCandidates.isLoading && (linkCandidates.data?.list?.length ?? 0) === 0 ? (
              <p className="px-2 py-3 text-xs text-muted-foreground">{t('plugin_ticket.link.none')}</p>
            ) : null}
          </div>
          {linkChoice ? <p className="text-xs text-muted-foreground">{t('plugin_ticket.link.picked', { no: linkChoice })}</p> : null}
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
  onOpenTarget,
}: {
  ticket: Ticket
  active: boolean
  checked: boolean
  onToggle: (on: boolean) => void
  onSelect: () => void
  onOpenTarget: (ticketNo: string) => void
}) {
  const { t, i18n } = useTranslation()
  return (
    <div
      className={cn(
        'flex flex-col gap-1 rounded-md px-2 py-2 transition-colors hover:bg-accent',
        active && 'bg-accent',
      )}
    >
      <div className="flex items-start gap-2">
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
      {/* 被合并的单在队列里要一眼认出来，并且一点就到主单；独立成行，避免按钮里套按钮 */}
      {ticket.mergedIntoNo ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-6 self-start max-w-full justify-start gap-1 px-1.5 text-[11px] text-muted-foreground hover:text-foreground"
          onClick={() => onOpenTarget(ticket.mergedIntoNo as string)}
        >
          <GitMerge aria-hidden="true" className="h-3 w-3 shrink-0" />
          <span className="truncate">
            {t('plugin_ticket.merge.mergedBadge', { no: ticket.mergedIntoNo })}
            <ArrowUpRight aria-hidden="true" className="ms-1 inline h-3 w-3" />
          </span>
        </Button>
      ) : null}
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
  fields,
  onEditValues,
  canned,
  onManageCanned,
  attachments,
  activities,
  quota,
  onUpload,
  onDeleteAttachment,
  uploading,
  links,
  onOpenMerge,
  onOpenLink,
  onUnlink,
  onOpenTicket,
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
  fields: TicketField[]
  onEditValues: (ticket: Ticket) => void
  canned: CannedReply[]
  onManageCanned: () => void
  attachments: TicketAttachment[]
  activities: TicketActivity[]
  quota?: { maxCount: number; maxBytes: number }
  onUpload: (file: File, internal: boolean) => Promise<void>
  onDeleteAttachment: (att: TicketAttachment) => Promise<void>
  uploading: boolean
  links?: LinksResult
  onOpenMerge: () => void
  onOpenLink: () => void
  onUnlink: (otherNo: string) => void
  onOpenTicket: (ticketNo: string) => void
}) {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState('')
  const [internal, setInternal] = useState(false)
  const bottomRef = useRef<HTMLDivElement | null>(null)
  const fileRef = useRef<HTMLInputElement | null>(null)

  // 动态与回复按时间合并成一条流：坐席要看的是「这一单发生了什么」，不是两张表顺序拼接。
  const stream: ThreadItem[] = [
    ...replies.map((reply) => ({ kind: 'reply' as const, at: reply.createdAt, reply })),
    ...activities.map((act) => ({ kind: 'act' as const, at: act.createdAt, act })),
  ].sort((a, b) => a.at.localeCompare(b.at))

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [stream.length, ticket?.ticketNo])

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

  // 复制工单号：客户自助查询与关单确认都要念它，不该让人手抄 20 位。
  const copyTicketNo = async (no: string) => {
    try {
      await navigator.clipboard.writeText(no)
      toast.success(t('plugin_ticket.copied'))
    } catch {
      // 非安全上下文或用户没给权限时浏览器会拒，提示手动选择复制而不是假装成功
      toast.error(t('plugin_ticket.copyFailed'))
    }
  }

  const activityValue = (action: string, value: string) => {
    if (!value) return t('plugin_ticket.activity.empty')
    if (action === 'status_changed') return t(`plugin_ticket.status.${value}`, value)
    if (action === 'priority_changed') return t(`plugin_ticket.priority.${value}`, value)
    return value
  }

  const activityLabel = (a: TicketActivity) => {
    const from = activityValue(a.action, a.fromValue)
    const to = activityValue(a.action, a.toValue)
    switch (a.action) {
      case 'created':
        return t('plugin_ticket.activity.created')
      case 'status_changed':
        return t('plugin_ticket.activity.statusChanged', { from, to })
      case 'priority_changed':
        return t('plugin_ticket.activity.priorityChanged', { from, to })
      case 'assignee_changed':
        return t('plugin_ticket.activity.assigneeChanged', { from, to })
      case 'group_changed':
        return t('plugin_ticket.activity.groupChanged', { from, to })
      case 'tags_changed':
        return t('plugin_ticket.activity.tagsChanged', { from, to })
      case 'custom_fields_changed':
        return t('plugin_ticket.activity.fieldsChanged', { from, to })
      case 'attachment_added':
        return t('plugin_ticket.activity.attachmentAdded', { name: a.detail })
      case 'attachment_removed':
        return t('plugin_ticket.activity.attachmentRemoved', { name: a.detail })
      // 合并与关联：主单号/对侧单号在 detail 与 to/from 里，按 merge.go 的落法取
      case 'merged_into':
        return t('plugin_ticket.activity.mergedInto', { no: a.detail })
      case 'merged_tickets':
        // 变量名不用 count：i18next 会把数字 count 当复数索引去找 _one/_other 键
        return t('plugin_ticket.activity.mergedTickets', { total: to, no: a.detail })
      case 'linked':
        return t('plugin_ticket.activity.linked', { no: to })
      case 'unlinked':
        return t('plugin_ticket.activity.unlinked', { no: from })
      case 'rating_added':
        return t('plugin_ticket.activity.ratingAdded', { score: to })
      case 'rule_applied':
        return t('plugin_ticket.activity.ruleApplied', { rule: to })
      default:
        // 后端新增动作码时前端不该整条丢掉：至少把原值显示出来。
        return `${a.action}${to ? `: ${to}` : ''}`
    }
  }

  const insertCanned = (id: string) => {
    const row = canned.find((c) => c.id === id)
    if (row) setDraft(row.content)
  }

  return (
    <Card className="flex min-h-0 flex-col">
      <div className="space-y-2 border-b px-4 py-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate text-sm font-medium text-foreground" title={ticket.title}>
              {ticket.title}
            </span>
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
          {/* 工单号是客户自助查询和关单确认要念的东西，不能跟着标题一起被省略号吃掉 */}
          <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
            <span className="inline-flex items-center gap-1">
              <span className="font-mono">{ticket.ticketNo}</span>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="h-5 w-5 text-muted-foreground hover:text-foreground"
                aria-label={t('plugin_ticket.copyNo')}
                onClick={() => void copyTicketNo(ticket.ticketNo)}
              >
                <Copy aria-hidden="true" className="h-3 w-3" />
              </Button>
            </span>
            <span>
              {ticket.reporterName || t(`plugin_ticket.repliers.${ticket.reporterType}`)}
              {ticket.bizNo ? ` · ${ticket.bizType}/${ticket.bizNo}` : ''}
              {ticket.assigneeName || (ticket.assigneeId && ticket.assigneeId !== '0')
                ? ` · ${t('plugin_ticket.assignee')}: ${ticket.assigneeName || `#${ticket.assigneeId}`}`
                : ` · ${t('plugin_ticket.assignee')}: ${t('plugin_ticket.assigneeUnassigned')}`}
              {ticket.groupName ? ` · ${t('plugin_ticket.groups.label')}: ${ticket.groupName}` : ''}
              {ticket.creatorName ? ` · ${t('plugin_ticket.createdBy')}: ${ticket.creatorName}` : ''}
            </span>
          </div>
          {fields.length > 0 ? (
            <div className="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
              {fields.map((f) => (
                <span key={f.code}>
                  {t('plugin_ticket.fields.valuePair', {
                    label: f.label,
                    value: fieldValueText(
                      f.type,
                      ticket.customValues?.[f.code],
                      t('plugin_ticket.fields.unset'),
                      t('plugin_ticket.fields.yes'),
                      t('plugin_ticket.fields.no'),
                    ),
                  })}
                </span>
              ))}
            </div>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-2">
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
          {/* 管理类动作收进「更多」：十个同权重按钮铺开会把头部压成一坨，窄屏还会把文字挤没 */}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm">
                <Ellipsis aria-hidden="true" />
                {t('plugin_ticket.actions.more')}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="min-w-44">
              <DropdownMenuItem onClick={() => onOpenMove(ticket)}>
                <Users aria-hidden="true" />
                {t('plugin_ticket.groups.move')}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onEditTags(ticket)}>
                <Tag aria-hidden="true" />
                {t('plugin_ticket.tags.edit')}
              </DropdownMenuItem>
              {fields.length > 0 ? (
                <DropdownMenuItem onClick={() => onEditValues(ticket)}>
                  <ListChecks aria-hidden="true" />
                  {t('plugin_ticket.fields.editValues')}
                </DropdownMenuItem>
              ) : null}
              {/* 已被合并过的单不再摆「合并」入口：那点下去只会拿到后端的拒绝 */}
              {!links?.merge.mergedInto ? (
                <DropdownMenuItem onClick={onOpenMerge}>
                  <GitMerge aria-hidden="true" />
                  {t('plugin_ticket.merge.action')}
                </DropdownMenuItem>
              ) : null}
              <DropdownMenuItem onClick={onOpenLink}>
                <Link2 aria-hidden="true" />
                {t('plugin_ticket.link.action')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
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
        </div>
      </div>

      <LinkedTicketsPanel data={links} onOpenTicket={onOpenTicket} onUnlink={onUnlink} />

      {attachments.length ? (
        <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2">
          {/* 配额是后端硬拦的（超了直接 400），提前把余量说清楚，别让人撞了才知道 */}
          {quota ? (
            <span className="me-1 text-[11px] text-muted-foreground">
              {t('plugin_ticket.attachments.quota', {
                count: attachments.length,
                max: quota.maxCount,
                used: Math.max(1, Math.round(attachments.reduce((sum, a) => sum + a.size, 0) / 1048576)),
                cap: Math.max(1, Math.round(quota.maxBytes / 1048576)),
              })}
            </span>
          ) : null}
          {attachments.map((a) => (
            <div key={a.id} className="inline-flex items-center gap-1">
              <Button
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
              {/* 删除会连带删掉存储对象，不可恢复，所以必须二次确认 */}
              <Can perm="ticket:reply">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={t('plugin_ticket.attachments.delete')}
                  onClick={() => void onDeleteAttachment(a)}
                >
                  <Trash2 aria-hidden="true" />
                </Button>
              </Can>
            </div>
          ))}
        </div>
      ) : null}

      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-4">
        {loading && replies.length === 0 ? (
          <div className="text-center text-sm text-muted-foreground">{t('plugin_ticket.loading')}</div>
        ) : null}
        {stream.map((item) => {
          if (item.kind === 'act') {
            const a = item.act
            return (
              <div key={`act-${a.id}`} className="flex justify-center">
                <span className="max-w-[85%] rounded-full bg-muted px-2.5 py-1 text-center text-[11px] text-muted-foreground">
                  <span className="font-medium">{a.actorName || t(`plugin_ticket.repliers.${a.actorType}`)}</span>
                  {' · '}
                  {activityLabel(a)}
                  {' · '}
                  {formatTime(a.createdAt, i18n.language)}
                </span>
              </div>
            )
          }
          const r = item.reply
          return (
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
          )
        })}
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

/**
 * 关联与合并面板：按 kind 分组（双向关联 / 本单被并入 / 并入本单的重复单）。
 * 只有双向关联给解除按钮——合并不可撤销，界面不该摆一个点下去必然失败的按钮。
 * 浅底 + divide-y 分组，不再在详情卡片里套一圈边框。
 */
function LinkedTicketsPanel({
  data,
  onOpenTicket,
  onUnlink,
}: {
  data?: LinksResult
  onOpenTicket: (ticketNo: string) => void
  onUnlink: (otherNo: string) => void
}) {
  const { t } = useTranslation()
  const merge = data?.merge
  const sections = [
    { key: 'link', label: t('plugin_ticket.link.groupLink'), rows: data?.links ?? [] },
    { key: 'merged_into', label: t('plugin_ticket.link.groupMergedInto'), rows: merge?.mergedInto ? [merge.mergedInto] : [] },
    { key: 'merged_from', label: t('plugin_ticket.link.groupMergedFrom'), rows: merge?.mergedFrom ?? [] },
  ].filter((g) => g.rows.length > 0)
  if (!data || sections.length === 0) return null
  return (
    <div className="space-y-1.5 border-b bg-muted/40 px-4 py-2">
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="font-medium text-foreground">{t('plugin_ticket.link.title')}</span>
        {data.merge.mergedCount > 0 ? (
          <Badge variant="secondary">{t('plugin_ticket.link.mergedCount', { count: data.merge.mergedCount })}</Badge>
        ) : null}
      </div>
      {sections.map((section) => (
        <div key={section.key} className="min-w-0">
          <p className="text-[11px] text-muted-foreground">{section.label}</p>
          <div className="divide-y divide-border/60">
            {section.rows.map((row) => (
              <div key={`${row.kind}-${row.ticketNo}`} className="flex min-w-0 flex-wrap items-center gap-2 py-1 text-xs">
                <Button
                  type="button"
                  variant="link"
                  size="sm"
                  className="h-5 shrink-0 justify-start px-0 font-mono text-xs"
                  onClick={() => onOpenTicket(row.ticketNo)}
                >
                  {row.ticketNo}
                  <ArrowUpRight aria-hidden="true" className="h-3 w-3" />
                </Button>
                <span className="min-w-24 flex-1 truncate" title={row.title}>
                  {row.title}
                </span>
                <Badge variant={statusVariant(row.status)}>{t(`plugin_ticket.status.${row.status}`, row.status)}</Badge>
                {row.kind === 'link' ? (
                  <Can perm="ticket:edit">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-6 shrink-0 gap-1 px-1.5 text-[11px] text-destructive"
                      onClick={() => onUnlink(row.ticketNo)}
                    >
                      <Unlink aria-hidden="true" className="h-3 w-3" />
                      {t('plugin_ticket.link.remove')}
                    </Button>
                  </Can>
                ) : (
                  <span className="shrink-0 text-[11px] text-muted-foreground">{t('plugin_ticket.link.locked')}</span>
                )}
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

/**
 * 标签多选：候选来自已有的 GET /tags（点选），allowCreate 时还能手输。
 * SimpleSelect 是单选控件，标签是数组，所以这里用点选标签行；上限与工单标签同一口径。
 */
function TagMultiSelect({
  value,
  onChange,
  options,
  allowCreate,
  hint,
}: {
  value: string[]
  onChange: (tags: string[]) => void
  options: string[]
  allowCreate?: boolean
  hint?: string
}) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const toggle = (tag: string) => onChange(value.includes(tag) ? value.filter((x) => x !== tag) : [...value, tag])
  const addTyped = () => {
    const tag = text.trim()
    if (!tag) return
    if (!value.includes(tag)) onChange([...value, tag])
    setText('')
  }
  return (
    <div className="space-y-1.5">
      {value.length ? (
        <div className="flex flex-wrap items-center gap-1.5">
          {value.map((tag) => (
            <Badge key={tag} variant="secondary" className="gap-1">
              #{tag}
              <button
                type="button"
                onClick={() => onChange(value.filter((x) => x !== tag))}
                aria-label={t('plugin_ticket.rules.removeTag', { tag })}
                className="text-muted-foreground hover:text-foreground"
              >
                ×
              </button>
            </Badge>
          ))}
        </div>
      ) : null}
      {options.length ? (
        <div className="flex flex-wrap items-center gap-1.5">
          {options.map((tag) => (
            <Button
              key={tag}
              type="button"
              size="sm"
              variant={value.includes(tag) ? 'default' : 'outline'}
              className="h-6 px-2 text-xs"
              onClick={() => toggle(tag)}
            >
              #{tag}
            </Button>
          ))}
        </div>
      ) : (
        <p className="text-[11px] text-muted-foreground">{t('plugin_ticket.rules.noTagOptions')}</p>
      )}
      {allowCreate ? (
        <div className="flex items-center gap-1.5">
          <Input
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addTyped()
              }
            }}
            placeholder={t('plugin_ticket.rules.tagPlaceholder')}
            aria-label={t('plugin_ticket.rules.addTags')}
            maxLength={20}
            className="h-8"
          />
          <Button type="button" variant="outline" size="sm" className="h-8 shrink-0" disabled={!text.trim()} onClick={addTyped}>
            {t('plugin_ticket.rules.addTag')}
          </Button>
        </div>
      ) : null}
      {hint ? <p className="text-[11px] text-muted-foreground">{hint}</p> : null}
    </div>
  )
}
