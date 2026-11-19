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

export default function LoginTrendCard() {
  const { t, i18n } = useTranslation()
  const [days, setDays] = useState<TrendDays>(30)
  const { data } = useDashboardStats(days)
  const fmt = (n: number) => new Intl.NumberFormat(i18n.language).format(n)
  const ranges: ChartCardRange[] = DAYS.map((d, i) => ({ value: String(d), label: t(`dashboard.trendRange.${RANGE_KEYS[i]}`) }))
  const totalLogins = data ? data.loginTrend.reduce((s, p) => s + p.logins, 0) : 0

  return (
    <ChartCard
      title={t('dashboard.loginTrend')}
      ranges={ranges}
      range={String(days)}
      onRangeChange={(v) => setDays(Number(v) as TrendDays)}
      // 「查看报表」借 Studio Admin 的画法挂在标题右侧；权限点与报表页一致，没权限的用户看不到入口
      actions={
        <Can perm="loginLog:list">
          <Link
            to="/monitor/login-log"
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
              value: fmt(totalLogins),
              label: t('dashboard.trendSummary.logins'),
              hint: t('dashboard.trendSummary.window', { days }),
            }
          : undefined
      }
    >
      {data ? (
        <TrendArea
          data={data.loginTrend}
          xKey="date"
          series={[
            { key: 'logins', label: t('dashboard.series.logins'), color: 'hsl(var(--chart-2))' },
            { key: 'failures', label: t('dashboard.series.failures'), color: 'hsl(var(--destructive))' },
          ]}
        />
      ) : (
        <Skeleton className="h-72 w-full" />
      )}
    </ChartCard>
  )
}
