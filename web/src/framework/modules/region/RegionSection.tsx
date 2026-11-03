import { useEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronDown, ChevronRight, Eye, FileUp, History, Loader2, RefreshCw, RotateCcw, Undo2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Skeleton } from '@/framework/ui/skeleton'
import {
  useImportRegionFile,
  usePreviewRegion,
  useRegionVersion,
  useRegionVersions,
  useResetRegion,
  useRegionStatus,
  useRollbackRegionVersion,
  useSyncRegion,
  useSyncRegionStatus,
  type RegionPreviewResult,
  type RegionVersion,
} from './api'

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words text-sm">{children}</dd>
    </div>
  )
}

/** 系统设置 → 省市区数据：显示当前数据版本，可从数据源重新同步或恢复内置数据 */
export function RegionSection() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data, isLoading } = useRegionStatus()
  const sync = useSyncRegion()
  const reset = useResetRegion()
  const preview = usePreviewRegion()
  const importFile = useImportRegionFile()
  const [draft, setDraft] = useState<string | null>(null)
  const [previewRes, setPreviewRes] = useState<RegionPreviewResult | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)
  const uploadRef = useRef<HTMLInputElement | null>(null)
  // 后台同步：提交后轮询 status（1.2s），结束弹出结果/错误；中途按钮显示阶段。
  const [syncing, setSyncing] = useState(false)
  const status = useSyncRegionStatus(syncing)
  const lastHandled = useRef<string>('')

  useEffect(() => {
    if (!syncing) return
    const st = status.data
    if (!st) return
    if (st.running) return
    const key = st.error ?? `${st.result?.added}-${st.result?.renamed}-${st.result?.removed}-${st.result?.info.syncedAt}`
    if (lastHandled.current === key) return
    lastHandled.current = key
    setSyncing(false)
    if (st.error) {
      toast.error(st.error, { id: 'region-sync' })
      return
    }
    if (st.result) {
      const res = st.result
      toast.success(t('region.syncDone', { added: res.added, renamed: res.renamed, removed: res.removed }))
      if (res.removed > 0) toast.warning(t('region.syncRemoved', { n: res.removed }))
    }
  }, [syncing, status.data, t])

  // 上传数据文件：先 dry-run 看差异，样本弹窗里再决定要不要应用——与 URL 同步同一套预览体验。
  const onUpload = async (file: File) => {
    try {
      setPreviewRes(await importFile.mutateAsync({ file, apply: false }))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'), { id: 'region-upload' })
    }
  }

  if (isLoading || !data) return <Skeleton className="h-64 w-full" />

  const source = draft ?? (data.sourceUrl || data.defaultSource)

  const runSync = async () => {
    try {
      // 后台任务：POST 只代表受理；进度与结果由 status 轮询（下面的 effect）接手。
      setPreviewRes(null)
      await sync.mutateAsync(source.trim())
      toast.info(t('region.syncStarted'))
    } catch (e) {
      // 固定 id：连续失败只保留一条提示，不会在右上角叠成一摞
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'), { id: 'region-sync' })
    }
  }

  // 预览：dry-run，不落库；看完差异再决定要不要同步。
  const runPreview = async () => {
    try {
      setPreviewRes(await preview.mutateAsync(source.trim()))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'), { id: 'region-preview' })
    }
  }

  const runReset = async () => {
    if (!(await confirm({ destructive: true, description: t('region.confirmReset') }))) return
    try {
      await reset.mutateAsync()
      setDraft(null)
      toast.success(t('region.resetDone'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'), { id: 'region-reset' })
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-muted-foreground">{t('region.desc')}</p>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            {t('region.title')}
            <Badge variant={data.builtin ? 'outline' : 'success'}>
              {data.builtin ? t('region.builtin') : t('region.synced')}
            </Badge>
          </CardTitle>
          <CardDescription>{t('region.cardDesc')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <dl className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
            <Row label={t('region.counts')}>
              {t('region.countsValue', { provinces: data.provinces, prefectures: data.prefectures, counties: data.counties })}
            </Row>
            <Row label={t('region.dataDate')}>{data.dataDate || '—'}</Row>
            <Row label={t('region.source')}>
              <span className="break-all font-mono text-xs">{data.sourceUrl || '—'}</span>
            </Row>
            <Row label={t('region.license')}>{data.license || '—'}</Row>
            <Row label={t('region.syncedAt')}>{data.syncedAt || '—'}</Row>
            <Row label={t('region.syncedBy')}>{data.syncedBy || '—'}</Row>
          </dl>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="region-source" className="text-sm font-medium">
              {t('region.sourceLabel')}
              <RequiredMark />
            </label>
            <Input
              id="region-source"
              value={source}
              inputMode="url"
              placeholder="https://example.com/regions.json"
              onChange={(e) => setDraft(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">{t('region.sourceHint')}</p>
          </div>
        </CardContent>
        <CardFooter className="flex flex-wrap gap-2">
          <Can perm="region:sync">
            <Button type="button" variant="outline" disabled={syncing || preview.isPending || sync.isPending || !source.trim()} onClick={() => void runPreview()}>
              {preview.isPending ? (
                <Loader2 className="me-2 size-4 animate-spin" aria-hidden="true" />
              ) : (
                <Eye className="me-2 size-4" aria-hidden="true" />
              )}
              {t('region.preview')}
            </Button>
            <Button type="button" disabled={syncing || sync.isPending || !source.trim()} onClick={() => void runSync()}>
              {syncing || sync.isPending ? (
                <Loader2 className="me-2 size-4 animate-spin" aria-hidden="true" />
              ) : (
                <RefreshCw className="me-2 size-4" aria-hidden="true" />
              )}
              {syncing
                ? t('region.syncStage', { stage: t(`region.stage.${status.data?.stage ?? 'fetching'}`) })
                : sync.isPending
                  ? t('region.submitting')
                  : t('region.sync')}
            </Button>
            <input
              ref={uploadRef}
              type="file"
              accept=".json,application/json"
              aria-label={t('region.upload')}
              className="sr-only"
              onChange={(e) => {
                const f = (e.target.files ?? [])[0]
                if (f) void onUpload(f)
                if (uploadRef.current) uploadRef.current.value = ''
              }}
            />
            <Button type="button" variant="outline" disabled={importFile.isPending} onClick={() => uploadRef.current?.click()}>
              {importFile.isPending ? (
                <Loader2 className="me-2 size-4 animate-spin" aria-hidden="true" />
              ) : (
                <FileUp className="me-2 size-4" aria-hidden="true" />
              )}
              {t('region.upload')}
            </Button>
            <Button type="button" variant="outline" disabled={reset.isPending || data.builtin} onClick={() => void runReset()}>
              <RotateCcw className="me-2 size-4" aria-hidden="true" />
              {t('region.reset')}
            </Button>
            <Button type="button" variant="outline" onClick={() => setHistoryOpen(true)}>
              <History className="me-2 size-4" aria-hidden="true" />
              {t('region.history')}
            </Button>
          </Can>
        </CardFooter>
      </Card>

      <RegionVersionsDialog open={historyOpen} onClose={() => setHistoryOpen(false)} />

      <ResponsiveDialog
        open={!!previewRes}
        onOpenChange={(o) => !o && setPreviewRes(null)}
        title={t('region.previewTitle')}
        description={t('region.previewDesc', { added: previewRes?.added ?? 0, removed: previewRes?.removed ?? 0, renamed: previewRes?.renamed ?? 0 })}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setPreviewRes(null)}>
              {t('common.cancel')}
            </Button>
            <Can perm="region:sync">
              <Button type="button" onClick={() => { setPreviewRes(null); void runSync() }}>
                <RefreshCw className="me-2 size-4" aria-hidden="true" />
                {t('region.sync')}
              </Button>
            </Can>
          </>
        }
      >
        <div className="flex flex-col gap-4 text-sm">
          <DiffList title={t('region.diff.added', { count: previewRes?.added ?? 0 })} samples={previewRes?.addedSamples} />
          <DiffList title={t('region.diff.renamed', { count: previewRes?.renamed ?? 0 })} samples={previewRes?.renamedSamples} />
          <DiffList title={t('region.diff.removed', { count: previewRes?.removed ?? 0 })} samples={previewRes?.removedSamples} />
        </div>
      </ResponsiveDialog>
    </div>
  )
}

/** 版本历史：每次覆盖保存前留下的快照，可看摘要、可回滚（回滚再走一次校验，版本继续递增） */
function RegionVersionsDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const versions = useRegionVersions(open)
  const rollback = useRollbackRegionVersion()
  // 展开才拉那一条的正文：列表里没有整棵树，也不该有。
  const [expanded, setExpanded] = useState<number | undefined>(undefined)

  const onRollback = async (v: RegionVersion) => {
    if (!(await confirm({ destructive: true, description: t('region.confirmRollback', { version: v.version }) }))) return
    try {
      const res = await rollback.mutateAsync(v.version)
      toast.success(t('region.rollbackDone', { version: v.version, added: res.added, renamed: res.renamed, removed: res.removed }))
      if (res.removed > 0) toast.warning(t('region.syncRemoved', { n: res.removed }))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'), { id: 'region-rollback' })
    }
  }

  const list = versions.data ?? []
  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      title={t('region.historyTitle')}
      description={t('region.historyDesc')}
      size="lg"
      footer={
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {versions.isLoading ? <p className="text-sm text-muted-foreground">{t('region.historyLoading')}</p> : null}
      {versions.isError ? <p className="text-sm text-destructive">{t('common.operateFailed')}</p> : null}
      {!versions.isLoading && list.length === 0 ? <EmptyState title={t('region.historyEmpty')} /> : null}
      <ul className="flex flex-col divide-y">
        {list.map((v) => (
          <li key={v.version} className="flex flex-col gap-1 py-2">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="num font-medium">v{v.version}</span>
              <span className="num text-xs text-muted-foreground">{showTime(v.createdAt, i18n.language)}</span>
              <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{v.createdBy || '—'}</span>
              <span className="text-xs text-muted-foreground">
                {t('region.countsValue', { provinces: v.provinces, prefectures: v.prefectures, counties: v.counties })}
              </span>
              <span className="font-mono text-xs text-muted-foreground">{v.sha256.slice(0, 8)}</span>
              <Button type="button" variant="ghost" size="sm" onClick={() => setExpanded(expanded === v.version ? undefined : v.version)}>
                {expanded === v.version ? (
                  <ChevronDown className="me-1 size-4" aria-hidden="true" />
                ) : (
                  <ChevronRight className="me-1 size-4" aria-hidden="true" />
                )}
                {t('region.versionSummary')}
              </Button>
              <Button type="button" variant="outline" size="sm" disabled={rollback.isPending} onClick={() => void onRollback(v)}>
                <Undo2 className="me-1 size-4" aria-hidden="true" />
                {t('region.rollbackVersion')}
              </Button>
            </div>
            {expanded === v.version ? <VersionSummary version={v.version} /> : null}
          </li>
        ))}
      </ul>
    </ResponsiveDialog>
  )
}

/** 单个版本的摘要：整棵树在这里只被读成省级名单，不原样上屏（几百 KB 没人能在弹窗里读完）。 */
function VersionSummary({ version }: { version: number }) {
  const { t } = useTranslation()
  const { data, isLoading } = useRegionVersion(version)
  if (isLoading) return <p className="text-xs text-muted-foreground">{t('region.historyLoading')}</p>
  if (!data) return null
  return (
    <dl className="grid gap-x-6 gap-y-2 rounded-md bg-muted/40 p-2 text-xs sm:grid-cols-2">
      <Row label={t('region.source')}>
        <span className="break-all font-mono">{data.sourceUrl || '—'}</span>
      </Row>
      <Row label={t('region.dataDate')}>{data.dataDate || '—'}</Row>
      <Row label={t('region.sha')}>
        <span className="break-all font-mono">{data.sha256}</span>
      </Row>
      <Row label={t('region.provinceList')}>{provinceNames(data.payload, t('region.namesSeparator')) || '—'}</Row>
    </dl>
  )
}

// showTime 按当前语言本地化；后端给的是 RFC3339，解析不了就显示占位。
function showTime(iso: string, lang: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString(lang)
}

// provinceNames 从该版本的整棵树正文里取省级（第一层）名称，解析失败就当没有。
function provinceNames(payload: string, sep: string): string {
  try {
    const nodes = JSON.parse(payload) as unknown
    if (!Array.isArray(nodes)) return ''
    return nodes
      .map((n) => (n && typeof n === 'object' && 'name' in n && typeof n.name === 'string' ? n.name : ''))
      .filter(Boolean)
      .join(sep)
  } catch {
    return ''
  }
}

function DiffList({ title, samples }: { title: string; samples?: { code: string; name: string; old?: string }[] }) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-col gap-1.5">
      <p className="font-medium">{title}</p>
      {!samples?.length ? (
        <p className="text-xs text-muted-foreground">{t('region.diff.none')}</p>
      ) : (
        <ul className="flex flex-col gap-1 rounded-md border border-border p-2 font-mono text-xs">
          {samples.map((s) => (
            <li key={s.code} className="flex flex-wrap gap-2">
              <span className="text-muted-foreground">{s.code}</span>
              <span>{s.name}</span>
              {s.old ? <span className="text-muted-foreground">（{t('region.diff.was')} {s.old}）</span> : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
