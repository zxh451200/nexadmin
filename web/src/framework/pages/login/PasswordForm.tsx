import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { Eye, EyeOff, Loader2, Lock, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/framework/auth/useAuth'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ApiError } from '@/framework/request/http'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import type { LoginChallenge } from '@/framework/auth/types'
import { CaptchaField, useCaptcha, type CaptchaMode } from './CaptchaField'

export function PasswordForm({
  onSuccess,
  onForgot,
  // TOTP 挑战由 PasswordForm 内联处理（验证码在密码下方），不再弹 SecondFactorDialog；
  // onChallenge 保留在接口上供 PhoneForm 等其他入口使用，PasswordForm 自身不再调用。
  onChallenge: _onChallenge,
  demoCredentials,
  fillNonce = 0,
}: {
  onSuccess: () => void
  onForgot?: () => void
  onChallenge?: (c: LoginChallenge) => void
  /** 演示站凭据（/system/status 下发）：非 null 时预填账号与密码 */
  demoCredentials?: { account: string; password: string } | null
  /** 父组件的「填入演示账号」按钮计数：变化时重新预填（用户清空后再填回去） */
  fillNonce?: number
}) {
  const { t } = useTranslation()
  const login = useAuth((s) => s.login)
  const verifyLogin = useAuth((s) => s.verifyLogin)
  // 首屏不挂验证码：总开关开启时也走自适应——服务端连续失败达到阈值后才在错误响应里
  // 带 CAPTCHA_REQUIRED（重档带 CAPTCHA_REQUIRED_HARD），这里据此把验证码区打开；
  // 总开关关闭时服务端永远不要求，验证码区始终不出现。
  const [needCaptcha, setNeedCaptcha] = useState(false)
  // baseMode 是服务端指派的形态：软阈值（3 次）滑块、硬阈值（6 次）点选；用户只能在
  // 当前形态与图形码兜底之间切换，不能降级（见 CaptchaField 的切换链接）
  const [baseMode, setBaseMode] = useState<CaptchaMode>('slider')
  const { captcha, refresh, mode, setMode } = useCaptcha(needCaptcha, baseMode)
  // 升级档：服务端指派形态换成点选（密码失败响应、滑块交卷失败响应里带
  // CAPTCHA_REQUIRED_HARD 都会走到这里）——滑块专用的自动化工具到这儿一并被筛出去。
  // 用户不能降级，只能切图形码兜底。
  const escalateToClick = () => {
    setNeedCaptcha(true)
    setBaseMode('click')
    setMode('click')
  }
  const [show, setShow] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [verifying, setVerifying] = useState(false)
  // 锁定倒计时按绝对截止时间算剩余：后台标签页定时器节流、机器睡眠恢复后不会多报（多报会让用户以为还在锁定期）
  const [lockUntil, setLockUntil] = useState(0)
  const [lockLeft, setLockLeft] = useState(0)
  // 服务端要求 TOTP 二次验证时的挑战；非 null 时密码下方出现动态码输入，登录按钮变「验证」
  const [challenge, setChallenge] = useState<LoginChallenge | null>(null)
  const [totpCode, setTotpCode] = useState('')
  const [totpError, setTotpError] = useState('')

  // 锁定倒计时：每秒按截止时间重算，归零后提示自动消失
  useEffect(() => {
    if (lockUntil <= 0) return
    const tick = () => {
      const left = Math.max(0, Math.ceil((lockUntil - Date.now()) / 1000))
      setLockLeft(left)
      if (left <= 0) setLockUntil(0)
    }
    tick()
    const timer = window.setInterval(tick, 1000)
    return () => window.clearInterval(timer)
  }, [lockUntil])
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

  // 演示站预填：凭据就绪时填一次，fillNonce 变化（点「填入演示账号」）时再填。
  // demoCredentials 由 useDemoAccount 用 useMemo 保持稳定，不会在用户编辑时反复重置。
  useEffect(() => {
    if (!demoCredentials) return
    form.setValue('username', demoCredentials.account)
    form.setValue('password', demoCredentials.password)
  }, [demoCredentials, fillNonce, form])

  const onSubmit = async (v: Values) => {
    if (needCaptcha && !v.captcha) {
      form.setError('captcha', { message: t('login.captchaRequired') })
      return
    }
    // 二次验证阶段：用挑战 ID + TOTP 码换令牌
    if (challenge) {
      const value = totpCode.trim()
      if (!/^\d{6}$/.test(value)) {
        setTotpError(t('login.otpInvalid'))
        return
      }
      setVerifying(true)
      setTotpError('')
      try {
        const step = await verifyLogin(challenge.id, value)
        if ('challenge' in step) {
          setChallenge(step.challenge)
          setTotpCode('')
        } else {
          onSuccess()
        }
      } catch (e) {
        setTotpCode('')
        setTotpError(e instanceof Error ? e.message : t('login.otpFailed'))
      } finally {
        setVerifying(false)
      }
      return
    }
    // 首次登录：用户名 + 密码
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
        // 内联处理二次验证（TOTP 输入出现在密码下方），不弹 SecondFactorDialog。
        setChallenge(step.challenge)
      } else {
        onSuccess()
      }
    } catch (e) {
      if (e instanceof ApiError && e.code === 423) setLockUntil(Date.now() + (e.retryAfter ?? 0) * 1000)
      if (e instanceof ApiError && e.reason === 'CAPTCHA_REQUIRED') {
        setNeedCaptcha(true)
        setBaseMode('slider')
      }
      // 升级档：连错达到硬阈值（hardAfterFails）后，服务端指派形态换成点选（见 escalateToClick）。
      if (e instanceof ApiError && e.reason === 'CAPTCHA_REQUIRED_HARD') escalateToClick()
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
                <Input {...field} autoComplete="username" placeholder={t('login.usernamePlaceholder')} disabled={!!challenge} />
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
                  <Input {...field} type={show ? 'text' : 'password'} autoComplete="current-password" className="pe-12" placeholder={t('login.passwordPlaceholder')} disabled={!!challenge} />
                </FormControl>
                <button
                  type="button"
                  onClick={() => setShow((s) => !s)}
                  aria-label={show ? t('login.hidePassword') : t('login.showPassword')}
                  className="absolute inset-y-0 inset-e-0 flex w-11 items-center justify-center text-muted-foreground hover:text-foreground md:w-9"
                >
                  {show ? <Eye className="size-4" aria-hidden="true" /> : <EyeOff className="size-4" aria-hidden="true" />}
                </button>
              </div>
              <FormMessage />
            </FormItem>
          )}
        />
        {needCaptcha && !challenge ? <CaptchaField form={form} captcha={captcha} onRefresh={refresh} mode={mode} onModeChange={setMode} baseMode={baseMode} required={needCaptcha} onEscalate={escalateToClick} /> : null}

        {challenge ? (
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <ShieldCheck className="size-4 text-muted-foreground" aria-hidden="true" />
              <label htmlFor="totp-inline-code" className="text-sm text-muted-foreground">
                {t('login.totpInlineLabel')}
                <RequiredMark />
              </label>
            </div>
            {totpError ? (
              <Alert variant="destructive" role="alert">
                <AlertDescription>{totpError}</AlertDescription>
              </Alert>
            ) : null}
            <Input
              id="totp-inline-code"
              value={totpCode}
              onChange={(e) => {
                setTotpCode(e.target.value)
                setTotpError('')
              }}
              autoComplete="one-time-code"
              inputMode="numeric"
              maxLength={6}
              placeholder={t('login.otpPlaceholder')}
              autoFocus
            />
          </div>
        ) : null}

        <Button type="submit" className="w-full" disabled={submitting || verifying}>
          {(submitting || verifying) && <Loader2 className="me-2 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />}
          {challenge ? t('login.otpSubmit') : t('login.submit')}
        </Button>
        {onForgot && !challenge ? (
          <div className="flex justify-end">
            <Button type="button" variant="link" className="h-auto p-0" onClick={onForgot}>
              {t('login.forgotPassword')}
            </Button>
          </div>
        ) : null}
      </form>
    </Form>
  )
}
