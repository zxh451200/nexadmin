import { useId, type ReactNode } from 'react'
import type { Control, FieldPath, FieldValues } from 'react-hook-form'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Label } from '@/framework/ui/label'
import { Switch } from '@/framework/ui/switch'
import { cn } from '@/lib/utils'

// 两种开关形态共用同一套外框与排版，避免各页面再各写一套
const rowClass = 'flex flex-row items-center justify-between gap-4'
const boxedClass = 'rounded-lg border border-border p-3'

export interface SwitchRowProps {
  label: ReactNode
  description?: ReactNode
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
  /** 放进已有边框的容器（卡片、divide-y 分组）时去掉自身边框与内边距，避免「卡片里再套一圈」 */
  bare?: boolean
  className?: string
}

/** 不受 react-hook-form 约束的开关行：用于局部 state 或非表单场景。 */
export function SwitchRow({ label, description, checked, onCheckedChange, disabled, bare, className }: SwitchRowProps) {
  const id = useId()
  return (
    <div className={cn(rowClass, !bare && boxedClass, className)}>
      <div className="min-w-0 space-y-0.5">
        <Label htmlFor={id} className="text-sm font-medium">
          {label}
        </Label>
        {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
      </div>
      <Switch
        id={id}
        checked={checked}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
        aria-label={typeof label === 'string' ? label : undefined}
      />
    </div>
  )
}

export interface SwitchFieldProps<T extends FieldValues> {
  control: Control<T>
  name: FieldPath<T>
  label: ReactNode
  description?: ReactNode
  disabled?: boolean
  /** 放进已有边框的容器（卡片、divide-y 分组）时去掉自身边框与内边距，避免「卡片里再套一圈」 */
  bare?: boolean
  /** 字段值不是布尔时指定开/关对应的值，如 '1' / '0' */
  valueOn?: unknown
  valueOff?: unknown
  className?: string
}

/** 表单里唯一的开关字段形态：标签在左（可带说明），开关在右，独占整行。表格里的状态快切不走这里。 */
export function SwitchField<T extends FieldValues>({
  control,
  name,
  label,
  description,
  disabled,
  bare,
  valueOn = true,
  valueOff = false,
  className,
}: SwitchFieldProps<T>) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem className={cn(rowClass, 'space-y-0', !bare && boxedClass, className)}>
          <div className="min-w-0 space-y-0.5">
            <FormLabel>{label}</FormLabel>
            {description ? <FormDescription className="text-xs">{description}</FormDescription> : null}
            <FormMessage />
          </div>
          <FormControl>
            <Switch checked={field.value === valueOn} onCheckedChange={(c) => field.onChange(c ? valueOn : valueOff)} disabled={disabled} />
          </FormControl>
        </FormItem>
      )}
    />
  )
}
