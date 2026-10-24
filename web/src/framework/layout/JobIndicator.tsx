import { Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { usePageLive } from '@/framework/hooks/use-page-active'
import { useActiveJobCount } from '@/framework/modules/exportcenter/api'
import { Button } from '@/framework/ui/button'
import { cn } from '@/lib/utils'

/** 顶栏的小提示：有进行中的导出任务时显示数量徽标，点击进入导出中心；浏览器标签页不可见时暂停轮询 */
export function JobIndicator() {
  const { t } = useTranslation()
  const live = usePageLive()
  const { data: count = 0 } = useActiveJobCount((q) => (live ? ((q.state.data ?? 0) > 0 ? 3000 : 30000) : false))
  return (
    <Button
      asChild
      variant="ghost"
      size="icon"
      className={cn('relative size-9', count === 0 && 'hidden md:inline-flex')}
    >
      <Link to="/jobs" aria-label={count > 0 ? t('jobs.indicator.label', { count }) : t('jobs.indicator.none')}>
        <Download className="size-4" aria-hidden="true" />
        {count > 0 ? (
          <span className="absolute -end-0.5 -top-0.5 flex min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-medium leading-4 text-primary-foreground">
            {count > 99 ? '99+' : count}
          </span>
        ) : null}
      </Link>
    </Button>
  )
}
