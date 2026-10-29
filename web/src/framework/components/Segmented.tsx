import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'
import { segmentedItemClass, segmentedListClass } from '@/framework/ui/tabs'

export interface SegmentedOption {
  value: string
  label: ReactNode
  /** label 只有图标时必须给，读屏靠它 */
  ariaLabel?: string
  /** 图标按钮等需要改尺寸时用，与条目基础样式合并 */
  className?: string
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
 * 分段控件：两三个互斥选项的行内切换（图表类型、视图切换、日志/统计等）。
 *
 * 外观与 Tabs、顶部标签条同一口径（样式常量在 ui/tabs.tsx），只是语义是紧凑开关、
 * 不承担导航面板职责。别在这里另起一套圆角/底色，否则同一产品会出现两种「选中」。
 */
export function Segmented({ value, onChange, options, ariaLabel, className }: SegmentedProps) {
  return (
    <div role="group" aria-label={ariaLabel} className={cn(segmentedListClass, className)}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={value === o.value}
          aria-label={o.ariaLabel}
          data-state={value === o.value ? 'active' : 'inactive'}
          onClick={() => onChange(o.value)}
          className={cn(segmentedItemClass, o.className)}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}
