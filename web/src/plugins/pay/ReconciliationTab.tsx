import { useState } from 'react'
import { ListChecks, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DatePicker } from '@/framework/components/DatePicker'
import { FilterBar } from '@/framework/components/FilterBar'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { RadioGroup, RadioGroupItem } from '@/framework/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import {
  useReconcileDiffs,
  useReconcileTasks,
  useResolveDiff,
  useRunReconcileTask,
  type DiffStatus,
  type PayReconcileDiff,
  type PayReconcileTask,
  type ReconcileAmount,
  type ReconcileSource,
} from './api'
import { CapabilityNote } from './CapabilityControls'
import {
  capabilitiesOfChannel,
  supportsAction,
  unsupportedAction,
  useCapabilityIndex,
  useChannelDirectory,
} from './capabilities'
import { formatAmount, formatTime } from './format'
import { ListErrorBanner } from './ListErrorBanner'

const ALL = '__all'
const STATUSES = ['open', 'closed'] as const

const kindVariant = (kind: string): 'destructive' | 'warning' | 'info' =>
  kind === 'long' ? 'warning' : kind === 'short' ? 'destructive' : 'info'

const diffStatusVariant = (s: DiffStatus): 'success' | 'info' | 'secondary' =>
  s === 'resolved' ? 'success' : s === 'ignored' ? 'secondary' : 'info'

export function ReconciliationTab() {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState<{ billDate?: string; channelCode?: string; status?: string }>({})
  const [filters, setFilters] = useState<{ billDate?: string; channelCode?: string; status?: string }>({})
  const [runOpen, setRunOpen] = useState(false)
  const [detail, setDetail] = useState<PayReconcileTask | null>(null)

  const { data = [], isLoading, isError, refetch } = useReconcileTasks(filters)
  const { channels } = useChannelDirectory()

  const columns: Column<PayReconcileTask>[] = [
    {
      key: 'billDate',
      title: t('plugin_pay.fields.billDate'),
      mobile: 'title',
      render: (r) => (
        <div className="min-w-0">
          <span className="num block truncate font-medium text-foreground">{r.billDate}</span>
          <div className="text-xs text-muted-foreground">
            {r.channelCode}/{r.subjectId || t('plugin_pay.reconcile.allSubjects')}
          </div>
        </div>
      ),
    },
    { key: 'status', title: t('plugin_pay.fields.status'), mobile: 'sub', render: (r) => <Badge variant={r.status === 'closed' ? 'success' : 'info'}>{t(`plugin_pay.reconcileStatus.${r.status}`)}</Badge> },
    {
      key: 'source',
      title: t('plugin_pay.reconcile.source'),
      render: (r) => <Badge variant="outline">{t(`plugin_pay.reconcile.sourceValue.${r.source}`, { defaultValue: r.source })}</Badge>,
    },
    {
      // 取数留痕：账单行数与摘要能区分「账单本来为空」与「取数/解析失败」
      key: 'bill',
      title: t('plugin_pay.reconcile.bill'),
      render: (r) => (
        <div className="min-w-0 text-xs">
          <span className="num text-muted-foreground">{t('plugin_pay.reconcile.billRows')}: {r.billRows ?? '-'}</span>
          {r.billDigest ? <div className="num break-all text-muted-foreground">{r.billDigest.slice(0, 12)}…</div> : null}
          {r.failReason ? <div className="break-words text-destructive">{r.failReason}</div> : null}
        </div>
      ),
    },
    { key: 'local', title: t('plugin_pay.reconcile.localTotal'), align: 'end', render: (r) => <span className="num">{formatAmount(r.totalLocalMinor)}</span> },
    { key: 'channel', title: t('plugin_pay.reconcile.channelTotal'), align: 'end', render: (r) => <span className="num">{formatAmount(r.totalChannelMinor)}</span> },
    { key: 'diff', title: t('plugin_pay.reconcile.diffCount'), align: 'end', render: (r) => (r.diffCount > 0 ? <Badge variant="warning">{r.diffCount}</Badge> : <span className="text-muted-foreground">0</span>) },
    {
      key: 'createdAt',
      title: t('plugin_pay.fields.createdAt'),
      mobile: 'hidden',
      render: (r) => <span className="num text-xs text-muted-foreground">{formatTime(r.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      {isError ? <ListErrorBanner onRetry={() => void refetch()} /> : null}
      <FilterBar
        onSearch={() => setFilters(draft)}
        onReset={() => {
          setDraft({})
          setFilters({})
        }}
      >
        <DatePicker
          className="md:w-44"
          value={draft.billDate ?? ''}
          onChange={(v) => setDraft((d) => ({ ...d, billDate: v || undefined }))}
          placeholder={t('plugin_pay.fields.billDate')}
          ariaLabel={t('plugin_pay.fields.billDate')}
        />
        <Select value={draft.channelCode ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, channelCode: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-40" aria-label={t('plugin_pay.fields.channel')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.filter.allChannels')}</SelectItem>
            {channels.map((c) => (
              <SelectItem key={c.code} value={c.code}>
                {c.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={draft.status ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, status: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-36" aria-label={t('plugin_pay.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.filter.allStatuses')}</SelectItem>
            {STATUSES.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_pay.reconcileStatus.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FilterBar>
      <DataTable
        columns={columns}
        data={data}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-pay-reconcile"
        emptyText={t('plugin_pay.empty')}
        toolbar={
          <Can perm="pay:settle">
            <Button onClick={() => setRunOpen(true)}>
              <Plus aria-hidden="true" />
              {t('plugin_pay.reconcile.run')}
            </Button>
          </Can>
        }
        rowActions={(r) => (
          <Button variant="ghost" size="icon" aria-label={t('plugin_pay.reconcile.diffs')} onClick={() => setDetail(r)}>
            <ListChecks aria-hidden="true" />
          </Button>
        )}
      />
      {runOpen ? <RunReconcileDialog onClose={() => setRunOpen(false)} /> : null}
      {detail ? <DiffsDialog task={detail} onClose={() => setDetail(null)} /> : null}
    </div>
  )
}

function RunReconcileDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const run = useRunReconcileTask()
  const index = useCapabilityIndex()
  const { channels, providerOf } = useChannelDirectory()
  const [channelCode, setChannelCode] = useState('')
  const [source, setSource] = useState<ReconcileSource>('manual')
  const [billDate, setBillDate] = useState(() => ymd(new Date()))
  const [subjectId, setSubjectId] = useState('')
  const [rows, setRows] = useState<{ paymentNo: string; amount: string }[]>([{ paymentNo: '', amount: '' }])

  const provider = providerOf(channelCode)
  const caps = capabilitiesOfChannel(index, providerOf, channelCode)
  // 能力未知时不拦「渠道账单」（后端仍会判一次），明确不支持时才置灰
  const billSupported = supportsAction(caps, 'bill')

  const setRow = (i: number, patch: Partial<{ paymentNo: string; amount: string }>) =>
    setRows((list) => list.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))

  const pickChannel = (code: string) => {
    setChannelCode(code)
    // 换到不支持账单的渠道时自动退回本地录入，避免留下一个必然失败的来源
    if (!supportsAction(capabilitiesOfChannel(index, providerOf, code), 'bill') && source === 'channel') setSource('manual')
  }

  const submit = async () => {
    if (!channelCode || !billDate) {
      toast.error(t('plugin_pay.reconcile.invalid'))
      return
    }
    if (source === 'channel' && !billSupported) {
      toast.error(unsupportedAction(t, provider, 'bill'))
      return
    }
    const channelAmounts: ReconcileAmount[] = rows
      .map((r) => ({ paymentNo: r.paymentNo.trim(), amountMinor: Math.round(Number(r.amount) * 100) }))
      .filter((r) => r.paymentNo && r.amountMinor > 0)
    try {
      await run.mutateAsync({
        channelCode,
        billDate,
        subjectId: subjectId.trim(),
        source,
        // 渠道账单由后端取数，不需要手工金额；本地录入保持原有 payload（向后兼容）
        ...(source === 'manual' ? { channelAmounts } : {}),
      })
      toast.success(t('common.operateSuccess'))
      onClose()
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={(v) => !v && onClose()}
      title={t('plugin_pay.reconcile.run')}
      size="lg"
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()}>{t('common.confirm')}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-sm">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <label className="flex flex-col gap-1">
            <span>
              {t('plugin_pay.fields.channel')}
              <RequiredMark />
            </span>
            <Select value={channelCode} onValueChange={pickChannel}>
              <SelectTrigger aria-label={t('plugin_pay.fields.channel')}>
                <SelectValue placeholder={t('plugin_pay.auth.pickChannel')} />
              </SelectTrigger>
              <SelectContent>
                {channels.map((c) => (
                  <SelectItem key={c.code} value={c.code}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          <label className="flex flex-col gap-1">
            <span>
              {t('plugin_pay.fields.billDate')}
              <RequiredMark />
            </span>
            <DatePicker value={billDate} onChange={setBillDate} allowClear={false} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_pay.fields.subject')}
            <Input value={subjectId} onChange={(e) => setSubjectId(e.target.value)} placeholder={t('plugin_pay.reconcile.subjectOptional')} />
          </label>
        </div>
        <div className="flex flex-col gap-2">
          <span className="text-xs text-muted-foreground">{t('plugin_pay.reconcile.source')}</span>
          {/* 用单选而不是分段控件：不支持的来源要能置灰并就地说明原因 */}
          <RadioGroup value={source} onValueChange={(v) => setSource(v as ReconcileSource)} className="flex flex-wrap gap-4">
            <label className="flex items-center gap-2 text-sm">
              <RadioGroupItem id="pay-reconcile-source-manual" value="manual" />
              {t('plugin_pay.reconcile.sourceValue.manual')}
            </label>
            <label className="flex items-center gap-2 text-sm">
              <RadioGroupItem id="pay-reconcile-source-channel" value="channel" disabled={!billSupported} />
              {t('plugin_pay.reconcile.sourceValue.channel')}
              {!billSupported ? (
                <span className="text-xs text-muted-foreground">{t('plugin_pay.capabilities.unsupportedSuffix')}</span>
              ) : null}
            </label>
          </RadioGroup>
          <p className="text-xs text-muted-foreground">
            {source === 'channel' ? t('plugin_pay.reconcile.sourceChannelHint') : t('plugin_pay.reconcile.sourceManualHint')}
          </p>
          {source === 'channel' && !billSupported ? (
            <CapabilityNote tone="warn">{unsupportedAction(t, provider, 'bill')}</CapabilityNote>
          ) : null}
          {!channelCode ? <p className="text-xs text-muted-foreground">{t('plugin_pay.reconcile.pickChannelFirst')}</p> : null}
        </div>
        {source === 'manual' ? (
          <div>
            <div className="mb-1 flex items-center justify-between">
              <span className="text-xs text-muted-foreground">{t('plugin_pay.reconcile.channelAmounts')}</span>
              <Button variant="outline" size="sm" onClick={() => setRows((r) => [...r, { paymentNo: '', amount: '' }])}>
                <Plus aria-hidden="true" />
                {t('plugin_pay.reconcile.addRow')}
              </Button>
            </div>
            <p className="mb-2 text-xs text-muted-foreground">{t('plugin_pay.reconcile.emptyHint')}</p>
            <div className="flex flex-col gap-2">
              {rows.map((r, i) => (
                <div key={i} className="flex items-center gap-2">
                  <Input
                    className="flex-1"
                    value={r.paymentNo}
                    onChange={(e) => setRow(i, { paymentNo: e.target.value })}
                    placeholder={t('plugin_pay.fields.paymentNo')}
                    aria-label={t('plugin_pay.fields.paymentNo')}
                  />
                  <Input
                    className="w-32"
                    type="number"
                    min="0"
                    step="0.01"
                    value={r.amount}
                    onChange={(e) => setRow(i, { amount: e.target.value })}
                    placeholder={t('plugin_pay.fields.amount')}
                    aria-label={t('plugin_pay.fields.amount')}
                  />
                  <Button
                    variant="ghost"
                    size="icon"
                    className="text-destructive"
                    aria-label={t('common.delete')}
                    disabled={rows.length === 1}
                    onClick={() => setRows((list) => list.filter((_, idx) => idx !== i))}
                  >
                    <Trash2 aria-hidden="true" />
                  </Button>
                </div>
              ))}
            </div>
          </div>
        ) : null}
      </div>
    </ResponsiveDialog>
  )
}

function DiffsDialog({ task, onClose }: { task: PayReconcileTask; onClose: () => void }) {
  const { t } = useTranslation()
  const { data = [], isLoading } = useReconcileDiffs(task.id)
  const resolve = useResolveDiff()
  const [notes, setNotes] = useState<Record<string, string>>({})

  const act = async (diff: PayReconcileDiff, action: 'resolved' | 'ignored') => {
    try {
      await resolve.mutateAsync({ id: diff.id, action, note: notes[diff.id] ?? '' })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={(v) => !v && onClose()}
      title={`${t('plugin_pay.reconcile.diffs')} · ${task.billDate} · ${task.channelCode}`}
      size="lg"
      footer={
        <Button variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      <div className="flex flex-col gap-2">
        {/* 取数留痕：先说清这次是本地录入还是渠道账单，再看差异 */}
        <p className="text-xs text-muted-foreground">
          {t('plugin_pay.reconcile.source')}: {t(`plugin_pay.reconcile.sourceValue.${task.source}`, { defaultValue: task.source })}
          {task.source === 'channel' ? ` · ${t('plugin_pay.reconcile.billRows')}: ${task.billRows ?? '-'}` : ''}
          {task.billDigest ? ` · ${t('plugin_pay.reconcile.billDigest')}: ${task.billDigest.slice(0, 12)}…` : ''}
        </p>
        {task.failReason ? <CapabilityNote tone="warn">{t('plugin_pay.reconcile.failedWithReason', { reason: task.failReason })}</CapabilityNote> : null}
      </div>
      {isLoading ? <p className="text-sm text-muted-foreground">{t('common.loading')}</p> : null}
      {!isLoading && data.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {task.failReason
            ? t('plugin_pay.reconcile.emptyBecauseFailed')
            : task.source === 'channel' && (task.billRows ?? 0) === 0
              ? t('plugin_pay.reconcile.channelBillEmpty')
              : t('plugin_pay.reconcile.noDiff')}
        </p>
      ) : null}
      <div className="flex flex-col gap-2">
        {data.map((d) => (
          <div key={d.id} className="rounded-lg border border-border p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={kindVariant(d.kind)}>{t(`plugin_pay.diffKind.${d.kind}`)}</Badge>
              <Badge variant={diffStatusVariant(d.status)}>{t(`plugin_pay.diffStatus.${d.status}`)}</Badge>
              <span className="num text-muted-foreground">{d.paymentNo || d.refundNo || '-'}</span>
              <span className="num ms-auto">
                {t('plugin_pay.reconcile.localShort')} {formatAmount(d.localAmountMinor)} / {t('plugin_pay.reconcile.channelShort')}{' '}
                {formatAmount(d.channelAmountMinor)}
              </span>
              <span className="num font-medium">
                {t('plugin_pay.reconcile.diffShort')} {formatAmount(d.diffAmountMinor)}
              </span>
            </div>
            {d.status === 'open' ? (
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <Input
                  className="min-w-40 flex-1"
                  value={notes[d.id] ?? ''}
                  onChange={(e) => setNotes((n) => ({ ...n, [d.id]: e.target.value }))}
                  placeholder={t('plugin_pay.reconcile.notePlaceholder')}
                  aria-label={t('plugin_pay.reconcile.note')}
                />
                <Can perm="pay:settle">
                  <Button size="sm" variant="outline" onClick={() => void act(d, 'resolved')}>
                    {t('plugin_pay.reconcile.resolve')}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => void act(d, 'ignored')}>
                    {t('plugin_pay.reconcile.ignore')}
                  </Button>
                </Can>
              </div>
            ) : (
              <p className="mt-2 text-xs text-muted-foreground">
                {d.resolvedBy || '-'} · {d.resolveNote || '-'}
              </p>
            )}
          </div>
        ))}
      </div>
    </ResponsiveDialog>
  )
}

function ymd(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
