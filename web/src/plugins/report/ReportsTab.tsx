import { useState } from 'react'
import { Bell, Eye, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { useHeaderSort } from '@/framework/hooks/use-header-sort'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { api } from '@/framework/request/http'
import { useDeleteReport, useDeleteReports, useReportList, type Report } from './api'
import { ReportDialog } from './ReportDialog'
import { SubscriptionDialog } from './SubscriptionDialog'
import { builtinText } from './builtinText'

/**
 * 报表清单：查看与统一走 page 级的 `monitor/report-view`（与大屏的 `report-screen` 同构），
 * 不在列表下挂内嵌预览——内嵌预览会把首屏让给表格、把报表挤到折叠线以下，
 * 且列表一挂载就要跑完第一张报表所有图表的数据查询。
 */
export function ReportsTab() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const confirm = useConfirm()
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  // 表头排序：默认按分组升序（与后端回退序一致），改排序回第一页
  const { sort, desc, sortProp } = useHeaderSort('group', false, () => setPage(1))
  const { data, isLoading } = useReportList({ keyword, page, pageSize, sort, desc })
  const reports = data?.list ?? []
  const remove = useDeleteReport()
  const removeBatch = useDeleteReports()
  const [picked, setPicked] = useState<string[]>([])
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Report | null>(null)
  const [subscribing, setSubscribing] = useState<Report | null>(null)

  const openView = (id: string) => navigate(`/monitor/report-view?id=${id}`)

  const onEdit = async (r: Report) => {
    try {
      // 管理端读取完整配置（含 report:report 权限校验）
      setEditing(await api.get<Report>(`/plugins/report/reports/${r.id}`))
      setOpen(true)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  // 删掉本页最后一条时不用自己回退页码：DataTable 有「这一页空了就退一页」的自愈逻辑
  const onDelete = async (r: Report) => {
    if (!(await confirm({ destructive: true, description: t('plugin_report.report.deleteConfirm', { name: builtinText(t, r.name) }) }))) return
    try {
      await remove.mutateAsync(r.id)
      setPicked((s) => s.filter((id) => id !== r.id))
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  // 批量删：一次点名多条。deleted 是后端实删条数（点名但本租户没有的计入 blocked），部分删除时不写成「操作成功」。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('plugin_report.report.deleteBatchConfirm', { count: ids.length }) }))) return
    try {
      const { deleted } = await removeBatch.mutateAsync(ids)
      setPicked([])
      toast.success(deleted < ids.length ? t('plugin_report.report.deletedPartial', { deleted, total: ids.length }) : t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<Report>[] = [
    { key: 'name', title: t('common.name'), sortKey: 'name', mobile: 'title', render: (r) => <span className="font-medium text-foreground">{builtinText(t, r.name)}</span> },
    { key: 'group', title: t('plugin_report.report.group'), sortKey: 'group', mobile: 'sub', render: (r) => <span>{builtinText(t, r.groupName) || '-'}</span> },
    { key: 'remark', title: t('common.remark'), render: (r) => (r.remark ? <span className="block max-w-64 truncate text-muted-foreground" title={r.remark}>{r.remark}</span> : <span className="text-muted-foreground">-</span>) },
  ]

  return (
    <div className="flex flex-col gap-4">
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
        <Input className="md:w-56" value={draft} onChange={(e) => setDraft(e.target.value)} placeholder={t('plugin_report.searchPlaceholder')} aria-label={t('common.keyword')} />
      </FilterBar>
      <DataTable
        columns={columns}
        data={reports}
        rowKey="id"
        loading={isLoading}
        sort={sortProp}
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
        emptyText={keyword ? t('plugin_report.report.emptySearch') : t('plugin_report.report.empty')}
        selection={{ selected: picked, onChange: setPicked }}
        batchActions={(ids) => (
          <Can perm="report:report">
            <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        onRowClick={(r) => openView(r.id)}
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
          <>
            {/* 查看对 report:view 开放：只看报表的角色不该只有一个「什么都没说」的可点行 */}
            <Button variant="ghost" size="icon" aria-label={t('plugin_report.report.open')} onClick={() => openView(r.id)}>
              <Eye aria-hidden="true" />
            </Button>
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
          </>
        )}
      />
      <ReportDialog open={open} onOpenChange={setOpen} editing={editing} />
      <SubscriptionDialog report={subscribing} onClose={() => setSubscribing(null)} />
    </div>
  )
}
