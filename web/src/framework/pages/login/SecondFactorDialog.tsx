import { useEffect, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { LoginChallenge } from '@/framework/auth/types'
import { useAuth } from '@/framework/auth/useAuth'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'

interface Props {
  /** 非空时显示；由登录接口返回的二次验证挑战 */
  challenge: LoginChallenge | null
  onVerified: () => void
  onCancel: () => void
  onChallenge: (challenge: LoginChallenge) => void
}

/** 登录需要二次验证时：输入 6 位验证码，或（服务端支持时）切换为恢复码。验证成功后建立登录态。 */
export function SecondFactorDialog({ challenge, onVerified, onCancel, onChallenge }: Props) {
  const { t } = useTranslation()
  const verifyLogin = useAuth((s) => s.verifyLogin)
  const [recovery, setRecovery] = useState(false)
  const [code, setCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const canRecovery = challenge?.methods.includes('recovery') ?? false

  // 换了一个挑战（重新登录）就清空输入
  useEffect(() => {
    setRecovery(false)
    setCode('')
    setError('')
  }, [challenge?.id])

  const submit = async () => {
    const value = code.trim()
    if (!challenge) return
    if (recovery ? value.length < 6 : !/^\d{6}$/.test(value)) {
      setError(t(recovery ? 'login.recoveryCodeInvalid' : 'login.otpInvalid'))
      return
    }
    setSubmitting(true)
    setError('')
    try {
      const step = await verifyLogin(challenge.id, value, recovery ? 'recovery' : undefined)
      if ('challenge' in step) onChallenge(step.challenge)
      else onVerified()
    } catch (e) {
      setCode('')
      setError(e instanceof Error ? e.message : t('login.otpFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <ResponsiveDialog
      open={challenge !== null}
      onOpenChange={(o) => {
        if (!o) onCancel()
      }}
      title={t('login.secondFactorTitle')}
      description={t(recovery ? 'login.recoveryDesc' : 'login.secondFactorDesc')}
      size="sm"
      footer={
        <>
          <Button type="button" variant="outline" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
          <Button type="button" disabled={submitting} onClick={() => void submit()}>
            {submitting && <Loader2 className="me-2 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />}
            {t('login.otpSubmit')}
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
        noValidate
      >
        {error ? (
          <Alert variant="destructive" role="alert">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <div className="flex flex-col gap-2">
          <Label htmlFor="second-factor-code">{t(recovery ? 'login.recoveryCode' : 'login.otpCode')}</Label>
          <Input
            id="second-factor-code"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            autoComplete="one-time-code"
            inputMode={recovery ? 'text' : 'numeric'}
            maxLength={recovery ? 64 : 6}
            placeholder={t(recovery ? 'login.recoveryCodePlaceholder' : 'login.otpPlaceholder')}
            autoFocus
          />
        </div>
        {canRecovery ? (
          <Button
            type="button"
            variant="link"
            className="h-auto self-start p-0"
            onClick={() => {
              setRecovery((r) => !r)
              setCode('')
              setError('')
            }}
          >
            {t(recovery ? 'login.useOtpCode' : 'login.useRecoveryCode')}
          </Button>
        ) : null}
        <button type="submit" className="sr-only" tabIndex={-1} aria-hidden="true" />
      </form>
    </ResponsiveDialog>
  )
}
