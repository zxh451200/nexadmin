import { useState } from 'react'
import { Ban, Eye, Inbox, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import {
  useDiscardOutbox,
  useOutboxCounts,
  useOutboxEvent,
  useOutboxEvents,
  useRetryOutbox,
  type OutboxQuery,
  type PayOutboxEvent,
} from './api'
import { formatTime } from './format'
import { ListErrorBanner } from './ListErrorBanner'

const ALL = '__all'
const STATUSES = ['pending', 'sent', 'dead', 'discarded'] as const

const statusVariant = (s: string): 'success' | 'destructive' | 'warning' | 'info' | 'secondary' => {
  if (s === 'dead') return 'destructive'
  if (s === 'pending') return 'info'
  if (s === 'sent') return 'success'
  if (s === 'discarded') return 'secondary'
  return 'warning'
}

/** 死信只能重投或丢弃：其它状态的动作一律不出现（点了后端会 409）。 */
const actionable = (s: string) => s === 'dead'

function prettyPayload(payload?: string): string {
  if (!payload) return ''
  try {
    return JSON.stringify(JSON.parse(payload), null, 2)
  } catch {
    return payload
  }
}

export function OutboxTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<OutboxQuery>({})
  const [filters, setFilters] = useState<OutboxQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detail, setDetail] = useState<PayOutboxEvent | null>(null)
  const [discarding, setDiscarding] = useState<PayOutboxEvent | null>(null)

  const { data, isLoading, isError, refetch } = useOutboxEvents({ ...filters, page, pageSize })
  const { data: counts } = useOutboxCounts()
  const retry = useRetryOutbox()

  const onRetry = async (row: PayOutboxEvent) => {
    if (!(await confirm({ destructive: true, description: t('plugin_pay.outbox.retryConfirm', { id: row.eventId }) }))) return
    try {
      await retry.mutateAsync(row.eventId)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<PayOutboxEvent>[] = [
    {
      key: 'eventId',
      title: t('plugin_pay.outbox.eventId'),
      mobile: 'title',
      render: (r) => (
        <div className="min-w-0">
          <span className="block truncate font-medium text-foreground">{r.eventId}</span>
          <div className="text-xs text-muted-foreground">{r.eventName}</div>
        </div>
      ),
    },
    {
      key: 'status',
      title: t('plugin_pay.fields.status'),
      mobile: 'sub',
      render: (r) => <Badge variant={statusVariant(r.status)}>{t(`plugin_pay.outboxStatus.${r.status}`, { defaultValue: r.status })}</Badge>,
    },
    {
      key: 'attempts',
      title: t('plugin_pay.outbox.attempts'),
      align: 'end',
      render: (r) => <span className="num">{r.attempts}</span>,
    },
    {
      key: 'lastError',
      title: t('plugin_pay.outbox.lastError'),
      render: (r) =>
        r.lastError ? (
          <span className="block max-w-xs break-words text-xs text-destructive">{r.lastError}</span>
        ) : (
          <span className="text-muted-foreground">-</span>
        ),
    },
    {
      key: 'timeline',
      title: t('plugin_pay.outbox.timeline'),
      render: (r) => {
        const time = (v?: string | null) => (v ? formatTime(v, i18n.language) : '')
        const shown = r.sentAt
          ? `${t('plugin_pay.outbox.sentAt')} ${time(r.sentAt)}`
          : r.deadAt
            ? `${t('plugin_pay.outbox.deadAt')} ${time(r.deadAt)}`
            : `${t('plugin_pay.outbox.nextRetryAt')} ${time(r.nextRetryAt) || '-'}`
        return <span className="num block break-words text-xs text-muted-foreground">{shown}</span>
      },
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
      <div className="flex flex-col gap-2">
        <p className="text-xs text-muted-foreground">{t('plugin_pay.outbox.description')}</p>
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="info">{t('plugin_pay.outbox.countPending', { n: counts?.pending ?? 0 })}</Badge>
          <Badge variant="destructive">{t('plugin_pay.outbox.countDead', { n: counts?.dead ?? 0 })}</Badge>
          <Badge variant="secondary">{t('plugin_pay.outbox.countSent', { n: counts?.sent ?? 0 })}</Badge>
          <Badge variant="outline">{t('plugin_pay.outbox.countDiscarded', { n: counts?.discarded ?? 0 })}</Badge>
          <span className="text-xs text-muted-foreground">{t('plugin_pay.outbox.deadHint')}</span>
        </div>
      </div>
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
          placeholder={t('plugin_pay.outbox.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select
          value={draft.status ?? ALL}
          onValueChange={(v) => setDraft((d) => ({ ...d, status: v === ALL ? undefined : v }))}
        >
          <SelectTrigger className="md:w-44" aria-label={t('plugin_pay.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.filter.allStatuses')}</SelectItem>
            {STATUSES.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_pay.outboxStatus.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="eventId"
        loading={isLoading}
        columnSettingsKey="plugin-pay-outbox"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_pay.outbox.empty')}
        rowActions={(r) => (
          <div className="flex items-center">
            <Button variant="ghost" size="icon" aria-label={t('common.detail')} onClick={() => setDetail(r)}>
              <Eye aria-hidden="true" />
            </Button>
            {actionable(r.status) ? (
              <Can perm="pay:outbox">
                <Button variant="ghost" size="icon" aria-label={t('plugin_pay.outbox.retry')} onClick={() => void onRetry(r)}>
                  <RefreshCw aria-hidden="true" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="text-destructive"
                  aria-label={t('plugin_pay.outbox.discard')}
                  onClick={() => setDiscarding(r)}
                >
                  <Ban aria-hidden="true" />
                </Button>
              </Can>
            ) : null}
          </div>
        )}
      />
      {detail ? <OutboxDetailDialog row={detail} onClose={() => setDetail(null)} /> : null}
      {discarding ? <DiscardDialog row={discarding} onClose={() => setDiscarding(null)} /> : null}
    </div>
  )
}

function DetailField({ label, value }: { label: string; value?: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 break-words text-sm text-foreground">{value || '-'}</dd>
    </div>
  )
}

function OutboxDetailDialog({ row, onClose }: { row: PayOutboxEvent; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  // 列表不带载荷：详情按 eventId 现取一次（契约第 4 条）。
  const { data, isLoading } = useOutboxEvent(row.eventId)
  const detail = data ?? row
  const payload = prettyPayload(detail.payload)
  const time = (v?: string | null) => (v ? formatTime(v, i18n.language) : '-')

  return (
    <ResponsiveDialog
      open
      onOpenChange={(v) => !v && onClose()}
      title={`${t('plugin_pay.outbox.detail')} · ${row.eventId}`}
      size="lg"
      footer={
        <Button variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      <div className="flex min-w-0 flex-col gap-3 text-sm">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={statusVariant(detail.status)}>
            {t(`plugin_pay.outboxStatus.${detail.status}`, { defaultValue: detail.status })}
          </Badge>
          <Badge variant="outline">{detail.eventName}</Badge>
          <span className="num text-muted-foreground">
            {t('plugin_pay.outbox.attempts')}: {detail.attempts}
          </span>
        </div>
        {isLoading ? <p className="text-xs text-muted-foreground">{t('common.loading')}</p> : null}
        <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <DetailField label={t('plugin_pay.outbox.eventId')} value={detail.eventId} />
          <DetailField label={t('plugin_pay.fields.createdAt')} value={time(detail.createdAt)} />
          <DetailField label={t('plugin_pay.outbox.sentAt')} value={time(detail.sentAt)} />
          <DetailField label={t('plugin_pay.outbox.deadAt')} value={time(detail.deadAt)} />
          <DetailField label={t('plugin_pay.outbox.nextRetryAt')} value={time(detail.nextRetryAt)} />
          <DetailField label={t('plugin_pay.outbox.lastError')} value={detail.lastError} />
        </dl>
        <div className="min-w-0">
          <p className="text-xs text-muted-foreground">{t('plugin_pay.outbox.payload')}</p>
          {payload ? (
            <pre className="mt-1 max-h-80 max-w-full overflow-auto rounded-md bg-muted p-2 text-xs break-words whitespace-pre-wrap">{payload}</pre>
          ) : (
            <p className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
              <Inbox aria-hidden="true" className="h-3.5 w-3.5" />
              {t('plugin_pay.outbox.payloadEmpty')}
            </p>
          )}
        </div>
      </div>
    </ResponsiveDialog>
  )
}

function DiscardDialog({ row, onClose }: { row: PayOutboxEvent; onClose: () => void }) {
  const { t } = useTranslation()
  const discard = useDiscardOutbox()
  const [note, setNote] = useState('')

  const submit = async () => {
    if (!note.trim()) {
      toast.error(t('plugin_pay.outbox.discardNoteRequired'))
      return
    }
    try {
      await discard.mutateAsync({ eventId: row.eventId, note: note.trim() })
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
      title={`${t('plugin_pay.outbox.discard')} · ${row.eventId}`}
      size="sm"
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="destructive" onClick={() => void submit()}>
            {t('plugin_pay.outbox.discard')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-sm">
        <p className="text-xs text-muted-foreground">{t('plugin_pay.outbox.discardHint')}</p>
        <label className="flex flex-col gap-1">
          <span>
            {t('plugin_pay.outbox.discardNoteLabel')}
            <RequiredMark />
          </span>
          <Input
            value={note}
            maxLength={200}
            onChange={(e) => setNote(e.target.value)}
            placeholder={t('plugin_pay.outbox.discardNotePlaceholder')}
          />
        </label>
      </div>
    </ResponsiveDialog>
  )
}
