import { useState } from 'react'
import { CalendarClock, CircleAlert, DatabaseBackup, Download, HardDrive, Loader2, Settings, Trash2, Wrench } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { StatCard } from '@/framework/components/StatCard'
import { usePageLive, useRefreshOnResume } from '@/framework/hooks/use-page-active'
import { Alert, AlertDescription, AlertTitle } from '@/framework/ui/alert'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { formatSize } from '@/framework/upload/precheck'
import { BackupSettingsDialog } from './BackupSettingsDialog'
import {
  downloadBackup,
  useBackupConfig,
  useBackupList,
  useBackupStatus,
  useCreateBackup,
  useDeleteBackup,
  type BackupRecord,
} from './api'

const errMsg = (e: unknown, fallback: string) => (e instanceof Error && e.message ? e.message : fallback)

function saveBlob(filename: string, blob: Blob) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

export default function BackupPage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const live = usePageLive()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [downloading, setDownloading] = useState<string | null>(null)

  // 有备份在进行时才轮询；页面不可见（切走标签 / 浏览器后台）时暂停，回来时立即刷新一次
  const status = useBackupStatus(live)
  const list = useBackupList(page, pageSize, live)
  const config = useBackupConfig()
  const create = useCreateBackup()
  const del = useDeleteBackup()
  useRefreshOnResume(live, () => Promise.all([list.refetch(), status.refetch()]))

  const st = status.data
  const rows = list.data?.list ?? []
  const hasRunning = (st?.running ?? false) || rows.some((r) => r.status === 'running')
  const fmtTime = (s: string) => new Date(s).toLocaleString(i18n.language, { hour12: false })

  const backupNow = async () => {
    try {
      await create.mutateAsync()
      void status.refetch()
      toast.success(t('backup.started'))
    } catch {
      // 错误 toast（含“本机没有 mysqldump”的原因）由 http 拦截器弹出
    }
  }

  const download = async (r: BackupRecord) => {
    setDownloading(r.id)
    try {
      saveBlob(r.fileName, await downloadBackup(r.id))
    } catch (e) {
      toast.error(errMsg(e, t('common.operateFailed')))
    } finally {
      setDownloading(null)
    }
  }

  const remove = async (r: BackupRecord) => {
    if (!(await confirm({ description: t('backup.deleteConfirm', { name: r.fileName }), destructive: true }))) return
    try {
      await del.mutateAsync(r.id)
      toast.success(t('common.operateSuccess'))
    } catch {
      // 错误 toast 由 http 拦截器弹出
    }
  }

  const statusBadge = (r: BackupRecord) => {
    if (r.status === 'running')
      return (
        <Badge variant="info" className="gap-1">
          <Loader2 className="size-3 animate-spin motion-reduce:animate-none" aria-hidden="true" />
          {t('backup.status.running')}
        </Badge>
      )
    if (r.status === 'failed')
      return (
        <Badge variant="destructive" title={r.error}>
          {t('backup.status.failed')}
        </Badge>
      )
    return <Badge variant="success">{t('backup.status.success')}</Badge>
  }

  const columns: Column<BackupRecord>[] = [
    {
      key: 'createdAt',
      title: t('backup.col.time'),
      mobile: 'title',
      render: (r) => (
        <div className="min-w-0">
          <span className="num">{fmtTime(r.createdAt)}</span>
          {r.status === 'failed' && r.error ? <p className="max-w-xs truncate text-xs text-destructive">{r.error}</p> : null}
        </div>
      ),
    },
    { key: 'size', title: t('backup.col.size'), align: 'end', render: (r) => <span className="num">{r.status === 'success' ? formatSize(r.size) : '-'}</span> },
    { key: 'source', title: t('backup.col.source'), mobile: 'sub', render: (r) => t(`backup.source.${r.source}`) },
    { key: 'status', title: t('backup.col.status'), render: statusBadge },
    {
      key: 'sha256',
      title: t('backup.col.sha256'),
      render: (r) => (r.sha256 ? <code className="num text-xs" title={r.sha256}>{r.sha256.slice(0, 12)}</code> : '-'),
    },
  ]

  const restoreHelp = (
    <div className="mx-auto max-w-xl space-y-2 text-start">
      <p className="text-center text-sm font-medium">{t('backup.empty.title')}</p>
      <p className="text-center text-sm text-muted-foreground">{t('backup.empty.restoreHint')}</p>
      <pre className="overflow-x-auto rounded-md border border-border bg-muted/50 p-3 text-xs">
        <code>{'nexadmin-server stop\nnexadmin-server restore ./backup/<file>.sql.gz\nnexadmin-server start'}</code>
      </pre>
    </div>
  )

  return (
    <PageContainer>
      <PageHeader
        title={t('backup.title')}
        description={t('backup.description')}
        actions={
          <Can perm="backup:edit">
            <Button variant="outline" onClick={() => setSettingsOpen(true)}>
              <Settings aria-hidden="true" />
              {t('backup.settings.open')}
            </Button>
            <Button onClick={() => void backupNow()} disabled={create.isPending || hasRunning || !st?.available}>
              {hasRunning ? <Loader2 className="animate-spin motion-reduce:animate-none" aria-hidden="true" /> : <DatabaseBackup aria-hidden="true" />}
              {hasRunning ? t('backup.running') : t('backup.now')}
            </Button>
          </Can>
        }
      />

      {st && !st.available ? (
        <Alert variant="destructive">
          <CircleAlert className="size-4" aria-hidden="true" />
          <AlertTitle>{t('backup.unavailable.title')}</AlertTitle>
          <AlertDescription>{st.reason}</AlertDescription>
        </Alert>
      ) : null}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 md:gap-4 xl:grid-cols-4">
        {!st ? (
          Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-32 w-full" />)
        ) : (
          <>
            <StatCard
              title={t('backup.card.tool')}
              value={st.available ? (st.tool ?? '-') : t('backup.card.toolMissing')}
              hint={`${st.engine} · v${st.version}`}
              icon={<Wrench aria-hidden="true" />}
            />
            <StatCard
              title={t('backup.card.disk')}
              value={st.diskFreeBytes == null ? t('backup.card.unknown') : formatSize(st.diskFreeBytes)}
              hint={st.dir}
              icon={<HardDrive aria-hidden="true" />}
            />
            <StatCard
              title={t('backup.card.schedule')}
              value={st.schedule ? t('backup.settings.daily', { time: st.schedule }) : t('backup.settings.scheduleOff')}
              hint={st.nextRunAt ? t('backup.card.next', { time: fmtTime(st.nextRunAt) }) : undefined}
              icon={<CalendarClock aria-hidden="true" />}
            />
            <StatCard
              title={t('backup.card.last')}
              value={st.lastSuccessAt ? fmtTime(st.lastSuccessAt) : t('backup.card.never')}
              hint={config.data ? t('backup.card.keep', { days: config.data.keepDays || '∞', copies: config.data.keepCount || '∞' }) : undefined}
              icon={<DatabaseBackup aria-hidden="true" />}
            />
          </>
        )}
      </div>

      <DataTable<BackupRecord>
        columns={columns}
        data={rows}
        rowKey="id"
        loading={list.isLoading}
        emptyText={restoreHelp}
        pagination={{
          page,
          pageSize,
          total: list.data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        rowActions={(r) => (
          <>
            {r.status === 'success' ? (
              <Can perm="backup:download">
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t('common.download')}
                  disabled={downloading === r.id}
                  onClick={() => void download(r)}
                >
                  {downloading === r.id ? <Loader2 className="animate-spin motion-reduce:animate-none" aria-hidden="true" /> : <Download aria-hidden="true" />}
                </Button>
              </Can>
            ) : null}
            {r.status !== 'running' ? (
              <Can perm="backup:edit">
                <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void remove(r)}>
                  <Trash2 aria-hidden="true" />
                </Button>
              </Can>
            ) : null}
          </>
        )}
      />

      <BackupSettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} value={config.data} />
    </PageContainer>
  )
}
