import { LayoutGrid } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Skeleton } from '@/framework/ui/skeleton'
import { isHttpUrl, usePortalApps, type PortalApp } from './api'

function AppLogo({ app }: { app: PortalApp }) {
  if (app.logo) {
    return <img src={app.logo} alt="" className="size-12 rounded-lg border border-border bg-background object-contain md:size-14" />
  }
  return (
    <span
      className="flex size-12 items-center justify-center rounded-lg bg-primary/10 text-lg font-semibold text-primary md:size-14 md:text-xl"
      aria-hidden="true"
    >
      {app.name.trim().charAt(0).toUpperCase()}
    </span>
  )
}

function AppCard({ app }: { app: PortalApp }) {
  const body = (
    <>
      <AppLogo app={app} />
      <span className="w-full truncate text-sm font-medium text-foreground">{app.name}</span>
      {app.description ? <span className="line-clamp-2 w-full text-xs text-muted-foreground">{app.description}</span> : null}
    </>
  )
  const cls =
    'card-edge flex min-h-11 flex-col items-center gap-2 rounded-lg border bg-card p-4 text-center transition-shadow duration-200'
  if (!isHttpUrl(app.homeUrl)) return <div className={`${cls} opacity-60`}>{body}</div>
  return (
    <a
      href={app.homeUrl}
      target="_blank"
      rel="noopener noreferrer"
      className={`${cls} hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring`}
    >
      {body}
    </a>
  )
}

export default function PortalPage() {
  const { t } = useTranslation()
  const { data, isLoading } = usePortalApps()

  return (
    <PageContainer>
      <PageHeader title={t('sso.portal.title')} description={t('sso.portal.description')} />
      {isLoading ? (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3 md:gap-4 lg:grid-cols-4 xl:grid-cols-5" role="status" aria-busy="true">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-32 w-full" />
          ))}
        </div>
      ) : !data || data.length === 0 ? (
        <EmptyState
          icon={<LayoutGrid className="h-6 w-6" aria-hidden="true" />}
          title={t('sso.portal.emptyTitle')}
          description={t('sso.portal.emptyDesc')}
        />
      ) : (
        <ul className="grid grid-cols-2 gap-3 md:grid-cols-3 md:gap-4 lg:grid-cols-4 xl:grid-cols-5">
          {data.map((app) => (
            <li key={app.id} className="flex">
              <div className="flex w-full flex-col [&>*]:flex-1">
                <AppCard app={app} />
              </div>
            </li>
          ))}
        </ul>
      )}
    </PageContainer>
  )
}
