import type { ReactNode } from 'react'
import type { Control, FieldValues } from 'react-hook-form'
import { Checkbox } from '@/framework/ui/checkbox'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { RadioGroup, RadioGroupItem } from '@/framework/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'

// 表单字段较多且结构各异，这里统一放宽 control / name 类型
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type AnyControl = Control<any>
export interface Opt {
  value: string
  label: string
}
interface Base {
  control: AnyControl
  name: string
  label: ReactNode
  description?: ReactNode
}

export function TextField({
  control,
  name,
  label,
  description,
  type = 'text',
  placeholder,
  inputMode,
}: Base & { type?: 'text' | 'password' | 'number' | 'url'; placeholder?: string; inputMode?: 'url' | 'numeric' }) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input
              {...field}
              value={field.value ?? ''}
              type={type}
              placeholder={placeholder}
              inputMode={inputMode}
              autoComplete={type === 'password' ? 'new-password' : 'off'}
              onChange={(e) => field.onChange(type === 'number' ? e.target.valueAsNumber : e.target.value)}
            />
          </FormControl>
          {description ? <FormDescription>{description}</FormDescription> : null}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function TextareaField({ control, name, label, description, placeholder }: Base & { placeholder?: string }) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Textarea {...field} value={field.value ?? ''} rows={3} placeholder={placeholder} />
          </FormControl>
          {description ? <FormDescription>{description}</FormDescription> : null}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function SwitchField({ control, name, label, description }: Base) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem className="flex flex-row items-center justify-between gap-4 rounded-lg border border-border p-3">
          <div className="min-w-0 space-y-0.5">
            <FormLabel>{label}</FormLabel>
            {description ? <FormDescription>{description}</FormDescription> : null}
          </div>
          <FormControl>
            <Switch checked={!!field.value} onCheckedChange={field.onChange} />
          </FormControl>
        </FormItem>
      )}
    />
  )
}

export function CheckField({ control, name, label }: Omit<Base, 'description'>) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem className="flex min-h-10 flex-row items-center gap-2 space-y-0">
          <FormControl>
            <Checkbox checked={!!field.value} onCheckedChange={(c) => field.onChange(c === true)} />
          </FormControl>
          <FormLabel className="flex min-h-10 cursor-pointer items-center font-normal">{label}</FormLabel>
        </FormItem>
      )}
    />
  )
}

export function SelectField({ control, name, label, description, options }: Base & { options: Opt[] }) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <Select value={field.value ?? ''} onValueChange={field.onChange}>
            <FormControl>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
            </FormControl>
            <SelectContent>
              {options.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {description ? <FormDescription>{description}</FormDescription> : null}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function RadioField({ control, name, label, options }: Base & { options: Opt[] }) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <RadioGroup value={field.value ?? ''} onValueChange={field.onChange} className="flex flex-col gap-2 sm:flex-row sm:gap-6">
              {options.map((o) => {
                const id = `${name}-${o.value}`
                return (
                  <div key={o.value} className="flex min-h-10 items-center gap-2">
                    <RadioGroupItem value={o.value} id={id} />
                    <label htmlFor={id} className="flex min-h-10 flex-1 cursor-pointer items-center text-sm text-foreground">
                      {o.label}
                    </label>
                  </div>
                )
              })}
            </RadioGroup>
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export type { FieldValues }
