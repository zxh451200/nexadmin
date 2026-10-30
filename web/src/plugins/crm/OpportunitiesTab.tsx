import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DatePicker } from '@/framework/components/DatePicker'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import {
  useCustomers,
  useDeleteOpportunity,
  useOpportunities,
  useSaveOpportunity,
  type CrmOpportunity,
  type CrmOpportunityForm,
  type OpportunityQuery,
} from './api'
import { formatMoney, formatTime, stageVariant } from './format'

const ALL = '__all'

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
  const { data } = useCustomers({ page: 1, pageSize: 200 })
  const isNew = !editing
  const [draft, setDraft] = useState<Partial<CrmOpportunityForm>>({})

  const value: Partial<CrmOpportunityForm> = editing
    ? {
        customerId: editing.customerId,
        title: editing.title,
        stage: editing.stage,
        amountMinor: editing.amountMinor,
        currency: editing.currency,
        expectedAt: editing.expectedAt,
        owner: editing.owner,
        remark: editing.remark,
      }
    : draft

  const set = (patch: Partial<CrmOpportunityForm>) => {
    if (editing) return
    setDraft((d) => ({ ...d, ...patch }))
  }

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
      toast.error(t('common.operateFailed'))
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
          {t('plugin_crm.fields.title')}
          <Input value={value.title ?? ''} onChange={(e) => set({ title: e.target.value })} />
        </label>
        <label className="flex flex-col gap-1">
          {t('plugin_crm.fields.customerId')}
          <Select
            value={String(value.customerId ?? '') || ALL}
            onValueChange={(v) => set({ customerId: v === ALL ? undefined : Number(v) })}
          >
            <SelectTrigger aria-label={t('plugin_crm.fields.customerId')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('plugin_crm.opp.noCustomer')}</SelectItem>
              {(data?.list ?? []).map((cu) => (
                <SelectItem key={cu.id} value={cu.id}>
                  {cu.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
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

  const { data, isLoading } = useOpportunities({ ...filters, page, pageSize })
  const remove = useDeleteOpportunity()

  const onDelete = async (o: CrmOpportunity) => {
    if (await confirm({ destructive: true, description: t('plugin_crm.opp.deleteConfirm', { title: o.title }) })) {
      try {
        await remove.mutateAsync(o.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const columns: Column<CrmOpportunity>[] = [
    {
      key: 'title',
      title: t('plugin_crm.fields.title'),
      mobile: 'title',
      render: (o) => <span className="font-medium text-foreground">{o.title}</span>,
    },
    { key: 'customer', title: t('plugin_crm.fields.customerId'), mobile: 'sub', render: (o) => (o.customerId ? String(o.customerId) : '-') },
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
            <SelectItem value={ALL}>{t('plugin_crm.all')}</SelectItem>
            {STAGES.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_crm.stage.${s}`)}
              </SelectItem>
            ))}
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
      <OpportunityFormDialog key={editing?.id ?? 'new'} open={formOpen} onOpenChange={setFormOpen} editing={editing} />
    </div>
  )
}
