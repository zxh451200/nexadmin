import { ArrowDown, ArrowUp, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/framework/ui/badge'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

/** KPI 卡照抄 shadcn 教育仪表盘：text-sm 标题 + 右上角 Info、text-3xl 大数字 + 涨跌徽章、
    右下角右对齐 muted 明细行。基线不可比时不展示百分比：1 → 0 会算出 -100% 这种噪声。 */
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

export default function StatsCard() {
  const { t, i18n } = useTranslation()
  const { data, isLoading } = useDashboardStats()
  const fmt = (n: number) => new Intl.NumberFormat(i18n.language).format(n)

  if (isLoading || !data) {
    return (
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 md:gap-4 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className="h-32 w-full" />
        ))}
      </div>
    )
  }

  const cards = [
    { title: t('dashboard.stat.users'), value: fmt(data.users.value), delta: deltaBadge(data.users, t), detail: undefined as string | undefined },
    { title: t('dashboard.stat.todayLogin'), value: fmt(data.todayLogin.value), delta: deltaBadge(data.todayLogin, t), detail: undefined },
    { title: t('dashboard.stat.unreadMessages'), value: fmt(data.unreadMessages.value), delta: deltaBadge(data.unreadMessages, t), detail: undefined },
    {
      title: t('dashboard.stat.storage'),
      value: `${data.storage.usedGb}`,
      delta: deltaBadge(data.storage, t),
      detail: data.storage.totalGb > 0 ? t('dashboard.kpi.storageDetail', { total: data.storage.totalGb }) : undefined,
    },
  ]

  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 md:gap-4 xl:grid-cols-4">
      {cards.map((c) => (
        <Card key={c.title}>
          <CardHeader>
            <CardTitle className="text-sm">{c.title}</CardTitle>
            <CardAction>
              <Info className="size-3 text-muted-foreground" aria-hidden="true" />
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <span className="num text-3xl leading-none tracking-tight">{c.value}</span>
              {c.delta}
            </div>
            {c.detail ? <div className="num text-end text-xs text-muted-foreground">{c.detail}</div> : null}
          </CardContent>
        </Card>
      ))}
    </div>
  )
}
