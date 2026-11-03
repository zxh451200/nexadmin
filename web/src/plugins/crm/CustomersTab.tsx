import { useState, type ReactNode } from 'react'
import { Download, Eye, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { CountrySelect, useCountryNames } from '@/framework/components/CountrySelect'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { RadioGroup, RadioGroupItem } from '@/framework/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import {
  fetchCustomersForExport,
  useBindExternal,
  useCustomers,
  useDeleteCustomer,
  useDeleteCustomers,
  useOpportunities,
  useSaveCustomer,
  type CrmCustomer,
  type CrmCustomerForm,
  type CustomerQuery,
  type CustomerType,
} from './api'
import { ActivitiesSection, ContactsSection } from './CustomerProfile'
import { formatMoney, formatTime, stageVariant } from './format'
import { downloadCsv } from '@/framework/components/export/csv'

const ALL = '__all'
const MINE = '__mine'
const NO_LEVEL = '__none'
const NO_ID_TYPE = '__none'

const SOURCES = ['hotel', 'mall', 'order', 'manual'] as const

const STATUSES = ['active', 'inactive'] as const

// 等级是收口过的三档；历史/导入数据里的其它值在编辑时仍原样保留。
const LEVELS = ['A', 'B', 'C'] as const

// 客户类型与证件类型：与后端 TypeSet / IdTypeSet 同一套码。
const TYPES: CustomerType[] = ['person', 'company']
const ID_TYPES = ['id_card', 'passport', 'officer_card', 'hmt_permit', 'other'] as const

const isLinked = (cu: CrmCustomer) => !!cu.externalSource && !!cu.externalNo

// 类型徽标：客户是个人还是单位，列表、详情、商机三处都要一眼分得清。
export function TypeBadge({ type }: { type?: CustomerType }) {
  const { t } = useTranslation()
  return (
    <Badge variant={type === 'company' ? 'info' : 'outline'}>{t(`plugin_crm.type.${type ?? 'person'}`)}</Badge>
  )
}

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
          type: editing.type ?? 'person',
          code: editing.code,
          level: editing.level,
          status: editing.status,
          externalSource: editing.externalSource ?? '',
          externalNo: editing.externalNo ?? '',
          owner: editing.owner,
          phone: editing.phone,
          email: editing.email,
          country: editing.country,
          nationality: editing.nationality,
          idType: editing.idType,
          idNo: editing.idNo,
          taxNo: editing.taxNo,
          industry: editing.industry,
          address: editing.address,
          remark: editing.remark,
        }
      : { type: 'person' },
  )

  const value = draft
  const isCompany = (value.type ?? 'person') === 'company'

  const set = (patch: Partial<CrmCustomerForm>) => setDraft((d) => ({ ...d, ...patch }))

  // 换类型就清掉另一组字段：企业客户背着一条护照号，列表、导出和入住核验都会说假话。
  const setType = (next: CustomerType) =>
    setDraft((d) =>
      next === 'company'
        ? { ...d, type: next, nationality: '', idType: '', idNo: '' }
        : { ...d, type: next, taxNo: '', industry: '', address: '' },
    )

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
    if (!!data.idType?.trim() !== !!data.idNo?.trim()) {
      toast.error(t('plugin_crm.idPairRequired'))
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
        <div className="flex flex-col gap-1.5">
          <Label id="crm-customer-type">{t('plugin_crm.fields.type')}</Label>
          <RadioGroup
            aria-labelledby="crm-customer-type"
            value={value.type ?? 'person'}
            onValueChange={(v) => setType(v as CustomerType)}
            className="grid-cols-1 gap-1 sm:grid-cols-2"
          >
            {TYPES.map((tp) => (
              <Label
                key={tp}
                htmlFor={`crm-customer-type-${tp}`}
                className="flex min-h-11 cursor-pointer items-center gap-2 rounded-md border border-border bg-card px-3 text-sm font-normal md:min-h-9"
              >
                <RadioGroupItem id={`crm-customer-type-${tp}`} value={tp} />
                {t(`plugin_crm.type.${tp}`)}
              </Label>
            ))}
          </RadioGroup>
        </div>
        <label className="flex flex-col gap-1">
          <span>
            {t(isCompany ? 'plugin_crm.fields.nameCompany' : 'plugin_crm.fields.namePerson')}
            <RequiredMark />
          </span>
          <Input
            value={value.name ?? ''}
            placeholder={t(`plugin_crm.customer.namePlaceholder.${isCompany ? 'company' : 'person'}`)}
            onChange={(e) => set({ name: e.target.value })}
          />
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
        {isCompany ? (
          <>
            <div className="grid grid-cols-2 gap-3">
              <label className="flex flex-col gap-1">
                {t('plugin_crm.fields.taxNo')}
                <Input
                  maxLength={32}
                  value={value.taxNo ?? ''}
                  placeholder={t('plugin_crm.customer.taxNoPlaceholder')}
                  onChange={(e) => set({ taxNo: e.target.value })}
                />
              </label>
              <label className="flex flex-col gap-1">
                {t('plugin_crm.fields.industry')}
                <Input maxLength={64} value={value.industry ?? ''} onChange={(e) => set({ industry: e.target.value })} />
              </label>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <label className="flex flex-col gap-1">
                {t('plugin_crm.fields.country')}
                <CountrySelect
                  value={value.country}
                  onChange={(v) => set({ country: v })}
                  placeholder={t('plugin_crm.picker.country')}
                />
              </label>
              <label className="flex flex-col gap-1">
                {t('plugin_crm.fields.address')}
                <Input maxLength={200} value={value.address ?? ''} onChange={(e) => set({ address: e.target.value })} />
              </label>
            </div>
          </>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-3">
              <label className="flex flex-col gap-1">
                {t('plugin_crm.fields.nationality')}
                <CountrySelect
                  value={value.nationality}
                  onChange={(v) => set({ nationality: v })}
                  placeholder={t('plugin_crm.picker.nationality')}
                />
              </label>
              <label className="flex flex-col gap-1">
                {t('plugin_crm.fields.country')}
                <CountrySelect
                  value={value.country}
                  onChange={(v) => set({ country: v })}
                  placeholder={t('plugin_crm.picker.country')}
                />
              </label>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <label className="flex flex-col gap-1">
                {t('plugin_crm.fields.idType')}
                <Select value={value.idType || NO_ID_TYPE} onValueChange={(v) => set({ idType: v === NO_ID_TYPE ? '' : v })}>
                  <SelectTrigger aria-label={t('plugin_crm.fields.idType')}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={NO_ID_TYPE}>{t('plugin_crm.idType.none')}</SelectItem>
                    {ID_TYPES.map((it) => (
                      <SelectItem key={it} value={it}>
                        {t(`plugin_crm.idType.${it}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </label>
              <label className="flex flex-col gap-1">
                {t('plugin_crm.fields.idNo')}
                <Input maxLength={64} value={value.idNo ?? ''} onChange={(e) => set({ idNo: e.target.value })} />
              </label>
            </div>
          </>
        )}
        <p className="text-xs text-muted-foreground">
          {t(isCompany ? 'plugin_crm.customer.typeHintCompany' : 'plugin_crm.customer.typeHintPerson')}
        </p>
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
            {t(isCompany ? 'plugin_crm.fields.switchboard' : 'plugin_crm.fields.mobile')}
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
  const nameOf = useCountryNames()
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
            <TypeBadge type={customer.type} />
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
            <DetailRow
              label={t(customer.type === 'company' ? 'plugin_crm.fields.switchboard' : 'plugin_crm.fields.mobile')}
              value={customer.phone}
            />
            <DetailRow label={t('plugin_crm.fields.email')} value={customer.email} />
            <DetailRow label={t('plugin_crm.fields.country')} value={customer.country ? nameOf(customer.country) : ''} />
            {/* 身份字段按类型分组：个人看证件与国籍，企业看信用代码、行业与注册地址。 */}
            {customer.type === 'company' ? (
              <>
                <DetailRow label={t('plugin_crm.fields.taxNo')} value={customer.taxNo} />
                <DetailRow label={t('plugin_crm.fields.industry')} value={customer.industry} />
                <DetailRow label={t('plugin_crm.fields.address')} value={customer.address} />
              </>
            ) : (
              <>
                <DetailRow label={t('plugin_crm.fields.nationality')} value={customer.nationality ? nameOf(customer.nationality) : ''} />
                <DetailRow
                  label={t('plugin_crm.fields.idType')}
                  value={customer.idType ? t(`plugin_crm.idType.${customer.idType}`, { defaultValue: customer.idType }) : ''}
                />
                <DetailRow label={t('plugin_crm.fields.idNo')} value={customer.idNo} />

              </>
            )}
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
          {/* P0：联系人与跟进时间线——都继承客户的数据范围，读写走同一套可见性。 */}
          <ContactsSection customer={customer} />
          <ActivitiesSection customerId={customer.id} />
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
  const nameOf = useCountryNames()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<CustomerQuery>({})
  const [filters, setFilters] = useState<CustomerQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<CrmCustomer | null>(null)
  const [detail, setDetail] = useState<CrmCustomer | null>(null)
  // 多选状态叫 picked：detail 已经是「当前详情面板选中的行」，别再占一个 selected。
  const [picked, setPicked] = useState<string[]>([])

  const { data, isLoading } = useCustomers({ ...filters, page, pageSize })
  const remove = useDeleteCustomer()
  const removeBatch = useDeleteCustomers()
  const [exporting, setExporting] = useState(false)

  const onExport = async () => {
    setExporting(true)
    try {
      const rows = await fetchCustomersForExport({ ...filters })
      const stamp = new Date().toISOString().slice(0, 10)
      downloadCsv(`crm-customers-${stamp}.csv`, [
        [
          t('plugin_crm.fields.name'), t('plugin_crm.fields.type'), t('plugin_crm.fields.code'),
          t('plugin_crm.fields.level'), t('plugin_crm.fields.identity'), t('plugin_crm.fields.phone'),
          t('plugin_crm.fields.email'), t('plugin_crm.fields.owner'), t('plugin_crm.fields.status'),
          t('plugin_crm.fields.externalAnchor'), t('plugin_crm.fields.createdAt'),
        ],
        ...rows.map((cu) => [
          cu.name, t(`plugin_crm.type.${cu.type ?? 'person'}`), cu.code ?? '',
          cu.level ?? '', cu.type === 'company' ? (cu.taxNo ?? '') : (cu.idNo ?? ''), cu.phone ?? '',
          cu.email ?? '', cu.owner ?? '', t(`plugin_crm.status.${cu.status}`),
          cu.externalSource ? `${cu.externalSource}/${cu.externalNo ?? ''}` : '',
          formatTime(cu.createdAt, i18n.language),
        ]),
      ])
    } catch {
      toast.error(t('common.operateFailed'))
    } finally {
      setExporting(false)
    }
  }

  const onDelete = async (cu: CrmCustomer) => {
    if (await confirm({ destructive: true, description: t('plugin_crm.customer.deleteConfirm', { name: cu.name }) })) {
      try {
        await remove.mutateAsync(cu.id)
        // 行没了还留在勾选里会让批量按钮带着一个不存在的 id 再删一次。
        setPicked((ids) => ids.filter((id) => id !== cu.id))
        toast.success(t('common.operateSuccess'))
      } catch {
        // 失败原因已由请求层统一提示（如客户名下还有商机）。
      }
    }
  }

  // 批量删：一次点名 ids；名下仍挂商机的由后端计入 blocked、不删，所以部分删除不能报「操作成功」。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('plugin_crm.customer.deleteBatchConfirm', { count: ids.length }) }))) return
    try {
      const { deleted } = await removeBatch.mutateAsync(ids)
      setPicked([])
      toast.success(deleted < ids.length ? t('plugin_crm.customer.deletedPartial', { deleted, total: ids.length }) : t('common.operateSuccess'))
    } catch {
      // 失败原因（整批不可见 / 全部仍挂商机）已由请求层统一提示。
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
      key: 'type',
      title: t('plugin_crm.fields.type'),
      render: (cu) => <TypeBadge type={cu.type} />,
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
      // 身份标识：企业看统一社会信用代码，个人看证件类型 + 号码——前台就是靠这两样认人。
      key: 'identity',
      title: t('plugin_crm.fields.identity'),
      render: (cu) => {
        const value =
          cu.type === 'company'
            ? cu.taxNo
            : cu.idNo
              ? `${t(`plugin_crm.idType.${cu.idType ?? ''}`, { defaultValue: cu.idType ?? '' })} ${cu.idNo}`
              : ''
        return value ? <span className="num">{value}</span> : <span className="text-muted-foreground">-</span>
      },
    },
    {
      // 电话是销售联系客户的第一入口：个人看手机，企业看总机
      key: 'phone',
      title: t('plugin_crm.fields.phone'),
      mobile: 'hidden',
      render: (cu) => (cu.phone ? <span className="num">{cu.phone}</span> : <span className="text-muted-foreground">-</span>),
    },
    {
      key: 'nationality',
      title: t('plugin_crm.fields.nationality'),
      render: (cu) =>
        cu.nationality ? nameOf(cu.nationality) : <span className="text-muted-foreground">-</span>,
    },
    {
      key: 'country',
      title: t('plugin_crm.fields.country'),
      render: (cu) =>
        cu.country ? nameOf(cu.country) : <span className="text-muted-foreground">-</span>,
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
        <Select value={draft.type ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, type: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-32" aria-label={t('plugin_crm.fields.type')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_crm.filter.allTypes')}</SelectItem>
            {TYPES.map((tp) => (
              <SelectItem key={tp} value={tp}>
                {t(`plugin_crm.type.${tp}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={draft.source ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, source: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-40" aria-label={t('plugin_crm.fields.externalSource')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_crm.filter.allSources')}</SelectItem>
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
            <SelectItem value={ALL}>{t('plugin_crm.filter.allStatuses')}</SelectItem>
            {STATUSES.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_crm.status.${s}`)}
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
        selection={{ selected: picked, onChange: setPicked }}
        batchActions={(ids) => (
          <Can perm="crm:edit">
            <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        toolbar={
          <>
            <Button variant="outline" onClick={() => void onExport()} disabled={exporting}>
              <Download className="me-2 size-4" aria-hidden="true" />
              {t('common.export')}
            </Button>
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
          </>
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
