import { useState } from 'react'
import { Download, Eraser, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useAsyncJob } from '@/framework/hooks/use-async-job'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DetailAction } from '@/framework/components/DetailAction'
import { FilterBar } from '@/framework/components/FilterBar'
import { OptionPicker } from '@/framework/components/OptionPicker'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { createLogExportJob, useCleanLogs, useJobs, useLogDetail, useLogs, useRunJob, type CronLog, type LogQuery } from './api'
import { formatTime, statusVariant } from './format'

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
  /** 从任务列表跳转过来时预置的任务筛选（多选数组） */
  jobIds?: string[]
  onJobsChange: (jobIds: string[] | undefined) => void
}

export function LogsTab({ jobIds, onJobsChange }: Props) {
  const { t, i18n } = useTranslation()
  const { start: startExportJob } = useAsyncJob()
  const [status, setStatus] = useState<string[] | undefined>()
  const [applied, setApplied] = useState<string[] | undefined>()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detail, setDetail] = useState<string | null>(null)
  const [cleanOpen, setCleanOpen] = useState(false)
  // 自动刷新：运行中恒 3s 紧盯，平时 10s 慢刷；可关
  const [autoRefresh, setAutoRefresh] = useState(true)

  const query: LogQuery = { jobId: jobIds?.length ? jobIds.join(',') : undefined, status: applied?.length ? applied.join(',') : undefined, page, pageSize }
  const { data, isLoading } = useLogs(query, autoRefresh)
  const { data: jobs } = useJobs({ page: 1, pageSize: 200 })
  const runJob = useRunJob()

  // 失败记录就地重跑：按该日志的任务再触发一次手动执行（正在运行会收到 409 提示）。
  const onRerun = async (l: CronLog) => {
    try {
      await runJob.mutateAsync({ id: l.jobId })
      toast.success(t('plugin_cron.rerun.started', { name: l.jobName }))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onExport = async () => {
    try {
      // 统一走异步任务：提交后由 useAsyncJob 提示「已提交到导出中心」，
      // 进度与下载都在导出中心看；创建失败的具体原因由请求层弹过，不再叠加。
      await startExportJob(() =>
        createLogExportJob({ jobId: jobIds?.length ? jobIds.join(',') : undefined, status: applied?.length ? applied.join(',') : undefined }),
      )
    } catch {
      // 具体原因（筛选太宽等）请求层已经按服务端文案弹过了，这里不再叠一条更空的
    }
  }

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
          onJobsChange(undefined)
          setPage(1)
        }}
      >
        <OptionPicker
          title={t('plugin_cron.fields.job')}
          value={jobIds ?? []}
          onChange={onJobsChange}
          options={(jobs?.list ?? []).map((j) => ({ value: j.id, label: j.name }))}
          className="md:w-56"
          placeholder={t('plugin_cron.fields.job')}
        />
        <OptionPicker
          title={t('plugin_cron.fields.status')}
          value={status ?? []}
          onChange={(v) => setStatus(v.length ? v : undefined)}
          options={STATUSES.map((s) => ({ value: s, label: t(`plugin_cron.status.${s}`) }))}
          className="md:w-44"
          placeholder={t('plugin_cron.fields.status')}
        />
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
          <>
            <label className="flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground">
              <Switch checked={autoRefresh} onCheckedChange={setAutoRefresh} aria-label={t('plugin_cron.autoRefresh')} />
              {t('plugin_cron.autoRefresh')}
            </label>
            <Can perm="cron:log">
              <Button variant="outline" onClick={() => void onExport()}>
                <Download aria-hidden="true" />
                {t('plugin_cron.btn.export')}
              </Button>
            </Can>
            <Can perm="cron:clean">
              <Button variant="outline" onClick={() => setCleanOpen(true)}>
                <Eraser aria-hidden="true" />
                {t('plugin_cron.btn.clean')}
              </Button>
            </Can>
          </>
        }
        rowActions={(l) => (
          <>
            {l.status === 'failed' ? (
              <Can perm="cron:run">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={t('plugin_cron.rerun.action')}
                  disabled={runJob.isPending}
                  onClick={() => void onRerun(l)}
                >
                  <RotateCcw className="size-4" aria-hidden="true" />
                </Button>
              </Can>
            ) : null}
            <DetailAction onClick={() => setDetail(l.id)} />
          </>
        )}
      />

      <LogDetailDialog id={detail} onClose={() => setDetail(null)} />
      <CleanDialog open={cleanOpen} onOpenChange={setCleanOpen} jobId={jobIds?.length ? jobIds.join(',') : undefined} />
    </div>
  )
}
