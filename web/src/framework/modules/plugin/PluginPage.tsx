import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, KeyRound, Puzzle, Search } from 'lucide-react'
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
import { PluginFilterBar } from './FilterBar'
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

/** 市场目录未登记（第三方/手工安装）的已安装插件，单独归到「其他」分类。 */
const OTHER_CATEGORY = '_other'

function PluginCard({ p, canLicense, onLicense }: { p: PluginInfo; canLicense: boolean; onLicense: (p: PluginInfo) => void }) {
  const { t, i18n } = useTranslation()
  const fmt = (iso?: string | null) => (iso ? new Date(iso).toLocaleDateString(i18n.language) : '—')
  const shownName = t(`plugin_${p.id.replaceAll('-', '_')}.title`, { defaultValue: p.name })
  const shownDesc = p.description ? t(`plugin_${p.id.replaceAll('-', '_')}.description`, { defaultValue: p.description }) : ''
  const soon = isExpiringSoon(p)
  const rows: [string, string][] = [
    [t('pluginMgr.field.customer'), p.customer || '—'],
    [t('pluginMgr.field.expiresAt'), fmt(p.expiresAt)],
    ...(p.serial ? ([[t('pluginMgr.field.serial'), p.serial]] as [string, string][]) : []),
  ]
  return (
    <Card className="flex flex-col">
      <CardHeader className="pb-2">
        <CardTitle className="flex flex-wrap items-center gap-2 text-base font-medium">
          <span className="min-w-0 truncate">{shownName}</span>
          <span className="num text-xs font-normal text-muted-foreground">v{p.version}</span>
          <Badge variant={p.edition === 'pro' ? 'info' : 'secondary'} className="ms-auto">
            {t(`pluginMgr.edition.${p.edition}`)}
          </Badge>
        </CardTitle>
        {p.description ? <p className="line-clamp-2 text-sm text-muted-foreground">{shownDesc}</p> : null}
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={STATUS_VARIANT[p.status]}>{t(`pluginMgr.status.${p.status}`)}</Badge>
          {p.needRestart ? <Badge variant="warning">{t('pluginMgr.restartRequired')}</Badge> : null}
          {p.pendingHost ? <Badge variant="warning">{t('pluginMgr.pendingHost')}</Badge> : null}
          {p.stopping ? <Badge variant="warning">{t('pluginMgr.stopping')}</Badge> : null}
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
}

function InstalledTab({ onLicense, onGoMarket }: { onLicense: (p: PluginInfo) => void; onGoMarket: () => void }) {
  const { t } = useTranslation()
  const { has } = usePermission()
  const { data = [], isLoading } = usePlugins()
  const canLicense = has('plugin:license')
  const [keyword, setKeyword] = useState('')
  const [category, setCategory] = useState('')
  const expiring = data.filter((p) => isExpiringSoon(p))
  // 插件名与简介在后端是固定中文，内置插件在自己的语言包里给了译名；未收录的第三方插件回落到后端文案。
  const shownName = (p: PluginInfo) => t(`plugin_${p.id.replaceAll('-', '_')}.title`, { defaultValue: p.name })

  const categories = useMemo(() => {
    const set = new Set<string>()
    data.forEach((p) => set.add(p.category || OTHER_CATEGORY))
    return Array.from(set)
  }, [data])

  const list = useMemo(() => {
    const kw = keyword.trim().toLowerCase()
    return data.filter((p) => {
      if (category && (p.category || OTHER_CATEGORY) !== category) return false
      if (!kw) return true
      const translated = t(`plugin_${p.id.replaceAll('-', '_')}.title`, { defaultValue: p.name })
      return [p.name, translated, p.id, p.description ?? ''].some((s) => s.toLowerCase().includes(kw))
    })
  }, [data, keyword, category, t])

  const filtering = !!keyword.trim() || !!category
  const installed = list.filter((p) => !p.bundled)
  const builtin = list.filter((p) => p.bundled)

  const clearFilters = () => {
    setKeyword('')
    setCategory('')
  }

  return (
    <div className="flex flex-col gap-4">
      {expiring.length > 0 ? (
        <Alert variant="warning">
          <AlertTriangle aria-hidden="true" />
          <div>
            <AlertTitle>{t('pluginMgr.expiring.title')}</AlertTitle>
            <AlertDescription>
              {expiring
                .map((p) => t('pluginMgr.expiring.item', { name: shownName(p), days: daysUntil(p.expiresAt) }))
                .join(t('pluginMgr.expiring.sep'))}
            </AlertDescription>
          </div>
        </Alert>
      ) : null}

      {!isLoading && data.length > 0 ? (
        <PluginFilterBar
          keyword={keyword}
          onKeyword={setKeyword}
          placeholder={t('pluginMgr.searchInstalled')}
          category={category}
          onCategory={setCategory}
          categories={categories}
        />
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
              action={<code className="block max-w-full break-all rounded-md bg-muted px-3 py-2 text-start text-xs text-foreground">{t('pluginMgr.installCmd')}</code>}
            />
          </CardContent>
        </Card>
      ) : list.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-sm text-muted-foreground">
            <Search className="mx-auto mb-3 h-6 w-6" aria-hidden="true" />
            <p>{t('pluginMgr.market.nomatch')}</p>
            {filtering ? (
              <Button className="mt-3" size="sm" variant="outline" onClick={clearFilters}>
                {t('pluginMgr.clearFilters')}
              </Button>
            ) : null}
          </CardContent>
        </Card>
      ) : (
        <div className="flex flex-col gap-6">
          <section className="flex flex-col gap-3">
            <h2 className="text-sm font-medium text-muted-foreground">{t('pluginMgr.group.installed')}</h2>
            {installed.length > 0 ? (
              <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-2 xl:grid-cols-3">
                {installed.map((p) => (
                  <PluginCard key={p.id} p={p} canLicense={canLicense} onLicense={onLicense} />
                ))}
              </div>
            ) : (
              <Card>
                <EmptyState
                  icon={<Puzzle className="h-6 w-6" aria-hidden="true" />}
                  title={t('pluginMgr.empty.installed.title')}
                  description={t('pluginMgr.empty.installed.desc')}
                  action={
                    <Button variant="outline" size="sm" onClick={onGoMarket}>
                      {t('pluginMgr.empty.installed.action')}
                    </Button>
                  }
                />
              </Card>
            )}
          </section>
          {builtin.length > 0 ? (
            <section className="flex flex-col gap-3">
              <h2 className="flex flex-wrap items-baseline gap-2 text-sm font-medium text-muted-foreground">
                {t('pluginMgr.group.bundled')}
                <span className="num text-xs font-normal">{t('pluginMgr.group.bundledCount', { count: builtin.length })}</span>
              </h2>
              <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-2 xl:grid-cols-3">
                {builtin.map((p) => (
                  <PluginCard key={p.id} p={p} canLicense={canLicense} onLicense={onLicense} />
                ))}
              </div>
            </section>
          ) : null}
        </div>
      )}
    </div>
  )
}

export default function PluginPage() {
  const { t } = useTranslation()
  const { isSuper } = usePermission()
  if (!isSuper) {
    return (
      <PageContainer>
        <PageHeader title={t('pluginMgr.title')} />
        <EmptyState title={t('pluginMgr.platformOnly')} />
      </PageContainer>
    )
  }
  return <PlatformPluginPage />
}

function PlatformPluginPage() {
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
          <InstalledTab onLicense={setTarget} onGoMarket={() => setParams({ tab: 'market' })} />
        </TabsContent>
        <TabsContent value="market" className="mt-4 data-[state=inactive]:hidden" forceMount>
          <MarketTab focus={focus} />
        </TabsContent>
      </Tabs>

      <LicenseDialog plugin={target} onOpenChange={(o) => !o && setTarget(null)} />
    </PageContainer>
  )
}
