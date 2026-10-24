import type { ComponentType } from 'react'
import { KeyRound } from 'lucide-react'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { startOAuthLogin } from '@/framework/auth/oauth'
import { Button } from '@/framework/ui/button'
import { DingtalkIcon, FeishuIcon, GithubIcon, GoogleIcon, OidcIcon, WecomIcon } from './BrandIcon'

type IconComp = ComponentType<{ className?: string; 'aria-hidden'?: boolean | 'true' | 'false' }>

// 企业微信、钉钉、Google、GitHub 用 Ant Design 图标库；OIDC 用 OpenID 官方标志；飞书用字节跳动 IconPark 的 new-lark 图标（见 BrandIcon.tsx）

const icons: Record<string, IconComp> = {
  wecom: WecomIcon,
  dingtalk: DingtalkIcon,
  feishu: FeishuIcon,
  google: GoogleIcon,
  github: GithubIcon,
  oidc: OidcIcon,
}

interface Props {
  providers: { key: string; name: string }[]
  redirect: string
}

export function SocialLogin({ providers, redirect }: Props) {
  const { t } = useTranslation()
  if (providers.length === 0) return null

  // 向服务端要授权地址（一次性 state、回调白名单都在服务端），再整页跳转；回来后由 /login/oauth/:provider 页完成登录
  const go = async (p: { key: string; name: string }) => {
    try {
      await startOAuthLogin(p.key, redirect)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('login.oauthStartFailed', { name: p.name }))
    }
  }

  return (
    <div className="mt-8">
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <span className="h-px flex-1 bg-border" />
        <span>{t('login.quickLogin')}</span>
        <span className="h-px flex-1 bg-border" />
      </div>
      <div className="mt-4 flex flex-wrap items-center justify-center gap-3">
        {providers.map((p) => {
          const Icon = icons[p.key] ?? KeyRound
          return (
            <Button
              key={p.key}
              type="button"
              variant="outline"
              size="icon"
              className="size-11 rounded-full"
              aria-label={t('login.loginWith', { name: p.name })}
              title={p.name}
              onClick={() => void go(p)}
            >
              <Icon className="size-5" aria-hidden="true" />
            </Button>
          )
        })}
      </div>
    </div>
  )
}
