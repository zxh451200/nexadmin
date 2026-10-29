import { useState } from 'react'
import { Ban, Download, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import type { Column } from '@/framework/components/table/types'
import { usePageLive, useRefreshOnResume } from '@/framework/hooks/use-page-active'
import { Badge, type BadgeProps } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Progress } from '@/framework/ui/progress'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { fmtDateTime } from '../oper-log/format'
import {
  downloadJobFile,
  isActive,
  JOB_STATUSES,
  JOB_TYPES,
  useCancelJob,
  useDeleteJob,
  useJobList,
  useRetryJob,
  type Job,
  type JobStatus,
} from './api'

const ALL = 'all'
const POLL_MS = 2000
const STATUS_VARIANT: Record<JobStatus, BadgeProps['variant']> = {
  queued: 'secondary',
  running: 'info',
  success: 'success',
  failed: 'destructive',
  canceled: 'outline',
}

const errMsg = (e: unknown, fallback: string) => (e instanceof Error && e.message ? e.message : fallback)

export default function ExportCenterPage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const live = usePageLive()
  const [type, setType] = useState(ALL)
  const [status, setStatus] = useState(ALL)
  const [applied, setApplied] = useState({ type: ALL, status: ALL })
  const [all, setAll] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [busy, setBusy] = useState<string | null>(null)

  // 有进行中的任务且页面可见时才轮询；切到别的标签或浏览器标签页隐藏时暂停，恢复时立即刷新一次
  const { data, isLoading, refetch } = useJobList(
    {
      page,
      pageSize,
      type: applied.type === ALL ? undefined : applied.type,
      status: applied.status === ALL ? undefined : applied.status,
      all,
    },
    (q) => (live && q.state.data?.list.some((j) => isActive(j.status)) ? POLL_MS : false),
  )
  useRefreshOnResume(live, refetch)
  const cancel = useCancelJob()
  const retry = useRetryJob()
  const del = useDeleteJob()

  const run = async (id: string, fn: () => Promise<unknown>, okKey?: string) => {
    setBusy(id)
    try {
      await fn()
      if (okKey) toast.success(t(okKey))
    } catch (e) {
      toast.error(errMsg(e, t('common.operateFailed')))
    } finally {
      setBusy(null)
    }
  }

  const onCancel = async (j: Job) => {
    if (!(await confirm({ description: t('jobs.confirm.cancel') }))) return
    await run(j.id, () => cancel.mutateAsync(j.id), 'jobs.toast.canceled')
  }
  const onDelete = async (j: Job) => {
    if (!(await confirm({ destructive: true, description: t('jobs.confirm.delete') }))) return
    await run(j.id, () => del.mutateAsync(j.id), 'jobs.toast.deleted')
  }

  const typeLabel = (v: string) => t(`jobs.type.${v}`, { defaultValue: v })
  const duration = (j: Job) => {
    if (!j.startedAt) return '-'
    const end = j.finishedAt ? new Date(j.finishedAt).getTime() : Date.now()
    const sec = Math.max(0, Math.round((end - new Date(j.startedAt).getTime()) / 1000))
    return sec < 60 ? t('jobs.duration.sec', { n: sec }) : t('jobs.duration.min', { m: Math.floor(sec / 60), s: sec % 60 })
  }

  const columns: Column<Job>[] = [
    { key: 'type', title: t('jobs.col.type'), mobile: 'title', render: (j) => typeLabel(j.type) },
    {
      key: 'status',
      title: t('jobs.col.status'),
      mobile: 'sub',
      render: (j) => (
        <div className="flex flex-col items-start gap-1">
          <Badge variant={STATUS_VARIANT[j.status]}>{t(`jobs.status.${j.status}`)}</Badge>
          {j.status === 'failed' && j.error ? <span className="max-w-64 text-xs text-destructive">{j.error}</span> : null}
        </div>
      ),
    },
    {
      key: 'progress',
      title: t('jobs.col.progress'),
      width: 200,
      render: (j) => (
        <div className="flex min-w-32 flex-col gap-1">
          <div className="flex items-center gap-2">
            <Progress value={j.progress} aria-label={t('jobs.col.progress')} className="h-1.5" />
            <span className="num w-10 shrink-0 text-end text-xs text-muted-foreground">{j.progress}%</span>
          </div>
          {isActive(j.status) && j.message ? <span className="truncate text-xs text-muted-foreground">{j.message}</span> : null}
        </div>
      ),
    },
    ...(all ? [{ key: 'ownerName', title: t('jobs.col.owner') } satisfies Column<Job>] : []),
    {
      key: 'createdAt',
      title: t('jobs.col.createdAt'),
      render: (j) => <span className="num">{fmtDateTime(j.createdAt, i18n.language)}</span>,
    },
    { key: 'duration', title: t('jobs.col.duration'), render: (j) => <span className="num">{duration(j)}</span> },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('jobs.menu')} description={t('jobs.description')} />
      <FilterBar
        onSearch={() => {
          setApplied({ type, status })
          setPage(1)
        }}
        onReset={() => {
          setType(ALL)
          setStatus(ALL)
          setApplied({ type: ALL, status: ALL })
          setPage(1)
        }}
      >
        <Select value={type} onValueChange={setType}>
          <SelectTrigger id="job-type" className="md:w-48" aria-label={t('jobs.filter.type')}>
            <SelectValue placeholder={t('jobs.filter.type')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('jobs.type.all')}</SelectItem>
            {JOB_TYPES.map((k) => (
              <SelectItem key={k} value={k}>
                {typeLabel(k)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={status} onValueChange={setStatus}>
          <SelectTrigger id="job-status" className="md:w-48" aria-label={t('jobs.filter.status')}>
            <SelectValue placeholder={t('jobs.filter.status')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('jobs.status.all')}</SelectItem>
            {JOB_STATUSES.map((k) => (
              <SelectItem key={k} value={k}>
                {t(`jobs.status.${k}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FilterBar>
      <DataTable<Job>
        toolbar={
          <>
            <Can perm="job:all">
              <div className="inline-flex rounded-md border p-0.5" role="group" aria-label={t('jobs.col.owner')}>
                {[false, true].map((v) => (
                  <Button
                    key={String(v)}
                    type="button"
                    size="sm"
                    variant={all === v ? 'default' : 'ghost'}
                    aria-pressed={all === v}
                    onClick={() => {
                      setAll(v)
                      setPage(1)
                    }}
                  >
                    {t(v ? 'jobs.scope.all' : 'jobs.scope.mine')}
                  </Button>
                ))}
              </div>
            </Can>
            <Button type="button" variant="outline" onClick={() => void refetch()}>
              <RefreshCw className="me-2 size-4" aria-hidden="true" />
              {t('jobs.action.refresh')}
            </Button>
          </>
        }
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="jobs"
        rowActions={(j) => (
          <div className="flex items-center">
            {j.downloadable ? (
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('jobs.action.download')}
                disabled={busy === j.id}
                onClick={() => run(j.id, () => downloadJobFile(j))}
              >
                <Download className="size-4" aria-hidden="true" />
              </Button>
            ) : null}
            {isActive(j.status) ? (
              <Button type="button" variant="ghost" size="icon" aria-label={t('jobs.action.cancel')} disabled={busy === j.id} onClick={() => onCancel(j)}>
                <Ban className="size-4" aria-hidden="true" />
              </Button>
            ) : null}
            {j.status === 'failed' ? (
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('jobs.action.retry')}
                disabled={busy === j.id}
                onClick={() => run(j.id, () => retry.mutateAsync(j.id), 'jobs.toast.retried')}
              >
                <RotateCcw className="size-4" aria-hidden="true" />
              </Button>
            ) : null}
            {!isActive(j.status) ? (
              <Button type="button" variant="ghost" size="icon" aria-label={t('jobs.action.delete')} disabled={busy === j.id} onClick={() => onDelete(j)}>
                <Trash2 className="size-4 text-destructive" aria-hidden="true" />
              </Button>
            ) : null}
          </div>
        )}
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
      />
    </PageContainer>
  )
}
