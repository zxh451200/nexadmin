import { useEffect, useState } from 'react'
import { AlertTriangle, KeyRound, Puzzle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { usePermission } from '@/framework/auth/usePermission'
import { Alert, AlertDescription, AlertTitle } from '@/framework/ui/alert'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { daysUntil, isExpiringSoon, usePlugins, type PluginInfo, type PluginStatus } from './api'
import { LicenseDialog } from './LicenseDialog'
import MarketTab from './MarketTab'

const STATUS_VARIANT: Record<PluginStatus, 'success' | 'warning' | 'destructive'> = {
  active: 'success',
  unlicensed: 'warning',
  expired: 'destructive',
  invalid: 'destructive',
  revoked: 'destructive',
  disabled: 'warning',
}

const INSTALL_CMD = 'go run ./cmd/nexadmin plugin install <目录或zip>'

function InstalledTab({ onLicense }: { onLicense: (p: PluginInfo) => void }) {
  const { t, i18n } = useTranslation()
  const { has } = usePermission()
  const { data = [], isLoading } = usePlugins()
  const canLicense = has('plugin:license')
  const expiring = data.filter((p) => isExpiringSoon(p))
  const fmt = (iso?: string | null) => (iso ? new Date(iso).toLocaleDateString(i18n.language) : '—')

  return (
    <div className="flex flex-col gap-4">
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
                    <Button variant="outline" size="sm" className="mt-auto w-full sm:w-auto sm:self-end" onClick={() => onLicense(p)}>
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
    </div>
  )
}

export default function PluginPage() {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams()
  const [target, setTarget] = useState<PluginInfo | null>(null)
  // 官网插件市场的“一键安装”深链：/system/plugin?tab=market&plugin=<id>
  const initialTab = params.get('tab') === 'market' ? 'market' : 'installed'
  const [focus] = useState(() => params.get('plugin') ?? '')

  // 深链只消费一次：清掉 URL 参数，避免刷新/切回标签时再次套用筛选。
  useEffect(() => {
    if (!params.has('plugin')) return
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.delete('plugin')
        return next
      },
      { replace: true },
    )
  }, [params, setParams])

  return (
    <PageContainer>
      <PageHeader title={t('pluginMgr.title')} description={t('pluginMgr.description')} />
      <Tabs defaultValue={initialTab}>
        <TabsList>
          <TabsTrigger value="installed">{t('pluginMgr.tab.installed')}</TabsTrigger>
          <TabsTrigger value="market">{t('pluginMgr.tab.market')}</TabsTrigger>
        </TabsList>
        <TabsContent value="installed" className="mt-4 data-[state=inactive]:hidden" forceMount>
          <InstalledTab onLicense={setTarget} />
        </TabsContent>
        <TabsContent value="market" className="mt-4 data-[state=inactive]:hidden" forceMount>
          <MarketTab focus={focus} />
        </TabsContent>
      </Tabs>

      <LicenseDialog plugin={target} onOpenChange={(o) => !o && setTarget(null)} />
    </PageContainer>
  )
}
