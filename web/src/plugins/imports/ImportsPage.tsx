import { useRef, useState } from 'react'
import { FileDown, FileUp, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { errorsCSVUrl, templateUrl, useJobs, useRunImport, useSources, type ImportJob } from './api'
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
  const fileRef = useRef<HTMLInputElement | null>(null)
  const [pendingSource, setPendingSource] = useState<string | undefined>()

  const submit = async () => {
    const file = fileRef.current?.files?.[0]
    const src = pendingSource ?? source
    if (!file || !src) {
      toast.error(t('plugin_imports.pickFileFirst'))
      return
    }
    try {
      const job = await run.mutateAsync({ source: src, file })
      toast.success(t('plugin_imports.doneSummary', { ok: job.okRows, fail: job.failRows }))
      if (fileRef.current) fileRef.current.value = ''
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const columns: Column<ImportJob>[] = [
    {
      key: 'fileName',
      title: t('plugin_imports.fields.fileName'),
      mobile: 'title',
      render: (j) => (
        <div>
          <span className="font-medium text-foreground">{j.fileName || '—'}</span>
          <div className="text-xs text-muted-foreground">{j.source}</div>
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

      <div className="rounded-lg border bg-card p-4">
        <div className="flex flex-wrap items-center gap-2">
          <Select value={pendingSource ?? ''} onValueChange={setPendingSource}>
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
            <Button variant="outline" onClick={() => window.open(templateUrl(pendingSource), '_blank')}>
              <FileDown aria-hidden="true" />
              {t('plugin_imports.downloadTemplate')}
            </Button>
          ) : null}
          <input
            ref={fileRef}
            type="file"
            accept=".csv,text/csv"
            aria-label={t('plugin_imports.fields.file')}
            className="text-sm"
          />
          <Can perm="imports:run">
            <Button onClick={() => void submit()} disabled={run.isPending}>
              <Upload aria-hidden="true" />
              {t('plugin_imports.run')}
            </Button>
          </Can>
        </div>
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
        rowActions={(j) =>
          j.failRows > 0 ? (
            <Button variant="ghost" size="sm" onClick={() => window.open(errorsCSVUrl(j.id), '_blank')}>
              <FileUp aria-hidden="true" />
              {t('plugin_imports.errorsCsv')}
            </Button>
          ) : null
        }
      />
    </PageContainer>
  )
}
