import { useCallback, useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { UseFormReturn } from 'react-hook-form'
import { api } from '@/framework/request/http'
import type { CaptchaResult } from '@/framework/auth/types'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Skeleton } from '@/framework/ui/skeleton'

/** enabled=false 时不请求图片；变为 true（如登录连续失败后）再拉取 */
export function useCaptcha(enabled = true) {
  const [captcha, setCaptcha] = useState<CaptchaResult | null>(null)
  const refresh = useCallback(async () => {
    setCaptcha(null)
    try {
      setCaptcha(await api.get<CaptchaResult>('/auth/captcha', undefined, { silent: true }))
    } catch {
      setCaptcha(null)
    }
  }, [])
  useEffect(() => {
    if (enabled) void refresh()
  }, [enabled, refresh])
  return { captcha, refresh }
}

interface CaptchaFieldProps {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  form: UseFormReturn<any>
  captcha: CaptchaResult | null
  onRefresh: () => void
}

export function CaptchaField({ form, captcha, onRefresh }: CaptchaFieldProps) {
  const { t } = useTranslation()
  return (
    <FormField
      control={form.control}
      name="captcha"
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('login.captcha')}</FormLabel>
          <div className="flex items-start gap-3">
            <FormControl>
              <Input {...field} autoComplete="off" maxLength={6} placeholder={t('login.captchaPlaceholder')} />
            </FormControl>
            <button
              type="button"
              onClick={onRefresh}
              aria-label={t('login.refreshCaptcha')}
              className="group relative flex h-11 w-28 shrink-0 items-center justify-center overflow-hidden rounded-md border border-border bg-muted md:h-9"
            >
              {captcha ? (
                <img src={captcha.image} alt={t('login.captcha')} className="h-full w-full object-cover" />
              ) : (
                <Skeleton className="h-full w-full" />
              )}
              <span className="absolute inset-0 flex items-center justify-center bg-background/60 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
                <RefreshCw className="size-4" aria-hidden="true" />
              </span>
            </button>
          </div>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
