import { ClipboardList } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { usePageLive } from '@/framework/hooks/use-page-active'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { cn } from '@/lib/utils'
import { useFillStats } from './api'

export default function PendingFormsCard() {
  const { t } = useTranslation()
  const live = usePageLive()
  const { data, isError } = useFillStats({ refetchInterval: live ? 60_000 : false })
  if (isError) return null // 插件不可用（未启用/未授权）时整卡隐藏
  const pending = data?.pending ?? 0
  return (
    <Card className="transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-base font-medium">
          <ClipboardList className="size-4 text-primary" aria-hidden="true" />
          <Link to="/system/formbuilder/fill" className="rounded-sm hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            {t('plugin_formbuilder.card.pending')}
          </Link>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {!data ? (
          <Skeleton className="h-16 w-full" />
        ) : (
          <div className="flex flex-col gap-1">
            <p className={cn('num text-3xl font-semibold', pending > 0 && 'text-primary')}>{pending}</p>
            <p className="text-xs text-muted-foreground">{pending > 0 ? t('plugin_formbuilder.card.pendingHint') : t('plugin_formbuilder.card.noPending')}</p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
