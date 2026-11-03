import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DatePicker } from '@/framework/components/DatePicker'
import { FilterBar } from '@/framework/components/FilterBar'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import {
  useContacts,
  useDeleteOpportunities,
  useDeleteOpportunity,
  useOpportunities,
  useSaveOpportunity,
  type CrmOpportunity,
  type CrmOpportunityForm,
  type OpportunityQuery,
} from './api'
import { TypeBadge } from './CustomersTab'
import { formatMoney, formatTime, stageVariant } from './format'

const ALL = '__all'
const MINE = '__mine'

const STAGES = ['lead', 'qualified', 'proposal', 'negotiate', 'won', 'lost'] as const

function OpportunityFormDialog({
  open,
  onOpenChange,
  editing,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  editing: CrmOpportunity | null
}) {
  const { t } = useTranslation()
  const save = useSaveOpportunity()
  const isNew = !editing
  // 由父组件按 id 重挂载，初始值直接取自编辑对象。
  const [draft, setDraft] = useState<Partial<CrmOpportunityForm>>(() =>
    editing
      ? {
          customerId: editing.customerId,
          contactId: editing.contactId,
          title: editing.title,
          stage: editing.stage,
          amountMinor: editing.amountMinor,
          currency: editing.currency,
          expectedAt: editing.expectedAt,
          owner: editing.owner,
          remark: editing.remark,
        }
      : {},
  )

  const value = draft
  const set = (patch: Partial<CrmOpportunityForm>) => setDraft((d) => ({ ...d, ...patch }))
  // 联系人下拉：跟随所选客户加载（数量小不分页）；编辑态用它回显已选联系人名。
  const { data: contacts = [] } = useContacts(
    value.customerId && value.customerId !== '0' ? value.customerId : undefined,
  )

  const submit = async () => {
    const data = { ...value, title: (value.title ?? '').trim() } as CrmOpportunityForm
    if (!data.title) {
      toast.error(t('plugin_crm.titleRequired'))
      return
    }
    try {
      await save.mutateAsync({ id: editing?.id, data })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 失败原因已由请求层统一提示，这里不再叠一条笼统文案。
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={isNew ? t('plugin_crm.opp.create') : t('plugin_crm.opp.edit')}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-sm">
        <label className="flex flex-col gap-1">
          <span>
            {t('plugin_crm.fields.title')}
            <RequiredMark />
          </span>
          <Input value={value.title ?? ''} onChange={(e) => set({ title: e.target.value })} />
        </label>
        <label className="flex flex-col gap-1">
          {t('plugin_crm.fields.customerId')}
          <RemoteSelect
            url="/plugins/crm/customers"
            value={value.customerId && value.customerId !== '0' ? value.customerId : ''}
            onChange={(v) => set({ customerId: typeof v === 'string' ? v : '' })}
            placeholder={t('plugin_crm.opp.chooseCustomer')}
            allowClear
            initialOptions={
              editing?.customerId && editing.customerId !== '0' && editing.customerName
                ? [{ value: editing.customerId, label: editing.customerName }]
                : undefined
            }
          />
        </label>
        {/* 主对接联系人：跟随所属客户，清空客户时一并清空（后端校验归属一致）。 */}
        <label className="flex flex-col gap-1">
          {t('plugin_crm.fields.contact')}
          <RemoteSelect
            key={value.customerId ?? ''}
            url={`/plugins/crm/customers/${value.customerId ?? '0'}/contacts`}
            value={value.contactId && value.contactId !== '0' ? value.contactId : ''}
            onChange={(v) => set({ contactId: typeof v === 'string' && v ? v : '' })}
            placeholder={t('plugin_crm.contact.chooseContact')}
            allowClear
            disabled={!value.customerId || value.customerId === '0'}
            initialOptions={contacts
              .filter((c) => c.id === editing?.contactId)
              .map((c) => ({ value: c.id, label: c.name }))}
          />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.stage')}
            <Select value={value.stage ?? 'lead'} onValueChange={(v) => set({ stage: v as CrmOpportunityForm['stage'] })}>
              <SelectTrigger aria-label={t('plugin_crm.fields.stage')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {STAGES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {t(`plugin_crm.stage.${s}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.amountMinor')}
            <Input
              type="number"
              value={value.amountMinor ?? 0}
              min={0}
              onChange={(e) => set({ amountMinor: Number(e.target.value) || 0 })}
            />
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.expectedAt')}
            <DatePicker value={value.expectedAt} onChange={(v) => set({ expectedAt: v })} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.owner')}
            <Input value={value.owner ?? ''} onChange={(e) => set({ owner: e.target.value })} />
          </label>
        </div>
        <label className="flex flex-col gap-1">
          {t('plugin_crm.fields.remark')}
          <Textarea rows={3} maxLength={500} value={value.remark ?? ''} onChange={(e) => set({ remark: e.target.value })} />
        </label>
      </div>
    </ResponsiveDialog>
  )
}

export function OpportunitiesTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<OpportunityQuery>({})
  const [filters, setFilters] = useState<OpportunityQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<CrmOpportunity | null>(null)
  // 多选状态叫 picked，与客户页保持一致。
  const [picked, setPicked] = useState<string[]>([])

  const { data, isLoading } = useOpportunities({ ...filters, page, pageSize })
  const remove = useDeleteOpportunity()
  const removeBatch = useDeleteOpportunities()

  const onDelete = async (o: CrmOpportunity) => {
    if (await confirm({ destructive: true, description: t('plugin_crm.opp.deleteConfirm', { title: o.title }) })) {
      try {
        await remove.mutateAsync(o.id)
        // 行没了还留在勾选里会让批量按钮带着一个不存在的 id 再删一次。
        setPicked((ids) => ids.filter((id) => id !== o.id))
        toast.success(t('common.operateSuccess'))
      } catch {
        // 失败原因已由请求层统一提示。
      }
    }
  }

  // 批量删：范围外的商机由后端计入 blocked、不删，部分删除时照实说删了几条。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('plugin_crm.opp.deleteBatchConfirm', { count: ids.length }) }))) return
    try {
      const { deleted } = await removeBatch.mutateAsync(ids)
      setPicked([])
      toast.success(deleted < ids.length ? t('plugin_crm.opp.deletedPartial', { deleted, total: ids.length }) : t('common.operateSuccess'))
    } catch {
      // 失败原因（整批不可见）已由请求层统一提示。
    }
  }

  const columns: Column<CrmOpportunity>[] = [
    {
      key: 'title',
      title: t('plugin_crm.fields.title'),
      mobile: 'title',
      render: (o) => <span className="font-medium text-foreground">{o.title}</span>,
    },
    {
      key: 'customer',
      title: t('plugin_crm.fields.customerId'),
      mobile: 'sub',
      render: (o) =>
        o.customerId && o.customerId !== '0' ? (
          <span className="inline-flex min-w-0 items-center gap-1.5">
            <span className="truncate">{o.customerName || o.customerId}</span>
            <TypeBadge type={o.customerType} />
          </span>
        ) : (
          <span className="text-muted-foreground">-</span>
        ),
    },
    {
      key: 'stage',
      title: t('plugin_crm.fields.stage'),
      render: (o) => <Badge variant={stageVariant(o.stage)}>{t(`plugin_crm.stage.${o.stage}`)}</Badge>,
    },
    {
      key: 'amount',
      title: t('plugin_crm.fields.amountMinor'),
      align: 'end',
      render: (o) => (o.amountMinor > 0 ? <span className="num">{formatMoney(o.amountMinor, o.currency)}</span> : <span className="text-muted-foreground">-</span>),
    },
    { key: 'owner', title: t('plugin_crm.fields.owner'), render: (o) => o.owner || '-' },
    {
      key: 'createdAt',
      title: t('plugin_crm.fields.createdAt'),
      render: (o) => <span className="num text-xs text-muted-foreground">{formatTime(o.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      <FilterBar
        onSearch={() => {
          setFilters(draft)
          setPage(1)
        }}
        onReset={() => {
          setDraft({})
          setFilters({})
          setPage(1)
        }}
      >
        <Input
          className="md:w-56"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_crm.opp.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select value={draft.stage ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, stage: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-44" aria-label={t('plugin_crm.fields.stage')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_crm.filter.allStages')}</SelectItem>
            {STAGES.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_crm.stage.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {/* 只看我的：owner = 当前账号，与角色数据范围叠加。 */}
        <Select value={draft.mine ? MINE : ALL} onValueChange={(v) => setDraft((d) => ({ ...d, mine: v === MINE }))}>
          <SelectTrigger className="md:w-32" aria-label={t('plugin_crm.fields.owner')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_crm.filter.allOwners')}</SelectItem>
            <SelectItem value={MINE}>{t('plugin_crm.mineOnly')}</SelectItem>
          </SelectContent>
        </Select>
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-crm-opportunities"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_crm.empty')}
        selection={{ selected: picked, onChange: setPicked }}
        batchActions={(ids) => (
          <Can perm="crm:opp">
            <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        toolbar={
          <Can perm="crm:opp">
            <Button
              onClick={() => {
                setEditing(null)
                setFormOpen(true)
              }}
            >
              <Plus aria-hidden="true" />
              {t('plugin_crm.opp.create')}
            </Button>
          </Can>
        }
        rowActions={(o) => (
          <Can perm="crm:opp">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('common.edit')}
              onClick={() => {
                setEditing(o)
                setFormOpen(true)
              }}
            >
              <Pencil aria-hidden="true" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="text-destructive"
              aria-label={t('common.delete')}
              onClick={() => void onDelete(o)}
            >
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      <OpportunityFormDialog
        key={formOpen ? (editing?.id ?? 'new') : 'closed'}
        open={formOpen}
        onOpenChange={setFormOpen}
        editing={editing}
      />
    </div>
  )
}
