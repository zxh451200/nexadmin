import { useState } from 'react'
import { useFormContext } from 'react-hook-form'
import type { ReactNode } from 'react'
import { Eye, EyeOff } from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
  const { t } = useTranslation()
  const [show, setShow] = useState(false)
  const isPassword = type === 'password'
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          {isPassword ? (
            // 与登录页密码框同款交互：眼睛开关切换明文/密文
            <div className="relative">
              <FormControl>
                <Input
                  {...field}
                  type={show ? 'text' : 'password'}
                  placeholder={placeholder}
                  autoComplete={autoComplete}
                  inputMode={inputMode}
                  className="pe-12"
                />
              </FormControl>
              <button
                type="button"
                onClick={() => setShow((s) => !s)}
                aria-label={show ? t('setup.hidePassword') : t('setup.showPassword')}
                className="absolute inset-y-0 end-0 flex w-11 items-center justify-center text-muted-foreground hover:text-foreground md:w-9"
              >
                {show ? <Eye className="size-4" aria-hidden="true" /> : <EyeOff className="size-4" aria-hidden="true" />}
              </button>
            </div>
          ) : (
            <FormControl>
              <Input {...field} type={type} placeholder={placeholder} autoComplete={autoComplete} inputMode={inputMode} />
            </FormControl>
          )}
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
