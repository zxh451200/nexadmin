import { Lock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { usePermission } from '@/framework/auth/usePermission'

/**
 * 平台共享配置的只读提示。后端这类写路由只认平台级调用方（表里没有 tenant_id，一行服务所有租户），
 * 界面就把写入口收掉并说清为什么——没有这行字，租户管理员只会以为自己的角色没配好去翻权限。
 * 平台调用方看不到任何东西，所以各对话框可以直接挂它。
 */
export function PlatformNotice({ className }: { className?: string }) {
  const { t } = useTranslation()
  const { isPlatform } = usePermission()
  if (isPlatform) return null
  return (
    <p className={cn('flex items-start gap-1.5 rounded-md border border-dashed p-2.5 text-xs text-muted-foreground', className)}>
      <Lock className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
      <span>{t('common.platformOnly')}</span>
    </p>
  )
}
