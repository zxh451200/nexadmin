import { Puzzle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { usePermission } from '@/framework/auth/usePermission'
import { Button } from '@/framework/ui/button'

/** 直接打开未启用（未安装 / 未授权 / 已吊销）插件的页面时展示，代替空白页 + 一串 404 */
export default function PluginInactivePage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const navigate = useNavigate()
  return (
    <div className="flex min-h-[60dvh] flex-col items-center justify-center gap-3 px-4 py-12 text-center">
      <div className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <Puzzle className="size-6" aria-hidden="true" />
      </div>
      <h1 className="text-xl font-semibold text-foreground md:text-2xl">{t('error.pluginInactive.title')}</h1>
      <p className="max-w-md text-sm text-muted-foreground">{t('error.pluginInactive.desc')}</p>
      <div className="mt-3 flex flex-col gap-3 sm:flex-row">
        <Button variant="outline" onClick={() => navigate(-1)}>
          {t('common.back')}
        </Button>
        {has('plugin:list') && (
          <Button onClick={() => navigate('/system/plugin')}>{t('error.pluginInactive.manage')}</Button>
        )}
      </div>
    </div>
  )
}
