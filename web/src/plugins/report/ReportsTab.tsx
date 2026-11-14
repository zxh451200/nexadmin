import { useEffect, useState } from 'react'
import { Bell, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { Button } from '@/framework/ui/button'
import { api } from '@/framework/request/http'
import { useDeleteReport, useDeleteReports, useReportList, type Report } from './api'
import { ReportDialog } from './ReportDialog'
import { ReportViewer } from './ReportViewer'
import { SubscriptionDialog } from './SubscriptionDialog'
import { builtinText } from './builtinText'

export function ReportsTab() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const { data, isLoading } = useReportList({ page, pageSize })
  const reports = data?.list ?? []
  const remove = useDeleteReport()
  const removeBatch = useDeleteReports()
  const [selected, setSelected] = useState<string>()
  // picked 是表格的多选，和上面「预览面板当前展示的报表」selected 是两件事，别混用。
  const [picked, setPicked] = useState<string[]>([])
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Report | null>(null)
  const [subscribing, setSubscribing] = useState<Report | null>(null)

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
      setPicked((s) => s.filter((id) => id !== r.id))
      // 本页被删空：回上一页，避免停在一个空页
      if (page > 1 && reports.length === 1) setPage(page - 1)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  // 批量删：一次点名多条。deleted 是后端实删条数（点名但本租户没有的计入 blocked），部分删除时不写成「操作成功」。
  // 预览面板不能留在已删报表上：picked 里含当前 selected 就撤掉预览，让 useEffect 重新选一条存在的。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('plugin_report.report.deleteBatchConfirm', { count: ids.length }) }))) return
    try {
      const { deleted } = await removeBatch.mutateAsync(ids)
      if (selected && ids.includes(selected)) setSelected(undefined)
      setPicked([])
      toast.success(deleted < ids.length ? t('plugin_report.report.deletedPartial', { deleted, total: ids.length }) : t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<Report>[] = [
    { key: 'name', title: t('common.name'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{builtinText(t, r.name)}</span> },
    { key: 'group', title: t('plugin_report.report.group'), mobile: 'sub', render: (r) => <span>{builtinText(t, r.groupName) || '-'}</span> },
    { key: 'remark', title: t('common.remark'), render: (r) => (r.remark ? <span className="block max-w-64 truncate text-muted-foreground" title={r.remark}>{r.remark}</span> : <span className="text-muted-foreground">-</span>) },
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
        selection={{ selected: picked, onChange: setPicked }}
        batchActions={(ids) => (
          <Can perm="report:report">
            <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
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
            <Button variant="ghost" size="icon" aria-label={t('plugin_report.subscription.title')} onClick={() => setSubscribing(r)}>
              <Bell aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(r)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {selected ? <ReportViewer key={selected} reportId={selected} /> : null}
      <ReportDialog open={open} onOpenChange={setOpen} editing={editing} />
      <SubscriptionDialog report={subscribing} onClose={() => setSubscribing(null)} />
    </div>
  )
}
