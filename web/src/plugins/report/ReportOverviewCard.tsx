import { ChartColumn } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { usePageLive } from '@/framework/hooks/use-page-active'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useOverview } from './api'

export default function ReportOverviewCard() {
  const { t } = useTranslation()
  const live = usePageLive()
  const { data, isError } = useOverview(live ? 60_000 : false)
  if (isError) return null // 插件不可用时整卡隐藏
  const items = data
    ? [
        { key: 'reports', value: data.reports },
        { key: 'screens', value: data.screens },
      ]
    : []
  return (
    <Card className="transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-base font-medium">
          <ChartColumn className="size-4 text-primary" aria-hidden="true" />
          <Link to="/monitor/report" className="rounded-sm hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            {t('plugin_report.card.overview')}
          </Link>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {!data ? (
          <Skeleton className="h-24 w-full" />
        ) : (
          <dl className="grid grid-cols-2 gap-2 text-center md:grid-cols-1 md:text-start xl:grid-cols-2 xl:text-center">
            {items.map((it) => (
              <div key={it.key} className="flex min-w-0 flex-col-reverse">
                <dt className="truncate text-xs text-muted-foreground">{t(`plugin_report.card.${it.key}`)}</dt>
                <dd className="num text-2xl font-semibold">{it.value}</dd>
              </div>
            ))}
          </dl>
        )}
      </CardContent>
    </Card>
  )
}
