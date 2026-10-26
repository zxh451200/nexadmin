import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface PageHeaderProps {
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  className?: string
}

export function PageHeader({ title, description, actions, className }: PageHeaderProps) {
  return (
    <div className={cn('flex flex-wrap items-start justify-between gap-3 md:flex-nowrap md:items-center', className)}>
      <div className="min-w-0 flex-1 basis-48">
        <h1 className="truncate text-lg font-semibold tracking-tight text-foreground md:text-2xl">{title}</h1>
        {description ? <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground md:mt-1 md:text-sm">{description}</p> : null}
      </div>
      {actions ? (
        // 手机上操作按钮靠右、紧凑，但触控高度保持 >=40px；放不下时整组换行
        <div className="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2 max-md:[&_button]:px-3 max-md:[&_button]:text-[13px]">
          {actions}
        </div>
      ) : null}
    </div>
  )
}

/** 统一页面内边距与最大宽度；子元素之间纵向间距统一 */
export function PageContainer({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn('page-enter mx-auto flex w-full max-w-7xl flex-col gap-3 p-3 md:gap-5 md:p-6', className)}>
      {children}
    </div>
  )
}
