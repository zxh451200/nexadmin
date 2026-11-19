import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'
import { Can } from '@/framework/auth/Can'
import { ChartCard, type ChartCardRange } from '@/framework/components/ChartCard'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats, type TrendDays } from '../api'
import { TrendArea } from './TrendArea'

const DAYS: TrendDays[] = [7, 14, 30]
const RANGE_KEYS = ['d7', 'd14', 'd30'] as const

export default function RequestTrendCard() {
  const { t, i18n } = useTranslation()
  const [days, setDays] = useState<TrendDays>(30)
  const { data } = useDashboardStats(days)
  const fmt = (n: number) => new Intl.NumberFormat(i18n.language).format(n)
  const ranges: ChartCardRange[] = DAYS.map((d, i) => ({ value: String(d), label: t(`dashboard.trendRange.${RANGE_KEYS[i]}`) }))
  const totalRequests = data ? data.requestTrend.reduce((s, p) => s + p.requests, 0) : 0

  return (
    <ChartCard
      title={t('dashboard.requestTrend')}
      ranges={ranges}
      range={String(days)}
      onRangeChange={(v) => setDays(Number(v) as TrendDays)}
      // 接口监控是平台级页面（apiMonitor:list），入口与报表页同一权限口径
      actions={
        <Can perm="apiMonitor:list">
          <Link
            to="/monitor/api"
            className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            {t('dashboard.viewReport')}
            <ArrowRight className="size-3.5" aria-hidden="true" />
          </Link>
        </Can>
      }
      summary={
        data
          ? {
              value: fmt(totalRequests),
              label: t('dashboard.trendSummary.requests'),
              hint: t('dashboard.trendSummary.window', { days }),
            }
          : undefined
      }
    >
      {data ? (
        <TrendArea
          data={data.requestTrend}
          xKey="date"
          series={[
            { key: 'requests', label: t('dashboard.series.requests'), color: 'hsl(var(--chart-3))' },
            { key: 'errors', label: t('dashboard.series.errors'), color: 'hsl(var(--destructive))' },
          ]}
        />
      ) : (
        <Skeleton className="h-72 w-full" />
      )}
    </ChartCard>
  )
}
