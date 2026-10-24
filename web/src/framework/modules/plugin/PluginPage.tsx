import { useState } from 'react'
import { AlertTriangle, KeyRound, Puzzle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { usePermission } from '@/framework/auth/usePermission'
import { Alert, AlertDescription, AlertTitle } from '@/framework/ui/alert'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { daysUntil, isExpiringSoon, usePlugins, type PluginInfo, type PluginStatus } from './api'
import { LicenseDialog } from './LicenseDialog'

const STATUS_VARIANT: Record<PluginStatus, 'success' | 'warning' | 'destructive'> = {
  active: 'success',
  unlicensed: 'warning',
  expired: 'destructive',
  invalid: 'destructive',
  revoked: 'destructive',
}

const INSTALL_CMD = 'go run ./cmd/nexadmin plugin install <目录或zip>'

export default function PluginPage() {
  const { t, i18n } = useTranslation()
  const { has } = usePermission()
  const { data = [], isLoading } = usePlugins()
  const [target, setTarget] = useState<PluginInfo | null>(null)
  const canLicense = has('plugin:license')
  const expiring = data.filter((p) => isExpiringSoon(p))
  const fmt = (iso?: string | null) => (iso ? new Date(iso).toLocaleDateString(i18n.language) : '—')

  return (
    <PageContainer>
      <PageHeader title={t('pluginMgr.title')} description={t('pluginMgr.description')} />

      {expiring.length > 0 ? (
        <Alert variant="warning">
          <AlertTriangle aria-hidden="true" />
          <div>
            <AlertTitle>{t('pluginMgr.expiring.title')}</AlertTitle>
            <AlertDescription>
              {expiring
                .map((p) => t('pluginMgr.expiring.item', { name: p.name, days: daysUntil(p.expiresAt) }))
                .join(t('pluginMgr.expiring.sep'))}
            </AlertDescription>
          </div>
        </Alert>
      ) : null}

      {isLoading ? (
        <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={i} className="h-48 w-full" />
          ))}
        </div>
      ) : data.length === 0 ? (
        <Card>
          <CardContent>
            <EmptyState
              icon={<Puzzle className="h-6 w-6" aria-hidden="true" />}
              title={t('pluginMgr.empty.title')}
              description={t('pluginMgr.empty.desc')}
              action={<code className="block max-w-full break-all rounded-md bg-muted px-3 py-2 text-start text-xs text-foreground">{INSTALL_CMD}</code>}
            />
          </CardContent>
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-2 xl:grid-cols-3">
          {data.map((p) => {
            const soon = isExpiringSoon(p)
            const rows: [string, string][] = [
              [t('pluginMgr.field.customer'), p.customer || '—'],
              [t('pluginMgr.field.expiresAt'), fmt(p.expiresAt)],
              ...(p.serial ? ([[t('pluginMgr.field.serial'), p.serial]] as [string, string][]) : []),
            ]
            return (
              <Card key={p.id} className="flex flex-col">
                <CardHeader className="pb-2">
                  <CardTitle className="flex flex-wrap items-center gap-2 text-base font-medium">
                    <span className="min-w-0 truncate">{p.name}</span>
                    <span className="num text-xs font-normal text-muted-foreground">v{p.version}</span>
                    <Badge variant={p.edition === 'pro' ? 'info' : 'secondary'} className="ms-auto">
                      {t(`pluginMgr.edition.${p.edition}`)}
                    </Badge>
                  </CardTitle>
                  {p.description ? <p className="line-clamp-2 text-sm text-muted-foreground">{p.description}</p> : null}
                </CardHeader>
                <CardContent className="flex flex-1 flex-col gap-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant={STATUS_VARIANT[p.status]}>{t(`pluginMgr.status.${p.status}`)}</Badge>
                    {soon ? <Badge variant="warning">{t('pluginMgr.expiring.badge', { days: daysUntil(p.expiresAt) })}</Badge> : null}
                  </div>
                  <dl className="divide-y divide-border text-sm">
                    {rows.map(([k, v]) => (
                      <div key={k} className="flex items-center justify-between gap-4 py-2">
                        <dt className="shrink-0 text-muted-foreground">{k}</dt>
                        <dd className="num min-w-0 truncate text-end">{v}</dd>
                      </div>
                    ))}
                    {p.reason ? (
                      <div className="flex items-start justify-between gap-4 py-2">
                        <dt className="shrink-0 text-muted-foreground">{t('pluginMgr.field.reason')}</dt>
                        <dd className="min-w-0 break-words text-end text-destructive">{p.reason}</dd>
                      </div>
                    ) : null}
                  </dl>
                  {canLicense && p.edition === 'pro' ? (
                    <Button variant="outline" size="sm" className="mt-auto w-full sm:w-auto sm:self-end" onClick={() => setTarget(p)}>
                      <KeyRound aria-hidden="true" />
                      {t('pluginMgr.license.action')}
                    </Button>
                  ) : null}
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}

      <LicenseDialog plugin={target} onOpenChange={(o) => !o && setTarget(null)} />
    </PageContainer>
  )
}
