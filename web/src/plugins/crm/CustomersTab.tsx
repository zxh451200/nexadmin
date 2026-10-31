import { useState, type ReactNode } from 'react'
import { Eye, Pencil, Plus, Trash2 } from 'lucide-react'
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
import { Textarea } from '@/framework/ui/textarea'
import {
  useBindExternal,
  useCustomers,
  useDeleteCustomer,
  useOpportunities,
  useSaveCustomer,
  type CrmCustomer,
  type CrmCustomerForm,
  type CustomerQuery,
} from './api'
import { formatMoney, formatTime, stageVariant } from './format'

const ALL = '__all'
const NO_LEVEL = '__none'

const SOURCES = ['hotel', 'mall', 'order', 'manual'] as const

const STATUSES = ['active', 'inactive'] as const

// 等级是收口过的三档；历史/导入数据里的其它值在编辑时仍原样保留。
const LEVELS = ['A', 'B', 'C'] as const

const isLinked = (cu: CrmCustomer) => !!cu.externalSource && !!cu.externalNo

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
  // 由父组件按 id 重挂载，初始值直接取自编辑对象。
  const [draft, setDraft] = useState<Partial<CrmCustomerForm>>(() =>
    editing
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
      : {},
  )

  const value = draft

  const set = (patch: Partial<CrmCustomerForm>) => setDraft((d) => ({ ...d, ...patch }))

  const levelOptions: string[] =
    value.level && !LEVELS.includes(value.level as (typeof LEVELS)[number])
      ? [...LEVELS, value.level]
      : [...LEVELS]

  const submit = async () => {
    const data = { ...value, name: (value.name ?? '').trim() } as CrmCustomerForm
    if (!data.name) {
      toast.error(t('plugin_crm.nameRequired'))
      return
    }
    if (data.externalSource && !data.externalNo?.trim()) {
      toast.error(t('plugin_crm.externalNoRequired'))
      return
    }
    try {
      await save.mutateAsync({ id: editing?.id, data })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 失败原因已由请求层统一提示。
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
            <Input
              maxLength={64}
              value={value.code ?? ''}
              placeholder={t('plugin_crm.customer.codePlaceholder')}
              onChange={(e) => set({ code: e.target.value })}
            />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_crm.fields.level')}
            <Select value={value.level || NO_LEVEL} onValueChange={(v) => set({ level: v === NO_LEVEL ? '' : v })}>
              <SelectTrigger aria-label={t('plugin_crm.fields.level')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_LEVEL}>{t('plugin_crm.level.none')}</SelectItem>
                {levelOptions.map((lv) => (
                  <SelectItem key={lv} value={lv}>
                    {t(`plugin_crm.level.${lv}`, { defaultValue: lv })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
        </div>
        <p className="text-xs text-muted-foreground">{t('plugin_crm.customer.codeHint')}</p>
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
              onValueChange={(v) =>
                set(v === ALL ? { externalSource: undefined, externalNo: '' } : { externalSource: v, externalNo: value.externalNo })
              }
              disabled={!isNew}
            >
              <SelectTrigger aria-label={t('plugin_crm.fields.externalSource')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{t('plugin_crm.customer.noAnchor')}</SelectItem>
                {SOURCES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {t(`plugin_crm.source.${s}`, { defaultValue: s })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          <label className="flex flex-col gap-1">
            <span>
              {t('plugin_crm.fields.externalNo')}
              {value.externalSource ? <span className="text-destructive"> *</span> : null}
            </span>
            <Input
              value={value.externalNo ?? ''}
              disabled={!isNew || !value.externalSource}
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
          <Textarea rows={3} maxLength={500} value={value.remark ?? ''} onChange={(e) => set({ remark: e.target.value })} />
        </label>
      </div>
    </ResponsiveDialog>
  )
}

function DetailRow({ label, value }: { label: string; value?: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 break-words text-sm text-foreground">{value || '-'}</dd>
    </div>
  )
}

function CustomerDetailDialog({ customer, onClose }: { customer: CrmCustomer | null; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const { data, isLoading } = useOpportunities(
    { customerId: customer?.id, page: 1, pageSize: 50 },
    !!customer,
  )
  const bind = useBindExternal()
  const [source, setSource] = useState('')
  const [externalNo, setExternalNo] = useState('')

  const sourceLabel = (cu: CrmCustomer) =>
    t(`plugin_crm.source.${cu.externalSource ?? ''}`, { defaultValue: cu.externalSource ?? '' })

  const canBind = !!customer && !isLinked(customer) && !!source && !!externalNo.trim()

  const doBind = async () => {
    if (!customer || !canBind) return
    try {
      await bind.mutateAsync({ customerId: customer.id, source, externalNo: externalNo.trim() })
      toast.success(t('common.operateSuccess'))
      onClose()
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  return (
    <ResponsiveDialog
      open={!!customer}
      onOpenChange={(v) => !v && onClose()}
      title={`${t('common.detail')} · ${customer?.name ?? ''}`}
      description={customer?.code || undefined}
      size="lg"
      footer={
        <Button variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {!customer ? null : (
        <div className="flex flex-col gap-5">
          <div className="flex flex-wrap items-center gap-2">
            {customer.status === 'active' ? (
              <Badge variant="success">{t('plugin_crm.status.active')}</Badge>
            ) : (
              <Badge variant="outline">{t('plugin_crm.status.inactive')}</Badge>
            )}
            {customer.level ? (
              <Badge variant="outline">
                {t(`plugin_crm.level.${customer.level}`, { defaultValue: customer.level })}
              </Badge>
            ) : null}
            {isLinked(customer) ? (
              <Badge variant="outline">
                {sourceLabel(customer)}/{customer.externalNo}
              </Badge>
            ) : null}
          </div>
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <DetailRow label={t('plugin_crm.fields.owner')} value={customer.owner} />
            <DetailRow label={t('plugin_crm.fields.phone')} value={customer.phone} />
            <DetailRow label={t('plugin_crm.fields.email')} value={customer.email} />
            <DetailRow
              label={t('plugin_crm.fields.externalAnchor')}
              value={isLinked(customer) ? `${sourceLabel(customer)} / ${customer.externalNo}` : t('plugin_crm.customer.notBound')}
            />
            <DetailRow label={t('plugin_crm.fields.createdAt')} value={formatTime(customer.createdAt, i18n.language)} />
          </dl>
          {customer.remark ? (
            <div>
              <div className="text-xs text-muted-foreground">{t('plugin_crm.fields.remark')}</div>
              <p className="mt-1 break-words whitespace-pre-wrap text-sm text-foreground">{customer.remark}</p>
            </div>
          ) : null}
          {!isLinked(customer) ? (
            <Can perm="crm:edit">
              <div className="rounded-md border border-border p-3">
                <div className="text-sm font-medium text-foreground">{t('plugin_crm.customer.bind')}</div>
                <p className="mt-1 text-xs text-muted-foreground">{t('plugin_crm.customer.bindHint')}</p>
                <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-[1fr_1fr_auto]">
                  <Select value={source || ALL} onValueChange={(v) => setSource(v === ALL ? '' : v)}>
                    <SelectTrigger aria-label={t('plugin_crm.fields.externalSource')}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={ALL}>{t('plugin_crm.customer.chooseSource')}</SelectItem>
                      {SOURCES.map((s) => (
                        <SelectItem key={s} value={s}>
                          {t(`plugin_crm.source.${s}`, { defaultValue: s })}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Input
                    value={externalNo}
                    onChange={(e) => setExternalNo(e.target.value)}
                    placeholder={t('plugin_crm.fields.externalNo')}
                    aria-label={t('plugin_crm.fields.externalNo')}
                  />
                  <Button type="button" disabled={!canBind || bind.isPending} onClick={() => void doBind()}>
                    {t('common.save')}
                  </Button>
                </div>
              </div>
            </Can>
          ) : null}
          <div className="flex flex-col gap-2">
            <div className="flex items-baseline justify-between gap-2">
              <div className="text-sm font-medium text-foreground">{t('plugin_crm.customer.opportunities')}</div>
              {data?.total ? <span className="num text-xs text-muted-foreground">{data.total}</span> : null}
            </div>
            {isLoading ? (
              <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
            ) : (data?.list ?? []).length ? (
              (data?.list ?? []).map((o) => (
                <div key={o.id} className="flex items-center justify-between gap-3 rounded-md border border-border px-3 py-2">
                  <div className="min-w-0">
                    <div className="truncate text-sm text-foreground">{o.title}</div>
                    <div className="num mt-0.5 text-xs text-muted-foreground">{o.expectedAt || '-'}</div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Badge variant={stageVariant(o.stage)}>{t(`plugin_crm.stage.${o.stage}`)}</Badge>
                    {o.amountMinor > 0 ? <span className="num text-sm">{formatMoney(o.amountMinor, o.currency)}</span> : null}
                  </div>
                </div>
              ))
            ) : (
              <p className="text-xs text-muted-foreground">{t('plugin_crm.customer.noOpportunity')}</p>
            )}
          </div>
        </div>
      )}
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
  const [detail, setDetail] = useState<CrmCustomer | null>(null)

  const { data, isLoading } = useCustomers({ ...filters, page, pageSize })
  const remove = useDeleteCustomer()

  const onDelete = async (cu: CrmCustomer) => {
    if (await confirm({ destructive: true, description: t('plugin_crm.customer.deleteConfirm', { name: cu.name }) })) {
      try {
        await remove.mutateAsync(cu.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        // 失败原因已由请求层统一提示（如客户名下还有商机）。
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
      key: 'level',
      title: t('plugin_crm.fields.level'),
      render: (cu) =>
        cu.level ? (
          <Badge variant="outline">{t(`plugin_crm.level.${cu.level}`, { defaultValue: cu.level })}</Badge>
        ) : (
          <span className="text-muted-foreground">-</span>
        ),
    },
    {
      key: 'external',
      title: t('plugin_crm.fields.externalAnchor'),
      mobile: 'sub',
      render: (cu) =>
        cu.externalSource ? (
          <Badge variant="outline">
            {t(`plugin_crm.source.${cu.externalSource}`, { defaultValue: cu.externalSource })}/{cu.externalNo}
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
                {t(`plugin_crm.source.${s}`, { defaultValue: s })}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={draft.status ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, status: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-32" aria-label={t('plugin_crm.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_crm.all')}</SelectItem>
            {STATUSES.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_crm.status.${s}`)}
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
        toolbar={
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
        }
        rowActions={(cu) => (
          <>
            <Button variant="ghost" size="icon" aria-label={t('common.detail')} onClick={() => setDetail(cu)}>
              <Eye aria-hidden="true" />
            </Button>
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
          </>
        )}
      />
      <CustomerFormDialog
        key={formOpen ? (editing?.id ?? 'new') : 'closed'}
        open={formOpen}
        onOpenChange={setFormOpen}
        editing={editing}
      />
      {detail ? <CustomerDetailDialog key={detail.id} customer={detail} onClose={() => setDetail(null)} /> : null}
    </div>
  )
}
