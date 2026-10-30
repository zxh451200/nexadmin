import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'

/** 列表请求失败时的行内提示：保留已有数据，给出重试入口。 */
export function ListErrorBanner({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2 text-sm">
      <span className="text-muted-foreground">{t('plugin_pay.loadFailed')}</span>
      <Button variant="outline" size="sm" onClick={onRetry}>
        {t('plugin_pay.retry')}
      </Button>
    </div>
  )
}
