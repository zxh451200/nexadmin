import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { useLogs, type SecurityLog } from './api'
import { formatTime } from './format'

export function LogsTab() {
  const { t, i18n } = useTranslation()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const { data, isLoading, isError, refetch, isFetching } = useLogs({ page, pageSize })

  const columns: Column<SecurityLog>[] = [
    { key: 'createdAt', title: t('plugin_security.logs.time'), mobile: 'sub', render: (l) => <span className="num">{formatTime(l.createdAt, i18n.language)}</span> },
    { key: 'operatorName', title: t('plugin_security.logs.operator'), render: (l) => l.operatorName },
    { key: 'action', title: t('plugin_security.logs.action'), mobile: 'title', render: (l) => t(`plugin_security.logs.actions.${l.action}`, { defaultValue: l.action }) },
    // 详情可能是长 JSON：列表先截断，点击展开全文，不依赖悬停
    { key: 'target', title: t('plugin_security.logs.target'), render: (l) => (l.target ? <span className="block max-w-[14rem] truncate" title={l.target}>{l.target}</span> : <span className="text-muted-foreground">-</span>) },
    { key: 'detail', title: t('plugin_security.logs.detail'), render: (l) => (l.detail ? (
      <details className="group max-w-72">
        <summary className="flex cursor-pointer list-none items-start gap-1 text-muted-foreground [&::-webkit-details-marker]:hidden">
          <span className="line-clamp-2 min-w-0 flex-1 group-open:line-clamp-none" title={l.detail}>{l.detail}</span>
          <ChevronDown aria-hidden="true" className="mt-0.5 size-3.5 shrink-0 transition-transform group-open:rotate-180" />
        </summary>
      </details>
    ) : <span className="text-muted-foreground">-</span>) },
    { key: 'ip', title: 'IP', render: (l) => <span className="num">{l.ip}</span> },
  ]

  return (
    <DataTable
      columns={columns}
      data={data?.list ?? []}
      rowKey="id"
      loading={isLoading}
      isError={isError}
      onRetry={() => void refetch()}
      retrying={isFetching}
      columnSettingsKey="plugin-security-logs"
      pagination={{
        page,
        pageSize,
        total: data?.total ?? 0,
        onChange: (p, s) => {
          setPage(p)
          setPageSize(s)
        },
      }}
      emptyText={t('plugin_security.logs.empty')}
    />
  )
}
