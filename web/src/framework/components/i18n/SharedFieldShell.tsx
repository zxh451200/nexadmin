import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useLocaleForm } from './LocaleFormProvider'

/** 消费方据此把自己的 Input/Textarea 置灰；标签与「共用」标记由 SharedFieldShell 负责。 */
export function useSharedField() {
  const { isSource } = useLocaleForm()
  return { disabled: !isSource }
}

/** 不随语言变化的字段外壳：译文态下置灰标签并打上「共用」标记，免得有人以为要重填。 */
export function SharedFieldShell({
  labelKey,
  required,
  children,
  className,
}: {
  labelKey: string
  required?: boolean
  children: ReactNode
  className?: string
}) {
  const { t } = useTranslation()
  const { isSource } = useLocaleForm()
  return (
    <label className={cn('flex flex-col gap-1 text-sm', className)}>
      {/* min-h-7：与 LocalizedField 的标签行等高，两列表单的输入框才能对齐 */}
      <span className={cn('inline-flex min-h-7 items-center gap-1.5', !isSource && 'text-muted-foreground')}>
        <span className="inline-flex items-baseline">
          {t(labelKey)}
          {required ? (
            <span aria-hidden="true" className="ms-0.5 text-destructive">*</span>
          ) : null}
        </span>
        {!isSource ? (
          <span className="rounded border px-1 text-[10px] font-semibold">{t('localeForm.sharedTag')}</span>
        ) : null}
      </span>
      {children}
    </label>
  )
}
