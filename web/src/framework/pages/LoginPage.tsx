import { useEffect, useRef, useState } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Hexagon, Languages, Sparkles, Users } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { resumeLoginContinuation, useAuth } from '@/framework/auth/useAuth'
import type { LoginContinuation } from '@/framework/auth/useAuth'
import type { AuthMethods, LoginChallenge } from '@/framework/auth/types'
import { LanguageSwitch } from '@/framework/i18n/LanguageSwitch'
import { api } from '@/framework/request/http'
import { ThemeToggle } from '@/framework/theme/ThemeToggle'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { ForgotPasswordDialog } from './login/ForgotPasswordDialog'
import { PasswordForm } from './login/PasswordForm'
import { PhoneForm } from './login/PhoneForm'
import { SecondFactorDialog } from './login/SecondFactorDialog'
import { SocialLogin } from './login/SocialLogin'

const FALLBACK: AuthMethods = { methods: ['password'], providers: [] }

interface PublicSettings {
  siteName?: string
  logoUrl?: string
  copyright?: string
}

const DEFAULT_NAME = 'NexAdmin'

const features: { icon: LucideIcon; title: string; desc: string }[] = [
  { icon: Users, title: 'loginPage.featureTenantTitle', desc: 'loginPage.featureTenantDesc' },
  { icon: Languages, title: 'loginPage.featureI18nTitle', desc: 'loginPage.featureI18nDesc' },
  { icon: Sparkles, title: 'loginPage.featureAiTitle', desc: 'loginPage.featureAiDesc' },
]

// 装饰动画的关键帧；减弱动态偏好下由 motion-reduce:animate-none 关闭
const keyframes = `
@keyframes login-float { 0%,100% { transform: translate3d(0,0,0) rotate(0deg); } 50% { transform: translate3d(0,-18px,0) rotate(8deg); } }
@keyframes login-float-slow { 0%,100% { transform: translate3d(0,0,0) rotate(0deg); } 50% { transform: translate3d(14px,12px,0) rotate(-10deg); } }
`

function Logo({ url, name, className }: { url?: string; name: string; className?: string }) {
  if (url) return <img src={url} alt={name} className={className} />
  return <Hexagon className={className} aria-hidden="true" />
}

function BrandPanel({ name, logoUrl, copyright }: { name: string; logoUrl?: string; copyright: string }) {
  const { t } = useTranslation()
  const mask = 'radial-gradient(ellipse at 30% 40%, hsl(var(--foreground)) 20%, transparent 75%)'
  return (
    <aside className="relative hidden overflow-hidden bg-brand-gradient text-primary-foreground lg:flex lg:w-[55%] lg:flex-col lg:justify-between lg:p-12 xl:p-16">
      <style>{keyframes}</style>
      <div className="dot-grid-light absolute inset-0" style={{ maskImage: mask, WebkitMaskImage: mask }} aria-hidden="true" />
      <div className="pointer-events-none absolute -start-24 -top-24 size-96 rounded-full bg-primary-foreground/20 blur-3xl" aria-hidden="true" />
      <div className="pointer-events-none absolute -bottom-32 end-0 size-[28rem] rounded-full bg-accent/30 blur-3xl" aria-hidden="true" />
      <div className="pointer-events-none absolute start-1/3 top-1/2 size-64 rounded-full bg-primary/40 blur-3xl" aria-hidden="true" />

      <Hexagon
        className="pointer-events-none absolute end-16 top-24 size-24 animate-[login-float_14s_ease-in-out_infinite] text-primary-foreground/25 motion-reduce:animate-none"
        strokeWidth={1}
        aria-hidden="true"
      />
      <Hexagon
        className="pointer-events-none absolute bottom-40 end-40 size-14 animate-[login-float-slow_18s_ease-in-out_infinite] fill-primary-foreground/10 text-primary-foreground/30 motion-reduce:animate-none"
        strokeWidth={1}
        aria-hidden="true"
      />
      <div
        className="pointer-events-none absolute start-12 top-1/2 size-28 animate-[login-float-slow_16s_ease-in-out_infinite] rounded-full border border-primary-foreground/25 motion-reduce:animate-none"
        aria-hidden="true"
      />
      <div
        className="pointer-events-none absolute bottom-24 start-1/2 size-16 animate-[login-float_12s_ease-in-out_infinite] rounded-full border-2 border-primary-foreground/20 motion-reduce:animate-none"
        aria-hidden="true"
      />

      <div className="relative flex items-center gap-3">
        <div className="flex size-10 items-center justify-center rounded-xl bg-primary-foreground/15">
          <Logo url={logoUrl} name={name} className="size-6 object-contain" />
        </div>
        <span className="text-lg font-semibold">{name}</span>
      </div>

      <div className="relative max-w-xl">
        <h2 className="text-5xl font-bold tracking-tight xl:text-6xl">{name}</h2>
        <p className="mt-4 text-2xl font-medium">{t('loginPage.slogan')}</p>
        <p className="mt-2 text-base opacity-80">{t('loginPage.sloganDesc')}</p>
        <ul className="mt-12 space-y-6">
          {features.map(({ icon: Icon, title, desc }) => (
            <li key={title} className="flex items-start gap-4">
              <span className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-primary-foreground/15">
                <Icon className="size-5" aria-hidden="true" />
              </span>
              <div>
                <p className="font-semibold">{t(title)}</p>
                <p className="mt-0.5 text-sm opacity-80">{t(desc)}</p>
              </div>
            </li>
          ))}
        </ul>
      </div>

      <p className="relative text-sm opacity-70">{copyright}</p>
    </aside>
  )
}

export default function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const token = useAuth((s) => s.token)
  const [forgotOpen, setForgotOpen] = useState(false)
  // 服务端要求二次验证时才会有值；没有注册二次验证的部署永远为 null，界面不变
  const [challenge, setChallenge] = useState<LoginChallenge | null>(null)
  const [continuing, setContinuing] = useState(true)
  const [continuationError, setContinuationError] = useState<{ message: string } | null>(null)
  const [continuationNext, setContinuationNext] = useState<string | null>(null)
  const continuation = useRef<Promise<LoginContinuation> | null | undefined>(undefined)

  useEffect(() => {
    let active = true
    // StrictMode 重跑 effect 时复用兑换请求，一次性 code 不能重复提交。
    try {
      if (continuation.current === undefined) continuation.current = resumeLoginContinuation()
      const pending = continuation.current
      if (!pending) {
        setContinuing(false)
        return
      }
      void pending.then((res) => {
        if (!active) return
        setContinuationNext(res.next)
        setChallenge(res.challenge)
      }).catch((err: unknown) => {
        if (active) setContinuationError({ message: err instanceof Error ? err.message : '' })
      }).finally(() => {
        if (active) setContinuing(false)
      })
    } catch {
      setContinuing(false)
      setContinuationError({ message: '' })
    }
    return () => { active = false }
  }, [])

  const raw = continuationNext ?? params.get('returnTo') ?? params.get('redirect') ?? '/'
  // 只允许站内相对路径，防开放重定向
  const redirect = raw.startsWith('/') && !raw.startsWith('//') && !raw.startsWith('/login') && !/[\\\u0000-\u0020\u007f]/.test(raw) ? raw : '/'

  const { data = FALLBACK } = useQuery({
    queryKey: ['auth', 'methods'],
    queryFn: () => api.get<AuthMethods>('/auth/methods', undefined, { silent: true }).catch(() => FALLBACK),
    staleTime: 60_000,
  })
  const methods = data.methods.length > 0 ? data.methods : FALLBACK.methods

  const { data: site } = useQuery({
    queryKey: ['settings', 'public'],
    queryFn: () => api.get<PublicSettings>('/settings/public', undefined, { silent: true }).catch(() => ({}) as PublicSettings),
    staleTime: 5 * 60_000,
  })
  const name = site?.siteName || DEFAULT_NAME
  const logoUrl = site?.logoUrl || undefined
  const copyright = site?.copyright || t('loginPage.defaultCopyright', { year: new Date().getFullYear(), name })

  if (token && !continuing && !challenge && !continuationError) return <Navigate to={redirect} replace />

  const done = () => navigate(redirect, { replace: true })
  const onForgot = data.forgotEnabled ? () => setForgotOpen(true) : undefined

  return (
    <div className="flex min-h-dvh flex-col bg-background lg:flex-row">
      <BrandPanel name={name} logoUrl={logoUrl} copyright={copyright} />

      <div className="flex flex-1 flex-col">
        <header className="flex items-center justify-between gap-3 px-5 pt-5 lg:hidden">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-brand-gradient text-primary-foreground glow">
              <Logo url={logoUrl} name={name} className="size-6 object-contain" />
            </div>
            <span className="truncate text-lg font-semibold text-foreground">{name}</span>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <LanguageSwitch />
            <ThemeToggle />
          </div>
        </header>

        <div className="hidden items-center justify-end gap-1 px-4 pt-5 lg:flex">
          <LanguageSwitch />
          <ThemeToggle />
        </div>

        <main className="flex flex-1 items-center justify-center px-6 pb-[max(2rem,env(safe-area-inset-bottom))] pt-4 lg:pb-12">
          <div className="w-full max-w-sm">
            <div className="mb-8 hidden size-10 items-center justify-center rounded-xl bg-primary text-primary-foreground lg:flex">
              <Logo url={logoUrl} name={name} className="size-5 object-contain" />
            </div>
            <div className="mb-8">
              <h1 className="text-3xl font-bold tracking-tight text-foreground">{t('loginPage.welcome')}</h1>
              <p className="mt-2 text-sm text-muted-foreground">{t('loginPage.welcomeHint')}</p>
            </div>
            {continuationError && <p role="alert" className="mb-4 text-sm text-destructive">{continuationError.message || t('login.otpFailed')}</p>}

            <fieldset disabled={continuing || challenge !== null}>
            {methods.length > 1 ? (
              <Tabs defaultValue={methods[0]}>
                <TabsList className="grid w-full grid-cols-2">
                  {methods.map((m) => (
                    <TabsTrigger key={m} value={m} className="min-h-11 md:min-h-0">
                      {t(m === 'password' ? 'login.tabPassword' : 'login.tabPhone')}
                    </TabsTrigger>
                  ))}
                </TabsList>
                <TabsContent value="password" className="mt-6">
                  <PasswordForm onSuccess={done} onForgot={onForgot} onChallenge={setChallenge} />
                </TabsContent>
                <TabsContent value="phone" className="mt-6">
                  <PhoneForm onSuccess={done} onChallenge={setChallenge} />
                </TabsContent>
              </Tabs>
            ) : methods[0] === 'phone' ? (
              <PhoneForm onSuccess={done} onChallenge={setChallenge} />
            ) : (
              <PasswordForm onSuccess={done} onForgot={onForgot} onChallenge={setChallenge} />
            )}

            <SocialLogin providers={data.providers} redirect={redirect} />
            </fieldset>

            <p className="mt-8 text-center text-xs text-muted-foreground lg:hidden">{copyright}</p>
          </div>
        </main>
      </div>

      <ForgotPasswordDialog open={forgotOpen} onOpenChange={setForgotOpen} />
      <SecondFactorDialog challenge={challenge} onChallenge={setChallenge} onVerified={() => { setChallenge(null); done() }} onCancel={() => {
        setChallenge(null)
        setContinuationNext(null)
      }} />
    </div>
  )
}
