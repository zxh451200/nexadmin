import { useState } from 'react'
import { Pencil, Play, Plus, ScrollText, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { useDeleteJob, useHandlers, useJobs, useRunJob, useSetEnabled, type CronJob, type JobQuery } from './api'
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

  const { data, isLoading } = useJobs({ ...filters, page, pageSize })
  const { data: handlers = [] } = useHandlers()
  const setEnabled = useSetEnabled()
  const remove = useDeleteJob()
  const run = useRunJob()

  const handlerLabel = (name: string) => t(`plugin_cron.handlers.${name}`, { defaultValue: name })

  const openForm = (j: CronJob | null) => {
    setEditing(j)
    setFormOpen(true)
  }
  const attempt = async (fn: () => Promise<unknown>, okMsg = t('common.operateSuccess')) => {
    try {
      await fn()
      toast.success(okMsg)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  const onRun = async (j: CronJob) => {
    if (await confirm({ title: t('plugin_cron.run'), description: t('plugin_cron.runConfirm', { name: j.name }) })) {
      await attempt(() => run.mutateAsync(j.id), t('plugin_cron.runStarted'))
    }
  }
  const onDelete = async (j: CronJob) => {
    if (await confirm({ destructive: true, description: t('plugin_cron.deleteConfirm', { name: j.name }) })) {
      await attempt(() => remove.mutateAsync(j.id))
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
            {j.lastStatus ? <Badge variant={statusVariant(j.lastStatus)}>{t(`plugin_cron.status.${j.lastStatus}`)}</Badge> : null}
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
            <SelectItem value={ALL}>{t('plugin_cron.fields.handler')}: {t('plugin_cron.all')}</SelectItem>
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
            <SelectItem value={ALL}>{t('plugin_cron.fields.enabled')}: {t('plugin_cron.all')}</SelectItem>
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
        toolbar={
          <Can perm="cron:edit">
            <Button onClick={() => openForm(null)}>
              <Plus aria-hidden="true" />
              {t('plugin_cron.create')}
            </Button>
          </Can>
        }
        rowActions={(j) => (
          <>
            <Can perm="cron:run">
              <Button variant="ghost" size="icon" aria-label={t('plugin_cron.run')} onClick={() => void onRun(j)}>
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
    </div>
  )
}
