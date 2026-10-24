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
import { useCharts, useDatasetOptions, useDeleteChart, type Chart } from './api'
import { ChartDialog } from './ChartDialog'

export function ChartsTab() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Chart | null>(null)
  const { data, isLoading } = useCharts({ keyword, page, pageSize })
  const { data: datasets = [] } = useDatasetOptions('chart')
  const remove = useDeleteChart()

  const openForm = (c: Chart | null) => {
    setEditing(c)
    setOpen(true)
  }
  const onDelete = async (c: Chart) => {
    if (!(await confirm({ destructive: true, description: t('plugin_report.chart.deleteConfirm', { name: c.name }) }))) return
    try {
      await remove.mutateAsync(c.id)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<Chart>[] = [
    { key: 'name', title: t('common.name'), mobile: 'title', render: (c) => <span className="font-medium text-foreground">{c.name}</span> },
    { key: 'type', title: t('plugin_report.chart.type'), mobile: 'sub', render: (c) => <Badge variant="outline">{t(`plugin_report.type.${c.type}`)}</Badge> },
    { key: 'dataset', title: t('plugin_report.chart.dataset'), render: (c) => <span>{datasets.find((d) => d.id === c.datasetId)?.name ?? '-'}</span> },
    {
      key: 'shape',
      title: t('plugin_report.chart.shape'),
      render: (c) => (
        <span className="num text-muted-foreground">
          {c.config.dimensions.length} / {c.config.measures.length}
        </span>
      ),
    },
    { key: 'remark', title: t('common.remark'), render: (c) => <span className="text-muted-foreground">{c.remark || '-'}</span> },
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
        toolbar={
          <Button onClick={() => openForm(null)}>
            <Plus aria-hidden="true" />
            {t('plugin_report.chart.create')}
          </Button>
        }
        rowActions={(c) => (
          <>
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(c)}>
              <Pencil aria-hidden="true" />
            </Button>
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
