import { useTranslation } from 'react-i18next'
import { ChartCard } from '@/framework/components/ChartCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

export default function RequestTrendCard() {
  const { t } = useTranslation()
  const { data } = useDashboardStats()
  return (
    <ChartCard title={t('dashboard.requestTrend')}>
      {data ? (
        <TrendChart
          data={data.requestTrend}
          xKey="date"
          type="area"
          series={[
            { key: 'requests', label: t('dashboard.series.requests') },
            { key: 'errors', label: t('dashboard.series.errors') },
          ]}
        />
      ) : (
        <Skeleton className="h-72 w-full" />
      )}
    </ChartCard>
  )
}
