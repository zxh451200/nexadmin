import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'
import { DotBarChart } from './DotBarChart'

export default function LoginTrendCard() {
  const { t } = useTranslation()
  const { data } = useDashboardStats()
  return (
    <Card className="flex h-full flex-col transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">{t('dashboard.loginTrend')}</CardTitle>
      </CardHeader>
      <CardContent>
        {data ? (
          <DotBarChart
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
      </CardContent>
    </Card>
  )
}
