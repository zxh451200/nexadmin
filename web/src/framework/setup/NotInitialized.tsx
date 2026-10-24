import { useNavigate } from 'react-router-dom'
import { ServerCog } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { LanguageSwitch } from '@/framework/i18n/LanguageSwitch'
import { ThemeToggle } from '@/framework/theme/ThemeToggle'
import { Button } from '@/framework/ui/button'

export default function NotInitialized() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  return (
    <div className="tech-grid relative flex min-h-dvh items-center justify-center bg-background p-4 pt-16 sm:pt-4">
      <div className="absolute end-4 top-4 flex items-center gap-1">
        <LanguageSwitch />
        <ThemeToggle />
      </div>
      <main className="glass glow w-full max-w-md rounded-xl border border-border p-6 text-center sm:p-8" role="alert">
        <div className="mx-auto mb-4 flex size-14 items-center justify-center rounded-2xl bg-warning/15 text-warning">
          <ServerCog className="size-7" aria-hidden="true" />
        </div>
        <h1 className="text-xl font-semibold text-foreground">{t('setup.notInitialized.title')}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{t('setup.notInitialized.desc')}</p>
        <Button className="mt-6 w-full" onClick={() => navigate('/setup')}>
          {t('setup.notInitialized.start')}
        </Button>
      </main>
    </div>
  )
}
