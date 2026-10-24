import { useQuery } from '@tanstack/react-query'
import { Timer } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { usePageLive } from '@/framework/hooks/use-page-active'
import { api } from '@/framework/request/http'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { cn } from '@/lib/utils'

interface CronStats {
  total: number
  enabled: number
  failed24h: number
}

export default function CronOverviewCard() {
  const { t } = useTranslation()
  const live = usePageLive()
  const { data, isError } = useQuery({
    queryKey: ['plugin-cron', 'stats'],
    queryFn: () => api.get<CronStats>('/plugins/cron/stats'),
    refetchInterval: live ? 60_000 : false,
  })
  const items: Array<{ key: string; value: number; warn?: boolean }> = data
    ? [
        { key: 'total', value: data.total },
        { key: 'enabled', value: data.enabled },
        { key: 'failed24h', value: data.failed24h, warn: data.failed24h > 0 },
      ]
    : []
  return (
    <Card className="transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-base font-medium">
          <Timer className="size-4 text-primary" aria-hidden="true" />
          <Link to="/system/cron" className="rounded-sm hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            {t('plugin_cron.card.overview')}
          </Link>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {isError ? (
          <p role="alert" className="py-4 text-sm text-destructive">
            {t('plugin_cron.card.loadFailed')}
          </p>
        ) : !data ? (
          <Skeleton className="h-24 w-full" />
        ) : (
          <dl className="grid grid-cols-3 gap-2 text-center md:grid-cols-1 md:text-start xl:grid-cols-3 xl:text-center">
            {items.map((it) => (
              <div key={it.key} className="flex min-w-0 flex-col-reverse">
                <dt className="truncate text-xs text-muted-foreground">{t(`plugin_cron.card.${it.key}`)}</dt>
                <dd className={cn('num text-2xl font-semibold', it.warn && 'text-destructive')}>{it.value}</dd>
              </div>
            ))}
          </dl>
        )}
      </CardContent>
    </Card>
  )
}
