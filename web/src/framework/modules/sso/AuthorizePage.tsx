import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { AppWindow, Loader2, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuth } from '@/framework/auth/useAuth'
import { LanguageSwitch } from '@/framework/i18n/LanguageSwitch'
import { ThemeToggle } from '@/framework/theme/ThemeToggle'
import { Button } from '@/framework/ui/button'
import { Card, CardContent } from '@/framework/ui/card'
import { isHttpUrl, useAuthorize, useAuthorizeInfo } from './api'

const DEFAULT_SCOPE = 'openid profile email'

function hostOf(uri: string) {
  try {
    return new URL(uri).host
  } catch {
    return uri
  }
}

export default function AuthorizePage() {
  const { t } = useTranslation()
  const [params] = useSearchParams()
  const user = useAuth((s) => s.user)
  const authorize = useAuthorize()
  const [leaving, setLeaving] = useState(false)

  const clientId = params.get('client_id') ?? ''
  const redirectUri = params.get('redirect_uri') ?? ''
  const scope = params.get('scope') || DEFAULT_SCOPE
  const state = params.get('state') ?? undefined
  const nonce = params.get('nonce') ?? undefined
  const codeChallenge = params.get('code_challenge') ?? undefined
  const codeChallengeMethod = params.get('code_challenge_method') ?? undefined

  const missing = !clientId || !redirectUri
  const { data: info, isLoading, isError } = useAuthorizeInfo(clientId, redirectUri)
  // 只有 info 接口确认过 client_id + redirect_uri 合法，才允许任何跳转（含取消）
  const verified = !!info && isHttpUrl(redirectUri)

  const onApprove = async () => {
    if (!verified) return
    try {
      const res = await authorize.mutateAsync({ clientId, redirectUri, scope, state, nonce, codeChallenge, codeChallengeMethod })
      if (!isHttpUrl(res.redirectTo)) {
        toast.error(t('sso.authorize.badRedirect'))
        return
      }
      setLeaving(true)
      window.location.assign(res.redirectTo)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onCancel = () => {
    if (!verified) return
    const u = new URL(redirectUri)
    u.searchParams.set('error', 'access_denied')
    if (state) u.searchParams.set('state', state)
    setLeaving(true)
    window.location.assign(u.toString())
  }

  const busy = authorize.isPending || leaving
  const scopes = info?.scopes ?? []

  return (
    <div className="flex min-h-dvh flex-col bg-background">
      <header className="flex items-center justify-end gap-1 px-4 pt-4">
        <LanguageSwitch />
        <ThemeToggle />
      </header>
      <main className="flex flex-1 items-center justify-center px-4 pb-[max(2rem,env(safe-area-inset-bottom))] pt-4">
        <Card className="w-full max-w-md">
          <CardContent className="flex flex-col gap-6 p-6 md:p-8">
            {isLoading && !missing ? (
              <div className="flex items-center justify-center py-10" role="status" aria-live="polite">
                <Loader2 className="size-6 animate-spin text-primary motion-reduce:animate-none" aria-hidden="true" />
                <span className="sr-only">{t('common.loading')}</span>
              </div>
            ) : missing || isError || !info ? (
              <div className="flex flex-col items-center gap-3 py-6 text-center" role="alert">
                <ShieldAlert className="size-10 text-destructive" aria-hidden="true" />
                <h1 className="text-lg font-semibold text-foreground">{t('sso.authorize.invalidTitle')}</h1>
                <p className="text-sm text-muted-foreground">{t('sso.authorize.invalidDesc')}</p>
              </div>
            ) : (
              <>
                <div className="flex flex-col items-center gap-3 text-center">
                  {info.logo ? (
                    <img src={info.logo} alt="" className="size-16 rounded-xl border border-border bg-background object-contain" />
                  ) : (
                    <span className="flex size-16 items-center justify-center rounded-xl bg-primary/10 text-primary" aria-hidden="true">
                      <AppWindow className="size-8" />
                    </span>
                  )}
                  <h1 className="text-lg font-semibold text-foreground">{t('sso.authorize.title', { name: info.name })}</h1>
                  {user ? (
                    <p className="text-sm text-muted-foreground">{t('sso.authorize.account', { name: user.nickname || user.username })}</p>
                  ) : null}
                </div>

                <section aria-labelledby="sso-scope-title" className="rounded-md border border-border bg-muted/40 p-4">
                  <h2 id="sso-scope-title" className="text-sm font-medium text-foreground">
                    {t('sso.authorize.willGet')}
                  </h2>
                  <ul className="mt-2 list-disc space-y-1 ps-5 text-sm text-muted-foreground">
                    {scopes.map((s) => (
                      <li key={s}>{t(`sso.authorize.scope.${s}`, { defaultValue: s })}</li>
                    ))}
                  </ul>
                </section>

                <p className="text-center text-xs text-muted-foreground">{t('sso.authorize.redirectTo', { host: hostOf(redirectUri) })}</p>

                <div className="flex flex-col-reverse gap-2 md:flex-row md:justify-end">
                  <Button type="button" variant="outline" className="min-h-11 md:min-h-0" disabled={busy} onClick={onCancel}>
                    {t('sso.authorize.deny')}
                  </Button>
                  <Button type="button" className="min-h-11 md:min-h-0" disabled={busy} onClick={onApprove}>
                    {busy ? <Loader2 className="animate-spin motion-reduce:animate-none" aria-hidden="true" /> : null}
                    {t('sso.authorize.allow')}
                  </Button>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </main>
    </div>
  )
}
