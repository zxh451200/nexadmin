import { useState } from 'react'
import { TriangleAlert, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/framework/auth/useAuth'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Button } from '@/framework/ui/button'

const DISMISS_KEY = 'nex-tenant-expire-dismissed'

/** 租户即将到期提示条：服务端在 /auth/me 里带 tenantExpireSoon 时显示；关闭后本次会话内不再出现 */
export function TenantExpireBanner() {
  const { t, i18n } = useTranslation()
  const soon = useAuth((s) => s.user?.tenantExpireSoon)
  const [dismissed, setDismissed] = useState(() => sessionStorage.getItem(DISMISS_KEY) === '1')
  if (!soon || dismissed) return null

  const date = new Date(soon.expireAt).toLocaleDateString(i18n.language)
  const text = soon.daysLeft > 0 ? t('tenant.expireSoon', { count: soon.daysLeft, date }) : t('tenant.expireSoonToday', { date })
  return (
    <Alert variant="warning" className="flex items-center gap-2 rounded-none border-x-0 border-t-0 px-4 py-2">
      <TriangleAlert className="size-4 shrink-0" aria-hidden="true" />
      <AlertDescription className="min-w-0 flex-1">{text}</AlertDescription>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-8 shrink-0"
        aria-label={t('tenant.expireSoonDismiss')}
        onClick={() => {
          sessionStorage.setItem(DISMISS_KEY, '1')
          setDismissed(true)
        }}
      >
        <X className="size-4" aria-hidden="true" />
      </Button>
    </Alert>
  )
}
