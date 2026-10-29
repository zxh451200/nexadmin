import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

/**
 * 译文编辑时的原文对照：一行灰字挂在输入框下方。
 * 只给纯文本字段用；富文本字段走 LocalizedField 内的折叠面板，不能把 HTML 当纯文本渲染。
 */
export function SourceHint({ value, className }: { value?: string | null; className?: string }) {
  const { t } = useTranslation()
  if (!value || !value.trim()) return null
  return (
    <p className={cn('line-clamp-2 text-xs text-muted-foreground', className)} title={value}>
      <span className="me-1 font-medium text-muted-foreground/80">{t('localeForm.source')}</span>
      {value}
    </p>
  )
}
