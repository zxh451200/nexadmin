import { Database, LogIn, Mail, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatCard } from '@/framework/components/StatCard'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

export default function StatsCard() {
  const { t } = useTranslation()
  const { data, isLoading } = useDashboardStats()
  return (
    <div className="grid grid-cols-2 gap-3 md:gap-4 xl:grid-cols-4">
      {isLoading || !data ? (
        Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-28 w-full" />)
      ) : (
        <>
          <StatCard title={t('dashboard.stat.users')} value={data.users.value} delta={data.users.delta} icon={<Users aria-hidden="true" />} />
          <StatCard
            title={t('dashboard.stat.todayLogin')}
            value={data.todayLogin.value}
            delta={data.todayLogin.delta}
            icon={<LogIn aria-hidden="true" />}
            spark={data.loginTrend.map((d) => Number(d.logins) || 0)}
          />
          <StatCard title={t('dashboard.stat.unreadMessages')} value={data.unreadMessages.value} delta={data.unreadMessages.delta} icon={<Mail aria-hidden="true" />} />
          <StatCard
            title={t('dashboard.stat.storage')}
            value={data.storage.usedGb}
            hint={`/ ${data.storage.totalGb} GB`}
            progress={(data.storage.usedGb / data.storage.totalGb) * 100}
            delta={data.storage.delta}
            icon={<Database aria-hidden="true" />}
          />
        </>
      )}
    </div>
  )
}
