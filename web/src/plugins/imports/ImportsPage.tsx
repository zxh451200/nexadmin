import { useState } from 'react'
import { ChevronDown, FileDown, FilePen, OctagonX, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { ExportCsvButton } from '@/framework/components/export/ExportCsvButton'
import { FileDropField } from '@/framework/components/FileDropField'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { DataTable, type Column } from '@/framework/components/DataTable'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/framework/ui/dropdown-menu'
import { Progress } from '@/framework/ui/progress'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import {
  downloadErrorsCsv,
  downloadRetryCsv,
  downloadTemplate,
  fetchJobsForExport,
  useCancelImport,
  useDeleteImport,
  useDeleteImports,
  useJobs,
  useRunImport,
  useSources,
  type ImportJob,
} from './api'
import { formatTime, statusVariant } from './format'

const ALL = '__all'

export default function ImportsPage() {
  const { t, i18n } = useTranslation()
  const [source, setSource] = useState<string | undefined>()
  const [status, setStatus] = useState<string | undefined>()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const sources = useSources()
  const jobs = useJobs({ source, status, page, pageSize })
  const run = useRunImport()
  const remove = useDeleteImport()
  const removeBatch = useDeleteImports()
  const cancelJob = useCancelImport()
  const confirm = useConfirm()
  const [selected, setSelected] = useState<string[]>([])
  const [pendingSource, setPendingSource] = useState<string | undefined>()
  const [files, setFiles] = useState<File[]>([])

  // 取消：已处理的部分保留，台账记为 cancelled；正在消失前的 404/409 由 toast 展示。
  const onCancel = async (j: ImportJob) => {
    const ok = await confirm({
      title: t('plugin_imports.cancel'),
      description: t('plugin_imports.cancelConfirm', { name: j.fileName }),
    })
    if (!ok) return
    try {
      await cancelJob.mutateAsync(j.id)
      toast.success(t('plugin_imports.cancelDone'))
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  // 没有数据源时工具栏照常渲染（来源下拉与「开始导入」置灰），避免页面像坏掉一样
  const noSources = sources.isSuccess && (sources.data ?? []).length === 0

  // 多选时逐个提交：每个文件一条台账，任何一个失败不影响其余文件
  const submit = async () => {
    const src = pendingSource ?? source
    if (!files.length || !src) {
      toast.error(t('plugin_imports.pickFileFirst'))
      return
    }
    let ok = 0
    let fail = 0
    let last: ImportJob | null = null
    for (const file of files) {
      try {
        last = await run.mutateAsync({ source: src, file })
        ok++
      } catch (e) {
        fail++
        toast.error(`${file.name}: ${(e as Error).message}`)
      }
    }
    if (files.length > 1) toast.success(t('plugin_imports.batchDone', { ok, fail }))
    else if (last) toast.success(t('plugin_imports.doneSummary', { ok: last.okRows, fail: last.failRows }))
    setFiles([])
  }

  const deleteJob = async (j: ImportJob) => {    const ok = await confirm({
      title: t('plugin_imports.delete'),
      description: t('plugin_imports.deleteConfirm', { name: j.fileName }),
      destructive: true,
    })
    if (!ok) return
    try {
      await remove.mutateAsync(j.id)
      setSelected((s) => s.filter((id) => id !== j.id))
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  // 批量删：跨页选中也一次提交。后端与单条同口径——批次里有排队中/执行中的任务就整批失败（先取消再删），
  // 越权或已不存在的 id 不计入实删数，所以 deleted 少于选中时要据实说「删了 n 条」，不写成全删成功。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('plugin_imports.deleteBatchConfirm', { count: ids.length }) }))) return
    try {
      const { deleted } = await removeBatch.mutateAsync(ids)
      setSelected([])
      toast.success(deleted < ids.length ? t('plugin_imports.deletedPartial', { deleted, total: ids.length }) : t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const sourceTitle = (name: string) => (sources.data ?? []).find((s) => s.name === name)?.title ?? name

  const columns: Column<ImportJob>[] = [
    {
      key: 'fileName',
      title: t('plugin_imports.fields.fileName'),
      mobile: 'title',
      render: (j) => (
        <div>
          <span className="font-medium text-foreground">{j.fileName || '—'}</span>
          {/* 台账里存的是来源标识（crm_customers），上屏要换成来源的显示名 */}
          <div className="text-xs text-muted-foreground">{sourceTitle(j.source)}</div>
        </div>
      ),
    },
    { key: 'total', title: t('plugin_imports.fields.totalRows'), align: 'end', render: (j) => <span className="num">{j.totalRows}</span> },
    { key: 'ok', title: t('plugin_imports.fields.okRows'), align: 'end', render: (j) => <span className="num">{j.okRows}</span> },
    {
      key: 'fail',
      title: t('plugin_imports.fields.failRows'),
      align: 'end',
      render: (j) => (j.failRows > 0 ? <Badge variant="destructive">{j.failRows}</Badge> : <span className="num">0</span>),
    },
    {
      key: 'status',
      title: t('plugin_imports.fields.status'),
      render: (j) => (
        <div className="flex min-w-32 flex-col gap-1">
          <Badge variant={statusVariant(j.status)}>{t(`plugin_imports.status.${j.status}`)}</Badge>
          {/* 异步执行中显示进度：已处理行 / 总行数（done/failed 台账本来就是全量，不需要条） */}
          {(j.status === 'running' || j.status === 'pending') && j.totalRows > 0 ? (
            <Progress value={Math.round(((j.okRows + j.failRows) / j.totalRows) * 100)} className="h-1.5" />
          ) : null}
        </div>
      ),
    },
    { key: 'createdBy', title: t('plugin_imports.fields.createdBy'), render: (j) => j.createdBy },
    {
      key: 'createdAt',
      title: t('plugin_imports.fields.createdAt'),
      render: (j) => <span className="num text-xs text-muted-foreground">{formatTime(j.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_imports.title')} description={t('plugin_imports.description')} />

      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">
              {t('plugin_imports.fields.source')}
              <RequiredMark />
            </span>
            <Select value={pendingSource ?? ''} onValueChange={setPendingSource} disabled={noSources}>
              <SelectTrigger className="md:w-56" aria-label={t('plugin_imports.fields.source')}>
                <SelectValue placeholder={t('plugin_imports.fields.source')} />
              </SelectTrigger>
              <SelectContent>
                {(sources.data ?? []).map((s) => (
                  <SelectItem key={s.name} value={s.name}>
                    {s.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          {pendingSource ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline">
                  <FileDown aria-hidden="true" />
                  {t('plugin_imports.downloadTemplate')}
                  <ChevronDown aria-hidden="true" className="opacity-60" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                <DropdownMenuItem onSelect={() => void downloadTemplate(pendingSource, 'csv')}>
                  {t('plugin_imports.templateCsv')}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => void downloadTemplate(pendingSource, 'xlsx')}>
                  {t('plugin_imports.templateXlsx')}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </div>
        {pendingSource ? (
          <div className="flex flex-wrap gap-1.5">
            {(sources.data ?? []).find((s) => s.name === pendingSource)?.columns.map((c) => (
              <Badge key={c.key} variant={c.required ? 'secondary' : 'outline'}>
                {c.label}
                {c.required ? ' *' : ''}
              </Badge>
            ))}
          </div>
        ) : null}
        <div className="flex max-w-2xl flex-col gap-1.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span id="imports-file-label" className="text-xs text-muted-foreground">
              {t('plugin_imports.fields.file')}
              <RequiredMark />
            </span>
            {files.length > 1 ? (
              <span className="text-xs text-muted-foreground">{t('plugin_imports.filesSelected', { count: files.length })}</span>
            ) : null}
          </div>
          <FileDropField
            multiple
            accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
            labelledBy="imports-file-label"
            disabled={noSources}
            files={files}
            onChange={setFiles}
          />
        </div>
        <Can perm="imports:run">
          <div>
            <Button onClick={() => void submit()} disabled={run.isPending || noSources}>
              <Upload aria-hidden="true" />
              {t('plugin_imports.run')}
            </Button>
          </div>
        </Can>
        {noSources ? <p className="text-xs text-muted-foreground">{t('plugin_imports.noSources')}</p> : null}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Select value={source ?? ALL} onValueChange={(v) => { setSource(v === ALL ? undefined : v); setPage(1) }}>
          <SelectTrigger className="md:w-56" aria-label={t('plugin_imports.fields.source')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_imports.filter.allSources')}</SelectItem>
            {(sources.data ?? []).map((s) => (
              <SelectItem key={s.name} value={s.name}>
                {s.title}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={status ?? ALL} onValueChange={(v) => { setStatus(v === ALL ? undefined : v); setPage(1) }}>
          <SelectTrigger className="md:w-40" aria-label={t('plugin_imports.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_imports.filter.allStatuses')}</SelectItem>
            <SelectItem value="pending">{t('plugin_imports.status.pending')}</SelectItem>
            <SelectItem value="running">{t('plugin_imports.status.running')}</SelectItem>
            <SelectItem value="done">{t('plugin_imports.status.done')}</SelectItem>
            <SelectItem value="failed">{t('plugin_imports.status.failed')}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <DataTable
        columns={columns}
        data={jobs.data?.list ?? []}
        rowKey="id"
        loading={jobs.isLoading}
        columnSettingsKey="plugin-imports-jobs"
        pagination={{
          page,
          pageSize,
          total: jobs.data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_imports.empty')}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="imports:run">
            <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        toolbar={
          // 台账是「谁在什么时候导入哪个文件、成功/失败多少行」的凭据，导出用于核对与归档：
          // 走列表同一个分页接口按筛选捞全，密钥类字段本来就不在台账里。
          <ExportCsvButton
            filename="import-jobs"
            perm="imports:list"
            getRows={() => fetchJobsForExport({ source, status })}
            columns={[
              { header: t('plugin_imports.fields.fileName'), value: (j) => j.fileName },
              { header: t('plugin_imports.fields.source'), value: (j) => sourceTitle(j.source) },
              { header: t('plugin_imports.fields.totalRows'), value: (j) => j.totalRows },
              { header: t('plugin_imports.fields.okRows'), value: (j) => j.okRows },
              { header: t('plugin_imports.fields.failRows'), value: (j) => j.failRows },
              { header: t('plugin_imports.fields.status'), value: (j) => t(`plugin_imports.status.${j.status}`) },
              { header: t('plugin_imports.fields.createdBy'), value: (j) => j.createdBy },
              { header: t('plugin_imports.fields.createdAt'), value: (j) => formatTime(j.createdAt, i18n.language) },
            ]}
          />
        }
        rowActions={(j) => (
          <div className="flex items-center justify-end gap-1">
            {j.failRows > 0 ? (
              <>
                <Button variant="ghost" size="sm" onClick={() => void downloadErrorsCsv(j.id)}>
                  <FileDown aria-hidden="true" />
                  {t('plugin_imports.errorsCsv')}
                </Button>
                {/* 待修复 CSV：失败行按模板口径原样导出，修正后可直接再导入 */}
                <Button variant="ghost" size="sm" onClick={() => void downloadRetryCsv(j.id)}>
                  <FilePen aria-hidden="true" />
                  {t('plugin_imports.retryCsv')}
                </Button>
              </>
            ) : null}
            <Can perm="imports:run">
              {j.status === 'running' || j.status === 'pending' ? (
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t('plugin_imports.cancel')}
                  title={t('plugin_imports.cancel')}
                  disabled={cancelJob.isPending}
                  onClick={() => void onCancel(j)}
                >
                  <OctagonX aria-hidden="true" className="text-destructive" />
                </Button>
              ) : (
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t('plugin_imports.delete')}
                  title={t('plugin_imports.delete')}
                  disabled={remove.isPending}
                  onClick={() => void deleteJob(j)}
                >
                  <Trash2 aria-hidden="true" className="text-destructive" />
                </Button>
              )}
            </Can>
          </div>
        )}
      />
    </PageContainer>
  )
}
