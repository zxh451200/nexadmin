import { useState } from 'react'
import { Ban, Download, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Segmented } from '@/framework/components/Segmented'
import type { Column } from '@/framework/components/table/types'
import { usePageLive, useRefreshOnResume } from '@/framework/hooks/use-page-active'
import { Badge, type BadgeProps } from '@/framework/ui/badge'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/framework/ui/tooltip'
import { Button } from '@/framework/ui/button'
import { Progress } from '@/framework/ui/progress'
import { OptionPicker } from '@/framework/components/OptionPicker'
import { cn } from '@/lib/utils'
import { fmtDateTime } from '../oper-log/format'
import {
  downloadJobFile,
  downloadJobFiles,
  isActive,
  JOB_STATUSES,
  JOB_TYPES,
  useCancelJob,
  useDeleteJob,
  useDeleteJobs,
  useJobList,
  useRetryJob,
  type Job,
  type JobStatus,
} from './api'

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
  const [type, setType] = useState<string[]>([])
  const [status, setStatus] = useState<string[]>([])
  const [applied, setApplied] = useState<{ type?: string; status?: string }>({})
  const [all, setAll] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [busy, setBusy] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [selected, setSelected] = useState<string[]>([])

  // 有进行中的任务且页面可见时才轮询；切到别的标签或浏览器标签页隐藏时暂停，恢复时立即刷新一次
  const { data, isLoading, refetch } = useJobList(
    {
      page,
      pageSize,
      type: applied.type,
      status: applied.status,
      all,
    },
    (q) => (live && q.state.data?.list.some((j) => isActive(j.status)) ? POLL_MS : false),
  )
  useRefreshOnResume(live, refetch)
  const cancel = useCancelJob()
  const retry = useRetryJob()
  const del = useDeleteJob()
  const delBatch = useDeleteJobs()

  const run = async (id: string, fn: () => Promise<unknown>, okKey?: string): Promise<boolean> => {
    setBusy(id)
    try {
      await fn()
      if (okKey) toast.success(t(okKey))
      return true
    } catch (e) {
      toast.error(errMsg(e, t('common.operateFailed')))
      return false
    } finally {
      setBusy(null)
    }
  }

  // 仅手动刷新时转圈；自动轮询每 2 秒一次，跟着转会一直抖
  const onRefresh = async () => {
    setRefreshing(true)
    try {
      await refetch()
    } finally {
      setRefreshing(false)
    }
  }

  const onCancel = async (j: Job) => {
    if (!(await confirm({ description: t('jobs.confirm.cancel') }))) return
    await run(j.id, () => cancel.mutateAsync(j.id), 'jobs.toast.canceled')
  }
  const onDelete = async (j: Job) => {
    if (!(await confirm({ destructive: true, description: t('jobs.confirm.delete') }))) return
    if (await run(j.id, () => del.mutateAsync(j.id), 'jobs.toast.deleted')) {
      setSelected((s) => s.filter((x) => x !== j.id))
    }
  }

  // 批量删除：作用域（非管理员只看得到自己的任务）与判据（有未结束的任务整批 409 拒绝、
  // 删任务连带删结果文件）都收在后端，这里只按后端返回的实删条数如实提示。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('jobs.confirm.deleteBatch', { count: ids.length }) }))) return
    try {
      const { deleted } = await delBatch.mutateAsync(ids)
      setSelected([])
      toast.success(deleted < ids.length ? t('jobs.toast.deletedPartial', { deleted, total: ids.length }) : t('jobs.toast.deleted'))
    } catch (e) {
      // 具体原因（如「有任务还没结束」）由 http 拦截器弹出，这里补一条通用失败提示
      toast.error(errMsg(e, t('common.operateFailed')))
    }
  }

  // 批量下载：只挑选中里可下载的任务（已完成且有结果文件），后端再逐条校验所有者，
  // 打包成一个 zip 返回；一条都不可下载时不发请求，直接提示。
  const onDownloadBatch = async (ids: string[]) => {
    const targets = (data?.list ?? []).filter((j) => ids.includes(j.id) && j.downloadable).map((j) => j.id)
    if (!targets.length) {
      toast.info(t('jobs.toast.noneDownloadable'))
      return
    }
    setBusy('batch')
    try {
      await downloadJobFiles(targets)
      toast.success(t('jobs.toast.downloadedBatch', { n: targets.length }))
    } catch (e) {
      // 具体原因（如所有者校验失败）由 http 拦截器弹出，这里补一条通用失败提示
      toast.error(errMsg(e, t('common.operateFailed')))
    } finally {
      setBusy(null)
    }
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
      render: (j) => {
        const badge = <Badge variant={STATUS_VARIANT[j.status]}>{t(`jobs.status.${j.status}`)}</Badge>
        // 失败原因收进悬浮提示，避免徽章下再压一行红字把行高撑开
        if (j.status !== 'failed' || !j.error) return badge
        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="inline-flex cursor-help" aria-label={j.error}>
                {badge}
              </span>
            </TooltipTrigger>
            <TooltipContent>{j.error}</TooltipContent>
          </Tooltip>
        )
      },
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
      render: (j) => (
        <div className="flex flex-col items-start gap-0.5">
          <span className="num">{fmtDateTime(j.createdAt, i18n.language)}</span>
          {/* 结果文件保留期：终态任务在创建时间下补一行小字，不挤坏表格 */}
          {!isActive(j.status) && j.expiresAt ? (
            <span className="text-xs text-muted-foreground">{t('jobs.keepUntil', { date: fmtDateTime(j.expiresAt, i18n.language) })}</span>
          ) : null}
        </div>
      ),
    },
    { key: 'duration', title: t('jobs.col.duration'), render: (j) => <span className="num">{duration(j)}</span> },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('jobs.menu')} description={t('jobs.description')} />
      <FilterBar
        onSearch={() => {
          setApplied({ type: type.length ? type.join(',') : undefined, status: status.length ? status.join(',') : undefined })
          setPage(1)
        }}
        onReset={() => {
          setType([])
          setStatus([])
          setApplied({})
          setPage(1)
        }}
      >
        <OptionPicker
          title={t('jobs.filter.type')}
          value={type}
          onChange={setType}
          options={JOB_TYPES.map((k) => ({ value: k, label: typeLabel(k) }))}
          className="md:w-48"
          placeholder={t('jobs.filter.type')}
        />
        <OptionPicker
          title={t('jobs.filter.status')}
          value={status}
          onChange={setStatus}
          options={JOB_STATUSES.map((k) => ({ value: k, label: t(`jobs.status.${k}`) }))}
          className="md:w-48"
          placeholder={t('jobs.filter.status')}
        />
      </FilterBar>
      <DataTable<Job>
        toolbar={
          <>
            <Can perm="job:all">
              <Segmented
                ariaLabel={t('jobs.col.owner')}
                value={all ? 'all' : 'mine'}
                onChange={(v) => {
                  setAll(v === 'all')
                  setPage(1)
                }}
                options={[
                  { value: 'mine', label: t('jobs.scope.mine') },
                  { value: 'all', label: t('jobs.scope.all') },
                ]}
              />
            </Can>
            <Button type="button" variant="outline" disabled={refreshing} onClick={() => void onRefresh()}>
              <RefreshCw className={cn('me-2 size-4', refreshing && 'animate-spin')} aria-hidden="true" />
              {t('jobs.action.refresh')}
            </Button>
          </>
        }
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="jobs"
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy === 'batch' || !(data?.list ?? []).some((j) => ids.includes(j.id) && j.downloadable)}
              onClick={() => void onDownloadBatch(ids)}
            >
              <Download className="me-2 size-4" aria-hidden="true" />
              {t('jobs.action.downloadBatch')}
            </Button>
            <Button variant="destructive" size="sm" disabled={delBatch.isPending} onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('jobs.action.delete')}
            </Button>
          </>
        )}
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
