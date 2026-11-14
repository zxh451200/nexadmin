import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { isOAuthRedirectRejected, oauthRedirectUri, startOAuthLogin } from '@/framework/auth/oauth'
import { ProviderIcon } from '@/framework/components/BrandIcon'
import { Button } from '@/framework/ui/button'

// 企业微信、钉钉、Google、GitHub 用 Ant Design 图标库；OIDC 用 OpenID 官方标志；飞书用字节跳动 IconPark 的 new-lark 图标（见 components/BrandIcon.tsx）

interface Props {
  providers: { key: string; name: string }[]
  redirect: string
  /** register：注册页的快捷注册，服务端按自助注册建号（见 oauth.ts 的 intent） */
  intent?: 'login' | 'register'
}

export function SocialLogin({ providers, redirect, intent = 'login' }: Props) {
  const { t } = useTranslation()
  if (providers.length === 0) return null
  const register = intent === 'register'

  // 向服务端要授权地址（一次性 state、回调白名单都在服务端），再整页跳转；回来后由 /login/oauth/:provider 页完成登录
  const go = async (p: { key: string; name: string }) => {
    try {
      await startOAuthLogin(p.key, redirect, intent)
    } catch (e) {
      // 请求层已经静音，这里只弹一条：白名单拒绝时给出可照做的地址，其余沿用服务端文案
      toast.error(
        isOAuthRedirectRejected(e)
          ? t('login.oauthRedirectNotAllowed', { url: oauthRedirectUri(p.key), name: p.name })
          : e instanceof Error
            ? e.message
            : t(register ? 'login.register.oauthStartFailed' : 'login.oauthStartFailed', { name: p.name }),
      )
    }
  }

  return (
    <div className="mt-8">
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <span className="h-px flex-1 bg-border" />
        <span>{t(register ? 'login.register.quickRegister' : 'login.quickLogin')}</span>
        <span className="h-px flex-1 bg-border" />
      </div>
      <div className="mt-4 flex flex-wrap items-center justify-center gap-3">
        {providers.map((p) => (
          <Button
            key={p.key}
            type="button"
            variant="outline"
            size="icon"
            className="size-11 rounded-full"
            aria-label={t(register ? 'login.register.registerWith' : 'login.loginWith', { name: p.name })}
            title={p.name}
            onClick={() => void go(p)}
          >
            <ProviderIcon provider={p.key} className="size-5" />
          </Button>
        ))}
      </div>
      {register && <p className="mt-3 text-center text-xs text-muted-foreground">{t('login.register.socialHint')}</p>}
    </div>
  )
}
