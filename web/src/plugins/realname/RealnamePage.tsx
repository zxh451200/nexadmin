import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Download, ShieldCheck, Settings2, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchRow } from '@/framework/components/SwitchField'
import { OptionPicker } from '@/framework/components/OptionPicker'
import { pageAfterDelete } from '@/framework/hooks/page-fallback'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useCountryNames } from '@/framework/components/CountrySelect'
import {
  STATUSES,
  exportVerifications,
  useConfig,
  useDeleteVerifications,
  useParse,
  useSaveConfig,
  useVerifications,
  useVerify,
  type ParseResult,
  type Verification,
  type VerifyConfig,
  type VerifyStatus,
} from './api'

const ALL = '__all'
const FOLD = '__fold'
const MASK = '********'

// 格式结论与在线结论分开着色：valid 只是"号码合规则"，不该显示成绿色通过。
const STATUS_TONE: Record<VerifyStatus, 'success' | 'secondary' | 'warning' | 'destructive'> = {
  match: 'success',
  valid: 'secondary',
  error: 'warning',
  invalid: 'destructive',
  mismatch: 'destructive',
}

const formatTime = (s: string) => (s ? new Date(s).toLocaleString() : '-')

function StatusBadge({ status }: { status: VerifyStatus }) {
  const { t } = useTranslation()
  return (
    <Badge variant={STATUS_TONE[status] ?? 'secondary'}>
      {t(`plugin_realname.status.${status}`, { defaultValue: status })}
    </Badge>
  )
}

function ConfigDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const { data: cfg } = useConfig(open)
  const save = useSaveConfig()
  const [draft, setDraft] = useState<VerifyConfig | null>(null)
  const [secret, setSecret] = useState('')

  const value = draft ?? cfg
  const set = (patch: Partial<VerifyConfig>) => setDraft({ ...(value as VerifyConfig), ...patch })

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!value) return
    try {
      await save.mutateAsync({ ...value, appSecret: secret || MASK })
      toast.success(t('common.operateSuccess'))
      setDraft(null)
      setSecret('')
      onOpenChange(false)
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="md"
      title={t('plugin_realname.configTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="realname-config-form" disabled={!value || save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      {!value ? (
        <p className="py-6 text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : (
        <form id="realname-config-form" onSubmit={onSubmit} className="flex flex-col gap-3">
          <SwitchRow
            label={t('plugin_realname.enabled')}
            description={t('plugin_realname.enabledHint')}
            checked={value.enabled}
            onCheckedChange={(v) => set({ enabled: v })}
          />
          <label className="flex flex-col gap-1 text-sm">
            <span>{t('plugin_realname.provider')}</span>
            <Select value={value.provider || ALL} onValueChange={(v) => set({ provider: v === ALL ? '' : v })}>
              <SelectTrigger aria-label={t('plugin_realname.provider')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{t('plugin_realname.providerNone')}</SelectItem>
                <SelectItem value="generic">{t('plugin_realname.providerGeneric')}</SelectItem>
              </SelectContent>
            </Select>
          </label>
          <label className="flex flex-col gap-1 text-sm">
            <span>
              {value.enabled && <RequiredMark />}
              {t('plugin_realname.endpoint')}
            </span>
            <Input
              value={value.endpoint}
              onChange={(e) => set({ endpoint: e.target.value })}
              placeholder="https://verify.example.com/idcard/match"
              autoComplete="off"
            />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            <span>{t('plugin_realname.appKey')}</span>
            <Input value={value.appKey} onChange={(e) => set({ appKey: e.target.value })} autoComplete="off" />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            <span>{t('plugin_realname.appSecret')}</span>
            <Input
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              placeholder={value.hasSecret ? MASK : undefined}
              autoComplete="new-password"
            />
            <span className="text-xs text-muted-foreground">{t('plugin_realname.secretHint')}</span>
          </label>
          <div className="grid gap-3 md:grid-cols-2">
            <label className="flex flex-col gap-1 text-sm">
              <span>{t('plugin_realname.timeoutMs')}</span>
              <Input
                type="number"
                min={1000}
                value={value.timeoutMs}
                onChange={(e) => set({ timeoutMs: Number(e.target.value) || 0 })}
              />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span>{t('plugin_realname.dailyLimit')}</span>
              <Input
                type="number"
                min={0}
                value={value.dailyLimit}
                onChange={(e) => set({ dailyLimit: Number(e.target.value) || 0 })}
              />
            </label>
          </div>
        </form>
      )}
    </ResponsiveDialog>
  )
}

export default function RealnamePage() {
  const { t } = useTranslation()
  const countryName = useCountryNames()
  const confirm = useConfirm()
  const { has } = usePermission()
  const [name, setName] = useState('')
  const [number, setNumber] = useState('')
  const [remark, setRemark] = useState('')
  const [parsed, setParsed] = useState<ParseResult | null>(null)
  const [configOpen, setConfigOpen] = useState(false)
  const parse = useParse()
  const verify = useVerify()

  const [draft, setDraft] = useState<{ keyword?: string; status?: string[]; all?: '1'; range?: DateRangeValue }>({})
  const [filters, setFilters] = useState<{ keyword?: string; status?: string; all?: '1'; range?: DateRangeValue }>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const { data, isLoading } = useVerifications({
    keyword: filters.keyword, status: filters.status, all: filters.all,
    from: filters.range?.from, to: filters.range?.to,
    page, pageSize,
  })
  const del = useDeleteVerifications()
  const [selected, setSelected] = useState<string[]>([])

  async function onDelete(row: Verification) {
    if (!(await confirm({ destructive: true, description: t('plugin_realname.deleteConfirm') }))) return
    try {
      const rows = data?.list ?? []
      await del.mutateAsync([row.id])
      setPage(pageAfterDelete(page, rows.length, 1))
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  async function onBatchDelete(ids: string[]) {
    if (!(await confirm({ destructive: true, description: t('plugin_realname.deleteSelectedConfirm', { count: ids.length }) }))) return
    try {
      await del.mutateAsync(ids)
      setPage(pageAfterDelete(page, data?.list?.length ?? 0, ids.length))
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  async function onExport() {
    try {
      await exportVerifications({ keyword: filters.keyword, status: filters.status, from: filters.range?.from, to: filters.range?.to, all: filters.all })
      toast.success(t('common.operateSuccess'))
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  async function onParse() {
    if (!number.trim()) {
      setParsed(null)
      return
    }
    try {
      setParsed(await parse.mutateAsync({ number }))
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    try {
      const res = await verify.mutateAsync({ name, number, remark })
      setParsed(res.parse)
      toast.success(t(res.online ? 'plugin_realname.doneOnline' : 'plugin_realname.doneOffline'))
    } catch {
      // 失败原因已由请求层统一提示。
    }
  }

  function renderDetail(r: Verification) {
    const parts = [
      r.reason ? t(`plugin_realname.reason.${r.reason}`, { defaultValue: r.reason }) : '',
      r.providerMsg,
    ].filter(Boolean)
    if (parts.length === 0) {
      return <span className="text-muted-foreground">-</span>
    }
    const text = parts.join(' · ')
    return (
      <span className="block max-w-64 truncate" title={text}>
        {text}
      </span>
    )
  }

  const columns: Column<Verification>[] = [
    { key: 'nameMasked', title: t('plugin_realname.name'), mobile: 'title', render: (r) => r.nameMasked || '-' },
    {
      key: 'numberMasked',
      title: t('plugin_realname.number'),
      mobile: 'sub',
      render: (r) => <span className="font-mono text-xs">{r.numberMasked || '-'}</span>,
    },
    { key: 'idType', title: t('plugin_realname.typeLabel'), mobile: 'hidden', render: (r) => t(`plugin_realname.kind.${r.idType}`, { defaultValue: r.idType }) },
    { key: 'status', title: t('plugin_realname.statusLabel'), render: (r) => <StatusBadge status={r.status} /> },
    { key: 'detail', title: t('plugin_realname.detail'), mobile: 'normal', render: renderDetail },
    { key: 'remark', title: t('plugin_realname.remark'), mobile: 'hidden', render: (r) => r.remark || '-' },
    { key: 'provider', title: t('plugin_realname.provider'), mobile: 'hidden', render: (r) => r.provider || '-' },
    { key: 'operatorName', title: t('plugin_realname.operator'), mobile: 'hidden', render: (r) => r.operatorName || '-' },
    { key: 'createdAt', title: t('plugin_realname.time'), mobile: 'normal', render: (r) => formatTime(r.createdAt) },
  ]

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_realname.title')}
        description={t('plugin_realname.description')}
        actions={
          <Can perm="realname:config">
            <Button variant="outline" onClick={() => setConfigOpen(true)}>
              <Settings2 className="me-2 h-4 w-4" aria-hidden="true" />
              {t('plugin_realname.configTitle')}
            </Button>
          </Can>
        }
      />

      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="space-y-3 p-6">
          <form onSubmit={onSubmit} className="space-y-3">
            <label className="flex flex-col gap-1 text-sm">
              <span>
                <RequiredMark />
                {t('plugin_realname.name')}
              </span>
              <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={50} autoComplete="off" />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span>
                <RequiredMark />
                {t('plugin_realname.number')}
              </span>
              <Input
                value={number}
                onChange={(e) => setNumber(e.target.value)}
                onBlur={() => void onParse()}
                maxLength={24}
                placeholder="11010119900307803X"
                autoComplete="off"
                className="font-mono"
              />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span>{t('plugin_realname.remark')}</span>
              <Input value={remark} onChange={(e) => setRemark(e.target.value)} maxLength={200} />
            </label>
            <div className="flex flex-wrap items-center gap-2">
              <Can perm="realname:verify">
                <Button type="submit" disabled={verify.isPending || !name.trim() || !number.trim()}>
                  <ShieldCheck className="me-2 h-4 w-4" aria-hidden="true" />
                  {verify.isPending ? t('common.loading') : t('plugin_realname.submit')}
                </Button>
              </Can>
              <Button type="button" variant="outline" onClick={() => void onParse()} disabled={parse.isPending || !number.trim()}>
                {t('plugin_realname.parseOnly')}
              </Button>
            </div>
          </form>
          <p className="text-xs text-muted-foreground">{t('plugin_realname.ledgerHint')}</p>
        </Card>

        <Card className="space-y-3 p-6">
          <h2 className="text-sm font-medium">{t('plugin_realname.parseResult')}</h2>
          {!parsed ? (
            <p className="text-sm text-muted-foreground">{t('plugin_realname.parseIdle')}</p>
          ) : (
            <>
              <div className="flex items-center gap-2">
                <StatusBadge status={parsed.status} />
                {parsed.reason ? (
                  <span className="text-xs text-muted-foreground">
                    {t(`plugin_realname.reason.${parsed.reason}`, { defaultValue: parsed.reason })}
                  </span>
                ) : null}
              </div>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm md:grid-cols-3">
                <div className="flex flex-col">
                  <dt className="text-xs text-muted-foreground">{t('plugin_realname.number')}</dt>
                  <dd className="font-mono text-xs">{parsed.numberMasked || '-'}</dd>
                </div>
                <div className="flex flex-col">
                  <dt className="text-xs text-muted-foreground">{t('plugin_realname.typeLabel')}</dt>
                  <dd>{parsed.kind ? t(`plugin_realname.kind.${parsed.kind}`) : '-'}</dd>
                </div>
                {parsed.country ? (
                  <div className="flex flex-col">
                    <dt className="text-xs text-muted-foreground">{t('plugin_realname.country')}</dt>
                    <dd>{countryName(parsed.country)}</dd>
                  </div>
                ) : null}
                <div className="flex flex-col">
                  <dt className="text-xs text-muted-foreground">{t('plugin_realname.region')}</dt>
                  <dd>{parsed.regionNames?.length ? parsed.regionNames.join(' / ') : '-'}</dd>
                </div>
                <div className="flex flex-col">
                  <dt className="text-xs text-muted-foreground">{t('plugin_realname.birthday')}</dt>
                  <dd>{parsed.birthday || '-'}</dd>
                </div>
                <div className="flex flex-col">
                  <dt className="text-xs text-muted-foreground">{t('plugin_realname.sex')}</dt>
                  <dd>{parsed.sex ? t(`plugin_realname.sexValue.${parsed.sex}`) : '-'}</dd>
                </div>
                <div className="flex flex-col">
                  <dt className="text-xs text-muted-foreground">{t('plugin_realname.age')}</dt>
                  <dd>{parsed.status === 'valid' ? `${parsed.age ?? 0}` : '-'}</dd>
                </div>
              </dl>
              {parsed.status === 'valid' && (parsed.regionLevel ?? 3) === 0 && parsed.kind !== 'hmt_permit' ? (
                <p className="text-xs text-muted-foreground">{t('plugin_realname.regionUnknownHint')}</p>
              ) : null}
              {/* 五星卡的申领地本来就是省级码、港澳台居住证是 6 位证件码：它们停在省级
                  不是数据缺失，只有大陆居民身份证才需要提示粒度。 */}
              {parsed.status === 'valid' && parsed.kind === 'resident_id' && (parsed.regionLevel ?? 3) > 0 && (parsed.regionLevel ?? 3) < 3 ? (
                <p className="text-xs text-muted-foreground">
                  {t('plugin_realname.regionCoarseHint', {
                    level: t(`plugin_realname.regionLevel.${parsed.regionLevel}`),
                  })}
                </p>
              ) : null}
              {parsed.legacy ? (
                <p className="text-xs text-muted-foreground">{t('plugin_realname.legacyHint')}</p>
              ) : null}
              {parsed.regionHistorical ? (
                <p className="text-xs text-muted-foreground">{t('plugin_realname.historicalHint')}</p>
              ) : null}
            </>
          )}
        </Card>
      </div>

      <FilterBar
        onSearch={() => {
          setFilters({ keyword: draft.keyword, status: draft.status?.length ? draft.status.join(',') : undefined, all: draft.all, range: draft.range })
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
          placeholder={t('plugin_realname.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <OptionPicker
          title={t('plugin_realname.statusLabel')}
          value={draft.status ?? []}
          onChange={(v) => setDraft((d) => ({ ...d, status: v.length ? v : undefined }))}
          options={STATUSES.map((s) => ({ value: s, label: t(`plugin_realname.status.${s}`) }))}
          className="md:w-44"
          placeholder={t('plugin_realname.statusLabel')}
        />
        <Select
          value={draft.all ?? FOLD}
          onValueChange={(v) => setDraft((d) => ({ ...d, all: v === ALL ? '1' : undefined }))}
        >
          <SelectTrigger className="md:w-40" aria-label={t('plugin_realname.dedup')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={FOLD}>{t('plugin_realname.dedupFolded')}</SelectItem>
            <SelectItem value={ALL}>{t('plugin_realname.dedupAll')}</SelectItem>
          </SelectContent>
        </Select>
        <DateRangePicker
          value={draft.range}
          onChange={(range) => setDraft((d) => ({ ...d, range }))}
          ariaLabel={t('plugin_realname.timeRange')}
          className="md:w-60"
          placeholder={t('plugin_realname.timeRange')}
        />
      </FilterBar>

      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-realname-verifications"
        toolbar={
          <Button variant="outline" size="sm" onClick={() => void onExport()} disabled={isLoading}>
            <Download className="me-1 size-4" aria-hidden="true" />
            {t('plugin_realname.export')}
          </Button>
        }
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="realname:delete">
            <Button variant="destructive" size="sm" onClick={() => void onBatchDelete(ids)}>
              <Trash2 className="me-1 size-4" aria-hidden="true" />
              {t('plugin_realname.deleteSelected')}
            </Button>
          </Can>
        )}
        rowActions={
          has('realname:delete')
            ? (r) => (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={t('common.delete')}
                  onClick={() => void onDelete(r)}
                >
                  <Trash2 className="h-4 w-4 text-destructive" aria-hidden="true" />
                </Button>
              )
            : undefined
        }
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_realname.empty')}
      />

      <ConfigDialog open={configOpen} onOpenChange={setConfigOpen} />
    </PageContainer>
  )
}
