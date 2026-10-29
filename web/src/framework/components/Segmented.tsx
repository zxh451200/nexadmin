import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface SegmentedOption {
  value: string
  label: ReactNode
}

export interface SegmentedProps {
  value: string
  onChange: (value: string) => void
  options: SegmentedOption[]
  /** 无可见标题时必须给，读屏靠它 */
  ariaLabel: string
  className?: string
}

/**
 * 分段控件：两三个互斥选项的行内切换（图表类型、视图切换等）。
 *
 * 和 Tabs 的分工：Tabs 是带面板的导航（tablist/tab/tabpanel 语义，内容区跟着换），
 * 分段控件只是一个紧凑开关，不承担导航语义。以前图表上方那份是手写的，
 * 需要同样外观的地方应该复用这里，而不是再抄一遍圆角胶囊。
 */
export function Segmented({ value, onChange, options, ariaLabel, className }: SegmentedProps) {
  return (
    <div role="group" aria-label={ariaLabel} className={cn('flex w-fit gap-0.5 rounded-full bg-muted p-1', className)}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            'inline-flex h-8 items-center rounded-full px-3.5 text-sm transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring motion-reduce:transition-none md:h-7',
            value === o.value
              ? 'bg-background font-medium text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}
