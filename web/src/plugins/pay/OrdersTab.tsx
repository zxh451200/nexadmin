import { useState, type ReactNode } from 'react'
import { RefreshCw, Ban, Eye } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Segmented } from '@/framework/components/Segmented'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useChannels, useCloseOrder, useOrder, useOrders, useSyncOrder, type OrderQuery, type PayOrder } from './api'
import { formatMoney, formatTime, payStatusVariant } from './format'
import { ListErrorBanner } from './ListErrorBanner'
import { PayStatsPanel } from './PayStatsPanel'

const ALL = '__all'

const STATUSES = [
  'pending',
  'succeeded',
  'failed',
  'closed',
  'refunding',
  'partially_refunded',
  'refunded',
] as const

export function OrdersTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<OrderQuery>({})
  const [filters, setFilters] = useState<OrderQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  // 视图写进 URL：?view=stats 可以直接分享给同事
  const [params, setParams] = useSearchParams()
  const view = params.get('view') === 'stats' ? 'stats' : 'logs'
  const setView = (v: string) => {
    const next = new URLSearchParams(params)
    if (v === 'stats') next.set('view', v)
    else next.delete('view')
    setParams(next, { replace: true })
  }
  const [detail, setDetail] = useState<PayOrder | null>(null)

  const { data, isLoading, isError, refetch } = useOrders({ ...filters, page, pageSize })
  const { data: channels = [] } = useChannels()
  const close = useCloseOrder()
  const sync = useSyncOrder()

  const onClose = async (o: PayOrder) => {
    if (await confirm({ destructive: true, description: t('plugin_pay.closeConfirm', { no: o.paymentNo }) })) {
      try {
        await close.mutateAsync(o.paymentNo)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const onSync = async (o: PayOrder) => {
    try {
      await sync.mutateAsync(o.paymentNo)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  // 统计下钻：把同一组条件带进列表，并把视图切回日志。
  const onDrillDown = (patch: Partial<OrderQuery>) => {
    setDraft(patch)
    setFilters(patch)
    setPage(1)
    setView('logs')
  }

  const extraFilters = (['bizType', 'scene', 'failReason'] as const).filter((k) => filters[k])
  const removeExtra = (key: 'bizType' | 'scene' | 'failReason') => {
    const next = { ...filters }
    delete next[key]
    const nextDraft = { ...draft }
    delete nextDraft[key]
    setFilters(next)
    setDraft(nextDraft)
    setPage(1)
  }

  const columns: Column<PayOrder>[] = [
    {
      key: 'paymentNo',
      title: t('plugin_pay.fields.paymentNo'),
      mobile: 'title',
      render: (o) => (
        <div>
          <span className="font-medium text-foreground">{o.paymentNo}</span>
          <div className="text-xs text-muted-foreground">
            {o.bizType}/{o.bizNo}
          </div>
        </div>
      ),
    },
    { key: 'subject', title: t('plugin_pay.fields.subject'), mobile: 'sub', render: (o) => o.subject || o.subjectId || '-' },
    { key: 'amount', title: t('plugin_pay.fields.amount'), align: 'end', render: (o) => <span className="num">{formatMoney(o.amountMinor, o.currency)}</span> },
    {
      key: 'refunded',
      title: t('plugin_pay.fields.refunded'),
      align: 'end',
      render: (o) => (o.refundedMinor > 0 ? <span className="num">{formatMoney(o.refundedMinor, o.currency)}</span> : <span className="text-muted-foreground">-</span>),
    },
    {
      key: 'fee',
      title: t('plugin_pay.fields.fee'),
      align: 'end',
      render: (o) => (o.feeMinor > 0 ? <span className="num">{formatMoney(o.feeMinor, o.currency)}</span> : <span className="text-muted-foreground">-</span>),
    },
    { key: 'channel', title: t('plugin_pay.fields.channel'), render: (o) => <Badge variant="outline">{o.channelCode}</Badge> },
    { key: 'status', title: t('plugin_pay.fields.status'), mobile: 'sub', render: (o) => <Badge variant={payStatusVariant(o.status)}>{t(`plugin_pay.status.${o.status}`)}</Badge> },
    {
      key: 'createdAt',
      title: t('plugin_pay.fields.createdAt'),
      render: (o) => <span className="num text-xs text-muted-foreground">{formatTime(o.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      {/* 外层「支付中台」已经是标签页，这里用轻量分段控件，别再叠一条 tab 条 */}
      <Segmented
        ariaLabel={t('plugin_pay.view.label')}
        value={view}
        onChange={setView}
        options={[
          { value: 'logs', label: t('plugin_pay.view.logs') },
          { value: 'stats', label: t('plugin_pay.view.stats') },
        ]}
      />
      {view === 'stats' ? <PayStatsPanel onDrillDown={onDrillDown} /> : (
        <div className="flex flex-col gap-4">
      {isError ? <ListErrorBanner onRetry={() => void refetch()} /> : null}
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
          placeholder={t('plugin_pay.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select value={draft.status ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, status: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-44" aria-label={t('plugin_pay.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.all')}</SelectItem>
            {STATUSES.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_pay.status.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={draft.channelCode ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, channelCode: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-40" aria-label={t('plugin_pay.fields.channel')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.all')}</SelectItem>
            {channels.map((c) => (
              <SelectItem key={c.code} value={c.code}>
                {c.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FilterBar>
      {extraFilters.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span>{t('plugin_pay.drillApplied')}</span>
          {extraFilters.map((key) => (
            <button
              key={key}
              type="button"
              onClick={() => removeExtra(key)}
              className="flex items-center gap-1 rounded-full border border-border bg-card px-2 py-0.5 text-foreground transition-colors hover:bg-muted/50"
            >
              {t(`plugin_pay.fields.${key}`)}: {filters[key]}
              <span aria-hidden="true">×</span>
            </button>
          ))}
        </div>
      ) : null}
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-pay-orders"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_pay.empty')}
        rowActions={(o) => (
          <div className="flex items-center">
            <Button variant="ghost" size="icon" aria-label={t('common.detail')} onClick={() => setDetail(o)}>
              <Eye aria-hidden="true" />
            </Button>
            {o.status === 'pending' ? (
              <Can perm="pay:edit">
                <Button variant="ghost" size="icon" aria-label={t('plugin_pay.sync')} onClick={() => void onSync(o)}>
                  <RefreshCw aria-hidden="true" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="text-destructive"
                  aria-label={t('plugin_pay.close')}
                  onClick={() => void onClose(o)}
                >
                  <Ban aria-hidden="true" />
                </Button>
              </Can>
            ) : null}
          </div>
        )}
      />
        </div>
      )}
      <OrderDetailDialog order={detail} onClose={() => setDetail(null)} />
    </div>
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

function OrderDetailDialog({ order, onClose }: { order: PayOrder | null; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const { data, isLoading } = useOrder(order?.paymentNo ?? null)
  const o = data ?? order
  const time = (v?: string | null) => (v ? formatTime(v, i18n.language) : '-')
  return (
    <ResponsiveDialog
      open={!!order}
      onOpenChange={(v) => !v && onClose()}
      title={`${t('common.detail')} · ${order?.paymentNo ?? ''}`}
      size="lg"
      footer={
        <Button variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {!o ? null : (
        <div className="flex flex-col gap-4">
          {isLoading ? <p className="text-xs text-muted-foreground">{t('common.loading')}</p> : null}
          <div className="flex items-center gap-2">
            <Badge variant={payStatusVariant(o.status)}>{t(`plugin_pay.status.${o.status}`)}</Badge>
            <span className="num text-sm text-muted-foreground">{formatMoney(o.amountMinor, o.currency)}</span>
          </div>
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <DetailRow label={t('plugin_pay.fields.paymentNo')} value={o.paymentNo} />
            <DetailRow label={t('plugin_pay.fields.bizType')} value={`${o.bizType}/${o.bizNo}`} />
            <DetailRow label={t('plugin_pay.fields.subject')} value={o.subject || o.subjectId} />
            <DetailRow label={t('plugin_pay.fields.channelCode')} value={o.channelCode} />
            <DetailRow label={t('plugin_pay.fields.method')} value={o.method} />
            <DetailRow label={t('plugin_pay.fields.scene')} value={o.scene} />
            <DetailRow label={t('plugin_pay.fields.amount')} value={formatMoney(o.amountMinor, o.currency)} />
            <DetailRow label={t('plugin_pay.fields.refunded')} value={formatMoney(o.refundedMinor, o.currency)} />
            <DetailRow label={t('plugin_pay.fields.fee')} value={formatMoney(o.feeMinor, o.currency)} />
            <DetailRow label={t('plugin_pay.fields.channelTxnId')} value={o.channelTxnId} />
            <DetailRow label={t('plugin_pay.fields.gatewayStatus')} value={o.gatewayStatus} />
            <DetailRow label={t('plugin_pay.fields.failReason')} value={o.failReason} />
            <DetailRow label={t('plugin_pay.fields.payer')} value={o.payerId} />
            <DetailRow label={t('plugin_pay.fields.operator')} value={o.operatorId} />
            <DetailRow
              label={t('plugin_pay.fields.payUrl')}
              value={
                o.payUrl ? (
                  <a className="break-all text-primary underline" href={o.payUrl} target="_blank" rel="noreferrer">
                    {o.payUrl}
                  </a>
                ) : (
                  '-'
                )
              }
            />
            <DetailRow label={t('plugin_pay.fields.paidAt')} value={time(o.paidAt)} />
            <DetailRow label={t('plugin_pay.fields.expireAt')} value={time(o.expireAt)} />
            <DetailRow label={t('plugin_pay.fields.closedAt')} value={time(o.closedAt)} />
            <DetailRow label={t('plugin_pay.fields.createdAt')} value={time(o.createdAt)} />
          </dl>
          {o.metadata && Object.keys(o.metadata).length > 0 ? (
            <div>
              <p className="text-xs text-muted-foreground">{t('plugin_pay.fields.metadata')}</p>
              <pre className="mt-1 overflow-x-auto rounded-md bg-muted p-2 text-xs">{JSON.stringify(o.metadata, null, 2)}</pre>
            </div>
          ) : null}
        </div>
      )}
    </ResponsiveDialog>
  )
}
