import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { Eye, EyeOff, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuth } from '@/framework/auth/useAuth'
import type { LoginChallenge, LoginResult, ChallengeResult } from '@/framework/auth/types'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { CaptchaField, useCaptcha } from './CaptchaField'

/**
 * 自助注册表单：账号 + 密码 + 确认密码 + 图形验证码（强制）。
 * 注册即登录：响应与 /auth/login 同形——有令牌直接进系统；部署了二次验证门时
 * 返回 {challenge}，交给登录页的二次验证对话框继续。
 */
export function RegisterForm({
  onSuccess,
  onLogin,
  onChallenge,
}: {
  onSuccess: () => void
  onLogin: () => void
  onChallenge?: (c: LoginChallenge) => void
}) {
  const { t } = useTranslation()
  const acceptLogin = useAuth((s) => s.acceptLogin)
  // 注册验证码始终必填（防脚本批量注册），进入表单即拉取
  const { captcha, refresh } = useCaptcha(true)
  const [show, setShow] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const schema = useMemo(
    () =>
      z.object({
        username: z
          .string()
          .trim()
          .min(1, t('login.usernameRequired'))
          .regex(/^[A-Za-z][A-Za-z0-9_]{2,31}$/, t('login.register.usernameRule')),
        password: z.string().min(1, t('login.passwordRequired')),
        confirm: z.string().min(1, t('login.register.confirmRequired')),
        captcha: z.string().trim().min(1, t('login.captchaRequired')),
      })
      .refine((v) => v.password === v.confirm, { path: ['confirm'], message: t('login.register.passwordMismatch') }),
    [t],
  )
  type Values = z.infer<typeof schema>
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { username: '', password: '', confirm: '', captcha: '' },
  })

  const onSubmit = async (v: Values) => {
    setSubmitting(true)
    try {
      const res = await api.post<LoginResult | ChallengeResult>('/auth/register', {
        username: v.username,
        password: v.password,
        captchaId: captcha?.key,
        captcha: v.captcha,
      })
      const step = acceptLogin(res)
      if ('challenge' in step) {
        onChallenge?.(step.challenge)
      } else {
        toast.success(t('login.register.success'))
        onSuccess()
      }
    } catch {
      // 错误 toast 由 http 拦截器统一弹出（账号已存在 / 验证码错误 / 额度已满等）
      form.setValue('captcha', '')
      void refresh()
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-5" noValidate>
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
                  <Input
                    {...field}
                    type={show ? 'text' : 'password'}
                    autoComplete="new-password"
                    className="pe-12"
                    placeholder={t('login.passwordPlaceholder')}
                  />
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
        <FormField
          control={form.control}
          name="confirm"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('login.register.confirmPassword')}</FormLabel>
              <FormControl>
                <Input {...field} type="password" autoComplete="new-password" placeholder={t('login.passwordPlaceholder')} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <CaptchaField form={form} captcha={captcha} onRefresh={refresh} />
        <Button type="submit" className="w-full" disabled={submitting}>
          {submitting && <Loader2 className="me-2 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />}
          {t('login.register.submit')}
        </Button>
        <p className="text-center text-sm text-muted-foreground">
          {t('login.register.hasAccount')}{' '}
          <Button type="button" variant="link" className="h-auto p-0" onClick={onLogin}>
            {t('login.register.toLogin')}
          </Button>
        </p>
      </form>
    </Form>
  )
}
