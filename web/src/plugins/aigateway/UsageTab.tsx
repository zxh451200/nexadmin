import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { Badge } from '@/framework/ui/badge'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { AI_KINDS, useUsage, useUsageSummary, type AiUsage, type UsageQuery } from './api'
import { formatTime } from './format'

const ALL = '__all'

export function UsageTab() {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState<UsageQuery>({})
  const [filters, setFilters] = useState<UsageQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const { data, isLoading } = useUsage({ ...filters, page, pageSize })
  const { data: summary = [] } = useUsageSummary()

  const columns: Column<AiUsage>[] = [
    {
      key: 'model',
      title: t('plugin_aigateway.fields.model'),
      mobile: 'title',
      render: (r) => (
        <div>
          <span className="font-medium text-foreground">{r.model || '-'}</span>
          <div className="text-xs text-muted-foreground">{r.provider}</div>
        </div>
      ),
    },
    { key: 'kind', title: t('plugin_aigateway.fields.kind'), mobile: 'sub', render: (r) => <Badge variant="outline">{t(`plugin_aigateway.kind.${r.kind}`)}</Badge> },
    { key: 'prompt', title: t('plugin_aigateway.fields.promptTokens'), align: 'end', render: (r) => <span className="num">{r.promptTokens}</span> },
    { key: 'completion', title: t('plugin_aigateway.fields.completionTokens'), align: 'end', render: (r) => <span className="num">{r.completionTokens}</span> },
    { key: 'total', title: t('plugin_aigateway.fields.totalTokens'), align: 'end', render: (r) => <span className="num font-medium">{r.totalTokens}</span> },
    { key: 'biz', title: t('plugin_aigateway.fields.biz'), render: (r) => (r.bizType ? `${r.bizType}/${r.bizNo ?? ''}` : '-') },
    {
      key: 'createdAt',
      title: t('plugin_aigateway.fields.createdAt'),
      render: (r) => <span className="num text-xs text-muted-foreground">{formatTime(r.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      {summary.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {summary.map((s) => (
            <Badge key={s.kind} variant="outline" className="num">
              {t(`plugin_aigateway.kind.${s.kind}`, { defaultValue: s.kind })}: {s.totalTokens}
            </Badge>
          ))}
        </div>
      )}
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
          className="md:w-48"
          value={draft.bizType ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, bizType: e.target.value }))}
          placeholder={t('plugin_aigateway.fields.biz')}
          aria-label={t('plugin_aigateway.fields.biz')}
        />
        <Select value={draft.kind ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, kind: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-44" aria-label={t('plugin_aigateway.fields.kind')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_aigateway.all')}</SelectItem>
            {AI_KINDS.map((k) => (
              <SelectItem key={k} value={k}>
                {t(`plugin_aigateway.kind.${k}`)}
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
        columnSettingsKey="plugin-aigateway-usage"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_aigateway.empty')}
      />
    </div>
  )
}
