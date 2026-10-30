import { useRef, useState } from 'react'
import { ChevronDown, FileDown, FileUp, Trash2, Upload, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { DataTable, type Column } from '@/framework/components/DataTable'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/framework/ui/dropdown-menu'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { downloadErrorsCsv, downloadTemplate, useDeleteImport, useJobs, useRunImport, useSources, type ImportJob } from './api'
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
  const confirm = useConfirm()
  const fileRef = useRef<HTMLInputElement | null>(null)
  const [pendingSource, setPendingSource] = useState<string | undefined>()
  const [files, setFiles] = useState<File[]>([])

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
    if (fileRef.current) fileRef.current.value = ''
    setFiles([])
  }

  // 选错一个不该逼着重新选整批；同时清掉 input 的值，否则再选同一个文件不触发 change。
  const dropFile = (idx: number) => {
    setFiles((prev) => prev.filter((_, i) => i !== idx))
    if (fileRef.current) fileRef.current.value = ''
  }

  const deleteJob = async (j: ImportJob) => {    const ok = await confirm({
      title: t('plugin_imports.delete'),
      description: t('plugin_imports.deleteConfirm', { name: j.fileName }),
      destructive: true,
    })
    if (!ok) return
    try {
      await remove.mutateAsync(j.id)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error((e as Error).message)
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
    { key: 'status', title: t('plugin_imports.fields.status'), render: (j) => <Badge variant={statusVariant(j.status)}>{t(`plugin_imports.status.${j.status}`)}</Badge> },
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
        <div className="flex flex-wrap items-center gap-2">
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
          <input
            ref={fileRef}
            type="file"
            multiple
            accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
            aria-label={t('plugin_imports.fields.file')}
            className="sr-only"
            onChange={(e) => setFiles([...(e.target.files ?? [])])}
          />
          <Button variant="outline" className="max-w-64" onClick={() => fileRef.current?.click()} disabled={noSources}>
            <FileUp aria-hidden="true" />
            <span className="truncate">
              {files.length === 0
                ? t('plugin_imports.fields.file')
                : files.length === 1
                  ? files[0].name
                  : t('plugin_imports.filesSelected', { count: files.length })}
            </span>
          </Button>
          <Can perm="imports:run">
            <Button onClick={() => void submit()} disabled={run.isPending || noSources}>
              <Upload aria-hidden="true" />
              {t('plugin_imports.run')}
            </Button>
          </Can>
        </div>
        {files.length ? (
          <ul className="flex flex-wrap gap-1.5">
            {files.map((f, i) => (
              <li
                key={`${f.name}-${i}`}
                className="inline-flex items-center gap-1 rounded-md border border-border bg-muted/40 py-1 pe-1 ps-2 text-xs"
              >
                <span className="max-w-56 truncate">{f.name}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="size-6"
                  aria-label={t('plugin_imports.removeFile', { name: f.name })}
                  onClick={() => dropFile(i)}
                >
                  <X aria-hidden="true" className="size-3.5" />
                </Button>
              </li>
            ))}
          </ul>
        ) : null}
        {pendingSource ? (
          <div className="mt-2 flex flex-wrap gap-1.5">
            {(sources.data ?? []).find((s) => s.name === pendingSource)?.columns.map((c) => (
              <Badge key={c.key} variant={c.required ? 'secondary' : 'outline'}>
                {c.label}
                {c.required ? ' *' : ''}
              </Badge>
            ))}
          </div>
        ) : null}
        {noSources ? <p className="text-xs text-muted-foreground">{t('plugin_imports.noSources')}</p> : null}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Select value={source ?? ALL} onValueChange={(v) => { setSource(v === ALL ? undefined : v); setPage(1) }}>
          <SelectTrigger className="md:w-56" aria-label={t('plugin_imports.fields.source')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_imports.all')}</SelectItem>
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
            <SelectItem value={ALL}>{t('plugin_imports.all')}</SelectItem>
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
        rowActions={(j) => (
          <div className="flex items-center justify-end gap-1">
            {j.failRows > 0 ? (
              <Button variant="ghost" size="sm" onClick={() => void downloadErrorsCsv(j.id)}>
                <FileDown aria-hidden="true" />
                {t('plugin_imports.errorsCsv')}
              </Button>
            ) : null}
            <Can perm="imports:run">
              <Button
                variant="ghost"
                size="icon"
                aria-label={t('plugin_imports.delete')}
                title={t('plugin_imports.delete')}
                disabled={j.status === 'running' || j.status === 'pending' || remove.isPending}
                onClick={() => void deleteJob(j)}
              >
                <Trash2 aria-hidden="true" className="text-destructive" />
              </Button>
            </Can>
          </div>
        )}
      />
    </PageContainer>
  )
}
