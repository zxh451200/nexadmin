import { useTranslation } from 'react-i18next'
import { ChartCard } from '@/framework/components/ChartCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

export default function LoginTrendCard() {
  const { t } = useTranslation()
  const { data } = useDashboardStats()
  return (
    <ChartCard title={t('dashboard.loginTrend')}>
      {data ? (
        <TrendChart
          data={data.loginTrend}
          xKey="date"
          type="line"
          series={[
            { key: 'logins', label: t('dashboard.series.logins') },
            { key: 'failures', label: t('dashboard.series.failures') },
          ]}
        />
      ) : (
        <Skeleton className="h-72 w-full" />
      )}
    </ChartCard>
  )
}
