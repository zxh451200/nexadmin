import { ArrowRight, Bell, ListTodo, Megaphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { usePermission } from '@/framework/auth/usePermission'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

export default function TodosCard() {
  const { t, i18n } = useTranslation()
  const { has } = usePermission()
  const { data } = useDashboardStats()
  const fmtTime = (s: string) => new Date(s).toLocaleString(i18n.language, { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  const unread = data?.unreadMessages.value ?? 0
  return (
    <Card className="flex h-full flex-col transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="flex-row items-center justify-between gap-3 space-y-0 pb-3">
        <div className="flex min-w-0 items-center gap-2">
          <CardTitle className="truncate text-sm">{t('dashboard.todos')}</CardTitle>
          {unread > 0 ? (
            <span
              className="num shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground"
              aria-label={`${t('dashboard.stat.unreadMessages')}: ${unread}`}
            >
              {unread}
            </span>
          ) : null}
        </div>
        {has('message:list') ? (
          <Link to="/message" className="inline-flex shrink-0 items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring motion-reduce:transition-none">
            {t('dashboard.viewMore')}
            <ArrowRight className="size-3.5 rtl:rotate-180" aria-hidden="true" />
          </Link>
        ) : null}
      </CardHeader>
      <CardContent className="flex flex-1 flex-col">
        {!data ? (
          <Skeleton className="h-52 w-full" />
        ) : data.todos.length === 0 ? (
          <div className="grid flex-1 place-items-center py-8 text-center text-sm text-muted-foreground">
            <div>
              <ListTodo className="mx-auto mb-3 size-8 text-muted-foreground/50" aria-hidden="true" />
              {t('dashboard.noTodos')}
            </div>
          </div>
        ) : (
          <ul className="-mx-4 flex-1 divide-y divide-border/70 md:-mx-6">
            {data.todos.map((todo) => {
              const isMessage = todo.kind === 'message'
              const Icon = isMessage ? Bell : Megaphone
              return (
                <li key={todo.id} className="transition-colors hover:bg-muted/30 motion-reduce:transition-none">
                  <Link
                    to={isMessage ? '/message' : '/content/notice'}
                    className="group grid min-h-14 grid-cols-[auto_minmax(0,1fr)] items-center gap-3 px-4 py-2.5 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring md:min-h-12 md:grid-cols-[auto_minmax(0,1fr)_auto] md:px-6"
                  >
                    <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-muted/70 text-muted-foreground transition-colors group-hover:bg-accent group-hover:text-accent-foreground motion-reduce:transition-none" aria-hidden="true">
                      <Icon className="size-4" />
                    </span>
                    <span className="min-w-0">
                      <span className="sr-only">{t(isMessage ? 'dashboard.todoMessage' : 'dashboard.todoNotice')}：</span>
                      <span className="block truncate text-sm font-medium leading-5">{todo.title}</span>
                      <span className="num mt-0.5 block truncate text-xs leading-4 text-muted-foreground md:hidden">{fmtTime(todo.time)}</span>
                    </span>
                    <span className="num hidden shrink-0 text-xs text-muted-foreground md:block">{fmtTime(todo.time)}</span>
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
