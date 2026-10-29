import { useState } from 'react'
import { Eye, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge, type BadgeProps } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { fmtDateTime } from '../oper-log/format'
import { DELIVERY_STATUSES, useDeliveries, useRetryDelivery, useWebhooks, type Delivery, type DeliveryStatus } from './api'

const STATUS_VARIANT: Record<DeliveryStatus, BadgeProps['variant']> = {
  success: 'success',
  failed: 'destructive',
  pending: 'warning',
}

/** 响应体预览上限（字符）；超出时提示已截断 */
const BODY_LIMIT = 2000

interface Filters {
  webhookId: string
  status: string
}

const ALL = 'all'
const emptyFilters: Filters = { webhookId: ALL, status: ALL }

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm break-all text-foreground">{children}</dd>
    </div>
  )
}

export function DeliveriesTab() {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState<Filters>(emptyFilters)
  const [applied, setApplied] = useState<Filters>(emptyFilters)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detail, setDetail] = useState<Delivery | null>(null)

  const { data: webhooks } = useWebhooks()
  // 后端投递记录不带 Webhook 名称，用已加载的 Webhook 列表按 id 补上
  const webhookNameOf = (d: Delivery) => d.webhookName ?? webhooks?.find((w) => w.id === d.webhookId)?.name ?? d.webhookId
  const { data, isLoading } = useDeliveries({
    page,
    pageSize,
    webhookId: applied.webhookId === ALL ? undefined : applied.webhookId,
    status: applied.status === ALL ? undefined : applied.status,
  })
  const retry = useRetryDelivery()

  const search = () => {
    setApplied(draft)
    setPage(1)
  }
  const reset = () => {
    setDraft(emptyFilters)
    setApplied(emptyFilters)
    setPage(1)
  }

  const onRetry = async (d: Delivery) => {
    try {
      await retry.mutateAsync(d.id)
      toast.success(t('openapi.deliveries.retryQueued'))
      setDetail(null)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const time = (iso?: string | null) => (iso ? <span className="num">{fmtDateTime(iso, i18n.language)}</span> : <span className="text-muted-foreground">-</span>)

  const columns: Column<Delivery>[] = [
    {
      key: 'webhookName',
      title: t('openapi.deliveries.fields.webhook'),
      mobile: 'title',
      render: (d) => <span className="font-medium text-foreground">{webhookNameOf(d)}</span>,
    },
    {
      key: 'event',
      title: t('openapi.deliveries.fields.event'),
      mobile: 'sub',
      render: (d) => <code className="font-mono text-xs">{d.event}</code>,
    },
    {
      key: 'status',
      title: t('openapi.deliveries.fields.status'),
      render: (d) => <Badge variant={STATUS_VARIANT[d.status]}>{t(`openapi.deliveries.status.${d.status}`)}</Badge>,
    },
    { key: 'attempts', title: t('openapi.deliveries.fields.attempts'), render: (d) => <span className="num">{d.attempts}</span> },
    {
      key: 'responseCode',
      title: t('openapi.deliveries.fields.responseCode'),
      render: (d) => (d.responseStatus ? <span className="num font-mono">{d.responseStatus}</span> : <span className="text-muted-foreground">-</span>),
    },
    { key: 'createdAt', title: t('openapi.deliveries.fields.createdAt'), render: (d) => time(d.createdAt) },
    { key: 'nextRetryAt', title: t('openapi.deliveries.fields.nextRetryAt'), render: (d) => time(d.nextAttemptAt) },
  ]

  const body = detail?.responseBody ?? ''
  const truncated = body.length > BODY_LIMIT

  return (
    <div className="flex flex-col gap-4">
      <FilterBar onSearch={search} onReset={reset}>
        <Select value={draft.webhookId} onValueChange={(v) => setDraft({ ...draft, webhookId: v })}>
          <SelectTrigger className="md:w-56" aria-label={t('openapi.deliveries.filter.webhook')}>
            <SelectValue placeholder={t('openapi.deliveries.filter.webhook')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('openapi.deliveries.filter.allWebhooks')}</SelectItem>
            {(webhooks ?? []).map((w) => (
              <SelectItem key={w.id} value={w.id}>
                {w.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={draft.status} onValueChange={(v) => setDraft({ ...draft, status: v })}>
          <SelectTrigger className="md:w-40" aria-label={t('openapi.deliveries.filter.status')}>
            <SelectValue placeholder={t('openapi.deliveries.filter.status')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('openapi.deliveries.filter.allStatus')}</SelectItem>
            {DELIVERY_STATUSES.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`openapi.deliveries.status.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FilterBar>

      <DataTable
        columns={columns}
        columnSettingsKey="openapiDeliveries"
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        emptyText={t('openapi.deliveries.empty')}
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        rowActions={(d) => (
          <>
            <Button variant="ghost" size="icon" aria-label={t('openapi.deliveries.detail')} onClick={() => setDetail(d)}>
              <Eye aria-hidden="true" />
            </Button>
            {d.status === 'failed' ? (
              <Can perm="openapi:edit">
                <Button variant="ghost" size="icon" aria-label={t('openapi.deliveries.retry')} disabled={retry.isPending} onClick={() => void onRetry(d)}>
                  <RotateCw aria-hidden="true" />
                </Button>
              </Can>
            ) : null}
          </>
        )}
      />

      <ResponsiveDialog
        open={!!detail}
        onOpenChange={(o) => !o && setDetail(null)}
        size="lg"
        title={t('openapi.deliveries.detail')}
        description={detail ? `${webhookNameOf(detail)} · ${detail.event}` : undefined}
        footer={
          <>
            {detail?.status === 'failed' ? (
              <Can perm="openapi:edit">
                <Button type="button" variant="outline" disabled={retry.isPending} onClick={() => detail && void onRetry(detail)}>
                  <RotateCw aria-hidden="true" />
                  {t('openapi.deliveries.retry')}
                </Button>
              </Can>
            ) : null}
            <Button type="button" onClick={() => setDetail(null)}>
              {t('common.close')}
            </Button>
          </>
        }
      >
        {detail ? (
          <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <DetailRow label={t('openapi.deliveries.fields.status')}>
              <Badge variant={STATUS_VARIANT[detail.status]}>{t(`openapi.deliveries.status.${detail.status}`)}</Badge>
            </DetailRow>
            <DetailRow label={t('openapi.deliveries.fields.attempts')}>
              <span className="num">{detail.attempts}</span>
            </DetailRow>
            <DetailRow label={t('openapi.deliveries.fields.responseCode')}>{detail.responseStatus ?? '-'}</DetailRow>
            <DetailRow label={t('openapi.deliveries.fields.nextRetryAt')}>{fmtDateTime(detail.nextAttemptAt, i18n.language)}</DetailRow>
            {detail.error ? (
              <div className="sm:col-span-2">
                <DetailRow label={t('openapi.deliveries.fields.error')}>
                  <span className="text-destructive">{detail.error}</span>
                </DetailRow>
              </div>
            ) : null}
            <div className="sm:col-span-2">
              <DetailRow label={t('openapi.deliveries.fields.responseBody')}>
                {body ? (
                  <pre className="max-h-72 overflow-auto rounded-md border border-border bg-muted/40 p-3 text-xs whitespace-pre-wrap">
                    {truncated ? body.slice(0, BODY_LIMIT) : body}
                  </pre>
                ) : (
                  <span className="text-muted-foreground">{t('openapi.deliveries.noBody')}</span>
                )}
                {truncated ? <p className="mt-1 text-xs text-muted-foreground">{t('openapi.deliveries.truncated', { count: BODY_LIMIT })}</p> : null}
              </DetailRow>
            </div>
          </dl>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
}
