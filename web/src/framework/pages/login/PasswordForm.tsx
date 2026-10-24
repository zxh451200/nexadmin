import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { Eye, EyeOff, Loader2, Lock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/framework/auth/useAuth'
import { ApiError } from '@/framework/request/http'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import type { LoginChallenge } from '@/framework/auth/types'
import { CaptchaField, useCaptcha } from './CaptchaField'

export function PasswordForm({ onSuccess, onForgot, onChallenge }: { onSuccess: () => void; onForgot?: () => void; onChallenge?: (c: LoginChallenge) => void }) {
  const { t } = useTranslation()
  const login = useAuth((s) => s.login)
  // 默认不要求验证码；服务端在同一用户名/IP 连续失败 3 次后返回 reason=CAPTCHA_REQUIRED，此时才出现验证码输入
  const [needCaptcha, setNeedCaptcha] = useState(false)
  const { captcha, refresh } = useCaptcha(needCaptcha)
  const [show, setShow] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [lockLeft, setLockLeft] = useState(0)

  // 锁定倒计时：每秒递减，归零后提示自动消失
  useEffect(() => {
    if (lockLeft <= 0) return
    const timer = window.setTimeout(() => setLockLeft((s) => s - 1), 1000)
    return () => window.clearTimeout(timer)
  }, [lockLeft])
  const lockTime = `${Math.floor(lockLeft / 60)}:${String(lockLeft % 60).padStart(2, '0')}`
  const schema = useMemo(
    () =>
      z.object({
        username: z.string().trim().min(1, t('login.usernameRequired')),
        password: z.string().min(1, t('login.passwordRequired')),
        captcha: z.string().trim(),
      }),
    [t],
  )
  type Values = z.infer<typeof schema>
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { username: '', password: '', captcha: '' } })

  const onSubmit = async (v: Values) => {
    if (needCaptcha && !v.captcha) {
      form.setError('captcha', { message: t('login.captchaRequired') })
      return
    }
    setSubmitting(true)
    try {
      const step = await login({
        type: 'password',
        username: v.username,
        password: v.password,
        ...(needCaptcha ? { captcha: v.captcha, captchaKey: captcha?.key } : {}),
      })
      if ('challenge' in step) {
        form.setValue('password', '')
        onChallenge?.(step.challenge)
      } else {
        onSuccess()
      }
    } catch (e) {
      if (e instanceof ApiError && e.code === 423) setLockLeft(Number(e.message.split(':')[1]) || 0)
      if (e instanceof ApiError && e.reason === 'CAPTCHA_REQUIRED') setNeedCaptcha(true)
      if (needCaptcha) {
        form.setValue('captcha', '')
        void refresh()
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-5" noValidate>
        {lockLeft > 0 ? (
          <Alert variant="destructive" role="alert">
            <Lock aria-hidden="true" />
            <AlertDescription>{t('login.locked', { time: lockTime })}</AlertDescription>
          </Alert>
        ) : null}
        <FormField
          control={form.control}
          name="username"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('login.username')}</FormLabel>
              <FormControl>
                <Input {...field} autoComplete="username" placeholder={t('login.usernamePlaceholder')} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="password"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('login.password')}</FormLabel>
              <div className="relative">
                <FormControl>
                  <Input {...field} type={show ? 'text' : 'password'} autoComplete="current-password" className="pe-12" placeholder={t('login.passwordPlaceholder')} />
                </FormControl>
                <button
                  type="button"
                  onClick={() => setShow((s) => !s)}
                  aria-label={show ? t('login.hidePassword') : t('login.showPassword')}
                  className="absolute inset-y-0 end-0 flex w-11 items-center justify-center text-muted-foreground hover:text-foreground md:w-9"
                >
                  {show ? <Eye className="size-4" aria-hidden="true" /> : <EyeOff className="size-4" aria-hidden="true" />}
                </button>
              </div>
              <FormMessage />
            </FormItem>
          )}
        />
        {needCaptcha ? <CaptchaField form={form} captcha={captcha} onRefresh={refresh} /> : null}
        {onForgot ? (
          <div className="flex justify-end">
            <Button type="button" variant="link" className="h-auto p-0" onClick={onForgot}>
              {t('login.forgotPassword')}
            </Button>
          </div>
        ) : null}
        <Button type="submit" className="w-full" disabled={submitting}>
          {submitting && <Loader2 className="me-2 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />}
          {t('login.submit')}
        </Button>
      </form>
    </Form>
  )
}
