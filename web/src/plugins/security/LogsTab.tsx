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
    // break-all 在 nowrap 单元格里是死代码：对象/详情（JSON 串）改截断 + 悬停看全文
    { key: 'target', title: t('plugin_security.logs.target'), render: (l) => (l.target ? <span className="block max-w-[14rem] truncate" title={l.target}>{l.target}</span> : <span className="text-muted-foreground">-</span>) },
    { key: 'detail', title: t('plugin_security.logs.detail'), render: (l) => (l.detail ? <span className="line-clamp-2 max-w-72 text-muted-foreground" title={l.detail}>{l.detail}</span> : <span className="text-muted-foreground">-</span>) },
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
