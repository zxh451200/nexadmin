import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { useCharts, useDatasetOptions, useDeleteChart, useDeleteCharts, type Chart } from './api'
import { ChartDialog } from './ChartDialog'
import { builtinText } from './builtinText'

export function ChartsTab() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Chart | null>(null)
  const [picked, setPicked] = useState<string[]>([])
  const { data, isLoading } = useCharts({ keyword, page, pageSize })
  const { data: datasets = [] } = useDatasetOptions('chart')
  const remove = useDeleteChart()
  const removeBatch = useDeleteCharts()

  const openForm = (c: Chart | null) => {
    setEditing(c)
    setOpen(true)
  }
  const onDelete = async (c: Chart) => {
    if (!(await confirm({ destructive: true, description: t('plugin_report.chart.deleteConfirm', { name: builtinText(t, c.name) }) }))) return
    try {
      await remove.mutateAsync(c.id)
      setPicked((s) => s.filter((id) => id !== c.id))
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  // 批量删：一次点名多条。deleted 是后端实删条数（点名但本租户没有的那些计入 blocked，不会被删），
  // 部分删除时不写成「操作成功」；引用到它们的报表/大屏位置显示「图表已删除」。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('plugin_report.chart.deleteBatchConfirm', { count: ids.length }) }))) return
    try {
      const { deleted } = await removeBatch.mutateAsync(ids)
      setPicked([])
      toast.success(deleted < ids.length ? t('plugin_report.chart.deletedPartial', { deleted, total: ids.length }) : t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<Chart>[] = [
    { key: 'name', title: t('common.name'), mobile: 'title', render: (c) => <span className="font-medium text-foreground">{builtinText(t, c.name)}</span> },
    { key: 'type', title: t('plugin_report.chart.type'), mobile: 'sub', render: (c) => <Badge variant="outline">{t(`plugin_report.type.${c.type}`)}</Badge> },
    { key: 'dataset', title: t('plugin_report.chart.dataset'), render: (c) => <span>{builtinText(t, datasets.find((d) => d.id === c.datasetId)?.name) || '-'}</span> },
    {
      key: 'shape',
      title: t('plugin_report.chart.shape'),
      render: (c) => (
        <span className="num text-muted-foreground">
          {c.config.dimensions.length} / {c.config.measures.length}
        </span>
      ),
    },
    { key: 'remark', title: t('common.remark'), render: (c) => (c.remark ? <span className="block max-w-64 truncate text-muted-foreground" title={c.remark}>{c.remark}</span> : <span className="text-muted-foreground">-</span>) },
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
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-report-charts"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_report.chart.empty')}
        selection={{ selected: picked, onChange: setPicked }}
        batchActions={(ids) => (
          <Can perm="report:chart">
            <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        toolbar={
          <Can perm="report:chart">
            <Button onClick={() => openForm(null)}>
              <Plus aria-hidden="true" />
              {t('plugin_report.chart.create')}
            </Button>
          </Can>
        }
        rowActions={(c) => (
          <>
            <Can perm="report:chart">
              <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(c)}>
                <Pencil aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="report:chart">
              <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(c)}>
                <Trash2 aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />
      <ChartDialog open={open} onOpenChange={setOpen} editing={editing} />
    </div>
  )
}
