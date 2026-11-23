import type { FormEventHandler, ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

export interface FormLayoutProps {
  children: ReactNode
  onSubmit?: FormEventHandler<HTMLFormElement>
  id?: string
  showRequiredHint?: boolean
  className?: string
}

/** 表单外壳：手机单列，label 在上；输入框手机 16px 防 iOS 放大 */
export function FormLayout({ children, onSubmit, id, showRequiredHint, className }: FormLayoutProps) {
  const { t } = useTranslation()
  return (
    <form
      id={id}
      onSubmit={onSubmit}
      noValidate
      className={cn(
        'flex flex-col gap-6 [&_input]:text-base [&_textarea]:text-base md:[&_input]:text-sm md:[&_textarea]:text-sm',
        className,
      )}
    >
      {showRequiredHint ? <p className="text-xs text-muted-foreground">{t('formLayout.requiredHint')}</p> : null}
      {children}
    </form>
  )
}

export interface FormGridProps {
  children: ReactNode
  /** md 以上列数；全站表单统一单列纵向，默认 1（'auto' 仅为兼容旧调用，同样按 1 处理） */
  cols?: 'auto' | 1 | 2 | 3
  className?: string
}

const colsClass: Record<1 | 2 | 3, string> = {
  1: 'md:grid-cols-1',
  2: 'md:grid-cols-2',
  3: 'md:grid-cols-3',
}

export function FormGrid({ children, cols = 1, className }: FormGridProps) {
  // 不再按字段数自动切两列：同样的表单在不同弹窗里忽横忽竖，是全站一致性最大的来源。
  const effective: 1 | 2 | 3 = cols === 'auto' ? 1 : cols
  return <div className={cn('grid grid-cols-1 gap-3', colsClass[effective], className)}>{children}</div>
}

/** FormGrid 的整行子项（如 Textarea、富文本、上传） */
export function FormGridFull({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('md:col-span-full', className)}>{children}</div>
}

export interface FormSectionProps {
  title?: ReactNode
  description?: ReactNode
  children: ReactNode
  className?: string
}

export function FormSection({ title, description, children, className }: FormSectionProps) {
  return (
    <section className={cn('flex flex-col gap-4', className)}>
      {title || description ? (
        <div className="border-b border-border pb-2">
          {title ? <h3 className="text-sm font-semibold text-foreground">{title}</h3> : null}
          {description ? <p className="mt-1 text-xs text-muted-foreground">{description}</p> : null}
        </div>
      ) : null}
      {children}
    </section>
  )
}
