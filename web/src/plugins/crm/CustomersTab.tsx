import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import {
  useCustomers,
  useDeleteCustomer,
  useSaveCustomer,
  type CrmCustomer,
  type CrmCustomerForm,
  type CustomerQuery,
} from './api'
import { formatTime } from './format'

const ALL = '__all'

const SOURCES = ['hotel', 'mall', 'order', 'manual'] as const

function CustomerFormDialog({
  open,
  onOpenChange,
  editing,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  editing: CrmCustomer | null
}) {
  const { t } = useTranslation()
  const save = useSaveCustomer()
  const isNew = !editing
  const [draft, setDraft] = useState<Partial<CrmCustomerForm>>({})

  // 打开时用编辑对象初始化（key 由父组件控制重挂载）。
  const value: Partial<CrmCustomerForm> = editing
    ? {
        name: editing.name,
        code: editing.code,
        level: editing.level,
        status: editing.status,
        externalSource: editing.externalSource ?? '',
        externalNo: editing.externalNo ?? '',
        owner: editing.owner,
        phone: editing.phone,
        email: editing.email,
        remark: editing.remark,
      }
    : draft

  const set = (patch: Partial<CrmCustomerForm>) => {
    if (editing) return
    setDraft((d) => ({ ...d, ...patch }))
  }

  const submit = async () => {
    const data = { ...value, name: (value.name ?? '').trim() } as CrmCustomerForm
    if (!data.name) {
      toast.error(t('plugin_crm.nameRequired'))
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
      title={isNew ? t('plugin_crm.customer.create') : t('plugin_crm.customer.edit')}
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
          {t('plugin_crm.fields.name')}
          <Input value={value.name ?? ''} onChange={(e) => set({ name: e.target.value })} />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.code')}
            <Input value={value.code ?? ''} onChange={(e) => set({ code: e.target.value })} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.level')}
            <Input value={value.level ?? ''} onChange={(e) => set({ level: e.target.value })} />
          </label>
        </div>
        <label className="flex flex-col gap-1">
          {t('plugin_crm.fields.status')}
          <Select value={value.status ?? 'active'} onValueChange={(v) => set({ status: v as CrmCustomerForm['status'] })}>
            <SelectTrigger aria-label={t('plugin_crm.fields.status')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="active">{t('plugin_crm.status.active')}</SelectItem>
              <SelectItem value="inactive">{t('plugin_crm.status.inactive')}</SelectItem>
            </SelectContent>
          </Select>
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.externalSource')}
            <Select
              value={value.externalSource ?? ALL}
              onValueChange={(v) => set({ externalSource: v === ALL ? undefined : v })}
              disabled={!isNew}
            >
              <SelectTrigger aria-label={t('plugin_crm.fields.externalSource')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{t('plugin_crm.customer.noAnchor')}</SelectItem>
                {SOURCES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {s}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.externalNo')}
            <Input
              value={value.externalNo ?? ''}
              disabled={!isNew}
              onChange={(e) => set({ externalNo: e.target.value })}
            />
          </label>
        </div>
        <p className="text-xs text-muted-foreground">{t('plugin_crm.customer.anchorHint')}</p>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.owner')}
            <Input value={value.owner ?? ''} onChange={(e) => set({ owner: e.target.value })} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.phone')}
            <Input value={value.phone ?? ''} onChange={(e) => set({ phone: e.target.value })} />
          </label>
        </div>
        <label className="flex flex-col gap-1">
          {t('plugin_crm.fields.email')}
          <Input value={value.email ?? ''} onChange={(e) => set({ email: e.target.value })} />
        </label>
        <label className="flex flex-col gap-1">
          {t('plugin_crm.fields.remark')}
          <Input value={value.remark ?? ''} onChange={(e) => set({ remark: e.target.value })} />
        </label>
      </div>
    </ResponsiveDialog>
  )
}

export function CustomersTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<CustomerQuery>({})
  const [filters, setFilters] = useState<CustomerQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<CrmCustomer | null>(null)

  const { data, isLoading } = useCustomers({ ...filters, page, pageSize })
  const remove = useDeleteCustomer()

  const onDelete = async (cu: CrmCustomer) => {
    if (await confirm({ destructive: true, description: t('plugin_crm.customer.deleteConfirm', { name: cu.name }) })) {
      try {
        await remove.mutateAsync(cu.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const columns: Column<CrmCustomer>[] = [
    {
      key: 'name',
      title: t('plugin_crm.fields.name'),
      mobile: 'title',
      render: (cu) => (
        <div>
          <span className="font-medium text-foreground">{cu.name}</span>
          <div className="text-xs text-muted-foreground">{cu.code || cu.id}</div>
        </div>
      ),
    },
    {
      key: 'external',
      title: t('plugin_crm.fields.externalAnchor'),
      mobile: 'sub',
      render: (cu) =>
        cu.externalSource ? (
          <Badge variant="outline">
            {cu.externalSource}/{cu.externalNo}
          </Badge>
        ) : (
          <span className="text-muted-foreground">-</span>
        ),
    },
    {
      key: 'status',
      title: t('plugin_crm.fields.status'),
      render: (cu) =>
        cu.status === 'active' ? (
          <Badge variant="success">{t('plugin_crm.status.active')}</Badge>
        ) : (
          <Badge variant="outline">{t('plugin_crm.status.inactive')}</Badge>
        ),
    },
    { key: 'owner', title: t('plugin_crm.fields.owner'), render: (cu) => cu.owner || '-' },
    {
      key: 'createdAt',
      title: t('plugin_crm.fields.createdAt'),
      render: (cu) => <span className="num text-xs text-muted-foreground">{formatTime(cu.createdAt, i18n.language)}</span>,
    },
  ]

  // 筛选行并入表格工具行：与「列设置」同一行，不再单独占一行（全站统一口径）。
  const filterToolbar = (
    <div className="flex flex-wrap items-center gap-2">
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
          placeholder={t('plugin_crm.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select value={draft.source ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, source: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-40" aria-label={t('plugin_crm.fields.externalSource')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_crm.all')}</SelectItem>
            {SOURCES.map((s) => (
              <SelectItem key={s} value={s}>
                {s}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FilterBar>
      <Can perm="crm:edit">
        <Button
          onClick={() => {
            setEditing(null)
            setFormOpen(true)
          }}
        >
          <Plus aria-hidden="true" />
          {t('plugin_crm.customer.create')}
        </Button>
      </Can>
    </div>
  )

  return (
    <>
    <DataTable
      columns={columns}
      data={data?.list ?? []}
      rowKey="id"
      loading={isLoading}
      columnSettingsKey="plugin-crm-customers"
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
      toolbar={filterToolbar}
        rowActions={(cu) => (
          <Can perm="crm:edit">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('common.edit')}
              onClick={() => {
                setEditing(cu)
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
              onClick={() => void onDelete(cu)}
            >
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      <CustomerFormDialog key={editing?.id ?? 'new'} open={formOpen} onOpenChange={setFormOpen} editing={editing} />
    </>
  )
}
