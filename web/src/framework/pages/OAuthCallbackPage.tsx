import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { clearPending, readPending, safeNext } from '@/framework/auth/oauth'
import type { AuthMethods, ChallengeResult, LoginChallenge, LoginResult, OAuthCallbackResult } from '@/framework/auth/types'
import { useAuth } from '@/framework/auth/useAuth'
import { api } from '@/framework/request/http'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Button } from '@/framework/ui/button'
import { BindAccountDialog } from './login/BindAccountDialog'
import { SecondFactorDialog } from './login/SecondFactorDialog'

/**
 * 第三方授权完成后的落地页（= auth.providers.<name>.redirect_uris 里的地址）：
 * 把 code + state 交给服务端换登录态；首次登录且匹配不上账号时弹「绑定已有账号」；
 * 个人中心发起的绑定（pending.mode = bind）完成后回到个人中心。
 */
export default function OAuthCallbackPage() {
  const { t } = useTranslation()
  const { provider = '' } = useParams()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const acceptLogin = useAuth((s) => s.acceptLogin)
  const [phase, setPhase] = useState<'working' | 'bind' | 'verify' | 'error'>('working')
  const [bindToken, setBindToken] = useState('')
  const [challenge, setChallenge] = useState<LoginChallenge | null>(null)
  const [message, setMessage] = useState('')
  const started = useRef(false) // state 是一次性的：StrictMode 下 effect 会跑两次，只能请求一次
  const next = useRef('/')

  const { data } = useQuery({
    queryKey: ['auth', 'methods'],
    queryFn: () => api.get<AuthMethods>('/auth/methods', undefined, { silent: true }),
    staleTime: 60_000,
  })
  const providerName = data?.providers.find((p) => p.key === provider)?.name ?? provider

  // 登录响应可能要求二次验证：有令牌则直接进入，否则弹出二次验证
  const proceed = useCallback(
    (res: LoginResult | ChallengeResult) => {
      const step = acceptLogin(res)
      if ('challenge' in step) {
        setChallenge(step.challenge)
        setPhase('verify')
      } else {
        navigate(next.current, { replace: true })
      }
    },
    [acceptLogin, navigate],
  )

  useEffect(() => {
    if (started.current) return
    started.current = true
    const pending = readPending()
    clearPending()
    next.current = safeNext(pending?.next)
    const code = params.get('code')
    const state = params.get('state')
    if (!code || !state || params.get('error')) {
      setMessage(t('login.oauthFailed'))
      setPhase('error')
      return
    }
    api
      .post<OAuthCallbackResult>(`/auth/oauth/${provider}/callback`, { code, state }, { silent: true })
      .then((res) => {
        if ('needBind' in res) {
          setBindToken(res.bindToken)
          setPhase('bind')
        } else if ('bound' in res) {
          toast.success(t('profile.bindings.bindDone'))
          navigate(next.current, { replace: true })
        } else {
          proceed(res)
        }
      })
      .catch((e: unknown) => {
        setMessage(e instanceof Error ? e.message : t('login.oauthFailed'))
        setPhase('error')
      })
  }, [params, provider, navigate, proceed, t])

  return (
    <main className="flex min-h-dvh items-center justify-center bg-background px-6">
      <div className="w-full max-w-sm text-center" role="status" aria-live="polite">
        {phase === 'error' ? (
          <div className="space-y-6">
            <Alert variant="destructive" role="alert">
              <AlertDescription>{message}</AlertDescription>
            </Alert>
            <Button asChild>
              <Link to="/login" replace>
                {t('login.backToLogin')}
              </Link>
            </Button>
          </div>
        ) : (
          <p className="flex items-center justify-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />
            {t('login.oauthProcessing')}
          </p>
        )}
      </div>
      <BindAccountDialog
        open={phase === 'bind'}
        providerName={providerName}
        bindToken={bindToken}
        onBound={proceed}
        onCancel={() => navigate('/login', { replace: true })}
      />
      <SecondFactorDialog
        challenge={phase === 'verify' ? challenge : null}
        onChallenge={setChallenge}
        onVerified={() => navigate(next.current, { replace: true })}
        onCancel={() => navigate('/login', { replace: true })}
      />
    </main>
  )
}
