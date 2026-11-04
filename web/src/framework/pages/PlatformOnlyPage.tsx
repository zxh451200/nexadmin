import { Building2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Button } from '@/framework/ui/button'

/**
 * 直接打开「平台级插件」后台页时展示，代替一串必然 403 的请求 + 错误信封。
 * 这类插件的表没有 tenant_id（在线客服的会话与消息、官网的内容与留言），数据不分租户，
 * 后台只对平台调用方开放——单租户部署里人人都是平台调用方，这页只在多租户下出现。
 */
export default function PlatformOnlyPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  return (
    <div className="flex min-h-[60dvh] flex-col items-center justify-center gap-3 px-4 py-12 text-center">
      <div className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <Building2 className="size-6" aria-hidden="true" />
      </div>
      <h1 className="text-xl font-semibold text-foreground md:text-2xl">{t('error.platformOnly.title')}</h1>
      <p className="max-w-md text-sm text-muted-foreground">{t('error.platformOnly.desc')}</p>
      <div className="mt-3 flex flex-col gap-3 sm:flex-row">
        <Button variant="outline" onClick={() => navigate(-1)}>
          {t('common.back')}
        </Button>
        <Button onClick={() => navigate('/', { replace: true })}>{t('error.backHome')}</Button>
      </div>
    </div>
  )
}
