import { useFormContext } from 'react-hook-form'
import type { ReactNode } from 'react'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { TimezoneSelect } from '@/framework/components/TimezoneSelect'

export function TimezoneField({ name, label }: { name: string; label: string }) {
  const { control } = useFormContext()
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <TimezoneSelect value={field.value} onChange={field.onChange} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

interface TextFieldProps {
  name: string
  label: string
  type?: string
  placeholder?: string
  autoComplete?: string
  inputMode?: 'text' | 'numeric' | 'url'
  description?: string
}

export function TextField({ name, label, type = 'text', placeholder, autoComplete = 'off', inputMode, description }: TextFieldProps) {
  const { control } = useFormContext()
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input {...field} type={type} placeholder={placeholder} autoComplete={autoComplete} inputMode={inputMode} />
          </FormControl>
          {description && <FormDescription>{description}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function SwitchField({ name, label, description }: { name: string; label: string; description?: string }) {
  const { control } = useFormContext()
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem className="flex items-center justify-between gap-4 rounded-lg border border-border p-3">
          <div className="min-w-0 space-y-0.5">
            <FormLabel>{label}</FormLabel>
            {description && <FormDescription>{description}</FormDescription>}
          </div>
          <FormControl>
            <Switch checked={!!field.value} onCheckedChange={field.onChange} />
          </FormControl>
        </FormItem>
      )}
    />
  )
}

interface SelectFieldProps {
  name: string
  label: string
  options: { value: string; label: ReactNode }[]
  onValueChange?: (v: string) => void
}

export function SelectField({ name, label, options, onValueChange }: SelectFieldProps) {
  const { control } = useFormContext()
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <Select
            value={field.value}
            onValueChange={(v) => {
              field.onChange(v)
              onValueChange?.(v)
            }}
          >
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
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
