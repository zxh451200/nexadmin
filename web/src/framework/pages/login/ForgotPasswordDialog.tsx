import { useEffect, useState } from 'react'
import { CheckCircle2, Eye, EyeOff, Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'

type Step = 1 | 2 | 3

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const PHONE_RE = /^1\d{10}$/
const COOLDOWN = 60

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 可用的找回渠道（来自 /auth/methods 的 forgotChannels）；缺省按邮箱+手机号都可用处理 */
  channels?: string[]
}

export function ForgotPasswordDialog({ open, onOpenChange, channels }: Props) {
  const { t } = useTranslation()
  const [step, setStep] = useState<Step>(1)
  const [target, setTarget] = useState('')
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [show, setShow] = useState(false)
  const [left, setLeft] = useState(0)
  const [busy, setBusy] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})

  const emailOnly = channels?.length === 1 && channels[0] === 'email'
  const phoneOnly = channels?.length === 1 && channels[0] === 'phone'

  // 每次打开都从头开始
  useEffect(() => {
    if (!open) return
    setStep(1)
    setTarget('')
    setCode('')
    setPassword('')
    setConfirm('')
    setShow(false)
    setLeft(0)
    setBusy(false)
    setErrors({})
  }, [open])

  useEffect(() => {
    if (left <= 0) return
    const timer = setTimeout(() => setLeft((n) => n - 1), 1000)
    return () => clearTimeout(timer)
  }, [left])

  // 后端按 target 格式分渠道：含 @ 走邮件验证码，否则按手机号走短信验证码
  const targetError = () => {
    const v = target.trim()
    if (v.includes('@')) return EMAIL_RE.test(v) ? '' : t('forgot.emailInvalid')
    if (emailOnly) return t('forgot.emailInvalid')
    return PHONE_RE.test(v) ? '' : t(phoneOnly ? 'forgot.phoneInvalid' : 'forgot.targetInvalid')
  }

  const send = async (next?: Step) => {
    const err = targetError()
    if (err) {
      setErrors({ target: err })
      return
    }
    setErrors({})
    setBusy(true)
    try {
      const res = await api.post<{ cooldown: number }>('/auth/forgot/send-code', { target: target.trim() })
      toast.success(t('forgot.sent'))
      setLeft(res?.cooldown ?? COOLDOWN)
      if (next) setStep(next)
    } catch {
      // 错误已统一 toast
    } finally {
      setBusy(false)
    }
  }

  const reset = async () => {
    const next: Record<string, string> = {}
    if (!/^\d{6}$/.test(code)) next.code = t('forgot.codeInvalid')
    if (!password) next.password = t('forgot.passwordRequired')
    if (confirm !== password || !confirm) next.confirm = t('forgot.passwordMismatch')
    setErrors(next)
    if (Object.keys(next).length > 0) return
    setBusy(true)
    try {
      await api.post('/auth/forgot/reset', { target: target.trim(), code, newPassword: password })
      setStep(3)
    } catch {
      // 错误已统一 toast（如“验证码错误”）
    } finally {
      setBusy(false)
    }
  }

  const spinner = <Loader2 className="me-2 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />

  const footer =
    step === 1 ? (
      <>
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
          {t('forgot.cancel')}
        </Button>
        <Button type="button" disabled={busy} onClick={() => void send(2)}>
          {busy && spinner}
          {t('forgot.sendCode')}
        </Button>
      </>
    ) : step === 2 ? (
      <>
        <Button type="button" variant="outline" disabled={busy} onClick={() => setStep(1)}>
          {t('forgot.back')}
        </Button>
        <Button type="button" disabled={busy} onClick={() => void reset()}>
          {busy && spinner}
          {t('forgot.submit')}
        </Button>
      </>
    ) : (
      <Button type="button" onClick={() => onOpenChange(false)}>
        {t('forgot.backToLogin')}
      </Button>
    )

  const description =
    step === 1 ? t('forgot.verifyDesc') : step === 2 ? t('forgot.resetDesc', { target: target.trim() }) : undefined

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="sm"
      title={t('forgot.title')}
      description={description}
      footer={footer}
    >
      <p className="mb-4 text-xs text-muted-foreground">
        <span className="num">{t('forgot.stepIndicator', { current: step, total: 3 })}</span>
        {' · '}
        {t(step === 1 ? 'forgot.stepVerify' : step === 2 ? 'forgot.stepReset' : 'forgot.stepDone')}
      </p>

      {step === 1 && (
        <div className="space-y-5">
          <div className="space-y-2">
            <Label htmlFor="forgot-target">
              {t(emailOnly ? 'forgot.email' : phoneOnly ? 'forgot.phone' : 'forgot.target')}
            </Label>
            <Input
              id="forgot-target"
              type={emailOnly ? 'email' : 'text'}
              inputMode={phoneOnly ? 'tel' : undefined}
              autoComplete={emailOnly ? 'email' : 'off'}
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              placeholder={t(
                emailOnly ? 'forgot.emailPlaceholder' : phoneOnly ? 'forgot.phonePlaceholder' : 'forgot.targetPlaceholder',
              )}
              aria-invalid={!!errors.target}
            />
            {errors.target && <p className="text-sm text-destructive">{errors.target}</p>}
          </div>
        </div>
      )}

      {step === 2 && (
        <div className="space-y-5">
          <div className="space-y-2">
            <Label htmlFor="forgot-code">{t('forgot.code')}</Label>
            <div className="flex items-start gap-3">
              <Input
                id="forgot-code"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder={t('forgot.codePlaceholder')}
                aria-invalid={!!errors.code}
              />
              <Button
                type="button"
                variant="outline"
                className="w-28 shrink-0"
                disabled={left > 0 || busy}
                onClick={() => void send()}
              >
                {left > 0 ? <span className="num">{t('forgot.resendIn', { count: left })}</span> : t('forgot.resend')}
              </Button>
            </div>
            {errors.code && <p className="text-sm text-destructive">{errors.code}</p>}
          </div>
          <div className="space-y-2">
            <Label htmlFor="forgot-password">{t('forgot.newPassword')}</Label>
            <div className="relative">
              <Input
                id="forgot-password"
                type={show ? 'text' : 'password'}
                autoComplete="new-password"
                className="pe-12"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={t('forgot.newPasswordPlaceholder')}
                aria-invalid={!!errors.password}
              />
              <button
                type="button"
                onClick={() => setShow((s) => !s)}
                aria-label={show ? t('forgot.hidePassword') : t('forgot.showPassword')}
                className="absolute inset-y-0 end-0 flex w-11 items-center justify-center text-muted-foreground hover:text-foreground md:w-9"
              >
                {show ? <Eye className="size-4" aria-hidden="true" /> : <EyeOff className="size-4" aria-hidden="true" />}
              </button>
            </div>
            {errors.password && <p className="text-sm text-destructive">{errors.password}</p>}
          </div>
          <div className="space-y-2">
            <Label htmlFor="forgot-confirm">{t('forgot.confirmPassword')}</Label>
            <Input
              id="forgot-confirm"
              type={show ? 'text' : 'password'}
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              placeholder={t('forgot.confirmPasswordPlaceholder')}
              aria-invalid={!!errors.confirm}
            />
            {errors.confirm && <p className="text-sm text-destructive">{errors.confirm}</p>}
          </div>
        </div>
      )}

      {step === 3 && (
        <div className="flex flex-col items-center py-6 text-center">
          <div className="mb-4 flex size-14 items-center justify-center rounded-full bg-primary/10 text-primary">
            <CheckCircle2 className="size-8" aria-hidden="true" />
          </div>
          <h3 className="text-lg font-semibold text-foreground">{t('forgot.successTitle')}</h3>
          <p className="mt-1 text-sm text-muted-foreground">{t('forgot.successDesc')}</p>
        </div>
      )}
    </ResponsiveDialog>
  )
}
