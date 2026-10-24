import type { ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'

interface ErrorPageProps {
  code: string
  title: string
  description: string
  icon?: ReactNode
  /** 是否嵌入布局内（否则整页居中） */
  embedded?: boolean
}

export function ErrorPage({ code, title, description, icon, embedded = true }: ErrorPageProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  return (
    <div
      className={
        embedded
          ? 'flex min-h-[60dvh] flex-col items-center justify-center px-4 py-12 text-center'
          : 'tech-grid flex min-h-dvh flex-col items-center justify-center bg-background px-4 py-12 text-center'
      }
    >
      {icon && <div className="mb-4 text-muted-foreground">{icon}</div>}
      <p className="num text-6xl font-semibold text-primary md:text-7xl">{code}</p>
      <h1 className="mt-4 text-xl font-semibold text-foreground md:text-2xl">{title}</h1>
      <p className="mt-2 max-w-md text-sm text-muted-foreground">{description}</p>
      <div className="mt-6 flex flex-col gap-3 sm:flex-row">
        <Button variant="outline" onClick={() => navigate(-1)}>
          {t('common.back')}
        </Button>
        <Button onClick={() => navigate('/', { replace: true })}>{t('error.backHome')}</Button>
      </div>
    </div>
  )
}
