import { useState } from 'react'
import { Eye } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useCallbacks, useChannels, type CallbackQuery, type PayCallback } from './api'
import { formatTime } from './format'
import { ListErrorBanner } from './ListErrorBanner'

const ALL = '__all'

export function CallbacksTab() {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState<CallbackQuery>({})
  const [filters, setFilters] = useState<CallbackQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detail, setDetail] = useState<PayCallback | null>(null)

  const { data, isLoading, isError, refetch } = useCallbacks({ ...filters, page, pageSize })
  const { data: channels = [] } = useChannels()

  const columns: Column<PayCallback>[] = [
    {
      key: 'paymentNo',
      title: t('plugin_pay.fields.paymentNo'),
      mobile: 'title',
      render: (r) => <span className="font-medium text-foreground">{r.paymentNo || '-'}</span>,
    },
    { key: 'channel', title: t('plugin_pay.fields.channel'), mobile: 'sub', render: (r) => <Badge variant="outline">{r.channelCode}</Badge> },
    { key: 'ok', title: t('plugin_pay.fields.verify'), render: (r) => (r.ok ? <Badge variant="success">{t('plugin_pay.verifyOk')}</Badge> : <Badge variant="destructive">{t('plugin_pay.verifyFail')}</Badge>) },
    { key: 'result', title: t('plugin_pay.fields.result'), render: (r) => <span className="text-xs">{r.result}</span> },
    {
      key: 'createdAt',
      title: t('plugin_pay.fields.createdAt'),
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
        <Select
          value={draft.channelCode ?? ALL}
          onValueChange={(v) => setDraft((d) => ({ ...d, channelCode: v === ALL ? undefined : v }))}
        >
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
        <Select
          value={draft.ok === true ? 'ok' : draft.ok === false ? 'fail' : ALL}
          onValueChange={(v) => setDraft((d) => ({ ...d, ok: v === 'ok' ? true : v === 'fail' ? false : undefined }))}
        >
          <SelectTrigger className="md:w-36" aria-label={t('plugin_pay.fields.verify')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.all')}</SelectItem>
            <SelectItem value="ok">{t('plugin_pay.verifyOk')}</SelectItem>
            <SelectItem value="fail">{t('plugin_pay.verifyFail')}</SelectItem>
          </SelectContent>
        </Select>
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-pay-callbacks"
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
        rowActions={(r) => (
          <Button variant="ghost" size="icon" aria-label={t('common.detail')} onClick={() => setDetail(r)}>
            <Eye aria-hidden="true" />
          </Button>
        )}
      />
      <ResponsiveDialog
        open={!!detail}
        onOpenChange={(v) => !v && setDetail(null)}
        title={`${t('common.detail')} · ${detail?.paymentNo || detail?.channelCode || ''}`}
        size="lg"
        footer={
          <Button variant="outline" onClick={() => setDetail(null)}>
            {t('common.close')}
          </Button>
        }
      >
        {detail ? (
          <div className="flex flex-col gap-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="outline">{detail.channelCode}</Badge>
              {detail.ok ? (
                <Badge variant="success">{t('plugin_pay.verifyOk')}</Badge>
              ) : (
                <Badge variant="destructive">{t('plugin_pay.verifyFail')}</Badge>
              )}
              <span className="text-muted-foreground">{detail.result}</span>
            </div>
            <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <dt className="text-xs text-muted-foreground">{t('plugin_pay.fields.paymentNo')}</dt>
                <dd className="mt-0.5 break-all">{detail.paymentNo || '-'}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">{t('plugin_pay.fields.createdAt')}</dt>
                <dd className="num mt-0.5">{formatTime(detail.createdAt, i18n.language)}</dd>
              </div>
            </dl>
            <div>
              <p className="text-xs text-muted-foreground">{t('plugin_pay.fields.payload')}</p>
              <pre className="mt-1 max-h-80 overflow-auto rounded-md bg-muted p-2 text-xs">{prettyPayload(detail.payload)}</pre>
            </div>
          </div>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
}

function prettyPayload(payload?: string): string {
  if (!payload) return '-'
  try {
    return JSON.stringify(JSON.parse(payload), null, 2)
  } catch {
    return payload
  }
}
