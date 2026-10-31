import { useState, type ReactNode } from 'react'
import { Loader2, RefreshCw, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Skeleton } from '@/framework/ui/skeleton'
import { useResetRegion, useRegionStatus, useSyncRegion } from './api'

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
  const [draft, setDraft] = useState<string | null>(null)

  if (isLoading || !data) return <Skeleton className="h-64 w-full" />

  const source = draft ?? (data.sourceUrl || data.defaultSource)

  const runSync = async () => {
    try {
      const res = await sync.mutateAsync(source.trim())
      toast.success(t('region.syncDone', { added: res.added, renamed: res.renamed, removed: res.removed }))
      if (res.removed > 0) toast.warning(t('region.syncRemoved', { n: res.removed }))
    } catch (e) {
      // 固定 id：连续失败只保留一条提示，不会在右上角叠成一摞
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'), { id: 'region-sync' })
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
            <Button type="button" disabled={sync.isPending || !source.trim()} onClick={() => void runSync()}>
              {sync.isPending ? (
                <Loader2 className="me-2 size-4 animate-spin" aria-hidden="true" />
              ) : (
                <RefreshCw className="me-2 size-4" aria-hidden="true" />
              )}
              {sync.isPending ? t('region.syncing') : t('region.sync')}
            </Button>
            <Button type="button" variant="outline" disabled={reset.isPending || data.builtin} onClick={() => void runReset()}>
              <RotateCcw className="me-2 size-4" aria-hidden="true" />
              {t('region.reset')}
            </Button>
          </Can>
        </CardFooter>
      </Card>
    </div>
  )
}
