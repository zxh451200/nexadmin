import { useState } from 'react'
import { RefreshCw, Ban } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useChannels, useCloseOrder, useOrders, useSyncOrder, type OrderQuery, type PayOrder } from './api'
import { formatMoney, formatTime, payStatusVariant } from './format'

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

  const { data, isLoading } = useOrders({ ...filters, page, pageSize })
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
    { key: 'subject', title: t('plugin_pay.fields.subject'), mobile: 'sub', render: (o) => o.subjectId || '-' },
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
    { key: 'status', title: t('plugin_pay.fields.status'), render: (o) => <Badge variant={payStatusVariant(o.status)}>{t(`plugin_pay.status.${o.status}`)}</Badge> },
    {
      key: 'createdAt',
      title: t('plugin_pay.fields.createdAt'),
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
          placeholder={t('plugin_pay.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select value={draft.status ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, status: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-44" aria-label={t('plugin_pay.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.fields.status')}: {t('plugin_pay.all')}</SelectItem>
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
            <SelectItem value={ALL}>{t('plugin_pay.fields.channel')}: {t('plugin_pay.all')}</SelectItem>
            {channels.map((c) => (
              <SelectItem key={c.code} value={c.code}>
                {c.name}
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
        rowActions={(o) =>
          o.status === 'pending' ? (
            <Can perm="pay:edit">
              <div className="flex items-center">
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
              </div>
            </Can>
          ) : null
        }
      />
    </div>
  )
}
