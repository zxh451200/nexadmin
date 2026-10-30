import { useState } from 'react'
import { Ban, CircleDollarSign, Plus } from 'lucide-react'
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
import {
  useAuthorizations,
  useCaptureAuth,
  useChannels,
  useCreateAuth,
  useVoidAuth,
  type AuthQuery,
  type PayAuthorization,
} from './api'
import { formatMoney, formatTime } from './format'
import { ListErrorBanner } from './ListErrorBanner'

const ALL = '__all'
const STATUSES = ['authorized', 'partial_captured', 'captured', 'voided', 'expired'] as const

const freezable = (s: string) => s === 'authorized' || s === 'partial_captured'

function statusVariant(s: string): 'success' | 'destructive' | 'info' | 'warning' | 'secondary' {
  if (s === 'captured') return 'success'
  if (s === 'authorized') return 'info'
  if (s === 'partial_captured') return 'warning'
  if (s === 'expired') return 'destructive'
  return 'secondary'
}

export function AuthorizationsTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<AuthQuery>({})
  const [filters, setFilters] = useState<AuthQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [createOpen, setCreateOpen] = useState(false)
  const [capture, setCapture] = useState<PayAuthorization | null>(null)

  const { data, isLoading, isError, refetch } = useAuthorizations({ ...filters, page, pageSize })
  const voidAuth = useVoidAuth()

  const onVoid = async (a: PayAuthorization) => {
    if (!(await confirm({ destructive: true, description: t('plugin_pay.auth.voidConfirm', { no: a.authNo }) }))) return
    try {
      await voidAuth.mutateAsync(a.authNo)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<PayAuthorization>[] = [
    {
      key: 'authNo',
      title: t('plugin_pay.fields.authNo'),
      mobile: 'title',
      render: (a) => (
        <div>
          <span className="font-medium text-foreground">{a.authNo}</span>
          <div className="text-xs text-muted-foreground">{a.bizType}/{a.bizNo}</div>
        </div>
      ),
    },
    { key: 'subjectId', title: t('plugin_pay.fields.subject'), mobile: 'sub', render: (a) => a.subjectId || '-' },
    { key: 'channelCode', title: t('plugin_pay.fields.channel'), render: (a) => <Badge variant="outline">{a.channelCode}</Badge> },
    { key: 'amount', title: t('plugin_pay.fields.amount'), align: 'end', render: (a) => <span className="num">{formatMoney(a.amountMinor, a.currency)}</span> },
    {
      key: 'captured',
      title: t('plugin_pay.fields.captured'),
      align: 'end',
      render: (a) => (a.capturedMinor > 0 ? <span className="num">{formatMoney(a.capturedMinor, a.currency)}</span> : <span className="text-muted-foreground">-</span>),
    },
    { key: 'status', title: t('plugin_pay.fields.status'), mobile: 'sub', render: (a) => <Badge variant={statusVariant(a.status)}>{t(`plugin_pay.authStatus.${a.status}`)}</Badge> },
    {
      key: 'expiresAt',
      title: t('plugin_pay.fields.expiresAt'),
      render: (a) => <span className="num text-xs text-muted-foreground">{a.expiresAt ? formatTime(a.expiresAt, i18n.language) : '-'}</span>,
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
          placeholder={t('plugin_pay.auth.searchPlaceholder')}
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
                {t(`plugin_pay.authStatus.${s}`)}
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
        columnSettingsKey="plugin-pay-authorizations"
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
          <Can perm="pay:auth">
            <Button onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden="true" />
              {t('plugin_pay.auth.create')}
            </Button>
          </Can>
        }
        rowActions={(a) =>
          freezable(a.status) ? (
            <Can perm="pay:auth">
              <div className="flex items-center">
                <Button variant="ghost" size="icon" aria-label={t('plugin_pay.auth.capture')} onClick={() => setCapture(a)}>
                  <CircleDollarSign aria-hidden="true" />
                </Button>
                <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('plugin_pay.auth.void')} onClick={() => void onVoid(a)}>
                  <Ban aria-hidden="true" />
                </Button>
              </div>
            </Can>
          ) : null
        }
      />
      {createOpen ? <CreateAuthDialog onClose={() => setCreateOpen(false)} /> : null}
      {capture ? <CaptureAuthDialog auth={capture} onClose={() => setCapture(null)} /> : null}
    </div>
  )
}

function CreateAuthDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const create = useCreateAuth()
  const { data: channels = [] } = useChannels()
  const [bizNo] = useState(() => `manual-${Date.now()}`)
  const [subjectId, setSubjectId] = useState('')
  const [channelCode, setChannelCode] = useState('')
  const [amount, setAmount] = useState('')
  const [currency, setCurrency] = useState('CNY')
  const [cardLast4, setCardLast4] = useState('')
  const [validDays, setValidDays] = useState('7')
  const [remark, setRemark] = useState('')

  const submit = async () => {
    const minor = Math.round(Number(amount) * 100)
    if (!channelCode || !minor || minor <= 0) {
      toast.error(t('plugin_pay.auth.invalid'))
      return
    }
    try {
      await create.mutateAsync({
        bizType: 'manual',
        bizNo,
        subjectId: subjectId.trim(),
        channelCode,
        amountMinor: minor,
        currency,
        cardLast4: cardLast4.trim(),
        validDays: Number(validDays) || 7,
        remark: remark.trim(),
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
      title={t('plugin_pay.auth.create')}
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
          <Input value={subjectId} onChange={(e) => setSubjectId(e.target.value)} placeholder={t('plugin_pay.auth.subjectPlaceholder')} />
        </label>
        <label className="flex flex-col gap-1">
          {t('plugin_pay.fields.channel')}
          <Select value={channelCode} onValueChange={setChannelCode}>
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
          {channels.length === 0 ? <span className="text-xs text-muted-foreground">{t('plugin_pay.auth.noChannel')}</span> : null}
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_pay.fields.amount')}
            <Input type="number" min="0" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_pay.fields.currency')}
            <Input value={currency} maxLength={8} onChange={(e) => setCurrency(e.target.value.toUpperCase())} />
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_pay.fields.cardLast4')}
            <Input value={cardLast4} maxLength={4} onChange={(e) => setCardLast4(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_pay.fields.validDays')}
            <Input type="number" min="1" max="30" value={validDays} onChange={(e) => setValidDays(e.target.value)} />
          </label>
        </div>
        <label className="flex flex-col gap-1">
          {t('plugin_pay.fields.remark')}
          <Input value={remark} maxLength={500} onChange={(e) => setRemark(e.target.value)} />
        </label>
      </div>
    </ResponsiveDialog>
  )
}

function CaptureAuthDialog({ auth, onClose }: { auth: PayAuthorization; onClose: () => void }) {
  const { t } = useTranslation()
  const capture = useCaptureAuth()
  const [bizNo] = useState(() => `${auth.authNo}-CAP-${Date.now()}`)
  const remaining = auth.amountMinor - auth.capturedMinor
  const [amount, setAmount] = useState((remaining / 100).toFixed(2))

  const submit = async () => {
    const minor = Math.round(Number(amount) * 100)
    if (!minor || minor <= 0 || minor > remaining) {
      toast.error(t('plugin_pay.auth.invalidCapture'))
      return
    }
    try {
      await capture.mutateAsync({ authNo: auth.authNo, amountMinor: minor, bizType: 'manual', bizNo })
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
      title={`${t('plugin_pay.auth.capture')} · ${auth.authNo}`}
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
      <div className="flex flex-col gap-3 text-sm">
        <p className="text-muted-foreground">
          {t('plugin_pay.auth.remaining', { amount: formatMoney(remaining, auth.currency) })}
        </p>
        <label className="flex flex-col gap-1">
          {t('plugin_pay.fields.amount')}
          <Input type="number" min="0" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} />
        </label>
      </div>
    </ResponsiveDialog>
  )
}
