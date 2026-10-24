import { useEffect, useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { Button } from '@/framework/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { api } from '@/framework/request/http'
import { useDeleteReport, useReportList, type Report } from './api'
import { ReportDialog } from './ReportDialog'
import { ReportViewer } from './ReportViewer'

export function ReportsTab() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data: reports = [], isLoading } = useReportList()
  const remove = useDeleteReport()
  const [selected, setSelected] = useState<string>()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Report | null>(null)

  useEffect(() => {
    if (!reports.some((r) => r.id === selected)) setSelected(reports[0]?.id)
  }, [reports, selected])

  const label = (r: Report) => (r.groupName ? `${r.groupName} / ${r.name}` : r.name)
  const onEdit = async () => {
    if (!selected) return
    try {
      // 管理端读取完整配置（含 report:report 权限校验）
      setEditing(await api.get<Report>(`/plugins/report/reports/${selected}`))
      setOpen(true)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  const onDelete = async () => {
    const cur = reports.find((r) => r.id === selected)
    if (!cur || !(await confirm({ destructive: true, description: t('plugin_report.report.deleteConfirm', { name: cur.name }) }))) return
    try {
      await remove.mutateAsync(cur.id)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={selected ?? ''} onValueChange={setSelected} disabled={reports.length === 0}>
          <SelectTrigger className="md:w-80" aria-label={t('plugin_report.report.pick')}>
            <SelectValue placeholder={isLoading ? t('common.loading') : t('plugin_report.report.pick')} />
          </SelectTrigger>
          <SelectContent>
            {reports.map((r) => (
              <SelectItem key={r.id} value={r.id}>
                {label(r)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Can perm="report:report">
          <div className="ms-auto flex flex-wrap items-center gap-2">
            <Button
              onClick={() => {
                setEditing(null)
                setOpen(true)
              }}
            >
              <Plus aria-hidden="true" />
              {t('plugin_report.report.create')}
            </Button>
            <Button variant="outline" disabled={!selected} onClick={() => void onEdit()}>
              <Pencil aria-hidden="true" />
              {t('common.edit')}
            </Button>
            <Button variant="outline" className="text-destructive" disabled={!selected} onClick={() => void onDelete()}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </div>
        </Can>
      </div>
      {selected ? <ReportViewer key={selected} reportId={selected} /> : <EmptyState title={isLoading ? t('common.loading') : t('plugin_report.report.empty')} />}
      <ReportDialog open={open} onOpenChange={setOpen} editing={editing} />
    </div>
  )
}
