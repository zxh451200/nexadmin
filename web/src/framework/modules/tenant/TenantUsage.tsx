import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/framework/ui/badge'
import { Progress } from '@/framework/ui/progress'
import { Skeleton } from '@/framework/ui/skeleton'
import { useTenantUsage } from './api'

const DAY = 86400000

/** 距到期天数（向上取整，已过期为负数） */
export function daysUntil(expireAt: string, now = Date.now()) {
  return Math.ceil((new Date(expireAt).getTime() - now) / DAY)
}

/** 到期徽标：已过期 destructive，30 天内 warning，其余 success */
export function ExpireBadge({ expireAt, status }: { expireAt: string; status?: string }) {
  const { t } = useTranslation()
  const days = daysUntil(expireAt)
  if (status === 'expired' || days < 0) {
    return <Badge variant="destructive">{days < 0 ? t('tenant.expiredDays', { count: -days }) : t('tenant.expired')}</Badge>
  }
  if (days === 0) return <Badge variant="warning">{t('tenant.expiresToday')}</Badge>
  return <Badge variant={days <= 30 ? 'warning' : 'success'}>{t('tenant.daysLeft', { count: days })}</Badge>
}

function formatMb(mb: number) {
  return mb >= 1024 ? `${+(mb / 1024).toFixed(1)} GB` : `${mb} MB`
}

/** 单项用量：有额度时显示进度条（≥80% warning，≥100% destructive），不限时只显示数值 */
function UsageBar({ label, used, max, format }: { label: string; used: number; max: number; format?: (n: number) => string }) {
  const { t } = useTranslation()
  const fmt = format ?? String
  const pct = max > 0 ? Math.min(100, Math.round((used / max) * 100)) : 0
  const tone = pct >= 100 ? '[&>div]:bg-destructive' : pct >= 80 ? '[&>div]:bg-warning' : ''
  return (
    <div className="flex min-w-36 flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2 text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className="num">
          {fmt(used)} / {max > 0 ? fmt(max) : t('tenantPkg.unlimited')}
        </span>
      </div>
      {max > 0 ? (
        <Progress value={pct} className={cn('h-1.5', tone)} aria-label={`${label} ${pct}%`} />
      ) : null}
    </div>
  )
}

/** 租户列表里的用量单元格：用户数与存储 */
export function TenantUsageCell({ id }: { id: string }) {
  const { t } = useTranslation()
  const { data, isLoading, isError } = useTenantUsage(id)
  if (isLoading) return <Skeleton className="h-8 w-36" />
  if (isError || !data) return <span className="text-xs text-muted-foreground">—</span>
  return (
    <div className="flex flex-col gap-1.5">
      <UsageBar label={t('tenant.usage.users')} used={data.users} max={data.maxUsers} />
      <UsageBar label={t('tenant.usage.storage')} used={data.storageMB} max={data.maxStorageMB} format={formatMb} />
    </div>
  )
}
