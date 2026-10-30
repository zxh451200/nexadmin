import { useState } from 'react'
import { Eraser } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DetailAction } from '@/framework/components/DetailAction'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useCleanLogs, useJobs, useLogDetail, useLogs, type CronLog, type LogQuery } from './api'
import { formatTime, statusVariant } from './format'

const ALL = '__all'
const STATUSES = ['running', 'success', 'failed'] as const

function Snippet({ text, tone }: { text: string; tone?: 'error' }) {
  if (!text) return <span className="text-muted-foreground">-</span>
  return (
    <span className={`block max-w-[16rem] truncate font-mono text-xs ${tone === 'error' ? 'text-destructive' : 'text-foreground'}`} title={text}>
      {text}
    </span>
  )
}

function LogDetailDialog({ id, onClose }: { id: string | null; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const { data } = useLogDetail(id ?? undefined)
  return (
    <ResponsiveDialog
      open={!!id}
      onOpenChange={(o) => !o && onClose()}
      size="lg"
      title={t('plugin_cron.logDetail')}
      description={data?.jobName}
      footer={
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {data ? (
        <div className="flex flex-col gap-4 text-sm">
          <dl className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <div>
              <dt className="text-xs text-muted-foreground">{t('plugin_cron.fields.status')}</dt>
              <dd>
                <Badge variant={statusVariant(data.status)}>{t(`plugin_cron.status.${data.status}`)}</Badge>
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('plugin_cron.fields.trigger')}</dt>
              <dd>{t(`plugin_cron.trigger.${data.trigger}`)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('plugin_cron.fields.startedAt')}</dt>
              <dd className="num">{formatTime(data.startedAt, i18n.language)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('plugin_cron.fields.costMs')} / {t('plugin_cron.fields.attempts')}</dt>
              <dd className="num">{t('plugin_cron.ms', { value: data.costMs })} / {data.attempts}</dd>
            </div>
          </dl>
          <div>
            <p className="mb-1 text-xs text-muted-foreground">{t('plugin_cron.fields.output')}</p>
            <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-md border border-border bg-muted/40 p-3 font-mono text-xs text-foreground">{data.output || '-'}</pre>
          </div>
          <div>
            <p className="mb-1 text-xs text-muted-foreground">{t('plugin_cron.fields.error')}</p>
            <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-md border border-destructive/30 bg-destructive/10 p-3 font-mono text-xs text-destructive">{data.error || '-'}</pre>
          </div>
        </div>
      ) : null}
    </ResponsiveDialog>
  )
}

function CleanDialog({ open, onOpenChange, jobId }: { open: boolean; onOpenChange: (o: boolean) => void; jobId?: string }) {
  const { t } = useTranslation()
  const clean = useCleanLogs()
  const [days, setDays] = useState('30')
  const submit = async () => {
    try {
      const r = await clean.mutateAsync({ days: Number(days), jobId })
      toast.success(t('plugin_cron.clean.done', { count: r.deleted }))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="sm"
      title={t('plugin_cron.clean.title')}
      description={t('plugin_cron.clean.description')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" variant="destructive" disabled={clean.isPending} onClick={() => void submit()}>
            {clean.isPending ? t('common.loading') : t('plugin_cron.btn.clean')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-2">
        <label className="text-sm font-medium text-foreground" id="cron-clean-days">
          {t('plugin_cron.clean.days')}
        </label>
        <Select value={days} onValueChange={setDays}>
          <SelectTrigger aria-labelledby="cron-clean-days">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="7">{t('plugin_cron.clean.d7')}</SelectItem>
            <SelectItem value="30">{t('plugin_cron.clean.d30')}</SelectItem>
            <SelectItem value="90">{t('plugin_cron.clean.d90')}</SelectItem>
            <SelectItem value="0">{t('plugin_cron.clean.all')}</SelectItem>
          </SelectContent>
        </Select>
      </div>
    </ResponsiveDialog>
  )
}

interface Props {
  /** 从任务列表跳转过来时预置的任务筛选 */
  jobId?: string
  onJobChange: (jobId: string | undefined) => void
}

export function LogsTab({ jobId, onJobChange }: Props) {
  const { t, i18n } = useTranslation()
  const [status, setStatus] = useState<string | undefined>()
  const [applied, setApplied] = useState<string | undefined>()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detail, setDetail] = useState<string | null>(null)
  const [cleanOpen, setCleanOpen] = useState(false)

  const query: LogQuery = { jobId, status: applied, page, pageSize }
  const { data, isLoading } = useLogs(query)
  const { data: jobs } = useJobs({ page: 1, pageSize: 200 })

  const columns: Column<CronLog>[] = [
    { key: 'jobName', title: t('plugin_cron.fields.job'), mobile: 'title', render: (l) => <span className="font-medium text-foreground">{l.jobName}</span> },
    { key: 'status', title: t('plugin_cron.fields.status'), mobile: 'sub', render: (l) => <Badge variant={statusVariant(l.status)}>{t(`plugin_cron.status.${l.status}`)}</Badge> },
    { key: 'trigger', title: t('plugin_cron.fields.trigger'), render: (l) => t(`plugin_cron.trigger.${l.trigger}`) },
    { key: 'startedAt', title: t('plugin_cron.fields.startedAt'), render: (l) => <span className="num">{formatTime(l.startedAt, i18n.language)}</span> },
    { key: 'costMs', title: t('plugin_cron.fields.costMs'), align: 'end', render: (l) => (l.status === 'running' ? '-' : <span className="num">{t('plugin_cron.ms', { value: l.costMs })}</span>) },
    { key: 'attempts', title: t('plugin_cron.fields.attempts'), align: 'end', render: (l) => <span className="num">{l.attempts}</span> },
    { key: 'output', title: t('plugin_cron.fields.output'), render: (l) => <Snippet text={l.output} /> },
    { key: 'error', title: t('plugin_cron.fields.error'), render: (l) => <Snippet text={l.error} tone="error" /> },
  ]

  return (
    <div className="flex flex-col gap-4">
      <FilterBar
        onSearch={() => {
          setApplied(status)
          setPage(1)
        }}
        onReset={() => {
          setStatus(undefined)
          setApplied(undefined)
          onJobChange(undefined)
          setPage(1)
        }}
      >
        <Select
          value={jobId ?? ALL}
          onValueChange={(v) => {
            onJobChange(v === ALL ? undefined : v)
            setPage(1)
          }}
        >
          <SelectTrigger className="md:w-56" aria-label={t('plugin_cron.fields.job')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_cron.all')}</SelectItem>
            {(jobs?.list ?? []).map((j) => (
              <SelectItem key={j.id} value={j.id}>
                {j.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={status ?? ALL} onValueChange={(v) => setStatus(v === ALL ? undefined : v)}>
          <SelectTrigger className="md:w-40" aria-label={t('plugin_cron.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_cron.all')}</SelectItem>
            {STATUSES.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_cron.status.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-cron-logs"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_cron.emptyLogs')}
        toolbar={
          <Can perm="cron:clean">
            <Button variant="outline" onClick={() => setCleanOpen(true)}>
              <Eraser aria-hidden="true" />
              {t('plugin_cron.btn.clean')}
            </Button>
          </Can>
        }
        rowActions={(l) => <DetailAction onClick={() => setDetail(l.id)} />}
      />

      <LogDetailDialog id={detail} onClose={() => setDetail(null)} />
      <CleanDialog open={cleanOpen} onOpenChange={setCleanOpen} jobId={jobId} />
    </div>
  )
}
