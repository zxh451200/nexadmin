import { AlertTriangle, CheckCircle2, RefreshCw, XCircle } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { usePageLive, useRefreshOnResume } from '@/framework/hooks/use-page-active'
import { Badge, type BadgeProps } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { cn } from '@/lib/utils'
import { HEALTH_ITEMS, useHealth, type HealthStatus } from './api'

const REFRESH_MS = 30_000

// 语义色 + 图标 + 文字三重表达，不只靠颜色区分
const STATUS: Record<HealthStatus, { variant: BadgeProps['variant']; Icon: LucideIcon }> = {
  ok: { variant: 'success', Icon: CheckCircle2 },
  warn: { variant: 'warning', Icon: AlertTriangle },
  fail: { variant: 'destructive', Icon: XCircle },
}

export function StatusBadge({ status }: { status: HealthStatus }) {
  const { t } = useTranslation()
  const { variant, Icon } = STATUS[status]
  return (
    <Badge variant={variant} className="gap-1">
      <Icon className="size-3.5" aria-hidden="true" />
      {t(`serverMonitor.health.status.${status}`)}
    </Badge>
  )
}

/** 健康检查卡片：各依赖的状态、延迟与说明；页面不在前台时暂停轮询，回到前台立即刷新一次 */
export function HealthCard() {
  const { t, i18n } = useTranslation()
  const live = usePageLive()
  const q = useHealth(live ? REFRESH_MS : false)
  useRefreshOnResume(live, q.refetch)
  const data = q.data

  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0 pb-2">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <CardTitle className="text-base font-medium">{t('serverMonitor.health.title')}</CardTitle>
          {data ? <StatusBadge status={data.status} /> : null}
        </div>
        <div className="flex items-center gap-2">
          {data ? (
            <span className="num text-xs text-muted-foreground">
              {t('serverMonitor.health.updatedAt', { time: new Date(data.ts).toLocaleTimeString(i18n.language, { hour12: false }) })}
            </span>
          ) : null}
          <Button type="button" variant="outline" size="sm" disabled={q.isFetching} onClick={() => void q.refetch()}>
            <RefreshCw className={cn('me-1.5 size-4', q.isFetching && 'animate-spin')} aria-hidden="true" />
            {t('common.refresh')}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {q.isLoading ? (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {HEALTH_ITEMS.map((k) => (
              <Skeleton key={k} className="h-20 w-full" />
            ))}
          </div>
        ) : q.isError || !data ? (
          <p className="text-sm text-destructive" role="alert">
            {t('serverMonitor.health.loadFailed')}
          </p>
        ) : (
          <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3" aria-live="polite">
            {HEALTH_ITEMS.map((k) => {
              const c = data.checks[k]
              if (!c) return null
              return (
                <li key={k} className="flex min-w-0 flex-col gap-1.5 rounded-lg border border-border p-3">
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate text-sm font-medium">{t(`serverMonitor.health.item.${k}`)}</span>
                    <StatusBadge status={c.status} />
                  </div>
                  <p className="num text-xs text-muted-foreground">{t('serverMonitor.health.latency', { ms: c.latencyMs })}</p>
                  <p className="break-words text-xs text-muted-foreground">{c.message || '-'}</p>
                </li>
              )
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
