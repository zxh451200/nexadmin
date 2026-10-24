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
import { useDatasets, useDeleteDataset, useSources, type Dataset } from './api'
import { DatasetDialog } from './DatasetDialog'

export function DatasetsTab() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Dataset | null>(null)
  const { data, isLoading } = useDatasets({ keyword, page, pageSize })
  const { data: sources = [] } = useSources()
  const remove = useDeleteDataset()

  const sourceLabel = (table: string) => t(`plugin_report.source.${table}`, { defaultValue: sources.find((s) => s.table === table)?.label ?? table })
  const openForm = (d: Dataset | null) => {
    setEditing(d)
    setOpen(true)
  }
  const onDelete = async (d: Dataset) => {
    if (!(await confirm({ destructive: true, description: t('plugin_report.dataset.deleteConfirm', { name: d.name }) }))) return
    try {
      await remove.mutateAsync(d.id)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<Dataset>[] = [
    { key: 'name', title: t('common.name'), mobile: 'title', render: (d) => <span className="font-medium text-foreground">{d.name}</span> },
    { key: 'source', title: t('plugin_report.dataset.source'), mobile: 'sub', render: (d) => <Badge variant="outline">{sourceLabel(d.source)}</Badge> },
    { key: 'columns', title: t('plugin_report.dataset.columns'), render: (d) => <span className="num">{d.columns.length}</span> },
    { key: 'filters', title: t('plugin_report.dataset.fixedFilters'), render: (d) => <span className="num">{d.filters.length}</span> },
    { key: 'remark', title: t('common.remark'), render: (d) => <span className="text-muted-foreground">{d.remark || '-'}</span> },
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
        columnSettingsKey="plugin-report-datasets"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_report.dataset.empty')}
        toolbar={
          <Button onClick={() => openForm(null)}>
            <Plus aria-hidden="true" />
            {t('plugin_report.dataset.create')}
          </Button>
        }
        rowActions={(d) => (
          <>
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(d)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Can perm="report:dataset">
              <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(d)}>
                <Trash2 aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />
      <DatasetDialog open={open} onOpenChange={setOpen} editing={editing} />
    </div>
  )
}
