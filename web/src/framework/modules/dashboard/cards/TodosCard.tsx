import { Bell } from 'lucide-react'
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
    <Card className="transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-base font-medium">
          <Bell className="size-4 text-primary" aria-hidden="true" />
          {t('dashboard.todos')}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {!data ? (
          <Skeleton className="h-48 w-full" />
        ) : data.todos.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">{t('dashboard.noTodos')}</p>
        ) : (
          <ul className="divide-y divide-border">
            {data.todos.map((todo) => (
              <li key={todo.id}>
                <Link
                  to={todo.kind === 'message' ? '/message' : '/content/notice'}
                  className="flex min-h-11 items-center gap-2 rounded-md py-2 text-sm hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <Badge variant={todo.kind === 'message' ? 'info' : 'warning'} className="shrink-0">
                    {t(todo.kind === 'message' ? 'dashboard.todoMessage' : 'dashboard.todoNotice')}
                  </Badge>
                  <span className="min-w-0 flex-1 truncate">{todo.title}</span>
                  <span className="num shrink-0 text-xs text-muted-foreground">{fmtTime(todo.time)}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
