import type { ReactNode } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'

interface ListErrorBannerProps {
  onRetry: () => void
  retrying?: boolean
  description?: ReactNode
}

/** 列表刷新失败时保留已有数据，并持续显示就地重试入口。 */
export function ListErrorBanner({ onRetry, retrying, description }: ListErrorBannerProps) {
  const { t } = useTranslation()
  return (
    <div role="alert" className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2 text-sm">
      <div className="min-w-0">
        <div className="font-medium text-foreground">{t('common.loadFailed')}</div>
        <div className="text-muted-foreground">{description ?? t('common.staleDataHint')}</div>
      </div>
      <Button type="button" variant="outline" size="sm" disabled={retrying} onClick={onRetry}>
        {retrying ? <Loader2 className="me-1 size-4 animate-spin" aria-hidden="true" /> : null}
        {t('common.retry')}
      </Button>
    </div>
  )
}
