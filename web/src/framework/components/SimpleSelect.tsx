import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { cn } from '@/lib/utils'

export interface SimpleSelectOption {
  value: string
  label: string
}

export interface SimpleSelectProps {
  value: string
  onChange: (value: string) => void
  options: SimpleSelectOption[]
  placeholder?: string
  /** 无可见文字标签时必须给，读屏与测试都靠它 */
  'aria-label'?: string
  /**
   * 维度名，显示在触发器里选中值的前面（如「状态 · 已解决」）。
   * 只给 aria-label 的话，选完值屏上就只剩一个值，看不出这是在筛什么。
   */
  label?: string
  className?: string
  disabled?: boolean
}

/** 只有选项没有分组的轻量下拉，尺寸/圆角/箭头与全站 SelectTrigger 一致，不要再手写原生 <select> */
export function SimpleSelect({
  value,
  onChange,
  options,
  placeholder,
  label,
  className,
  disabled,
  ...rest
}: SimpleSelectProps) {
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger
        aria-label={rest['aria-label'] ?? label}
        // max-w-56：长字典标签把触发器撑到无限宽、挤走工具栏兄弟元素；省略号由 ui/select 的 line-clamp-1 兜底
        className={cn('w-auto min-w-24 max-w-56 whitespace-nowrap', className)}
      >
        {label ? <span className="shrink-0 text-muted-foreground">{label}</span> : null}
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
