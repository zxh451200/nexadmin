import { Database, LogIn, Mail, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatCard } from '@/framework/components/StatCard'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

/** 基线不可比时不展示百分比：1 → 0 会算出 -100% 这种噪声。 */
function delta(s: { delta?: number; comparable?: boolean }): number | undefined {
  return s.comparable ? s.delta : undefined
}

export default function StatsCard() {
  const { t } = useTranslation()
  const { data, isLoading } = useDashboardStats()
  return (
    <div className="grid grid-cols-2 gap-3 md:gap-4 xl:grid-cols-4">
      {isLoading || !data ? (
        Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-28 w-full" />)
      ) : (
        <>
          <StatCard title={t('dashboard.stat.users')} value={data.users.value} delta={delta(data.users)} tone="chart-1" icon={<Users aria-hidden="true" />} />
          <StatCard
            title={t('dashboard.stat.todayLogin')}
            value={data.todayLogin.value}
            delta={delta(data.todayLogin)}
            tone="chart-2"
            icon={<LogIn aria-hidden="true" />}
            spark={data.loginTrend.map((d) => Number(d.logins) || 0)}
          />
          <StatCard title={t('dashboard.stat.unreadMessages')} value={data.unreadMessages.value} delta={delta(data.unreadMessages)} tone="chart-5" icon={<Mail aria-hidden="true" />} />
          <StatCard
            title={t('dashboard.stat.storage')}
            value={data.storage.usedGb}
            tone="chart-3"
            hint={data.storage.totalGb > 0 ? `/ ${data.storage.totalGb} GB` : undefined}
            progress={data.storage.totalGb > 0 ? (data.storage.usedGb / data.storage.totalGb) * 100 : undefined}
            delta={delta(data.storage)}
            icon={<Database aria-hidden="true" />}
          />
        </>
      )}
    </div>
  )
}
