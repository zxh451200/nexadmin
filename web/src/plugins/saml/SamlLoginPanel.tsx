import { useQuery } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'

interface SamlSource {
  id: string
  name: string
}

/** 登录页的企业单点登录入口：只列出本租户已启用的 SAML 源。 */
export default function SamlLoginPanel() {
  const { t } = useTranslation()
  const { data = [] } = useQuery({
    queryKey: ['auth', 'saml-sources'],
    queryFn: () => api.get<SamlSource[]>('/auth/saml/sources', undefined, { silent: true }).catch(() => [] as SamlSource[]),
    staleTime: 60_000,
  })
  if (data.length === 0) return null

  const start = (id: string) => {
    // 回到当前登录页（含部署前缀），ACS 成功后带回一次性交换码。
    const back = window.location.pathname
    window.location.href = `/api/v1/auth/saml/${encodeURIComponent(id)}/login?redirect=${encodeURIComponent(back)}`
  }

  return (
    <div className="mt-6">
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <span className="h-px flex-1 bg-border" />
        <span>{t('plugin_saml.ssoLogin')}</span>
        <span className="h-px flex-1 bg-border" />
      </div>
      <div className="mt-4 flex flex-wrap items-center justify-center gap-3">
        {data.map((s) => (
          <Button key={s.id} type="button" variant="outline" size="sm" title={s.name} onClick={() => start(s.id)}>
            <KeyRound className="size-4" aria-hidden="true" />
            {s.name}
          </Button>
        ))}
      </div>
    </div>
  )
}
