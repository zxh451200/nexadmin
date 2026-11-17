import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { usePermission } from '@/framework/auth/usePermission'
import { Badge } from '@/framework/ui/badge'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

export default function RecentNoticesCard() {
  const { t, i18n } = useTranslation()
  const { has } = usePermission()
  const { data } = useDashboardStats()
  const fmtDate = (s: string) => new Date(s).toLocaleDateString(i18n.language, { month: 'short', day: 'numeric' })
  const fmtTime = (s: string) => new Date(s).toLocaleString(i18n.language, { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  return (
    <Card className="flex h-full flex-col transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">{t('dashboard.recentNotices')}</CardTitle>
        {has('notice:list') && (
          <CardAction>
            <Link to="/content/notice" className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground">
              {t('dashboard.viewMore')}
              <ArrowRight className="size-3.5 rtl:rotate-180" aria-hidden="true" />
            </Link>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="flex flex-1 flex-col">
        {!data ? (
          <Skeleton className="h-48 w-full" />
        ) : data.recentNotices.length === 0 ? (
          <p className="grid flex-1 place-items-center py-8 text-center text-sm text-muted-foreground">{t('dashboard.noNotices')}</p>
        ) : (
          /* 行样式照抄模板 UpcomingEvents：迷你日历块（月份灰底 / 日期大字）+ 标题 + muted 时间 + 徽章 */
          <div className="flex flex-1 flex-col justify-between gap-4">
            {data.recentNotices.map((n) => {
              const d = new Date(n.publishAt)
              const month = d.toLocaleDateString(i18n.language, { month: 'short' })
              const day = d.getDate()
              return (
                <div key={n.id} className="flex items-center justify-between gap-4">
                  <div className="flex min-w-0 items-center gap-2.5">
                    <div className="size-11 shrink-0 overflow-hidden rounded-sm border">
                      <div className="grid h-1/3 place-items-center border-b bg-muted text-[10px] font-medium uppercase leading-none text-muted-foreground">
                        {month}
                      </div>
                      <div className="num grid h-2/3 place-items-center text-lg leading-none">{day}</div>
                    </div>
                    <div className="flex min-w-0 flex-col gap-1">
                      <div className="truncate text-sm font-medium leading-none">{n.title}</div>
                      <div className="num text-xs leading-none text-muted-foreground">{fmtTime(n.publishAt)}</div>
                    </div>
                  </div>
                  {n.level === 'important' ? (
                    <Badge variant="destructive" className="shrink-0 text-[10px]">
                      {t('dashboard.important')}
                    </Badge>
                  ) : n.type ? (
                    <Badge variant="outline" className="shrink-0 text-[10px]">
                      {n.type}
                    </Badge>
                  ) : (
                    <span className="num shrink-0 text-xs text-muted-foreground">{fmtDate(n.publishAt)}</span>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
