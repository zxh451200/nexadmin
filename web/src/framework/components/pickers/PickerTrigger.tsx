import * as React from 'react'
import { ChevronDown, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

export interface PickerTriggerProps extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, 'children' | 'value'> {
  /** 已选内容；为空时显示 placeholder */
  display?: React.ReactNode
  placeholder?: string
  allowClear?: boolean
  hasValue?: boolean
  onClear?: () => void
  icon?: React.ReactNode
}

/** 外观与 Input 一致的选择器触发按钮（手机 h-11，桌面 h-9，字号 16px/md:14px） */
export const PickerTrigger = React.forwardRef<HTMLButtonElement, PickerTriggerProps>(function PickerTrigger(
  { display, placeholder, allowClear, hasValue, onClear, icon, className, disabled, ...rest },
  ref,
) {
  const { t } = useTranslation()
  const showClear = allowClear && hasValue && !disabled
  return (
    // className 作用在最外层：宽度类（如 md:w-40）才会让选择框在筛选栏里和输入框排成一行
    <div className={cn('relative w-full', className)}>
      <button
        ref={ref}
        type="button"
        disabled={disabled}
        className={cn(
          'flex h-10 w-full items-center justify-between gap-2 rounded-md border border-input bg-background px-3 text-start text-base ring-offset-background',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 md:h-9 md:text-sm',
          'disabled:cursor-not-allowed disabled:opacity-50',
        )}
        {...rest}
      >
        {/* 给「清除」X 留位置放在文字区里，箭头仍在最右边；之前把右内边距加在整个按钮上，箭头被挤到 X 底下重叠 */}
        <span className={cn('flex min-w-0 flex-1 items-center gap-2', showClear && 'pe-8')}>
          {icon}
          <span className={cn('min-w-0 flex-1 truncate', !hasValue && 'text-muted-foreground')}>
            {hasValue ? display : placeholder ?? t('common.pleaseSelect')}
          </span>
        </span>
        <ChevronDown className="size-4 shrink-0 opacity-50" aria-hidden="true" />
      </button>
      {showClear && (
        <button
          type="button"
          aria-label={t('common.clear')}
          className="absolute end-8 top-1/2 inline-flex size-10 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground hover:text-foreground md:size-7"
          onClick={(e) => {
            e.stopPropagation()
            onClear?.()
          }}
        >
          <X className="size-4" aria-hidden="true" />
        </button>
      )}
    </div>
  )
})
