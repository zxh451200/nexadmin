import { ArrowDown, ArrowUp, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/framework/ui/badge'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

/** KPI 条照抄模板 analytics-kpi-strip：外层一张卡（ring 边框 + overflow-hidden），
    内部四格用 divide 分隔，移动端自动变两列 divide-y 堆叠。
    格内沿用教育仪表盘 KPI：text-sm 标题 + Info、text-3xl 大数字 + 涨跌徽章、右下 muted 明细行。
    基线不可比时不展示百分比：1 → 0 会算出 -100% 这种噪声。 */
function deltaBadge(s: { delta?: number; comparable?: boolean }, t: (k: string, vars?: Record<string, unknown>) => string) {
  if (!s.comparable || !s.delta || s.delta === 0) return null
  const up = s.delta > 0
  const Icon = up ? ArrowUp : ArrowDown
  return (
    <Badge variant={up ? 'success' : 'destructive'} className="rounded-sm px-1 text-xs font-normal">
      <Icon className="size-3" aria-hidden="true" />
      {t('dashboard.kpi.delta', { value: Math.abs(s.delta) })}
    </Badge>
  )
}

interface KpiCellProps {
  title: string
  value: string
  delta?: React.ReactNode
  detail?: string
}

function KpiCell({ title, value, delta, detail }: KpiCellProps) {
  return (
    <div className="flex flex-col gap-3 p-4 md:p-5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-normal text-muted-foreground">{title}</span>
        <Info className="size-3.5 shrink-0 text-muted-foreground/70" aria-hidden="true" />
      </div>
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <span className="num text-2xl leading-none tracking-tight md:text-3xl">{value}</span>
          {delta}
        </div>
        {detail ? <div className="num text-end text-xs text-muted-foreground">{detail}</div> : null}
      </div>
    </div>
  )
}

export default function StatsCard() {
  const { t, i18n } = useTranslation()
  const { data, isLoading } = useDashboardStats()
  const fmt = (n: number) => new Intl.NumberFormat(i18n.language).format(n)

  if (isLoading || !data) {
    return (
      <div className="card-edge overflow-hidden rounded-xl border border-border bg-card">
        <div className="grid grid-cols-2 divide-y divide-border md:grid-cols-4 md:divide-x md:divide-y-0 rtl:divide-x-reverse">
          {Array.from({ length: 4 }, (_, i) => (
            <div key={i} className="p-4 md:p-5">
              <Skeleton className="h-24 w-full" />
            </div>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="card-edge overflow-hidden rounded-xl border border-border bg-card">
      <div className="grid grid-cols-2 divide-y divide-border md:grid-cols-4 md:divide-x md:divide-y-0 rtl:divide-x-reverse">
        <KpiCell title={t('dashboard.stat.users')} value={fmt(data.users.value)} delta={deltaBadge(data.users, t)} />
        <KpiCell title={t('dashboard.stat.todayLogin')} value={fmt(data.todayLogin.value)} delta={deltaBadge(data.todayLogin, t)} />
        <KpiCell title={t('dashboard.stat.unreadMessages')} value={fmt(data.unreadMessages.value)} delta={deltaBadge(data.unreadMessages, t)} />
        <KpiCell
          title={t('dashboard.stat.storage')}
          value={`${data.storage.usedGb}`}
          delta={deltaBadge(data.storage, t)}
          detail={data.storage.totalGb > 0 ? t('dashboard.kpi.storageDetail', { total: data.storage.totalGb }) : undefined}
        />
      </div>
    </div>
  )
}
