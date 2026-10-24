import { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { ChallengeResult, LoginResult } from '@/framework/auth/types'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { api } from '@/framework/request/http'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'

interface Props {
  open: boolean
  providerName: string
  bindToken: string
  onBound: (res: LoginResult | ChallengeResult) => void
  onCancel: () => void
}

/** 第三方账号在本地匹配不上时：输入一次已有账号的用户名和密码完成绑定，下次即可直接用第三方登录 */
export function BindAccountDialog({ open, providerName, bindToken, onBound, onCancel }: Props) {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const submit = async () => {
    if (!username.trim() || !password) {
      setError(t('login.bindRequired'))
      return
    }
    setSubmitting(true)
    setError('')
    try {
      const res = await api.post<LoginResult | ChallengeResult>('/auth/oauth/bind', { bindToken, username: username.trim(), password }, { silent: true })
      onBound(res)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('login.oauthFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onCancel()
      }}
      title={t('login.bindTitle')}
      description={t('login.bindDesc', { name: providerName })}
      size="sm"
      footer={
        <>
          <Button type="button" variant="outline" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
          <Button type="button" disabled={submitting} onClick={() => void submit()}>
            {submitting && <Loader2 className="me-2 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />}
            {t('login.bindSubmit')}
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
          <Label htmlFor="bind-username">{t('login.username')}</Label>
          <Input
            id="bind-username"
            autoComplete="username"
            placeholder={t('login.usernamePlaceholder')}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="bind-password">{t('login.password')}</Label>
          <Input
            id="bind-password"
            type="password"
            autoComplete="current-password"
            placeholder={t('login.passwordPlaceholder')}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        <button type="submit" className="sr-only" tabIndex={-1} aria-hidden="true" />
      </form>
    </ResponsiveDialog>
  )
}
