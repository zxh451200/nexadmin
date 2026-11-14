import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

/**
 * 实时链路状态指示：WS hub 已连接 = 新消息帧直达（绿点脉动）；
 * 断线退回增量轮询（橙点）。IM 会话与工单中心共用同一枚，
 * 放在「会话/统计」页签行的右端留白里——工具栏不挤，指示也有自己的位置。
 */
export function LiveStatusBadge({ live, className }: { live: boolean; className?: string }) {
  const { t } = useTranslation()
  const label = live ? t('common.liveRealtime') : t('common.livePolling')
  return (
    <span
      className={cn(
        'inline-flex shrink-0 select-none items-center gap-1.5 rounded-full bg-muted px-2.5 py-1 text-xs font-medium text-muted-foreground',
        className,
      )}
      title={label}
      role="status"
      aria-label={label}
    >
      <span
        className={cn('size-1.5 shrink-0 rounded-full', live ? 'animate-pulse bg-success' : 'bg-warning')}
        aria-hidden="true"
      />
      {label}
    </span>
  )
}
