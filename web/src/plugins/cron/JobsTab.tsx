import { useState } from 'react'
import { Activity, Pencil, Play, Plus, ScrollText, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import { isJsonObjectText } from '@/framework/components/json'
import { cn } from '@/lib/utils'
import {
  useDeleteJob,
  useDeleteJobs,
  useHandlers,
  useJobs,
  useLogs,
  useRunJob,
  useSetEnabled,
  useSetJobsEnabled,
  type CronJob,
  type JobQuery,
} from './api'
// useRunJob 在 RunDialog 里调用（带参数覆盖）；本组件不再直接触发执行
import { formatTime, statusVariant } from './format'
import { JobFormDialog } from './JobFormDialog'

const ALL = '__all'

interface Props {
  onViewLogs: (job: CronJob) => void
}

export function JobsTab({ onViewLogs }: Props) {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<JobQuery>({})
  const [filters, setFilters] = useState<JobQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<CronJob | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  // 自动刷新：运行中 3s 紧盯、平时 10s 慢刷；可关——挂大屏做纯展示时不想让它一直打接口
  const [autoRefresh, setAutoRefresh] = useState(true)
  // 立即执行对话框：可带本次执行的临时参数覆盖；健康视图对话框：近 20 次执行一览
  const [runTarget, setRunTarget] = useState<CronJob | null>(null)
  const [healthTarget, setHealthTarget] = useState<CronJob | null>(null)

  const { data, isLoading } = useJobs({ ...filters, page, pageSize }, autoRefresh)
  const { data: handlers = [] } = useHandlers()
  const setEnabled = useSetEnabled()
  const setEnabledBatch = useSetJobsEnabled()
  const remove = useDeleteJob()
  const removeBatch = useDeleteJobs()

  const handlerLabel = (name: string) => t(`plugin_cron.handlers.${name}`, { defaultValue: name })

  const openForm = (j: CronJob | null) => {
    setEditing(j)
    setFormOpen(true)
  }
  const attempt = async (fn: () => Promise<unknown>, okMsg = t('common.operateSuccess')) => {
    try {
      await fn()
      toast.success(okMsg)
      return true
    } catch {
      toast.error(t('common.operateFailed'))
      return false
    }
  }
  const onDelete = async (j: CronJob) => {
    if (await confirm({ destructive: true, description: t('plugin_cron.deleteConfirm', { name: j.name }) })) {
      if (await attempt(() => remove.mutateAsync(j.id))) setSelected((s) => s.filter((id) => id !== j.id))
    }
  }
  // 批量删：一次提交点名的 id；后端只删当前租户下确实存在的行，deleted 是实删条数。
  // 正在运行的那次执行不受影响（后端不中断它），执行日志保留——所以据实提示，不写成「全部删除成功」。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('plugin_cron.deleteBatchConfirm', { count: ids.length }) }))) return
    try {
      const { deleted } = await removeBatch.mutateAsync(ids)
      setSelected([])
      toast.success(deleted < ids.length ? t('plugin_cron.deletedPartial', { deleted, total: ids.length }) : t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  // 批量启停：updated 是实改条数（越权或已删除的 id 不计入）。
  const onSetEnabledBatch = async (ids: string[], enabled: boolean) => {
    try {
      const { updated } = await setEnabledBatch.mutateAsync({ ids, enabled })
      setSelected([])
      toast.success(t('plugin_cron.batch.updated', { count: updated }))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<CronJob>[] = [
    { key: 'name', title: t('plugin_cron.fields.name'), mobile: 'title', render: (j) => <span className="font-medium text-foreground">{j.name}</span> },
    { key: 'cronExpr', title: t('plugin_cron.fields.cronExpr'), mobile: 'sub', render: (j) => <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{j.cronExpr}</code> },
    { key: 'handler', title: t('plugin_cron.fields.handler'), render: (j) => <Badge variant="outline">{handlerLabel(j.handler)}</Badge> },
    {
      key: 'enabled',
      title: t('plugin_cron.fields.enabled'),
      render: (j) => (
        <Can perm="cron:edit" fallback={<span>{j.enabled ? t('plugin_cron.enabledOn') : t('plugin_cron.enabledOff')}</span>}>
          <Switch
            checked={j.enabled}
            aria-label={t('plugin_cron.fields.enabled')}
            onCheckedChange={(v) => void attempt(() => setEnabled.mutateAsync({ id: j.id, enabled: v }))}
          />
        </Can>
      ),
    },
    {
      key: 'nextRunAt',
      title: t('plugin_cron.fields.nextRunAt'),
      render: (j) => (j.nextRunAt ? <span className="num">{formatTime(j.nextRunAt, i18n.language)}</span> : <span className="text-muted-foreground">-</span>),
    },
    {
      key: 'lastRun',
      title: t('plugin_cron.fields.lastRun'),
      render: (j) =>
        j.lastRunAt ? (
          <div className="flex flex-wrap items-center gap-2">
            {j.lastStatus ? (
              <button
                type="button"
                className="cursor-pointer"
                title={t('plugin_cron.health.open')}
                aria-label={t('plugin_cron.health.open')}
                onClick={() => setHealthTarget(j)}
              >
                <Badge variant={statusVariant(j.lastStatus)}>{t(`plugin_cron.status.${j.lastStatus}`)}</Badge>
              </button>
            ) : null}
            <span className="num text-xs text-muted-foreground">{formatTime(j.lastRunAt, i18n.language)}</span>
          </div>
        ) : (
          <span className="text-muted-foreground">-</span>
        ),
    },
    { key: 'retryCount', title: t('plugin_cron.fields.retryCount'), align: 'end', render: (j) => <span className="num">{j.retryCount}</span> },
  ]

  return (
    <div className="flex flex-col gap-4">
      <FilterBar
        onSearch={() => {
          setFilters(draft)
          setPage(1)
        }}
        onReset={() => {
          setDraft({})
          setFilters({})
          setPage(1)
        }}
      >
        <Input
          className="md:w-56"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_cron.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select value={draft.handler ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, handler: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-44" aria-label={t('plugin_cron.fields.handler')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_cron.filter.allHandlers')}</SelectItem>
            {handlers.map((h) => (
              <SelectItem key={h.name} value={h.name}>
                {handlerLabel(h.name)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={draft.enabled ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, enabled: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-40" aria-label={t('plugin_cron.fields.enabled')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_cron.filter.allStates')}</SelectItem>
            <SelectItem value="1">{t('plugin_cron.enabledOn')}</SelectItem>
            <SelectItem value="0">{t('plugin_cron.enabledOff')}</SelectItem>
          </SelectContent>
        </Select>
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-cron-jobs"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_cron.empty')}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="cron:edit">
            <Button variant="outline" size="sm" disabled={setEnabledBatch.isPending} onClick={() => void onSetEnabledBatch(ids, true)}>
              {t('plugin_cron.batch.enable')}
            </Button>
            <Button variant="outline" size="sm" disabled={setEnabledBatch.isPending} onClick={() => void onSetEnabledBatch(ids, false)}>
              {t('plugin_cron.batch.disable')}
            </Button>
            <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        toolbar={
          <>
            <label className="flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground">
              <Switch checked={autoRefresh} onCheckedChange={setAutoRefresh} aria-label={t('plugin_cron.autoRefresh')} />
              {t('plugin_cron.autoRefresh')}
            </label>
            <Can perm="cron:edit">
              <Button onClick={() => openForm(null)}>
                <Plus aria-hidden="true" />
                {t('plugin_cron.create')}
              </Button>
            </Can>
          </>
        }
        rowActions={(j) => (
          <>
            <Can perm="cron:log">
              <Button variant="ghost" size="icon" aria-label={t('plugin_cron.health.open')} title={t('plugin_cron.health.open')} onClick={() => setHealthTarget(j)}>
                <Activity aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="cron:run">
              <Button variant="ghost" size="icon" aria-label={t('plugin_cron.run')} onClick={() => setRunTarget(j)}>
                <Play aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="cron:log">
              <Button variant="ghost" size="icon" aria-label={t('plugin_cron.viewLogs')} onClick={() => onViewLogs(j)}>
                <ScrollText aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="cron:edit">
              <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(j)}>
                <Pencil aria-hidden="true" />
              </Button>
              <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(j)}>
                <Trash2 aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />

      <JobFormDialog open={formOpen} onOpenChange={setFormOpen} editing={editing} />
      {runTarget ? <RunDialog job={runTarget} onClose={() => setRunTarget(null)} /> : null}
      {healthTarget ? <HealthDialog job={healthTarget} onClose={() => setHealthTarget(null)} /> : null}
    </div>
  )
}

// RunDialog 是立即执行的确认 + 可选参数覆盖：留空用任务保存的参数，填写则只对本次生效。
function RunDialog({ job, onClose }: { job: CronJob; onClose: () => void }) {
  const { t } = useTranslation()
  const run = useRunJob()
  const [params, setParams] = useState('')
  const bad = params.trim() !== '' && !isJsonObjectText(params)
  const submit = async () => {
    if (bad) {
      toast.error(t('plugin_cron.runOnce.invalidJson'))
      return
    }
    try {
      await run.mutateAsync({ id: job.id, params: params.trim() || undefined })
      toast.success(t('plugin_cron.runStarted'))
      onClose()
    } catch {
      // 具体原因（正在运行、参数不合法）请求层已按服务端文案弹出
    }
  }
  return (
    <ResponsiveDialog
      open
      onOpenChange={(o) => !o && onClose()}
      size="sm"
      title={t('plugin_cron.run')}
      description={t('plugin_cron.runConfirm', { name: job.name })}
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button disabled={run.isPending} onClick={() => void submit()}>
            {t('plugin_cron.run')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-2">
        <Label htmlFor="cron-run-params">{t('plugin_cron.runOnce.paramsLabel')}</Label>
        <Textarea
          id="cron-run-params"
          rows={4}
          className="font-mono"
          value={params}
          spellCheck={false}
          onChange={(e) => setParams(e.target.value)}
          placeholder={job.params || '{}'}
          aria-invalid={bad}
        />
        <p className="text-xs text-muted-foreground">{t('plugin_cron.runOnce.paramsHint')}</p>
        {bad ? <p className="text-xs text-destructive">{t('plugin_cron.runOnce.invalidJson')}</p> : null}
      </div>
    </ResponsiveDialog>
  )
}

// HealthDialog 是任务级健康视图：近 20 次执行的状态点带 + 成功率/平均耗时汇总。
// 之前只能跳日志 tab 自己筛——任务一多，看某台任务最近稳不稳就是最常用的动作。
function HealthDialog({ job, onClose }: { job: CronJob; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const { data, isLoading } = useLogs({ jobId: job.id, page: 1, pageSize: 20 })
  const rows = data?.list ?? []
  const done = rows.filter((l) => l.status !== 'running')
  const okCount = done.filter((l) => l.status === 'success').length
  const rate = done.length ? Math.round((okCount / done.length) * 100) : null
  const avgCost = done.length ? Math.round(done.reduce((s, l) => s + l.costMs, 0) / done.length) : null
  return (
    <ResponsiveDialog
      open
      onOpenChange={(o) => !o && onClose()}
      size="md"
      title={t('plugin_cron.health.title', { name: job.name })}
      description={
        done.length
          ? `${t('plugin_cron.health.successRate')}: ${rate}% · ${t('plugin_cron.health.avgCost')}: ${t('plugin_cron.ms', { value: avgCost ?? 0 })}`
          : undefined
      }
      footer={
        <Button variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {isLoading ? (
        <p className="py-6 text-center text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : rows.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">{t('plugin_cron.health.empty')}</p>
      ) : (
        <div className="flex flex-col gap-3">
          {/* 状态点带：一眼看出最近稳不稳，悬停看具体时间 */}
          <div className="flex flex-wrap items-center gap-1">
            {[...rows].reverse().map((l) => (
              <span
                key={l.id}
                title={`${formatTime(l.startedAt, i18n.language)} · ${t(`plugin_cron.status.${l.status}`)}`}
                className={cn(
                  'h-4 w-2 rounded-sm',
                  l.status === 'success' ? 'bg-success' : l.status === 'failed' ? 'bg-destructive' : 'animate-pulse bg-warning',
                )}
              />
            ))}
          </div>
          <div className="divide-y divide-border rounded-lg border border-border">
            {rows.map((l) => (
              <div key={l.id} className="flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
                <Badge variant={statusVariant(l.status)}>{t(`plugin_cron.status.${l.status}`)}</Badge>
                <span className="text-xs text-muted-foreground">{l.trigger === 'manual' ? t('plugin_cron.trigger.manual') : t('plugin_cron.trigger.schedule')}</span>
                {l.status !== 'running' ? <span className="num text-xs text-muted-foreground">{t('plugin_cron.ms', { value: l.costMs })}</span> : null}
                {l.attempts > 1 ? <span className="num text-xs text-warning">×{l.attempts}</span> : null}
                {l.error ? <span className="min-w-0 flex-1 truncate text-xs text-destructive" title={l.error}>{l.error}</span> : null}
                <span className="num ms-auto text-xs text-muted-foreground">{formatTime(l.startedAt, i18n.language)}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </ResponsiveDialog>
  )
}
