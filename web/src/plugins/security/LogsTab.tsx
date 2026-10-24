import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { useLogs, type SecurityLog } from './api'
import { formatTime } from './format'

export function LogsTab() {
  const { t, i18n } = useTranslation()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const { data, isLoading } = useLogs({ page, pageSize })

  const columns: Column<SecurityLog>[] = [
    { key: 'createdAt', title: t('plugin_security.logs.time'), mobile: 'sub', render: (l) => <span className="num">{formatTime(l.createdAt, i18n.language)}</span> },
    { key: 'operatorName', title: t('plugin_security.logs.operator'), render: (l) => l.operatorName },
    { key: 'action', title: t('plugin_security.logs.action'), mobile: 'title', render: (l) => t(`plugin_security.logs.actions.${l.action}`, { defaultValue: l.action }) },
    { key: 'target', title: t('plugin_security.logs.target'), render: (l) => <span className="break-all">{l.target}</span> },
    { key: 'detail', title: t('plugin_security.logs.detail'), render: (l) => <span className="break-all text-muted-foreground">{l.detail}</span> },
    { key: 'ip', title: 'IP', render: (l) => <span className="num">{l.ip}</span> },
  ]

  return (
    <DataTable
      columns={columns}
      data={data?.list ?? []}
      rowKey="id"
      loading={isLoading}
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
