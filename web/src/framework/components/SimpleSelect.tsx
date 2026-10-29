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
  className?: string
  disabled?: boolean
}

/** 只有选项没有分组的轻量下拉，尺寸/圆角/箭头与全站 SelectTrigger 一致，不要再手写原生 <select> */
export function SimpleSelect({ value, onChange, options, placeholder, className, disabled, ...rest }: SimpleSelectProps) {
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger
        aria-label={rest['aria-label']}
        className={cn('w-auto min-w-24 whitespace-nowrap', className)}
      >
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
