import { useTranslation } from 'react-i18next'
import { useOnlineList } from '@/framework/modules/online/api'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'

/** 实时在线卡照抄模板 analytics 的 realtime-visitors：绿点 ping 脉冲 + Live 徽标、
    大号在线数，下方小格列出当前在线用户（头像缩写 / 昵称 / IP / 最近活跃）。
    数据来自 GET /online-users，30s 轮询；模板的分钟级柱状图我们后端没有对应数据，不硬造。 */
export default function OnlineCard() {
  const { t, i18n } = useTranslation()
  // placeholderData 保持上一次数据：轮询间隙卡片不闪空
  const { data, isLoading } = useOnlineList({ page: 1, pageSize: 6 }, { refetchInterval: 30_000 })
  const rtf = new Intl.RelativeTimeFormat(i18n.language, { numeric: 'auto' })
  const lastActive = (iso: string) => {
    const diffMin = Math.round((Date.now() - new Date(iso).getTime()) / 60000)
    if (diffMin < 1) return t('dashboard.onlineJustNow')
    if (diffMin < 60) return rtf.format(-diffMin, 'minute')
    return rtf.format(-Math.round(diffMin / 60), 'hour')
  }

  const users = data?.list ?? []
  const total = data?.total ?? 0

  return (
    <Card className="flex h-full flex-col transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">{t('dashboard.onlineTitle')}</CardTitle>
        <CardAction>
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="relative flex size-2">
              <span className="absolute inline-flex size-full animate-ping rounded-full bg-success opacity-75 motion-reduce:animate-none" aria-hidden="true" />
              <span className="relative inline-flex size-2 rounded-full bg-success" aria-hidden="true" />
            </span>
            Live
          </span>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-4">
        <div className="flex items-baseline gap-1.5">
          <span className="num text-2xl leading-none tracking-tight">{isLoading ? '—' : total}</span>
          <span className="text-sm text-muted-foreground">{t('dashboard.onlineNow')}</span>
        </div>
        {isLoading ? (
          <Skeleton className="h-28 w-full" />
        ) : users.length === 0 ? (
          <p className="grid flex-1 place-items-center py-6 text-center text-sm text-muted-foreground">{t('dashboard.onlineEmpty')}</p>
        ) : (
          <div className="grid grow grid-cols-1 sm:grid-cols-2">
            {users.map((u) => (
              <div
                key={u.id}
                className="flex items-center gap-2.5 border-b border-border/60 py-2.5 last:border-b-0 sm:odd:border-e sm:odd:pe-4 sm:even:ps-4 sm:[&:nth-last-child(-n+2)]:border-b-0"
              >
                <span className="grid size-8 shrink-0 place-items-center rounded-full bg-muted text-xs font-medium" aria-hidden="true">
                  {(u.nickname || u.username).slice(0, 1)}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium leading-none">{u.nickname || u.username}</div>
                  <div className="num mt-1 truncate text-xs leading-none text-muted-foreground">{u.ip}</div>
                </div>
                <span className="num shrink-0 text-xs text-muted-foreground">{lastActive(u.lastActiveAt)}</span>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
