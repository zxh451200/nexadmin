import { useTranslation } from 'react-i18next'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Progress } from '@/framework/ui/progress'
import { Skeleton } from '@/framework/ui/skeleton'
import { useNoticeReadStats, type Notice } from './api'

interface Props {
  notice: Notice | null
  onOpenChange: (open: boolean) => void
}

export function ReadStatsDialog({ notice, onOpenChange }: Props) {
  const { t } = useTranslation()
  const { data, isLoading } = useNoticeReadStats(notice?.id)
  const total = (data?.readCount ?? 0) + (data?.unreadCount ?? 0)
  const percent = total ? Math.round(((data?.readCount ?? 0) / total) * 100) : 0

  return (
    <ResponsiveDialog
      open={!!notice}
      onOpenChange={onOpenChange}
      size="md"
      title={t('notice.stats.title')}
      description={notice?.title}
      footer={
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
          {t('common.close')}
        </Button>
      }
    >
      {isLoading || !data ? (
        <div className="space-y-3">
          <Skeleton className="h-6 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between text-sm">
              <span className="text-foreground">
                {t('notice.stats.read')}: <span className="num font-medium text-success">{data.readCount}</span>
              </span>
              <span className="text-foreground">
                {t('notice.stats.unread')}: <span className="num font-medium text-warning">{data.unreadCount}</span>
              </span>
            </div>
            <Progress value={percent} aria-label={t('notice.stats.readRate')} />
            <p className="num text-end text-xs text-muted-foreground">
              {t('notice.stats.readRate')} {percent}%
            </p>
          </div>

          <div>
            <h3 className="mb-2 text-sm font-semibold text-foreground">{t('notice.stats.unreadList')}</h3>
            {data.unreadUsers.length === 0 ? (
              <p className="py-6 text-center text-sm text-muted-foreground">{t('notice.stats.allRead')}</p>
            ) : (
              <ul className="max-h-72 divide-y divide-border overflow-y-auto rounded-md border border-border">
                {data.unreadUsers.map((u) => (
                  <li key={u.id} className="flex min-h-11 items-center justify-between gap-3 px-3 py-2">
                    <div className="min-w-0">
                      <p className="truncate text-sm text-foreground">{u.nickname}</p>
                      <p className="truncate text-xs text-muted-foreground">@{u.username}</p>
                    </div>
                    {u.deptName ? <span className="shrink-0 text-xs text-muted-foreground">{u.deptName}</span> : null}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
    </ResponsiveDialog>
  )
}
