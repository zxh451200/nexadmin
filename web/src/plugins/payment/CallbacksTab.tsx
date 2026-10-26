import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { Badge } from '@/framework/ui/badge'
import { Input } from '@/framework/ui/input'
import { useCallbacks, type CallbackQuery, type PayCallback } from './api'
import { formatTime } from './format'

export function CallbacksTab() {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState<CallbackQuery>({})
  const [filters, setFilters] = useState<CallbackQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const { data, isLoading } = useCallbacks({ ...filters, page, pageSize })

  const columns: Column<PayCallback>[] = [
    {
      key: 'paymentNo',
      title: t('plugin_payment.fields.paymentNo'),
      mobile: 'title',
      render: (r) => <span className="font-medium text-foreground">{r.paymentNo || '-'}</span>,
    },
    { key: 'channel', title: t('plugin_payment.fields.channel'), mobile: 'sub', render: (r) => <Badge variant="outline">{r.channel}</Badge> },
    { key: 'ok', title: t('plugin_payment.fields.verify'), render: (r) => (r.ok ? <Badge variant="success">{t('plugin_payment.verifyOk')}</Badge> : <Badge variant="destructive">{t('plugin_payment.verifyFail')}</Badge>) },
    { key: 'result', title: t('plugin_payment.fields.result'), render: (r) => <span className="text-xs">{r.result}</span> },
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
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-payment-callbacks"
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
      />
    </div>
  )
}
