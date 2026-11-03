import { useCallback, useEffect, useState } from 'react'
import { RefreshCw, Volume2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { UseFormReturn } from 'react-hook-form'
import { toast } from 'sonner'
import { api } from '@/framework/request/http'
import type { CaptchaResult } from '@/framework/auth/types'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { Button } from '@/framework/ui/button'
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
  /** 服务端要求验证码时显示必填星号 */
  required?: boolean
}

export function CaptchaField({ form, captcha, onRefresh, required }: CaptchaFieldProps) {
  const { t, i18n } = useTranslation()
  const [audioBusy, setAudioBusy] = useState(false)

  // 语音验证码：读的是同一张图的音频，不消费验证码；听完仍然要手动输入
  const playAudio = async () => {
    if (!captcha?.key) return
    setAudioBusy(true)
    try {
      const { audio } = await api.get<{ audio: string }>(`/auth/captcha/${captcha.key}/audio`, { lang: i18n.language }, { silent: true })
      await new Audio(audio).play()
    } catch {
      toast.error(t('login.captchaAudioFailed'))
    } finally {
      setAudioBusy(false)
    }
  }

  return (
    <FormField
      control={form.control}
      name="captcha"
      render={({ field }) => (
        <FormItem>
          <FormLabel>
            {t('login.captcha')}
            {required ? <RequiredMark /> : null}
          </FormLabel>
          <div className="flex items-center gap-3">
            <FormControl>
              <Input {...field} autoComplete="off" maxLength={6} placeholder={t('login.captchaPlaceholder')} />
            </FormControl>
            <div className="flex shrink-0 items-center gap-1">
              <button
                type="button"
                onClick={onRefresh}
                aria-label={t('login.refreshCaptcha')}
                className="group relative flex h-12 w-36 shrink-0 items-center justify-center overflow-hidden rounded-md border border-border bg-muted md:h-14 md:w-40"
              >
                {captcha ? (
                  <img src={captcha.image} alt={t('login.captcha')} className="h-full w-full object-contain" />
                ) : (
                  <Skeleton className="h-full w-full" />
                )}
                <span className="absolute inset-0 flex items-center justify-center bg-background/60 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
                  <RefreshCw className="size-4" aria-hidden="true" />
                </span>
              </button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="h-12 w-12 md:h-14 md:w-14"
                aria-label={t('login.captchaAudio')}
                aria-busy={audioBusy}
                disabled={!captcha || audioBusy}
                onClick={() => void playAudio()}
              >
                <Volume2 aria-hidden="true" />
              </Button>
            </div>
          </div>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
