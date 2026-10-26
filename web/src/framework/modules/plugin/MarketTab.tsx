import { useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Download, ExternalLink, Loader2, RefreshCw, Search, Store, Trash2, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { usePermission } from '@/framework/auth/usePermission'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Skeleton } from '@/framework/ui/skeleton'
import {
  MARKET_KEY,
  serverAlive,
  useInstallPlugin,
  useMarket,
  useMarketJob,
  useRebuildServer,
  useRemovePlugin,
  type MarketEntry,
} from './api'

/** 安装/移除/重建任务的进度对话框：滚动日志 + 状态推进；触发自重启后等待服务恢复再整页刷新。 */
function JobDialog({ jobId, onClose }: { jobId: string | null; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: job, isError } = useMarketJob(jobId)
  const [waitingRestart, setWaitingRestart] = useState(false)
  const logRef = useRef<HTMLDivElement>(null)

  // 任务消失（404）或状态到 restarting：服务正在/已经自重启，转入恢复等待。
  useEffect(() => {
    if (!jobId) return
    if (job?.state === 'restarting' || (!job && isError)) setWaitingRestart(true)
  }, [jobId, job, isError])

  // 服务恢复后刷新整页（菜单/路由/已装列表都变了，整页重载最干净）。
  useEffect(() => {
    if (!waitingRestart) return
    let stop = false
    const tick = async () => {
      if (await serverAlive()) {
        window.location.reload()
        return
      }
      if (!stop) window.setTimeout(tick, 2500)
    }
    void tick()
    return () => {
      stop = true
    }
  }, [waitingRestart])

  useEffect(() => {
    logRef.current?.scrollTo({ top: logRef.current.scrollHeight })
  }, [job?.log.length])

  useEffect(() => {
    if (job?.state === 'done' || job?.state === 'failed') void qc.invalidateQueries({ queryKey: MARKET_KEY })
  }, [job?.state, qc])

  const stateNode = waitingRestart ? (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
      {t('pluginMgr.market.job.restarting')}
    </div>
  ) : job?.state === 'done' ? (
    <div className="flex items-center gap-2 text-sm">
      <CheckCircle2 className="h-4 w-4 text-primary" aria-hidden="true" />
      {t('pluginMgr.market.job.done')}
    </div>
  ) : job?.state === 'failed' ? (
    <div className="flex items-center gap-2 text-sm text-destructive">
      <XCircle className="h-4 w-4" aria-hidden="true" />
      {t('pluginMgr.market.job.failed')}
    </div>
  ) : (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
      {t('pluginMgr.market.job.running')}
    </div>
  )

  return (
    <ResponsiveDialog
      open={!!jobId}
      onOpenChange={(o) => !o && !waitingRestart && onClose()}
      title={t('pluginMgr.market.job.title')}
      description={t('pluginMgr.market.job.desc')}
      footer={
        <Button variant="outline" size="sm" disabled={waitingRestart} onClick={onClose}>
          {t('pluginMgr.market.job.close')}
        </Button>
      }
    >
      <div className="flex flex-col gap-3">
        {stateNode}
        <div ref={logRef} className="max-h-72 overflow-y-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs leading-relaxed">
          {(job?.log ?? []).map((line, i) => (
            <div key={i} className="whitespace-pre-wrap break-all text-muted-foreground">
              {line}
            </div>
          ))}
          {!job?.log?.length ? <span className="text-muted-foreground">{t('pluginMgr.market.job.empty')}</span> : null}
        </div>
      </div>
    </ResponsiveDialog>
  )
}

const STATUS_LABEL: Record<string, string> = {
  active: 'pluginMgr.status.active',
  unlicensed: 'pluginMgr.status.unlicensed',
  expired: 'pluginMgr.status.expired',
  invalid: 'pluginMgr.status.invalid',
  revoked: 'pluginMgr.status.revoked',
  disabled: 'pluginMgr.status.disabled',
}

export default function MarketTab({ focus = '' }: { focus?: string }) {
  const { t } = useTranslation()
  const { has } = usePermission()
  const canInstall = has('plugin:install')
  const { data = [], isLoading, isError, refetch } = useMarket()
  const install = useInstallPlugin()
  const remove = useRemovePlugin()
  const rebuild = useRebuildServer()
  const [jobId, setJobId] = useState<string | null>(null)
  const [keyword, setKeyword] = useState(focus)
  const [category, setCategory] = useState('')

  const categories = useMemo(() => {
    const set = new Set<string>()
    data.forEach((e) => e.category && set.add(e.category))
    return Array.from(set)
  }, [data])

  const entries = useMemo(() => {
    const kw = keyword.trim().toLowerCase()
    return data.filter((e) => {
      if (category && e.category !== category) return false
      if (!kw) return true
      return (
        e.name.toLowerCase().includes(kw) ||
        e.id.toLowerCase().includes(kw) ||
        (e.description ?? '').toLowerCase().includes(kw)
      )
    })
  }, [data, keyword, category])

  const onInstall = (e: MarketEntry) => {
    install.mutate(
      { id: e.id, force: e.installed },
      { onSuccess: (res) => setJobId(res.jobId) },
    )
  }

  const onRemove = (e: MarketEntry) => {
    if (!window.confirm(t('pluginMgr.market.remove.confirm', { name: e.name }))) return
    remove.mutate({ id: e.id }, { onSuccess: (res) => setJobId(res.jobId) })
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-64 max-w-full">
          <Search className="absolute start-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
          <Input
            className="ps-8"
            placeholder={t('pluginMgr.market.search')}
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
          />
        </div>
        {categories.map((c) => (
          <Button
            key={c}
            size="sm"
            variant={category === c ? 'default' : 'outline'}
            onClick={() => setCategory(category === c ? '' : c)}
          >
            {t(`pluginMgr.market.category.${c}`, c)}
          </Button>
        ))}
        {canInstall ? (
          <Button
            size="sm"
            variant="outline"
            className="ms-auto"
            disabled={rebuild.isPending}
            onClick={() => rebuild.mutate(undefined, { onSuccess: (res) => setJobId(res.jobId) })}
          >
            <RefreshCw className={rebuild.isPending ? 'animate-spin' : ''} aria-hidden="true" />
            {t('pluginMgr.market.rebuild')}
          </Button>
        ) : null}
      </div>

      {isLoading ? (
        <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={i} className="h-44 w-full" />
          ))}
        </div>
      ) : isError ? (
        <Card>
          <CardContent className="py-10 text-center text-sm text-muted-foreground">
            <XCircle className="mx-auto mb-3 h-6 w-6 text-destructive" aria-hidden="true" />
            <p>{t('pluginMgr.market.error')}</p>
            <Button className="mt-3" size="sm" variant="outline" onClick={() => void refetch()}>
              {t('pluginMgr.market.retry')}
            </Button>
          </CardContent>
        </Card>
      ) : entries.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-sm text-muted-foreground">
            <Store className="mx-auto mb-3 h-6 w-6" aria-hidden="true" />
            {data.length === 0 ? t('pluginMgr.market.empty') : t('pluginMgr.market.nomatch')}
          </CardContent>
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-2 xl:grid-cols-3">
          {entries.map((e) => (
            <Card key={e.id} className="flex flex-col">
              <CardHeader className="pb-2">
                <CardTitle className="flex flex-wrap items-center gap-2 text-base font-medium">
                  <span className="min-w-0 truncate">{e.name}</span>
                  {e.version ? <span className="num text-xs font-normal text-muted-foreground">v{e.version}</span> : null}
                  {e.price ? <span className="num text-xs text-primary">{e.price}</span> : null}
                  <Badge variant={e.edition === 'pro' ? 'info' : 'secondary'} className="ms-auto">
                    {t(`pluginMgr.edition.${e.edition}`)}
                  </Badge>
                </CardTitle>
                {e.description ? <p className="line-clamp-2 text-sm text-muted-foreground">{e.description}</p> : null}
              </CardHeader>
              <CardContent className="flex flex-1 flex-col gap-3">
                <div className="flex flex-wrap items-center gap-2">
                  {e.category ? <Badge variant="outline">{t(`pluginMgr.market.category.${e.category}`, e.category)}</Badge> : null}
                  {e.installed ? (
                    <>
                      <Badge variant="success">{t('pluginMgr.market.installed')}</Badge>
                      {e.status && STATUS_LABEL[e.status] ? (
                        <Badge variant="secondary">{t(STATUS_LABEL[e.status])}</Badge>
                      ) : null}
                    </>
                  ) : null}
                  {e.repo ? (
                    <a
                      className="ms-auto inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                      href={`https://github.com/${e.repo}`}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      {e.repo}
                      <ExternalLink className="h-3 w-3" aria-hidden="true" />
                    </a>
                  ) : null}
                </div>
                {canInstall ? (
                  <div className="mt-auto flex gap-2">
                    <Button size="sm" disabled={install.isPending} onClick={() => onInstall(e)}>
                      {install.isPending ? (
                        <Loader2 className="animate-spin" aria-hidden="true" />
                      ) : (
                        <Download aria-hidden="true" />
                      )}
                      {e.installed ? t('pluginMgr.market.update') : t('pluginMgr.market.install')}
                    </Button>
                    {e.installed ? (
                      <Button size="sm" variant="outline" disabled={remove.isPending} onClick={() => onRemove(e)}>
                        <Trash2 aria-hidden="true" />
                        {t('pluginMgr.market.remove')}
                      </Button>
                    ) : null}
                  </div>
                ) : null}
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <JobDialog jobId={jobId} onClose={() => setJobId(null)} />
    </div>
  )
}
