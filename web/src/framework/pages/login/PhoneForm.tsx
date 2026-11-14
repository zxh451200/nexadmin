import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import type { LoginChallenge } from '@/framework/auth/types'
import { useAuth } from '@/framework/auth/useAuth'
import { api, ApiError } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { CaptchaField, useCaptcha } from './CaptchaField'

const COOLDOWN = 60

export function PhoneForm({
  onSuccess,
  onChallenge,
  captchaEnabled = true,
}: {
  onSuccess: () => void
  onChallenge?: (c: LoginChallenge) => void
  /** 验证码总开关（/auth/methods 的 captchaEnabled）：关闭时不再要求、也不提交验证码 */
  captchaEnabled?: boolean
}) {
  const { t } = useTranslation()
  const login = useAuth((s) => s.login)
  // 与账号登录同款自适应：首屏不出现；发码接口按 IP 发码次数返回 CAPTCHA_REQUIRED 时才出现
  const [needCaptcha, setNeedCaptcha] = useState(false)
  const { captcha, refresh, mode, setMode } = useCaptcha(captchaEnabled && needCaptcha)
  const [submitting, setSubmitting] = useState(false)
  const [sending, setSending] = useState(false)
  const [left, setLeft] = useState(0)

  const schema = useMemo(
    () =>
      z.object({
        phone: z.string().regex(/^1\d{10}$/, t('login.phoneInvalid')),
        captcha: captchaEnabled && needCaptcha ? z.string().trim().min(1, t('login.captchaRequired')) : z.string().trim(),
        code: z.string().regex(/^\d{6}$/, t('login.smsCodeInvalid')),
      }),
    [t, captchaEnabled, needCaptcha],
  )
  type Values = z.infer<typeof schema>
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { phone: '', captcha: '', code: '' } })

  useEffect(() => {
    if (left <= 0) return
    const timer = setTimeout(() => setLeft((n) => n - 1), 1000)
    return () => clearTimeout(timer)
  }, [left])

  const sendCode = async () => {
    const withCaptcha = captchaEnabled && needCaptcha
    if (!(await form.trigger(withCaptcha ? ['phone', 'captcha'] : ['phone']))) return
    const { phone, captcha: text } = form.getValues()
    setSending(true)
    try {
      await api.post('/auth/sms-code', { phone, ...(withCaptcha ? { captcha: text, captchaKey: captcha?.key } : {}) })
      toast.success(t('login.smsSent'))
      setLeft(COOLDOWN)
    } catch (e) {
      // 服务端按 IP 发码次数判定需要人机验证：当场打开验证码区并拉取挑战（与账号登录同款）
      if (e instanceof ApiError && e.reason === 'CAPTCHA_REQUIRED') setNeedCaptcha(true)
    } finally {
      form.setValue('captcha', '')
      if (captchaEnabled && needCaptcha) void refresh()
      setSending(false)
    }
  }

  const onSubmit = async (v: Values) => {
    setSubmitting(true)
    try {
      const step = await login({ type: 'phone', phone: v.phone, code: v.code })
      if ('challenge' in step) onChallenge?.(step.challenge)
      else onSuccess()
    } catch {
      // 错误已统一 toast
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-5" noValidate>
        <FormField
          control={form.control}
          name="phone"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('login.phone')}</FormLabel>
              <FormControl>
                <Input {...field} type="tel" inputMode="numeric" maxLength={11} autoComplete="tel" placeholder={t('login.phonePlaceholder')} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="code"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('login.smsCode')}</FormLabel>
              <div className="flex items-start gap-3">
                <FormControl>
                  <Input {...field} inputMode="numeric" maxLength={6} autoComplete="one-time-code" placeholder={t('login.smsCodePlaceholder')} />
                </FormControl>
                <Button type="button" variant="outline" className="h-10 w-28 shrink-0 md:h-10" disabled={left > 0 || sending} onClick={sendCode}>
                  {sending ? (
                    <Loader2 className="size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />
                  ) : left > 0 ? (
                    <span className="num">{t('login.resendIn', { count: left })}</span>
                  ) : (
                    t('login.sendCode')
                  )}
                </Button>
              </div>
              <FormMessage />
            </FormItem>
          )}
        />
        {captchaEnabled && needCaptcha ? <CaptchaField form={form} captcha={captcha} onRefresh={refresh} mode={mode} onModeChange={setMode} /> : null}
        <Button type="submit" className="w-full" disabled={submitting}>
          {submitting && <Loader2 className="me-2 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />}
          {t('login.submit')}
        </Button>
      </form>
    </Form>
  )
}
