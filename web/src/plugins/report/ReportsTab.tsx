import { useEffect, useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { Button } from '@/framework/ui/button'
import { api } from '@/framework/request/http'
import { useDeleteReport, useReportList, type Report } from './api'
import { ReportDialog } from './ReportDialog'
import { ReportViewer } from './ReportViewer'
import { builtinText } from './builtinText'

export function ReportsTab() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const { data, isLoading } = useReportList({ page, pageSize })
  const reports = data?.list ?? []
  const remove = useDeleteReport()
  const [selected, setSelected] = useState<string>()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Report | null>(null)

  useEffect(() => {
    if (!selected && data && data.list.length > 0) setSelected(data.list[0].id)
  }, [data, selected])

  const onEdit = async (r: Report) => {
    try {
      // 管理端读取完整配置（含 report:report 权限校验）
      setEditing(await api.get<Report>(`/plugins/report/reports/${r.id}`))
      setOpen(true)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  const onDelete = async (r: Report) => {
    if (!(await confirm({ destructive: true, description: t('plugin_report.report.deleteConfirm', { name: builtinText(t, r.name) }) }))) return
    try {
      await remove.mutateAsync(r.id)
      if (r.id === selected) setSelected(undefined)
      // 本页被删空：回上一页，避免停在一个空页
      if (page > 1 && reports.length === 1) setPage(page - 1)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<Report>[] = [
    { key: 'name', title: t('common.name'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{builtinText(t, r.name)}</span> },
    { key: 'group', title: t('plugin_report.report.group'), mobile: 'sub', render: (r) => <span>{builtinText(t, r.groupName) || '-'}</span> },
    { key: 'remark', title: t('common.remark'), render: (r) => <span className="text-muted-foreground">{r.remark || '-'}</span> },
  ]

  return (
    <div className="flex flex-col gap-4">
      <DataTable
        columns={columns}
        data={reports}
        rowKey="id"
        loading={isLoading}
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        columnSettingsKey="plugin-report-reports"
        emptyText={t('plugin_report.report.empty')}
        onRowClick={(r) => setSelected(r.id)}
        toolbar={
          <Can perm="report:report">
            <Button
              onClick={() => {
                setEditing(null)
                setOpen(true)
              }}
            >
              <Plus aria-hidden="true" />
              {t('plugin_report.report.create')}
            </Button>
          </Can>
        }
        rowActions={(r) => (
          <Can perm="report:report">
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => void onEdit(r)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(r)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {selected ? <ReportViewer key={selected} reportId={selected} /> : null}
      <ReportDialog open={open} onOpenChange={setOpen} editing={editing} />
    </div>
  )
}
