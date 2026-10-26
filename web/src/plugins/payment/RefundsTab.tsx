import { useState } from 'react'
import { RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/framework/ui/dialog'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useCreateRefund, useRefunds, type PayRefund, type RefundQuery } from './api'
import { formatMoney, formatTime, refundStatusVariant } from './format'

const ALL = '__all'

export function RefundsTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<RefundQuery>({})
  const [filters, setFilters] = useState<RefundQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [formOpen, setFormOpen] = useState(false)

  const { data, isLoading } = useRefunds({ ...filters, page, pageSize })
  const create = useCreateRefund()

  const columns: Column<PayRefund>[] = [
    {
      key: 'refundNo',
      title: t('plugin_payment.fields.refundNo'),
      mobile: 'title',
      render: (r) => (
        <div>
          <span className="font-medium text-foreground">{r.refundNo}</span>
          <div className="text-xs text-muted-foreground">{r.paymentNo}</div>
        </div>
      ),
    },
    { key: 'amount', title: t('plugin_payment.fields.amount'), align: 'end', render: (r) => <span className="num">{formatMoney(r.amountMinor, r.currency)}</span> },
    { key: 'status', title: t('plugin_payment.fields.status'), render: (r) => <Badge variant={refundStatusVariant(r.status)}>{t(`plugin_payment.refundStatus.${r.status}`)}</Badge> },
    { key: 'reason', title: t('plugin_payment.fields.reason'), render: (r) => <span className="text-xs">{r.reason || '-'}</span> },
    { key: 'operator', title: t('plugin_payment.fields.operator'), render: (r) => r.operator || '-' },
    {
      key: 'createdAt',
      title: t('plugin_payment.fields.createdAt'),
      render: (r) => <span className="num text-xs text-muted-foreground">{formatTime(r.createdAt, i18n.language)}</span>,
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
          placeholder={t('plugin_payment.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select value={draft.status ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, status: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-44" aria-label={t('plugin_payment.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_payment.all')}</SelectItem>
            {(['pending', 'succeeded', 'failed'] as const).map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_payment.refundStatus.${s}`)}
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
        columnSettingsKey="plugin-payment-refunds"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_payment.empty')}
        toolbar={
          <Can perm="payment:refund">
            <Button onClick={() => setFormOpen(true)}>
              <RotateCcw aria-hidden="true" />
              {t('plugin_payment.refund')}
            </Button>
          </Can>
        }
      />

      <RefundDialog
        open={formOpen}
        onClose={() => setFormOpen(false)}
        onSubmit={async (paymentNo, amountMinor, reason) => {
          if (!(await confirm({ destructive: true, description: t('plugin_payment.refundConfirm', { no: paymentNo }) }))) return false
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
  onClose,
  onSubmit,
}: {
  open: boolean
  onClose: () => void
  onSubmit: (paymentNo: string, amountMinor: number, reason: string) => Promise<boolean>
}) {
  const { t } = useTranslation()
  const [paymentNo, setPaymentNo] = useState('')
  const [amount, setAmount] = useState('')
  const [reason, setReason] = useState('')
  const submit = async () => {
    const minor = Math.round(Number(amount) * 100)
    if (!paymentNo.trim() || !minor || minor <= 0) {
      toast.error(t('plugin_payment.amountInvalid'))
      return
    }
    if (await onSubmit(paymentNo.trim(), minor, reason)) onClose()
  }
  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t('plugin_payment.refund')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3 text-sm">
          <label className="flex flex-col gap-1">
            {t('plugin_payment.fields.paymentNo')}
            <Input value={paymentNo} onChange={(e) => setPaymentNo(e.target.value)} placeholder="PAY..." />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_payment.fields.amount')}
            <Input type="number" value={amount} onChange={(e) => setAmount(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_payment.fields.reason')}
            <Input value={reason} onChange={(e) => setReason(e.target.value)} />
          </label>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()}>
            <RotateCcw aria-hidden="true" />
            {t('plugin_payment.refund')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
