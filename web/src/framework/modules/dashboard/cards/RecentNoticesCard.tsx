import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { usePermission } from '@/framework/auth/usePermission'
import { Badge } from '@/framework/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

export default function RecentNoticesCard() {
  const { t, i18n } = useTranslation()
  const { has } = usePermission()
  const { data } = useDashboardStats()
  const fmtTime = (s: string) => new Date(s).toLocaleString(i18n.language, { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  return (
    <Card className="transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-base font-medium">{t('dashboard.recentNotices')}</CardTitle>
        {has('notice:list') && (
          <Link to="/content/notice" className="inline-flex min-h-11 items-center text-sm text-primary hover:underline md:min-h-0">
            {t('dashboard.viewMore')}
          </Link>
        )}
      </CardHeader>
      <CardContent>
        {!data ? (
          <Skeleton className="h-32 w-full" />
        ) : data.recentNotices.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">{t('dashboard.noNotices')}</p>
        ) : (
          <ul className="divide-y divide-border">
            {data.recentNotices.map((n) => (
              <li key={n.id} className="flex min-h-11 flex-wrap items-center gap-x-3 gap-y-1 py-2 text-sm sm:flex-nowrap">
                <span className="min-w-0 flex-1 basis-full truncate sm:basis-0">{n.title}</span>
                {/* 「重要」固定占一列（sm 以上），没有标签的行也留位；手机上标题独占一行，标签与时间在第二行 */}
                <span className="flex w-auto shrink-0 justify-end sm:w-12">
                  {n.level === 'important' && <Badge variant="destructive">{t('dashboard.important')}</Badge>}
                </span>
                <span className="num ms-auto w-auto shrink-0 text-end text-xs text-muted-foreground sm:ms-0 sm:w-28">{fmtTime(n.publishAt)}</span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
