import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'
import { TrendArea } from './TrendArea'

export default function RequestTrendCard() {
  const { t } = useTranslation()
  const { data } = useDashboardStats()
  return (
    <Card className="flex h-full flex-col transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">{t('dashboard.requestTrend')}</CardTitle>
      </CardHeader>
      <CardContent>
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
      </CardContent>
    </Card>
  )
}
