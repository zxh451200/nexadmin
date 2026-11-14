import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ArrowDownUp, CalendarClock, ChevronDown, Combine, CopyPlus, Download, Eye, Hand, Pencil, Plus, Settings2, SlidersHorizontal, Tag as TagIcon, Trash2, Upload, UserMinus, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useQueryClient } from '@tanstack/react-query'
import { useAuth } from '@/framework/auth/useAuth'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { CountrySelect, useCountryNames } from '@/framework/components/CountrySelect'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DatePicker } from '@/framework/components/DatePicker'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/framework/ui/dropdown-menu'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { RadioGroup, RadioGroupItem } from '@/framework/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Skeleton } from '@/framework/ui/skeleton'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import { OptionPicker } from '@/framework/components/OptionPicker'
import {
  fetchCustomer,
  fetchCustomersForExport,
  useBatchAddCustomerTags,
  useBatchUpdateCustomers,
  useClaimCustomer,
  useCustomerDuplicates,
  useCustomerTodos,
  useCustomerPool,
  useCustomerTags,
  useRevealIdNo,
  useBindExternal,
  useCustomers,
  useDeleteCustomer,
  useDeleteCustomers,
  useMergeCustomers,
  useMergePreview,
  useOpportunities,
  useReleaseCustomer,
  useSaveCustomer,
  useSaveCustomerField,
  useDeleteCustomerField,
  useCustomerFields,
  useSetCustomerTags,
  type CrmCustomer,
  type CrmField,
  type IdNoReveal,
  type CrmCustomerForm,
  type CustomerQuery,
  type CustomerDraft,
  toCustomerQuery,
  type CustomerType,
} from './api'
import { ActivitiesSection, ContactsSection } from './CustomerProfile'
import { CustomerImportDialog } from './CustomerImportDialog'
import { formatMoney, formatTime, stageVariant } from './format'
import { downloadCsv } from '@/framework/components/export/csv'

const ALL = '__all'
const MINE = '__mine'
const POOL = '__pool'
const NO_LEVEL = '__none'
const NO_ID_TYPE = '__none'

const SOURCES = ['hotel', 'mall', 'order', 'manual'] as const

const STATUSES = ['active', 'inactive'] as const

// 等级是收口过的三档；历史/导入数据里的其它值在编辑时仍原样保留。
const LEVELS = ['A', 'B', 'C'] as const

// 排序白名单与后端 customerSortCols 同一套键；created_at 是默认口径（新建在前），仍列出可选
const SORT_KEYS = ['created_at', 'name', 'code', 'level', 'owner'] as const

// 客户类型与证件类型：与后端 TypeSet / IdTypeSet 同一套码。
const TYPES: CustomerType[] = ['person', 'company']
const ID_TYPES = ['id_card', 'passport', 'officer_card', 'hmt_permit', 'other'] as const

const isLinked = (cu: CrmCustomer) => !!cu.externalSource && !!cu.externalNo

// 合并预览里「将补空的字段」的列名 → 展示名（与 fields.* 同一份文案，不造第二套）
const MERGE_FIELD_LABELS: Record<string, string> = {
  level: 'plugin_crm.fields.level',
  owner: 'plugin_crm.fields.owner',
  phone: 'plugin_crm.fields.phone',
  email: 'plugin_crm.fields.email',
  remark: 'plugin_crm.fields.remark',
  nationality: 'plugin_crm.fields.nationality',
  id_type: 'plugin_crm.fields.idType',
  id_no: 'plugin_crm.fields.idNo',
  tax_no: 'plugin_crm.fields.taxNo',
  industry: 'plugin_crm.fields.industry',
  address: 'plugin_crm.fields.address',
  country: 'plugin_crm.fields.country',
  external_source: 'plugin_crm.fields.externalSource',
  external_no: 'plugin_crm.fields.externalNo',
  tags: 'plugin_crm.fields.tags',
}

function mergeFieldLabel(col: string, t: (key: string) => string) {
  const key = MERGE_FIELD_LABELS[col]
  return key ? t(key) : col
}

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
          custom: (editing.customValues ?? {}) as CrmCustomerForm['custom'],
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
                <span className="text-[11px] text-muted-foreground">{t('plugin_crm.customer.countryHint')}</span>
              </label>
              <label className="flex flex-col gap-1">
                {t('plugin_crm.fields.address')}
                <Input maxLength={200} value={value.address ?? ''} onChange={(e) => set({ address: e.target.value })} />
              </label>
            </div>
          </>
        ) : (
          <>
            {/* 个人只收法定国籍（配证件做实名）。「国家/地区」是企业侧的所在地、跟着注册地址走，
                个人表单不再放第二个同款国家下拉——并排看就是重复字段（历史/导入数据仍照常展示）。 */}
            <div className="grid grid-cols-2 gap-3">
              <label className="flex flex-col gap-1">
                {t('plugin_crm.fields.nationality')}
                <CountrySelect
                  value={value.nationality}
                  onChange={(v) => set({ nationality: v })}
                  placeholder={t('plugin_crm.picker.nationality')}
                />
                <span className="text-[11px] text-muted-foreground">{t('plugin_crm.customer.nationalityHint')}</span>
              </label>
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
            </div>
            <label className="flex flex-col gap-1">
              {t('plugin_crm.fields.idNo')}
              <Input maxLength={64} value={value.idNo ?? ''} onChange={(e) => set({ idNo: e.target.value })} />
            </label>
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
        <CustomFieldsEditor
          values={(value.custom ?? {}) as Record<string, unknown>}
          onChange={(custom) => set({ custom: custom as CrmCustomerForm['custom'] })}
        />
      </div>
    </ResponsiveDialog>
  )
}

/**
 * 自定义字段的表单渲染：按启用中的字段定义逐个出控件（text=Input / number=Input /
 * select=Select / date=DatePicker / bool=Switch）。值随手改、随表单一起整份提交，
 * 必填/类型校验由后端在保存时把关（crm.field_required / crm.field_value_invalid）。
 */
function CustomFieldsEditor({
  values,
  onChange,
}: {
  values: Record<string, unknown>
  onChange: (values: Record<string, unknown>) => void
}) {
  const { t } = useTranslation()
  const { data: fields, isLoading } = useCustomerFields(true)
  if (isLoading) return <Skeleton className="h-20 w-full" />
  if (!fields?.length) return null
  return (
    <div className="flex flex-col gap-3 rounded-md border border-border/60 p-3">
      <div className="text-xs font-medium text-muted-foreground">{t('plugin_crm.fields.custom')}</div>
      {fields.map((f) => (
        <label key={f.code} className="flex flex-col gap-1">
          <span>
            {f.label}
            {f.required ? <RequiredMark /> : null}
          </span>
          {f.type === 'select' ? (
            <Select
              value={typeof values[f.code] === 'string' ? (values[f.code] as string) : ''}
              onValueChange={(v) => onChange({ ...values, [f.code]: v || undefined })}
            >
              <SelectTrigger aria-label={f.label}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="">{t('common.clear')}</SelectItem>
                {f.options.map((opt) => (
                  <SelectItem key={opt} value={opt}>
                    {opt}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : f.type === 'date' ? (
            <DatePicker
              value={typeof values[f.code] === 'string' ? (values[f.code] as string) : null}
              onChange={(v) => onChange({ ...values, [f.code]: v || undefined })}
              ariaLabel={f.label}
            />
          ) : f.type === 'bool' ? (
            <Switch
              checked={values[f.code] === true}
              onCheckedChange={(v) => onChange({ ...values, [f.code]: v })}
              aria-label={f.label}
            />
          ) : (
            <Input
              value={values[f.code] == null ? '' : String(values[f.code])}
              inputMode={f.type === 'number' ? 'decimal' : 'text'}
              aria-label={f.label}
              onChange={(e) => {
                const raw = e.target.value
                if (f.type === 'number') {
                  onChange({ ...values, [f.code]: raw === '' ? undefined : Number(raw) })
                } else {
                  onChange({ ...values, [f.code]: raw === '' ? undefined : raw })
                }
              }}
            />
          )}
        </label>
      ))}
    </div>
  )
}


// 明文证件号是单独一次请求（权限点 realname:reveal，后端记操作日志）。
// key 绑在 customer.id 上：换客户查看时不会把上一个客户的明文留在屏幕上。
function IdNoRow({ customer }: { customer: CrmCustomer }) {
  const { t } = useTranslation()
  const reveal = useRevealIdNo()
  const [shown, setShown] = useState<IdNoReveal | null>(null)
  const detail = shown
    ? [
        shown.regionNames?.length ? shown.regionNames.join(' / ') : '',
        shown.birthday,
        shown.sex ? t(`plugin_crm.reveal.sex.${shown.sex}`, { defaultValue: shown.sex }) : '',
        typeof shown.age === 'number' ? `${shown.age}` : '',
      ]
        .filter(Boolean)
        .join(' · ')
    : ''
  return (
    <div className="flex min-w-0 items-start justify-between gap-2">
      <div className="min-w-0">
        <DetailRow label={t('plugin_crm.fields.idNo')} value={shown?.idNo ?? customer.idNo} />
        {detail ? <p className="mt-0.5 text-xs text-muted-foreground">{detail}</p> : null}
        {shown && shown.valid === false ? (
          <p className="mt-0.5 text-xs text-destructive">{t('plugin_crm.reveal.invalid')}</p>
        ) : null}
      </div>
      {customer.idNo ? (
        <Can perm="realname:reveal">
          <Button
            variant="ghost"
            size="icon"
            aria-label={shown ? t('plugin_crm.reveal.hide') : t('plugin_crm.reveal.show')}
            title={t('plugin_crm.reveal.show')}
            disabled={reveal.isPending}
            onClick={() => {
              if (shown) {
                setShown(null)
                return
              }
              reveal.mutate(customer.id, { onSuccess: setShown })
            }}
          >
            <Eye aria-hidden="true" />
          </Button>
        </Can>
      ) : null}
    </div>
  )
}

function DetailRow({ label, value }: { label: string; value?: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 wrap-break-word text-sm text-foreground">{value || '-'}</dd>
    </div>
  )
}

// 合并客户里用的客户搜索选择器：按关键词取候选，Radix Select 不收空串值，占位用哨兵。
function CustomerPicker({
  value,
  onChange,
  excludeId,
  ariaLabel,
}: {
  value: string
  onChange: (id: string) => void
  excludeId?: string
  ariaLabel: string
}) {
  const { t } = useTranslation()
  const [kw, setKw] = useState('')
  const { data } = useCustomers({ keyword: kw || undefined, page: 1, pageSize: 20 })
  const candidates = (data?.list ?? []).filter((c) => c.id !== excludeId)
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <Input
        value={kw}
        onChange={(e) => setKw(e.target.value)}
        placeholder={t('plugin_crm.customer.mergeSearch')}
        aria-label={ariaLabel}
      />
      <Select value={value || '__pick'} onValueChange={onChange}>
        <SelectTrigger aria-label={ariaLabel}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="__pick" disabled>
            {t('plugin_crm.customer.mergePick')}
          </SelectItem>
          {candidates.map((c) => (
            <SelectItem key={c.id} value={c.id}>
              {c.name}
              <span className="ms-2 text-xs text-muted-foreground">{c.code || c.phone}</span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

function MergeDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const merge = useMergeCustomers()
  const [targetId, setTargetId] = useState('')
  const [sourceId, setSourceId] = useState('')
  const canMerge = !!targetId && !!sourceId && targetId !== sourceId
  // 选定两边就拉预览：确认前看到「带走什么、补上什么」——预览与合并是同一份规则的两个投影
  const { data: preview, isLoading: previewLoading } = useMergePreview(open ? targetId : undefined, open ? sourceId : undefined)

  const submit = async () => {
    if (!canMerge) return
    if (!(await confirm({ description: t('plugin_crm.customer.mergeConfirm') }))) return
    try {
      await merge.mutateAsync({ targetId, sourceId })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
      setTargetId('')
      setSourceId('')
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('plugin_crm.customer.mergeTitle')}
      description={t('plugin_crm.customer.mergeHint')}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button disabled={!canMerge || merge.isPending} onClick={() => void submit()}>
            {t('plugin_crm.customer.mergeTitle')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4 text-sm">
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <div className="flex flex-col gap-1">
            <span className="font-medium text-foreground">{t('plugin_crm.customer.mergeTarget')}</span>
            <CustomerPicker
              value={targetId}
              onChange={setTargetId}
              excludeId={sourceId || undefined}
              ariaLabel={t('plugin_crm.customer.mergeTarget')}
            />
            <p className="text-xs text-muted-foreground">{t('plugin_crm.customer.mergeTargetHint')}</p>
          </div>
          <div className="flex flex-col gap-1">
            <span className="font-medium text-foreground">{t('plugin_crm.customer.mergeSource')}</span>
            <CustomerPicker
              value={sourceId}
              onChange={setSourceId}
              excludeId={targetId || undefined}
              ariaLabel={t('plugin_crm.customer.mergeSource')}
            />
            <p className="text-xs text-muted-foreground">{t('plugin_crm.customer.mergeSourceHint')}</p>
          </div>
        </div>
        {canMerge ? (
          <div className="rounded-md border border-border p-3">
            <div className="mb-2 text-xs font-medium text-foreground">{t('plugin_crm.customer.merge.previewTitle')}</div>
            {previewLoading || !preview ? (
              <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
            ) : (
              <div className="space-y-2 text-xs">
                <p>
                  {t('plugin_crm.customer.merge.migrateSummary', {
                    contacts: preview.contacts,
                    opportunities: preview.opportunities,
                    activities: preview.activities,
                  })}
                </p>
                {preview.fillFields.length ? (
                  <div>
                    <span className="text-muted-foreground">{t('plugin_crm.customer.merge.fillTitle')}：</span>
                    <ul className="mt-1 space-y-0.5">
                      {preview.fillFields.map((f) => (
                        <li key={f.column} className="flex gap-2">
                          <span className="text-muted-foreground">{mergeFieldLabel(f.column, t)}</span>
                          <span className="num truncate">← {f.value}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                ) : (
                  <p className="text-muted-foreground">{t('plugin_crm.customer.merge.noFill')}</p>
                )}
                {preview.newTags.length ? (
                  <p>
                    <span className="text-muted-foreground">{t('plugin_crm.customer.merge.tagsTitle')}：</span>
                    {preview.newTags.join('、')}
                  </p>
                ) : null}
                {preview.newCustomFields.length ? (
                  <div>
                    <span className="text-muted-foreground">{t('plugin_crm.customer.merge.customTitle')}：</span>
                    <ul className="mt-1 space-y-0.5">
                      {preview.newCustomFields.map((f) => (
                        <li key={f.code} className="flex gap-2">
                          <span className="text-muted-foreground">{f.label}</span>
                          <span className="num truncate">← {f.value}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                ) : null}
              </div>
            )}
          </div>
        ) : null}
      </div>
    </ResponsiveDialog>
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
                <IdNoRow key={customer.id} customer={customer} />
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
              <p className="mt-1 wrap-break-word whitespace-pre-wrap text-sm text-foreground">{customer.remark}</p>
            </div>
          ) : null}
          <CustomValuesDetail customer={customer} />
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
  const isMobile = useIsMobile()
  // 字段定义是平台级表：写入口按平台调用方收口（与分组/规则/SLA 同一判据）。
  const { isPlatform } = usePermission()
  const [params, setParams] = useSearchParams()
  // URL 是筛选/排序/页码的真源初值：深链与刷新都从 ?keyword=…&mine=1&page=2 恢复同一份视图。
  // 之后状态变化写回 URL（replace，不压历史栈）。
  const initial = useMemo(() => {
    const d: CustomerDraft = {}
    for (const key of ['keyword', 'createdFrom', 'createdTo', 'sortKey'] as const) {
      const v = params.get(key)
      if (v) d[key] = v
    }
    for (const key of ['type', 'status', 'source', 'level', 'tag'] as const) {
      const v = params.get(key)
      if (v) d[key] = v.split(',').filter(Boolean)
    }
    if (params.get('sort') === 'asc') d.sort = 'asc'
    const owner = params.get('pool') === '1' ? POOL : params.get('mine') === '1' ? MINE : ALL
    return { d, owner, page: Math.max(1, Number(params.get('page')) || 1) }
    // 只在挂载时读一次：之后 URL 跟着状态走，不再反向驱动状态
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const [draft, setDraft] = useState<CustomerDraft>(initial.d)
  const [filters, setFilters] = useState<CustomerQuery>(toCustomerQuery(initial.d))
  // 归属筛选：全部 / 只看我的 / 公海。公海走独立接口（不做数据范围），不与 mine 叠加。
  const [ownerFilter, setOwnerFilter] = useState<string>(initial.owner)
  // 低频筛选（来源/等级/标签/建档时间/排序）收在「更多筛选」里：首屏只留四个主条件，
  // 否则 9 个控件会在 1240px 宽度里折成三行，把动作按钮和表格挤成一片。
  const [moreOpen, setMoreOpen] = useState(false)
  const [page, setPage] = useState(initial.page)
  const [pageSize, setPageSize] = useState(10)
  const [formOpen, setFormOpen] = useState(false)
  const [mergeOpen, setMergeOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [editing, setEditing] = useState<CrmCustomer | null>(null)
  const [detail, setDetail] = useState<CrmCustomer | null>(null)
  // 多选状态叫 picked：detail 已经是「当前详情面板选中的行」，别再占一个 selected。
  const [picked, setPicked] = useState<string[]>([])
  // 打标签：单客户整份替换（tagTarget）与批量追加（batchTags， picked 里全选 id）共用一套输入形态。
  const [tagTarget, setTagTarget] = useState<CrmCustomer | null>(null)
  const [batchTagOpen, setBatchTagOpen] = useState(false)
  const [fieldsOpen, setFieldsOpen] = useState(false)
  const me = useAuth((s) => s.user?.username ?? '')
  const batchUpdate = useBatchUpdateCustomers()
  const batchAddTags = useBatchAddCustomerTags()
  const queryClient = useQueryClient()
  // 待跟进清单「查看客户」：先拉完整资料再开详情面板（清单行只有 id 和名字）。
  const onOpenTodoCustomer = async (id: string) => {
    try {
      const cu = await queryClient.fetchQuery({
        queryKey: ['plugin_crm', 'customer', id],
        queryFn: () => fetchCustomer(id),
      })
      setDetail(cu)
      setTodosOpen(false)
    } catch {
      // 错误提示由请求层统一 toast，这里只负责不往下走
    }
  }

  const { data: owned, isLoading: ownedLoading } = useCustomers({
    ...filters,
    mine: ownerFilter === MINE,
    page,
    pageSize,
  })
  const { data: pool, isLoading: poolLoading } = useCustomerPool({
    keyword: filters.keyword, type: filters.type, status: filters.status, source: filters.source,
    page,
    pageSize,
  })
  const inPool = ownerFilter === POOL
  // 「更多筛选」上的数字：收起时也能看出低频条件里还有生效项，避免以为筛选被重置了。
  const moreActive = [draft.source, draft.level, draft.tag, draft.createdFrom, draft.createdTo, draft.sortKey].filter((v) => (Array.isArray(v) ? v.length : v)).length
  const data = inPool ? pool : owned
  const isLoading = inPool ? poolLoading : ownedLoading
  const remove = useDeleteCustomer()
  const removeBatch = useDeleteCustomers()
  const claim = useClaimCustomer()
  const release = useReleaseCustomer()
  const [exporting, setExporting] = useState(false)
  const { data: tagOptions } = useCustomerTags()
  // 导出要带自定义字段：表头用字段定义里的 label，值按 code 从客户身上取。
  const { data: customFields = [] } = useCustomerFields()
  const [duplicatesOpen, setDuplicatesOpen] = useState(false)
  // 待跟进弹窗：站内信的深链是 /crm/customers?todo=1，落地即弹（之后 URL 清掉、状态接管）。
  const [todosOpen, setTodosOpen] = useState(() => params.get('todo') === '1')

  // 状态 → URL（replace 不压历史栈）：分享/刷新/切页回来都是同一份视图
  useEffect(() => {
    const p = new URLSearchParams()
    for (const key of ['keyword', 'type', 'status', 'source', 'level', 'tag', 'createdFrom', 'createdTo', 'sortKey'] as const) {
      const v = filters[key]
      if (typeof v === 'string' && v) p.set(key, v)
    }
    if (filters.sort === 'asc') p.set('sort', 'asc')
    if (ownerFilter === MINE) p.set('mine', '1')
    if (ownerFilter === POOL) p.set('pool', '1')
    if (page > 1) p.set('page', String(page))
    setParams(p, { replace: true })
  }, [filters, ownerFilter, page, setParams])

  // 批量改动作共用一个提交壳：成功提示实改数（范围外的 id 后端静默跳过，不是全成功）。
  const runBatch = async (run: () => Promise<{ updated: number }>) => {
    try {
      const { updated } = await run()
      setPicked([])
      toast.success(t('plugin_crm.batch.updated', { updated }))
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  const onBatchUpdate = (ids: string[], patch: { owner?: string; status?: 'active' | 'inactive'; level?: string }) =>
    runBatch(() => batchUpdate.mutateAsync({ ids, ...patch }))

  const onBatchAddTags = (ids: string[], tags: string[]) => runBatch(() => batchAddTags.mutateAsync({ ids, tags }))

  const onClaim = async (cu: CrmCustomer) => {
    try {
      await claim.mutateAsync(cu.id)
      toast.success(t('plugin_crm.pool.claimed', { name: cu.name }))
    } catch {
      // 失败原因已由请求层统一提示（如刚被别人领走）。
    }
  }

  const onRelease = async (cu: CrmCustomer) => {
    if (!(await confirm({ description: t('plugin_crm.pool.releaseConfirm', { name: cu.name }) }))) return
    try {
      await release.mutateAsync(cu.id)
      toast.success(t('common.operateSuccess'))
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  const onExport = async () => {
    setExporting(true)
    try {
      const rows = await fetchCustomersForExport({ ...filters })
      const stamp = new Date().toISOString().slice(0, 10)
      // 自定义字段按定义顺序追加在固定列之后；国家/国籍导出 ISO 代码而非译名，
      // 导出的文件才能按同一张导入模板原样回灌（导入只认两字母代码）。
      const extraFields = customFields.filter((f) => f.enabled)
      const customValue = (cu: CrmCustomer, f: CrmField) => {
        const v = cu.customValues?.[f.code]
        if (v === undefined || v === null) return ''
        return typeof v === 'boolean' ? (v ? 'true' : 'false') : String(v)
      }
      downloadCsv(`crm-customers-${stamp}.csv`, [
        [
          t('plugin_crm.fields.name'), t('plugin_crm.fields.type'), t('plugin_crm.fields.code'),
          t('plugin_crm.fields.level'), t('plugin_crm.fields.identity'), t('plugin_crm.fields.phone'),
          t('plugin_crm.fields.email'), t('plugin_crm.fields.country'), t('plugin_crm.fields.nationality'),
          t('plugin_crm.fields.tags'), t('plugin_crm.fields.owner'), t('plugin_crm.fields.status'),
          t('plugin_crm.fields.externalAnchor'), ...extraFields.map((f) => f.label),
          t('plugin_crm.fields.createdAt'),
        ],
        ...rows.map((cu) => [
          cu.name, t(`plugin_crm.type.${cu.type ?? 'person'}`), cu.code ?? '',
          cu.level ?? '', cu.type === 'company' ? (cu.taxNo ?? '') : (cu.idNo ?? ''), cu.phone ?? '',
          cu.email ?? '', cu.country ?? '', cu.nationality ?? '', (cu.tags ?? []).join('、'),
          cu.owner ?? '', t(`plugin_crm.status.${cu.status}`),
          cu.externalSource ? `${cu.externalSource}/${cu.externalNo ?? ''}` : '',
          ...extraFields.map((f) => customValue(cu, f)),
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
        <div className="min-w-0">
          <span className="font-medium text-foreground">{cu.name}</span>
          <div className="max-w-[14rem] truncate text-xs text-muted-foreground" title={cu.code || cu.id}>
            {cu.code || cu.id}
          </div>
          {(cu.tags?.length ?? 0) > 0 ? (
            // 标签挂在名字副行而不是独立一列：标签数不定，一列会被撑得忽宽忽窄
            <div className="mt-0.5 flex flex-wrap gap-1" title={cu.tags!.join('、')}>
              {cu.tags!.slice(0, 3).map((tag) => (
                <Badge key={tag} variant="secondary" className="px-1.5 py-0 text-[10px]">
                  {tag}
                </Badge>
              ))}
              {cu.tags!.length > 3 ? <span className="text-[10px] text-muted-foreground">+{cu.tags!.length - 3}</span> : null}
            </div>
          ) : null}
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
        // 证件号是 15-64 位不可断数字：截断 + 悬停看全量
        return value ? (
          <span className="num block max-w-[14rem] truncate" title={value}>
            {value}
          </span>
        ) : (
          <span className="text-muted-foreground">-</span>
        )
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
      render: (cu) => {
        if (!cu.externalSource) return <span className="text-muted-foreground">-</span>
        // externalNo 无长度上限：徽章限宽截断，悬停看全量
        const text = `${t(`plugin_crm.source.${cu.externalSource}`, { defaultValue: cu.externalSource })}/${cu.externalNo}`
        return (
          <Badge variant="outline" className="max-w-48" title={text}>
            <span className="min-w-0 truncate">{text}</span>
          </Badge>
        )
      },
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
          setFilters(toCustomerQuery(draft))
          setPage(1)
        }}
        onReset={() => {
          setDraft({})
          setFilters({})
          setOwnerFilter(ALL)
          setPage(1)
        }}
      >
        <Input
          className="md:w-80"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_crm.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <OptionPicker
          title={t('plugin_crm.fields.type')}
          value={draft.type ?? []}
          onChange={(v) => setDraft((d) => ({ ...d, type: v.length ? v : undefined }))}
          options={TYPES.map((tp) => ({ value: tp, label: t(`plugin_crm.type.${tp}`) }))}
          className="md:w-36"
          placeholder={t('plugin_crm.fields.type')}
        />
        <OptionPicker
          title={t('plugin_crm.fields.status')}
          value={draft.status ?? []}
          onChange={(v) => setDraft((d) => ({ ...d, status: v.length ? v : undefined }))}
          options={STATUSES.map((s) => ({ value: s, label: t(`plugin_crm.status.${s}`) }))}
          className="md:w-36"
          placeholder={t('plugin_crm.fields.status')}
        />
        {/* 归属：全部 / 只看我的 / 公海。公海走独立列表，对所有人开放。 */}
        <Select value={ownerFilter} onValueChange={setOwnerFilter}>
          <SelectTrigger className="md:w-32" aria-label={t('plugin_crm.fields.owner')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_crm.filter.allOwners')}</SelectItem>
            <SelectItem value={MINE}>{t('plugin_crm.mineOnly')}</SelectItem>
            <SelectItem value={POOL}>{t('plugin_crm.pool.only')}</SelectItem>
          </SelectContent>
        </Select>
        {!isMobile ? (
          <Button
            type="button"
            variant={moreOpen ? 'secondary' : 'outline'}
            aria-expanded={moreOpen}
            aria-controls="crm-more-filters"
            onClick={() => setMoreOpen((v) => !v)}
          >
            <SlidersHorizontal className="me-2 size-4" aria-hidden="true" />
            {t(moreOpen ? 'plugin_crm.filter.less' : 'plugin_crm.filter.more')}
            {moreActive > 0 ? (
              <Badge variant="secondary" className="ms-1.5">
                {moreActive}
              </Badge>
            ) : null}
            <ChevronDown
              aria-hidden="true"
              className={moreOpen ? 'ms-1 size-4 rotate-180 transition-transform' : 'ms-1 size-4 transition-transform'}
            />
          </Button>
        ) : null}
        {moreOpen || isMobile ? (
          <div
            id="crm-more-filters"
            className="order-1 flex w-full basis-full flex-col gap-3 md:flex-row md:flex-wrap md:items-center md:rounded-lg md:border md:border-border md:bg-muted/30 md:p-3"
          >
            <OptionPicker
              title={t('plugin_crm.fields.externalSource')}
              value={draft.source ?? []}
              onChange={(v) => setDraft((d) => ({ ...d, source: v.length ? v : undefined }))}
              options={SOURCES.map((s) => ({ value: s, label: t(`plugin_crm.source.${s}`, { defaultValue: s }) }))}
              className="md:w-40"
              placeholder={t('plugin_crm.fields.externalSource')}
            />
            <OptionPicker
              title={t('plugin_crm.fields.level')}
              value={draft.level ?? []}
              onChange={(v) => setDraft((d) => ({ ...d, level: v.length ? v : undefined }))}
              options={LEVELS.map((lv) => ({ value: lv, label: t(`plugin_crm.level.${lv}`, { defaultValue: lv }) }))}
              className="md:w-36"
              placeholder={t('plugin_crm.fields.level')}
            />
            <OptionPicker
              title={t('plugin_crm.fields.tags')}
              value={draft.tag ?? []}
              onChange={(v) => setDraft((d) => ({ ...d, tag: v.length ? v : undefined }))}
              options={(tagOptions ?? []).map((tag) => ({ value: tag, label: tag }))}
              className="md:w-40"
              placeholder={t('plugin_crm.fields.tags')}
            />
            {/* 建档时间范围：含端点，后端按「to 当天结束前」收口 */}
            <div className="flex items-center gap-1.5">
              <DatePicker
                value={draft.createdFrom ?? null}
                onChange={(v) => setDraft((d) => ({ ...d, createdFrom: v || undefined }))}
                ariaLabel={t('plugin_crm.filter.createdFrom')}
                placeholder={t('plugin_crm.filter.createdFrom')}
                className="md:w-40"
              />
              <span className="text-xs text-muted-foreground">–</span>
              <DatePicker
                value={draft.createdTo ?? null}
                onChange={(v) => setDraft((d) => ({ ...d, createdTo: v || undefined }))}
                ariaLabel={t('plugin_crm.filter.createdTo')}
                placeholder={t('plugin_crm.filter.createdTo')}
                className="md:w-40"
              />
            </div>
            {/* 排序：白名单字段 + 方向切换（与工单筛选同款交互，不用点列头） */}
            <div className="flex items-center gap-1.5">
              <Select value={draft.sortKey ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, sortKey: v === ALL ? undefined : v }))}>
                <SelectTrigger className="md:w-48" aria-label={t('plugin_crm.filter.sortKey')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>{t('plugin_crm.filter.sortDefault')}</SelectItem>
                  {SORT_KEYS.map((k) => (
                    <SelectItem key={k} value={k}>
                      {t(`plugin_crm.sort.${k}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {draft.sortKey ? (
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="size-9"
                  aria-label={t('plugin_crm.filter.sortDir', { dir: draft.sort === 'asc' ? t('plugin_crm.sort.asc') : t('plugin_crm.sort.desc') })}
                  title={draft.sort === 'asc' ? t('plugin_crm.sort.asc') : t('plugin_crm.sort.desc')}
                  onClick={() => setDraft((d) => ({ ...d, sort: d.sort === 'asc' ? 'desc' : 'asc' }))}
                >
                  <ArrowDownUp aria-hidden="true" className="size-4" />
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}
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
          <>
            <Can perm="crm:edit">
              {/* 批量条第二动作：派给我/状态/等级/追加标签。派给我 = owner 填当前账号，
                  公海客户也能用它一键认领整批。 */}
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" size="sm">
                    {t('plugin_crm.batch.title')}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="min-w-40">
                  <DropdownMenuItem onSelect={() => void onBatchUpdate(ids, { owner: me })}>
                    {t('plugin_crm.batch.assignToMe')}
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => void onBatchUpdate(ids, { status: 'active' })}>
                    {t('plugin_crm.status.active')}
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => void onBatchUpdate(ids, { status: 'inactive' })}>
                    {t('plugin_crm.status.inactive')}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  {LEVELS.map((lv) => (
                    <DropdownMenuItem key={lv} onSelect={() => void onBatchUpdate(ids, { level: lv })}>
                      {t('plugin_crm.batch.setLevel', { level: t(`plugin_crm.level.${lv}`, { defaultValue: lv }) })}
                    </DropdownMenuItem>
                  ))}
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onSelect={() => setBatchTagOpen(true)}>
                    <TagIcon aria-hidden="true" />
                    {t('plugin_crm.batch.addTags')}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
                <Trash2 aria-hidden="true" />
                {t('common.delete')}
              </Button>
            </Can>
          </>
        )}
        toolbar={
          <>
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
            <Can perm="crm:edit">
              <Button variant="outline" disabled={inPool} onClick={() => setMergeOpen(true)}>
                <Combine aria-hidden="true" />
                {t('plugin_crm.customer.mergeTitle')}
              </Button>
            </Can>
            {/* 导入与导出入对出现；执行权限归导入中心（imports:run），没有它的人看不到入口。 */}
            <Can perm="imports:run">
              <Button variant="outline" onClick={() => setImportOpen(true)}>
                <Upload aria-hidden="true" />
                {t('plugin_crm.import.action')}
              </Button>
            </Can>
            <Button variant="outline" onClick={() => void onExport()} disabled={exporting || inPool} title={inPool ? t('plugin_crm.pool.exportDisabled') : undefined}>
              <Download className="me-2 size-4" aria-hidden="true" />
              {t('common.export')}
            </Button>
            {/* 低频动作收进「更多」：疑似重复是巡检工具、待跟进是当日清单、自定义字段是平台设置，
                都不该和新客户/合并/导出抢同一排视觉重量。 */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline">
                  {t('common.more')}
                  <ChevronDown aria-hidden="true" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                <DropdownMenuItem onSelect={() => setDuplicatesOpen(true)}>
                  <CopyPlus aria-hidden="true" />
                  {t('plugin_crm.duplicates.action')}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setTodosOpen(true)}>
                  <CalendarClock aria-hidden="true" />
                  {t('plugin_crm.todos.action')}
                </DropdownMenuItem>
                <Can perm="crm:edit">
                  <DropdownMenuItem
                    disabled={!isPlatform}
                    title={!isPlatform ? t('plugin_crm.fields.platformOnly') : undefined}
                    onSelect={() => setFieldsOpen(true)}
                  >
                    <Settings2 aria-hidden="true" />
                    {t('plugin_crm.fields.manage')}
                  </DropdownMenuItem>
                </Can>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        }
        rowActions={(cu) => (
          <>
            <Button variant="ghost" size="icon" aria-label={t('common.detail')} onClick={() => setDetail(cu)}>
              <Eye aria-hidden="true" />
            </Button>
            {inPool ? (
              <Can perm="crm:pool">
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t('plugin_crm.pool.claim')}
                  title={t('plugin_crm.pool.claim')}
                  disabled={claim.isPending}
                  onClick={() => void onClaim(cu)}
                >
                  <Hand aria-hidden="true" />
                </Button>
              </Can>
            ) : (
              <>
                <Can perm="crm:edit">
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={t('plugin_crm.fields.tags')}
                    title={t('plugin_crm.fields.tags')}
                    onClick={() => setTagTarget(cu)}
                  >
                    <TagIcon aria-hidden="true" />
                  </Button>
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
                </Can>
                <Can perm="crm:pool">
                  {cu.owner ? (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={t('plugin_crm.pool.release')}
                      title={t('plugin_crm.pool.release')}
                      disabled={release.isPending}
                      onClick={() => void onRelease(cu)}
                    >
                      <UserMinus aria-hidden="true" />
                    </Button>
                  ) : null}
                </Can>
                <Can perm="crm:edit">
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
          </>
        )}
      />
      <CustomerFormDialog
        key={formOpen ? (editing?.id ?? 'new') : 'closed'}
        open={formOpen}
        onOpenChange={setFormOpen}
        editing={editing}
      />
      {mergeOpen ? <MergeDialog open={mergeOpen} onOpenChange={setMergeOpen} /> : null}
      <CustomerImportDialog open={importOpen} onOpenChange={setImportOpen} />
      {detail ? <CustomerDetailDialog key={detail.id} customer={detail} onClose={() => setDetail(null)} /> : null}
      <CustomerTagDialog
        key={tagTarget?.id ?? 'tags-closed'}
        open={!!tagTarget}
        customer={tagTarget}
        suggestions={tagOptions ?? []}
        onOpenChange={(v) => !v && setTagTarget(null)}
      />
      <BatchTagDialog open={batchTagOpen} onOpenChange={setBatchTagOpen} count={picked.length} suggestions={tagOptions ?? []} onSave={(tags) => void onBatchAddTags(picked, tags)} />
      {fieldsOpen ? <FieldsDialog open={fieldsOpen} onOpenChange={setFieldsOpen} /> : null}
      <DuplicatesDialog open={duplicatesOpen} onOpenChange={setDuplicatesOpen} />
      {todosOpen ? (
        <TodosDialog
          open={todosOpen}
          onOpenChange={setTodosOpen}
          onOpenCustomer={(id) => void onOpenTodoCustomer(id)}
        />
      ) : null}
    </div>
  )
}

/** 疑似重复巡检：手机号/证件号/信用代码同值多客户的分组，引导去合并。 */
function DuplicatesDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const { data: groups, isLoading } = useCustomerDuplicates(open)
  return (
    <ResponsiveDialog open={open} onOpenChange={onOpenChange} size="lg" title={t('plugin_crm.duplicates.title')} description={t('plugin_crm.duplicates.hint')}>
      <div className="flex flex-col gap-3 p-4">
        {isLoading ? (
          <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
        ) : !groups?.length ? (
          <p className="py-6 text-center text-sm text-muted-foreground">{t('plugin_crm.duplicates.empty')}</p>
        ) : (
          groups.map((g) => (
            <div key={`${g.key}:${g.value}`} className="rounded-md border border-border">
              <div className="flex items-center gap-2 border-b border-border px-3 py-2 text-sm">
                <Badge variant="warning">{t(`plugin_crm.duplicates.key.${g.key}`)}</Badge>
                <span className="num font-mono">{g.value}</span>
                <span className="text-xs text-muted-foreground">{t('plugin_crm.duplicates.count', { count: g.customers.length })}</span>
                <span className="ms-auto text-xs text-muted-foreground">{t('plugin_crm.duplicates.mergeHint')}</span>
              </div>
              <ul className="divide-y divide-border">
                {g.customers.map((cu) => (
                  <li key={cu.id} className="flex flex-wrap items-center gap-x-3 gap-y-0.5 px-3 py-2 text-sm">
                    <span className="font-medium text-foreground">{cu.name}</span>
                    <span className="num text-xs text-muted-foreground">{cu.code || cu.id}</span>
                    <span className="text-xs text-muted-foreground">{t(`plugin_crm.type.${cu.type}`)}</span>
                    <span className="text-xs text-muted-foreground">{cu.owner ? t('plugin_crm.fields.owner') + ': ' + cu.owner : t('plugin_crm.pool.only')}</span>
                  </li>
                ))}
              </ul>
            </div>
          ))
        )}
      </div>
    </ResponsiveDialog>
  )
}

/**
 * 我的待跟进：到期未完成的跟进记录（可见性继承客户归属，后端按 owner 裁）。
 * 行动只有一个——去处理：点「查看客户」拉完整资料开详情面板。
 */
function TodosDialog({
  open,
  onOpenChange,
  onOpenCustomer,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onOpenCustomer: (id: string) => void
}) {
  const { t } = useTranslation()
  const { data, isLoading } = useCustomerTodos(open)
  const items = data?.items ?? []
  return (
    <ResponsiveDialog open={open} onOpenChange={onOpenChange} size="lg" title={t('plugin_crm.todos.title')} description={t('plugin_crm.todos.hint')}>
      <div className="flex flex-col gap-3 p-4">
        {isLoading ? (
          <Skeleton className="h-40 w-full" />
        ) : !items.length ? (
          <p className="py-6 text-center text-sm text-muted-foreground">{t('plugin_crm.todos.empty')}</p>
        ) : (
          <>
            {data && data.total > items.length ? (
              <p className="text-xs text-muted-foreground">{t('plugin_crm.todos.total', { total: data.total })}</p>
            ) : null}
            <ul className="divide-y divide-border">
              {items.map((td) => (
                <li key={td.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-1 py-2.5 text-sm">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium text-foreground">{td.customerName}</span>
                      <Badge variant="outline">{t(`plugin_crm.activity.kind.${td.kind}`)}</Badge>
                      {td.overdue ? <Badge variant="destructive">{t('plugin_crm.todos.overdue')}</Badge> : null}
                    </div>
                    <p className="truncate text-xs text-muted-foreground">{td.content}</p>
                  </div>
                  <span className="num text-xs text-muted-foreground">{td.nextAt}</span>
                  <Button variant="outline" size="sm" onClick={() => onOpenCustomer(td.customerId)}>
                    {t('plugin_crm.todos.open')}
                  </Button>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </ResponsiveDialog>
  )
}

/** TagEditor 是标签输入的最小形态：回车/逗号添加、点 X 移除、来自全站使用中标签的建议。 */
function TagEditor({ tags, onChange, suggestions }: { tags: string[]; onChange: (tags: string[]) => void; suggestions: string[] }) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState('')
  const add = () => {
    const tag = draft.trim()
    if (!tag || tags.includes(tag)) {
      setDraft('')
      return
    }
    onChange([...tags, tag])
    setDraft('')
  }
  const rest = suggestions.filter((s) => !tags.includes(s)).slice(0, 8)
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Input
          value={draft}
          maxLength={20}
          placeholder={t('plugin_crm.tags.inputPlaceholder')}
          aria-label={t('plugin_crm.fields.tags')}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              add()
            }
          }}
          onBlur={add}
        />
        <Button type="button" variant="outline" size="sm" onClick={add}>
          {t('common.add')}
        </Button>
      </div>
      {tags.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {tags.map((tag) => (
            <Badge key={tag} variant="secondary" className="gap-1 px-2 py-0.5 text-xs">
              {tag}
              <button
                type="button"
                aria-label={`${t('common.remove')} ${tag}`}
                onClick={() => onChange(tags.filter((x) => x !== tag))}
                className="text-muted-foreground hover:text-foreground"
              >
                <X aria-hidden="true" className="size-3" />
              </button>
            </Badge>
          ))}
        </div>
      ) : null}
      {rest.length > 0 ? (
        <div className="flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
          <span>{t('plugin_crm.tags.suggestions')}</span>
          {rest.map((s) => (
            <button
              key={s}
              type="button"
              className="rounded-full border border-border px-2 py-0.5 text-xs hover:bg-muted"
              onClick={() => onChange([...tags, s])}
            >
              + {s}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}

/**
 * 详情里的自定义字段展示：只显示有值的 code，名称按启用中的字段定义翻译；
 * 定义已停用/删除的旧值不显示（与工单同一口径），而不是显示一个没人认识的 code。
 */
function CustomValuesDetail({ customer }: { customer: CrmCustomer }) {
  const { t } = useTranslation()
  const entries = Object.entries(customer.customValues ?? {}).filter(([, v]) => v !== null && v !== '')
  const { data: fields } = useCustomerFields(entries.length > 0)
  if (!entries.length) return null
  const labelOf = (code: string) => fields?.find((f) => f.code === code)?.label ?? code
  const show = (v: unknown) => (typeof v === 'boolean' ? t(v ? 'common.yes' : 'common.no') : String(v))
  return (
    <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2">
      {entries.map(([code, v]) => (
        <DetailRow key={code} label={labelOf(code)} value={show(v)} />
      ))}
    </dl>
  )
}

/** 单客户打标：整份替换该客户的标签（与后端 PUT /customers/:id/tags 同语义）。 */function CustomerTagDialog({
  open,
  onOpenChange,
  customer,
  suggestions,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  customer: CrmCustomer | null
  suggestions: string[]
}) {
  const { t } = useTranslation()
  const save = useSetCustomerTags()
  // 弹窗在调用处以 key={customer.id} 挂载：每个客户各挂一次，初值天然是它的当前标签，
  // 关闭即丢草稿，不需要额外的 hydration 状态。
  const [tags, setTags] = useState<string[]>(customer?.tags ?? [])
  if (!open || !customer) return null
  return (
    <ResponsiveDialog open={open} onOpenChange={onOpenChange} size="sm" title={t('plugin_crm.tags.title', { name: customer.name })}>
      <div className="space-y-3 p-4">
        <TagEditor tags={tags} onChange={setTags} suggestions={suggestions} />
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button
            disabled={save.isPending}
            onClick={() => {
              void save
                .mutateAsync({ id: customer.id, tags })
                .then(() => {
                  toast.success(t('common.operateSuccess'))
                  onOpenChange(false)
                })
                .catch(() => undefined)
            }}
          >
            {t('common.save')}
          </Button>
        </div>
      </div>
    </ResponsiveDialog>
  )
}

const FIELD_TYPES = ['text', 'number', 'select', 'date', 'bool'] as const

function emptyField(): CrmField {
  return { id: '', code: '', label: '', type: 'text', options: [], required: false, enabled: true, sort: 0 }
}

/**
 * 自定义字段管理：列表（含停用）+ 内嵌的增改表单 + 删除。字段定义是平台级表，
 * 入口只在平台调用方可见（isPlatform 已在工具栏门控），后端也以 crm:edit 二次把关。
 */
function FieldsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data: fields, isLoading } = useCustomerFields(open)
  const save = useSaveCustomerField()
  const del = useDeleteCustomerField()
  const [draft, setDraft] = useState<CrmField | null>(null)

  const closeForm = () => setDraft(null)

  const onDelete = async (f: CrmField) => {
    if (!(await confirm({ destructive: true, description: t('plugin_crm.customer.field.deleteConfirm', { label: f.label }) }))) return
    try {
      await del.mutateAsync(f.id)
      toast.success(t('common.operateSuccess'))
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  return (
    <ResponsiveDialog open={open} onOpenChange={onOpenChange} size="lg" title={t('plugin_crm.customer.field.title')} description={t('plugin_crm.customer.field.hint')}>
      <div className="flex flex-col gap-3 p-4 text-sm">
        {draft === null ? (
          <>
            <Button variant="outline" size="sm" className="self-start" onClick={() => setDraft({ ...emptyField() })}>
              <Plus aria-hidden="true" />
              {t('plugin_crm.customer.field.add')}
            </Button>
            {isLoading ? (
              <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
            ) : !fields?.length ? (
              <p className="py-6 text-center text-xs text-muted-foreground">{t('plugin_crm.customer.field.empty')}</p>
            ) : (
              <ul className="divide-y divide-border rounded-md border border-border">
                {fields.map((f) => (
                  <li key={f.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
                    <span className="font-medium text-foreground">{f.label}</span>
                    <span className="num font-mono text-xs text-muted-foreground">{f.code}</span>
                    <Badge variant="outline">{t(`plugin_crm.customer.field.type.${f.type}`)}</Badge>
                    {f.required ? <Badge variant="warning">{t('plugin_crm.customer.field.required')}</Badge> : null}
                    {!f.enabled ? <Badge variant="secondary">{t('plugin_crm.customer.field.disabled')}</Badge> : null}
                    <div className="ms-auto flex items-center gap-1">
                      <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => setDraft(f)}>
                        <Pencil aria-hidden="true" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-destructive"
                        aria-label={t('common.delete')}
                        onClick={() => void onDelete(f)}
                      >
                        <Trash2 aria-hidden="true" />
                      </Button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </>
        ) : (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault()
              void save
                .mutateAsync(draft)
                .then(() => {
                  toast.success(t('common.operateSuccess'))
                  closeForm()
                })
                .catch(() => undefined)
            }}
          >
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <label className="flex flex-col gap-1">
                <span>
                  {t('plugin_crm.customer.field.label')}
                  <RequiredMark />
                </span>
                <Input maxLength={64} value={draft.label} onChange={(e) => setDraft({ ...draft, label: e.target.value })} />
              </label>
              <label className="flex flex-col gap-1">
                <span>{t('plugin_crm.customer.field.codeLabel')}</span>
                <Input
                  className="font-mono"
                  value={draft.code}
                  disabled={!!draft.id}
                  placeholder="vip_level"
                  onChange={(e) => setDraft({ ...draft, code: e.target.value })}
                />
                <span className="text-[11px] text-muted-foreground">{t('plugin_crm.customer.field.codeHint')}</span>
              </label>
              <label className="flex flex-col gap-1">
                <span>{t('plugin_crm.customer.field.typeLabel')}</span>
                <Select value={draft.type} onValueChange={(v) => setDraft({ ...draft, type: v as CrmField['type'], options: [] })}>
                  <SelectTrigger aria-label={t('plugin_crm.customer.field.typeLabel')}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {FIELD_TYPES.map((tp) => (
                      <SelectItem key={tp} value={tp}>
                        {t(`plugin_crm.customer.field.type.${tp}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </label>
              {draft.type === 'select' ? (
                <label className="flex flex-col gap-1">
                  <span>
                    {t('plugin_crm.customer.field.optionsLabel')}
                    <RequiredMark />
                  </span>
                  <Input
                    value={draft.options.join(',')}
                    placeholder={t('plugin_crm.customer.field.optionsPlaceholder')}
                    onChange={(e) => setDraft({ ...draft, options: e.target.value.split(',') })}
                  />
                  <span className="text-[11px] text-muted-foreground">{t('plugin_crm.customer.field.optionsHint')}</span>
                </label>
              ) : null}
              <label className="flex flex-col gap-1">
                <span>{t('plugin_crm.customer.field.sortLabel')}</span>
                <Input inputMode="numeric" value={draft.sort} onChange={(e) => setDraft({ ...draft, sort: Number(e.target.value) || 0 })} />
              </label>
              <label className="flex items-center gap-2 pt-5 text-sm">
                <Switch checked={draft.required} onCheckedChange={(v) => setDraft({ ...draft, required: v })} />
                {t('plugin_crm.customer.field.required')}
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Switch checked={draft.enabled} onCheckedChange={(v) => setDraft({ ...draft, enabled: v })} />
                {t('plugin_crm.customer.field.enabledLabel')}
              </label>
            </div>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={closeForm}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" disabled={save.isPending}>
                {t('common.save')}
              </Button>
            </div>
          </form>
        )}
      </div>
    </ResponsiveDialog>
  )
}

/** 批量追加标签：给勾选的客户每人补上这些词（已有该词的自动跳过）。 */
function BatchTagDialog({
  open,
  onOpenChange,
  count,
  suggestions,
  onSave,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  count: number
  suggestions: string[]
  onSave: (tags: string[]) => void
}) {
  const { t } = useTranslation()
  const [tags, setTags] = useState<string[]>([])
  if (!open) return null
  return (
    <ResponsiveDialog open={open} onOpenChange={onOpenChange} size="sm" title={t('plugin_crm.tags.batchTitle', { count })}>
      <div className="space-y-3 p-4">
        <TagEditor tags={tags} onChange={setTags} suggestions={suggestions} />
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button
            disabled={tags.length === 0}
            onClick={() => {
              onSave(tags)
              setTags([])
              onOpenChange(false)
            }}
          >
            {t('common.save')}
          </Button>
        </div>
      </div>
    </ResponsiveDialog>
  )
}
