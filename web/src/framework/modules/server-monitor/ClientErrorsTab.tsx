import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable } from '@/framework/components/DataTable'
import { DetailAction } from '@/framework/components/DetailAction'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import type { Column } from '@/framework/components/table/types'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { fmtDateTime } from '../oper-log/format'
import { useClientErrors, type ClientErrorRow } from './api'

/** 「前端错误」标签页：同指纹聚合后的前端错误列表，详情里看堆栈 */
export function ClientErrorsTab() {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detail, setDetail] = useState<ClientErrorRow | null>(null)

  const { data, isLoading } = useClientErrors({ page, pageSize, keyword: keyword.trim() || undefined })
  const time = (iso: string) => <span className="num">{fmtDateTime(iso, i18n.language)}</span>

  const columns: Column<ClientErrorRow>[] = [
    { key: 'lastSeenAt', title: t('serverMonitor.clientErrors.col.time'), mobile: 'sub', render: (r) => time(r.lastSeenAt) },
    {
      key: 'hitCount',
      title: t('serverMonitor.clientErrors.col.count'),
      render: (r) => <Badge variant={r.hitCount > 10 ? 'destructive' : 'secondary'}>{r.hitCount}</Badge>,
    },
    {
      key: 'message',
      title: t('serverMonitor.clientErrors.col.message'),
      mobile: 'title',
      render: (r) => (
        <span className="line-clamp-2 max-w-md break-words" title={r.message}>
          {r.message}
        </span>
      ),
    },
    { key: 'route', title: t('serverMonitor.clientErrors.col.route'), render: (r) => <span className="num">{r.route || '-'}</span> },
    {
      key: 'username',
      title: t('serverMonitor.clientErrors.col.user'),
      render: (r) => r.username || <span className="text-muted-foreground">{t('serverMonitor.clientErrors.anonymous')}</span>,
    },
  ]

  const info: [string, string][] = detail
    ? [
        [t('serverMonitor.clientErrors.col.count'), String(detail.hitCount)],
        [t('serverMonitor.clientErrors.detail.firstSeen'), fmtDateTime(detail.createdAt, i18n.language)],
        [t('serverMonitor.clientErrors.detail.lastSeen'), fmtDateTime(detail.lastSeenAt, i18n.language)],
        [t('serverMonitor.clientErrors.col.route'), detail.route || '-'],
        [t('serverMonitor.clientErrors.col.user'), detail.username || t('serverMonitor.clientErrors.anonymous')],
        [t('serverMonitor.clientErrors.detail.version'), detail.version || '-'],
        [t('serverMonitor.clientErrors.detail.ua'), detail.ua || '-'],
      ]
    : []

  return (
    <div className="space-y-3 md:space-y-4">
      <FilterBar
        onSearch={() => {
          setKeyword(draft)
          setPage(1)
        }}
        onReset={() => {
          setDraft('')
          setKeyword('')
          setPage(1)
        }}
      >
        <Input
          className="md:w-72"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t('serverMonitor.clientErrors.keywordPlaceholder')}
          aria-label={t('common.keyword')}
        />
      </FilterBar>
      <DataTable<ClientErrorRow>
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        onRowClick={setDetail}
        rowActions={(r) => <DetailAction onClick={() => setDetail(r)} />}
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
      />
      <ResponsiveDialog
        open={detail !== null}
        onOpenChange={(o) => {
          if (!o) setDetail(null)
        }}
        title={t('serverMonitor.clientErrors.detail.title')}
        description={detail?.message}
        size="xl"
        footer={
          <Button type="button" variant="outline" onClick={() => setDetail(null)}>
            {t('common.close')}
          </Button>
        }
      >
        {detail ? (
          <div className="space-y-4">
            <dl className="divide-y divide-border text-sm">
              {info.map(([k, v]) => (
                <div key={k} className="flex items-start justify-between gap-4 py-2">
                  <dt className="shrink-0 text-muted-foreground">{k}</dt>
                  <dd className="num min-w-0 break-all text-end">{v}</dd>
                </div>
              ))}
            </dl>
            <div>
              <p className="mb-1.5 text-sm font-medium">{t('serverMonitor.clientErrors.detail.stack')}</p>
              <pre className="max-h-80 overflow-auto rounded-lg bg-muted p-3 text-xs leading-relaxed whitespace-pre-wrap break-all">
                {detail.stack || t('serverMonitor.clientErrors.detail.noStack')}
              </pre>
            </div>
          </div>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
}
