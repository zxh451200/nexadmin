import { useState } from 'react'
import { CheckCircle2, Pencil, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Segmented } from '@/framework/components/Segmented'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import {
  useCommissionRules,
  useConfirmSettlement,
  useCreatePayout,
  useGenerateSettlements,
  useMarkPayoutPaid,
  usePayouts,
  useSaveCommissionRule,
  useSettlements,
  type PayCommissionRule,
  type PayPayoutBatch,
  type PaySettlement,
  type SettlementStatus,
} from './api'
import { formatBp, formatMoney, formatTime } from './format'
import { ListErrorBanner } from './ListErrorBanner'

const ALL = '__all'
type View = 'settlements' | 'rules' | 'payouts'

const settlementVariant = (s: SettlementStatus): 'secondary' | 'info' | 'success' =>
  s === 'paid' ? 'success' : s === 'confirmed' ? 'info' : 'secondary'

export function SettlementTab() {
  const { t } = useTranslation()
  const [view, setView] = useState<View>('settlements')
  return (
    <div className="flex flex-col gap-4">
      <Segmented
        ariaLabel={t('plugin_pay.settle.viewLabel')}
        value={view}
        onChange={(v) => setView(v as View)}
        options={[
          { value: 'settlements', label: t('plugin_pay.settle.settlements') },
          { value: 'rules', label: t('plugin_pay.settle.rules') },
          { value: 'payouts', label: t('plugin_pay.settle.payouts') },
        ]}
      />
      {view === 'settlements' ? <SettlementsPanel /> : view === 'rules' ? <RulesPanel /> : <PayoutsPanel />}
    </div>
  )
}

function RulesPanel() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<PayCommissionRule | null>(null)
  const { data = [], isLoading, isError, refetch } = useCommissionRules()

  const columns: Column<PayCommissionRule>[] = [
    { key: 'subjectId', title: t('plugin_pay.fields.subject'), mobile: 'title', render: (r) => r.subjectId || t('plugin_pay.settle.defaultSubject') },
    { key: 'bizType', title: t('plugin_pay.fields.bizType'), mobile: 'sub', render: (r) => r.bizType },
    { key: 'rateBp', title: t('plugin_pay.fields.feeRateBp'), align: 'end', render: (r) => <span className="num">{formatBp(r.rateBp)}</span> },
    {
      key: 'fixed',
      title: t('plugin_pay.settle.fixedFee'),
      align: 'end',
      render: (r) => <span className="num">{formatMoney(r.fixedMinor, r.currency)}</span>,
    },
    { key: 'status', title: t('plugin_pay.fields.status'), render: (r) => <Badge variant={r.status === 'active' ? 'success' : 'secondary'}>{t(`plugin_pay.settle.ruleStatus.${r.status}`)}</Badge> },
  ]

  return (
    <div className="flex flex-col gap-4">
      {isError ? <ListErrorBanner onRetry={() => void refetch()} /> : null}
      <DataTable
        columns={columns}
        data={data}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-pay-commission-rules"
        emptyText={t('plugin_pay.empty')}
        toolbar={
          <Can perm="pay:settle">
            <Button
              onClick={() => {
                setEditing(null)
                setOpen(true)
              }}
            >
              <Plus aria-hidden="true" />
              {t('plugin_pay.settle.ruleCreate')}
            </Button>
          </Can>
        }
        rowActions={(r) => (
          <Can perm="pay:settle">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('common.edit')}
              onClick={() => {
                setEditing(r)
                setOpen(true)
              }}
            >
              <Pencil aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {open ? <RuleDialog rule={editing} onClose={() => setOpen(false)} /> : null}
    </div>
  )
}

function RuleDialog({ rule, onClose }: { rule: PayCommissionRule | null; onClose: () => void }) {
  const { t } = useTranslation()
  const save = useSaveCommissionRule()
  const [subjectId, setSubjectId] = useState(rule?.subjectId ?? '')
  const [bizType, setBizType] = useState(rule?.bizType ?? '')
  const [rateBp, setRateBp] = useState(String(rule?.rateBp ?? 0))
  const [fixed, setFixed] = useState(((rule?.fixedMinor ?? 0) / 100).toFixed(2))
  const [currency, setCurrency] = useState(rule?.currency ?? 'CNY')
  const [status, setStatus] = useState(rule?.status ?? 'active')

  const submit = async () => {
    if (!bizType.trim()) {
      toast.error(t('plugin_pay.settle.invalidRule'))
      return
    }
    try {
      await save.mutateAsync({
        subjectId: subjectId.trim(),
        bizType: bizType.trim(),
        rateBp: Number(rateBp) || 0,
        fixedMinor: Math.round((Number(fixed) || 0) * 100),
        currency,
        status,
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
      title={rule ? t('plugin_pay.settle.ruleEdit') : t('plugin_pay.settle.ruleCreate')}
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-sm">
        <label className="flex flex-col gap-1">
          {t('plugin_pay.fields.subject')}
          <Input value={subjectId} onChange={(e) => setSubjectId(e.target.value)} placeholder={t('plugin_pay.settle.defaultSubject')} />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_pay.fields.bizType')}
            <Input value={bizType} onChange={(e) => setBizType(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_pay.fields.status')}
            <Select value={status} onValueChange={setStatus}>
              <SelectTrigger aria-label={t('plugin_pay.fields.status')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="active">{t('plugin_pay.settle.ruleStatus.active')}</SelectItem>
                <SelectItem value="disabled">{t('plugin_pay.settle.ruleStatus.disabled')}</SelectItem>
              </SelectContent>
            </Select>
          </label>
        </div>
        <div className="grid grid-cols-3 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_pay.fields.feeRateBp')}
            <Input type="number" min="0" max="9999" value={rateBp} onChange={(e) => setRateBp(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_pay.settle.fixedFee')}
            <Input type="number" min="0" step="0.01" value={fixed} onChange={(e) => setFixed(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_pay.fields.currency')}
            <Input value={currency} maxLength={8} onChange={(e) => setCurrency(e.target.value.toUpperCase())} />
          </label>
        </div>
      </div>
    </ResponsiveDialog>
  )
}

function SettlementsPanel() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [status, setStatus] = useState<string | undefined>(undefined)
  const [generateOpen, setGenerateOpen] = useState(false)
  const { data = [], isLoading, isError, refetch } = useSettlements({ status })
  const confirmSettlement = useConfirmSettlement()

  const onConfirm = async (s: PaySettlement) => {
    if (!(await confirm({ destructive: true, description: t('plugin_pay.settle.confirmHint', { period: `${s.periodStart} ~ ${s.periodEnd}` }) }))) return
    try {
      await confirmSettlement.mutateAsync(s.id)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<PaySettlement>[] = [
    {
      key: 'period',
      title: t('plugin_pay.settle.period'),
      mobile: 'title',
      render: (s) => (
        <div>
          <span className="num font-medium text-foreground">
            {s.periodStart} ~ {s.periodEnd}
          </span>
          <div className="text-xs text-muted-foreground">{s.subjectId || t('plugin_pay.settle.defaultSubject')}</div>
        </div>
      ),
    },
    { key: 'gross', title: t('plugin_pay.stats.gmv'), align: 'end', render: (s) => <span className="num">{formatMoney(s.grossMinor, s.currency)}</span> },
    { key: 'commission', title: t('plugin_pay.settle.commission'), align: 'end', render: (s) => <span className="num">{formatMoney(s.commissionMinor, s.currency)}</span> },
    { key: 'refund', title: t('plugin_pay.stats.refunded'), align: 'end', render: (s) => <span className="num">{formatMoney(s.refundMinor, s.currency)}</span> },
    { key: 'fee', title: t('plugin_pay.stats.fee'), align: 'end', render: (s) => <span className="num">{formatMoney(s.feeMinor, s.currency)}</span> },
    { key: 'net', title: t('plugin_pay.stats.net'), align: 'end', render: (s) => <span className="num font-medium">{formatMoney(s.netMinor, s.currency)}</span> },
    { key: 'orders', title: t('plugin_pay.stats.orders'), align: 'end', render: (s) => <span className="num">{s.orderCount}</span> },
    { key: 'status', title: t('plugin_pay.fields.status'), mobile: 'sub', render: (s) => <Badge variant={settlementVariant(s.status)}>{t(`plugin_pay.settlementStatus.${s.status}`)}</Badge> },
    {
      key: 'createdAt',
      title: t('plugin_pay.fields.createdAt'),
      render: (s) => <span className="num text-xs text-muted-foreground">{formatTime(s.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      {isError ? <ListErrorBanner onRetry={() => void refetch()} /> : null}
      <div className="flex flex-wrap items-center gap-2">
        <Select value={status ?? ALL} onValueChange={(v) => setStatus(v === ALL ? undefined : v)}>
          <SelectTrigger className="w-40" aria-label={t('plugin_pay.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.all')}</SelectItem>
            {(['draft', 'confirmed', 'paid'] as const).map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_pay.settlementStatus.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <DataTable
        columns={columns}
        data={data}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-pay-settlements"
        emptyText={t('plugin_pay.empty')}
        toolbar={
          <Can perm="pay:settle">
            <Button onClick={() => setGenerateOpen(true)}>
              <Plus aria-hidden="true" />
              {t('plugin_pay.settle.generate')}
            </Button>
          </Can>
        }
        rowActions={(s) =>
          s.status === 'draft' ? (
            <Can perm="pay:settle">
              <Button variant="ghost" size="icon" aria-label={t('plugin_pay.settle.confirm')} onClick={() => void onConfirm(s)}>
                <CheckCircle2 aria-hidden="true" />
              </Button>
            </Can>
          ) : null
        }
      />
      {generateOpen ? <GenerateDialog onClose={() => setGenerateOpen(false)} /> : null}
    </div>
  )
}

function GenerateDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const generate = useGenerateSettlements()
  const [subjectId, setSubjectId] = useState('')
  const [periodStart, setPeriodStart] = useState(() => ymd(new Date(new Date().getFullYear(), new Date().getMonth(), 1)))
  const [periodEnd, setPeriodEnd] = useState(() => ymd(new Date()))

  const submit = async () => {
    if (!periodStart || !periodEnd || periodEnd < periodStart) {
      toast.error(t('plugin_pay.settle.invalidPeriod'))
      return
    }
    try {
      await generate.mutateAsync({ subjectId: subjectId.trim(), periodStart, periodEnd })
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
      title={t('plugin_pay.settle.generate')}
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
        <label className="flex flex-col gap-1">
          {t('plugin_pay.fields.subject')}
          <Input value={subjectId} onChange={(e) => setSubjectId(e.target.value)} placeholder={t('plugin_pay.settle.defaultSubject')} />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_pay.settle.periodStart')}
            <Input type="date" value={periodStart} onChange={(e) => setPeriodStart(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_pay.settle.periodEnd')}
            <Input type="date" value={periodEnd} onChange={(e) => setPeriodEnd(e.target.value)} />
          </label>
        </div>
      </div>
    </ResponsiveDialog>
  )
}

function PayoutsPanel() {
  const { t, i18n } = useTranslation()
  const [status, setStatus] = useState<string | undefined>(undefined)
  const [createOpen, setCreateOpen] = useState(false)
  const [paying, setPaying] = useState<PayPayoutBatch | null>(null)
  const { data = [], isLoading, isError, refetch } = usePayouts({ status })

  const columns: Column<PayPayoutBatch>[] = [
    { key: 'batchNo', title: t('plugin_pay.fields.batchNo'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{r.batchNo}</span> },
    { key: 'status', title: t('plugin_pay.fields.status'), mobile: 'sub', render: (r) => <Badge variant={r.status === 'paid' ? 'success' : 'info'}>{t(`plugin_pay.payoutStatus.${r.status}`)}</Badge> },
    { key: 'total', title: t('plugin_pay.fields.amount'), align: 'end', render: (r) => <span className="num">{formatMoney(r.totalMinor, r.currency)}</span> },
    { key: 'count', title: t('plugin_pay.settle.settlementCount'), align: 'end', render: (r) => <span className="num">{r.settlementCount}</span> },
    { key: 'method', title: t('plugin_pay.settle.payoutMethod'), render: (r) => r.method || '-' },
    { key: 'receipt', title: t('plugin_pay.settle.receiptNo'), render: (r) => r.receiptNo || '-' },
    {
      key: 'createdAt',
      title: t('plugin_pay.fields.createdAt'),
      render: (r) => <span className="num text-xs text-muted-foreground">{formatTime(r.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      {isError ? <ListErrorBanner onRetry={() => void refetch()} /> : null}
      <div className="flex flex-wrap items-center gap-2">
        <Select value={status ?? ALL} onValueChange={(v) => setStatus(v === ALL ? undefined : v)}>
          <SelectTrigger className="w-40" aria-label={t('plugin_pay.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.all')}</SelectItem>
            {(['draft', 'paid'] as const).map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_pay.payoutStatus.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <DataTable
        columns={columns}
        data={data}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-pay-payouts"
        emptyText={t('plugin_pay.empty')}
        toolbar={
          <Can perm="pay:settle">
            <Button onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden="true" />
              {t('plugin_pay.settle.payoutCreate')}
            </Button>
          </Can>
        }
        rowActions={(r) =>
          r.status === 'draft' ? (
            <Can perm="pay:settle">
              <Button variant="ghost" size="icon" aria-label={t('plugin_pay.settle.markPaid')} onClick={() => setPaying(r)}>
                <CheckCircle2 aria-hidden="true" />
              </Button>
            </Can>
          ) : null
        }
      />
      {createOpen ? <CreatePayoutDialog onClose={() => setCreateOpen(false)} /> : null}
      {paying ? <MarkPaidDialog batch={paying} onClose={() => setPaying(null)} /> : null}
    </div>
  )
}

function CreatePayoutDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const { data = [], isLoading } = useSettlements({ status: 'confirmed' })
  const create = useCreatePayout()
  const [selected, setSelected] = useState<string[]>([])
  const [method, setMethod] = useState('')
  const [note, setNote] = useState('')

  const toggle = (id: string) =>
    setSelected((list) => (list.includes(id) ? list.filter((x) => x !== id) : [...list, id]))

  const submit = async () => {
    if (selected.length === 0) {
      toast.error(t('plugin_pay.settle.pickSettlement'))
      return
    }
    try {
      await create.mutateAsync({ settlementIds: selected, method: method.trim(), note: note.trim() })
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
      title={t('plugin_pay.settle.payoutCreate')}
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
        <p className="text-xs text-muted-foreground">{t('plugin_pay.settle.pickSettlementHint')}</p>
        {isLoading ? <p className="text-muted-foreground">{t('common.loading')}</p> : null}
        {!isLoading && data.length === 0 ? <p className="text-muted-foreground">{t('plugin_pay.settle.noConfirmed')}</p> : null}
        <div className="flex max-h-64 flex-col gap-2 overflow-y-auto">
          {data.map((s) => (
            <label key={s.id} className="flex items-center gap-3 rounded-md border border-border px-3 py-2">
              <Checkbox checked={selected.includes(s.id)} onCheckedChange={() => toggle(s.id)} aria-label={`${s.periodStart} ~ ${s.periodEnd}`} />
              <span className="num flex-1">
                {s.periodStart} ~ {s.periodEnd} · {s.subjectId || t('plugin_pay.settle.defaultSubject')}
              </span>
              <span className="num">{formatMoney(s.netMinor, s.currency)}</span>
            </label>
          ))}
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_pay.settle.payoutMethod')}
            <Input value={method} maxLength={32} onChange={(e) => setMethod(e.target.value)} placeholder="bank" />
          </label>
          <label className="flex flex-col gap-1">
            {t('common.remark')}
            <Input value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} />
          </label>
        </div>
      </div>
    </ResponsiveDialog>
  )
}

function MarkPaidDialog({ batch, onClose }: { batch: PayPayoutBatch; onClose: () => void }) {
  const { t } = useTranslation()
  const mark = useMarkPayoutPaid()
  const [receiptNo, setReceiptNo] = useState('')

  const submit = async () => {
    if (!receiptNo.trim()) {
      toast.error(t('plugin_pay.settle.receiptRequired'))
      return
    }
    try {
      await mark.mutateAsync({ id: batch.id, receiptNo: receiptNo.trim() })
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
      title={`${t('plugin_pay.settle.markPaid')} · ${batch.batchNo}`}
      size="sm"
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()}>{t('common.confirm')}</Button>
        </>
      }
    >
      <label className="flex flex-col gap-1 text-sm">
        {t('plugin_pay.settle.receiptNo')}
        <Input value={receiptNo} maxLength={64} onChange={(e) => setReceiptNo(e.target.value)} />
      </label>
    </ResponsiveDialog>
  )
}

function ymd(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
