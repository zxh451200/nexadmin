import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Badge } from '@/framework/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

export default function TodosCard() {
  const { t, i18n } = useTranslation()
  const { data } = useDashboardStats()
  const fmtTime = (s: string) => new Date(s).toLocaleString(i18n.language, { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  return (
    <Card className="flex h-full flex-col transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">{t('dashboard.todos')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col">
        {!data ? (
          <Skeleton className="h-48 w-full" />
        ) : data.todos.length === 0 ? (
          <p className="grid flex-1 place-items-center py-8 text-center text-sm text-muted-foreground">{t('dashboard.noTodos')}</p>
        ) : (
          /* 行样式照抄模板 ClassSchedule：状态色条 + 主副两行 + 右侧徽章，divide-y 分行、悬停微亮 */
          <ul className="-mx-4 flex-1 divide-y divide-border md:-mx-6">
            {data.todos.map((todo) => {
              const isMessage = todo.kind === 'message'
              return (
                <li key={todo.id} className="transition-colors hover:bg-muted/30">
                  <Link
                    to={isMessage ? '/message' : '/content/notice'}
                    className="grid grid-cols-1 gap-3 px-4 py-3 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring md:px-6"
                  >
                    <div className="flex gap-2.5">
                      <span
                        className="w-1 shrink-0 rounded-md"
                        style={{ background: isMessage ? 'hsl(var(--info))' : 'hsl(var(--warning))' }}
                        aria-hidden="true"
                      />
                      <div className="min-w-0">
                        <div className="truncate text-sm font-medium leading-none">{todo.title}</div>
                        <div className="num mt-1.5 text-xs leading-none text-muted-foreground">{fmtTime(todo.time)}</div>
                      </div>
                    </div>
                    <Badge variant={isMessage ? 'info' : 'warning'} className="justify-self-start">
                      {t(isMessage ? 'dashboard.todoMessage' : 'dashboard.todoNotice')}
                    </Badge>
                  </Link>
                </li>
              )
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
