import { useEffect, useState } from 'react'
import { RefreshCw, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import {
  useCreateRefund,
  useOrder,
  useRefunds,
  useSyncRefund,
  type PayRefund,
  type RefundQuery,
} from './api'
import { CapabilityNote } from './CapabilityControls'
import {
  capabilitiesOfChannel,
  supportsAction,
  unsupportedAction,
  useCapabilityIndex,
  useChannelDirectory,
} from './capabilities'
import { formatMoney, formatTime, refundStatusVariant } from './format'
import { ListErrorBanner } from './ListErrorBanner'

const ALL = '__all'

/** 只有「处理中」的退款需要人工向渠道补查：终态行再查一次只会白调一次渠道。 */
const syncable = (s: string) => s === 'pending'

/**
 * 补查动作 + 补查留痕（次数/最近时间）写在一个组件里：
 * 每行都要按「该退款所属渠道是否支持查退款结果」判定可用性，Hooks 只能按行组件调用。
 */
function RefundSyncCell({ refund }: { refund: PayRefund }) {
  const { t, i18n } = useTranslation()
  const index = useCapabilityIndex()
  const { providerOf } = useChannelDirectory()
  const sync = useSyncRefund()
  // 退款单视图暂未带 channelCode（契约缺口）：缺省时回查一次支付单拿渠道。
  const { data: order } = useOrder(refund.channelCode ? null : refund.paymentNo)
  const channelCode = refund.channelCode || order?.channelCode || ''
  const provider = providerOf(channelCode)
  const caps = capabilitiesOfChannel(index, providerOf, channelCode)
  const supported = supportsAction(caps, 'refundQuery')

  const onSync = async () => {
    try {
      const next = await sync.mutateAsync(refund.refundNo)
      toast.success(t('plugin_pay.refundSync.done', { status: t(`plugin_pay.refundStatus.${next.status}`) }))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const attempts = refund.syncAttempts ?? 0
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="num text-xs text-muted-foreground">
        {attempts > 0
          ? t('plugin_pay.refundSync.attempts', { n: attempts })
          : t('plugin_pay.refundSync.never')}
        {refund.lastSyncAt ? ` · ${t('plugin_pay.refundSync.lastSyncAt', { time: formatTime(refund.lastSyncAt, i18n.language) })}` : ''}
      </div>
      {syncable(refund.status) ? (
        supported ? (
          <Can perm="pay:edit" fallback={<span className="text-xs text-muted-foreground">{t('plugin_pay.refundSync.noPerm')}</span>}>
            <Button variant="outline" size="sm" disabled={sync.isPending} onClick={() => void onSync()}>
              <RefreshCw aria-hidden="true" />
              {t('plugin_pay.refundSync.action')}
            </Button>
          </Can>
        ) : (
          <div className="flex flex-wrap items-center gap-1.5">
            <Button variant="outline" size="sm" disabled aria-disabled="true">
              <RefreshCw aria-hidden="true" />
              {t('plugin_pay.refundSync.action')}
            </Button>
            <CapabilityNote>{unsupportedAction(t, provider, 'refundQuery')}</CapabilityNote>
          </div>
        )
      ) : null}
    </div>
  )
}

export function RefundsTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [params, setParams] = useSearchParams()
  // 订单页「去退款」带着单号过来（?refundNo=PAY…）：自动打开弹窗并预填；
  // 用完即清 URL 参数，下次手动打开不残留。
  const linkedNo = params.get('refundNo') ?? ''
  const [initialNo, setInitialNo] = useState('')
  const [draft, setDraft] = useState<RefundQuery>({})
  const [filters, setFilters] = useState<RefundQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [formOpen, setFormOpen] = useState(false)

  useEffect(() => {
    if (!linkedNo) return
    setInitialNo(linkedNo)
    setFormOpen(true)
    const next = new URLSearchParams(params)
    next.delete('refundNo')
    setParams(next, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [linkedNo])

  const { data, isLoading, isError, refetch } = useRefunds({ ...filters, page, pageSize })
  const create = useCreateRefund()

  const columns: Column<PayRefund>[] = [
    {
      key: 'refundNo',
      title: t('plugin_pay.fields.refundNo'),
      mobile: 'title',
      render: (r) => (
        <div className="min-w-0">
          <span className="block truncate font-medium text-foreground">{r.refundNo}</span>
          <div className="text-xs text-muted-foreground">{r.paymentNo}</div>
        </div>
      ),
    },
    { key: 'amount', title: t('plugin_pay.fields.amount'), align: 'end', render: (r) => <span className="num">{formatMoney(r.amountMinor, r.currency)}</span> },
    { key: 'status', title: t('plugin_pay.fields.status'), render: (r) => <Badge variant={refundStatusVariant(r.status)}>{t(`plugin_pay.refundStatus.${r.status}`)}</Badge> },
    { key: 'reason', title: t('plugin_pay.fields.reason'), render: (r) => <span className="text-xs wrap-break-word">{r.reason || r.failReason || '-'}</span> },
    { key: 'operator', title: t('plugin_pay.fields.operator'), mobile: 'hidden', render: (r) => r.operatorId || '-' },
    {
      key: 'sync',
      title: t('plugin_pay.refundSync.column'),
      mobile: 'normal',
      render: (r) => <RefundSyncCell refund={r} />,
    },
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
            <SelectItem value={ALL}>{t('plugin_pay.filter.allStatuses')}</SelectItem>
            {(['pending', 'succeeded', 'failed'] as const).map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_pay.refundStatus.${s}`)}
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
        columnSettingsKey="plugin-pay-refunds"
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
        toolbar={
          <Can perm="pay:refund">
            <Button onClick={() => setFormOpen(true)}>
              <RotateCcw aria-hidden="true" />
              {t('plugin_pay.refund')}
            </Button>
          </Can>
        }
      />

      <RefundDialog
        open={formOpen}
        initialPaymentNo={initialNo}
        onClose={() => setFormOpen(false)}
        onSubmit={async (paymentNo, amountMinor, reason) => {
          if (!(await confirm({ destructive: true, description: t('plugin_pay.refundConfirm', { no: paymentNo }) }))) return false
          try {
            await create.mutateAsync({ paymentNo, amountMinor, reason })
            toast.success(t('common.operateSuccess'))
            return true
          } catch {
            toast.error(t('common.operateFailed'))
            return false
          }
        }}
      />
    </div>
  )
}

function RefundDialog({
  open,
  initialPaymentNo,
  onClose,
  onSubmit,
}: {
  open: boolean
  /** 从订单页带过来的预填单号；每次打开时重置为它 */
  initialPaymentNo?: string
  onClose: () => void
  onSubmit: (paymentNo: string, amountMinor: number, reason: string) => Promise<boolean>
}) {
  const { t } = useTranslation()
  const [paymentNo, setPaymentNo] = useState('')
  const [amount, setAmount] = useState('')
  const [reason, setReason] = useState('')
  // 每次打开都按预填单号重置：弹窗常挂载，上一次的输入不能漏进这一次
  useEffect(() => {
    if (!open) return
    setPaymentNo(initialPaymentNo ?? '')
    setAmount('')
    setReason('')
  }, [open, initialPaymentNo])
  const submit = async () => {
    const minor = Math.round(Number(amount) * 100)
    if (!paymentNo.trim() || !minor || minor <= 0) {
      toast.error(t('plugin_pay.amountInvalid'))
      return
    }
    if (await onSubmit(paymentNo.trim(), minor, reason)) onClose()
  }
  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(v) => !v && onClose()}
      title={t('plugin_pay.refund')}
      size="sm"
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()}>
            <RotateCcw aria-hidden="true" />
            {t('plugin_pay.refund')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-sm">
        <label className="flex flex-col gap-1">
          <span>
            {t('plugin_pay.fields.paymentNo')}
            <RequiredMark />
          </span>
          <Input value={paymentNo} onChange={(e) => setPaymentNo(e.target.value)} placeholder="PAY..." />
        </label>
        <label className="flex flex-col gap-1">
          <span>
            {t('plugin_pay.fields.amount')}
            <RequiredMark />
          </span>
          <Input type="number" value={amount} onChange={(e) => setAmount(e.target.value)} />
        </label>
        <label className="flex flex-col gap-1">
          {t('plugin_pay.fields.reason')}
          <Input value={reason} onChange={(e) => setReason(e.target.value)} />
        </label>
      </div>
    </ResponsiveDialog>
  )
}
