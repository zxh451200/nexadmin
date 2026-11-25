import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Activity, BellRing, Wifi, WifiOff } from 'lucide-react'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { StatCard } from '@/framework/components/StatCard'
import { ChartCard } from '@/framework/components/ChartCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { EmptyState } from '@/framework/components/EmptyState'
import { Segmented } from '@/framework/components/Segmented'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { formatAgo, formatTime } from './format'
import { useDeviceOverview, type AlarmLevel, type AlarmStatus, type DeviceStatus } from './api'

function levelVariant(level: AlarmLevel): 'warning' | 'destructive' | 'info' {
  if (level === 'critical') return 'destructive'
  if (level === 'warn') return 'warning'
  return 'info'
}

function statusVariant(status: DeviceStatus): 'success' | 'secondary' | 'outline' {
  if (status === 'online') return 'success'
  if (status === 'offline') return 'secondary'
  return 'outline'
}

function alarmStatusVariant(status: AlarmStatus): 'warning' | 'info' | 'secondary' {
  if (status === 'active') return 'warning'
  if (status === 'ack') return 'info'
  return 'secondary'
}

export default function OverviewPage() {
  const { t, i18n } = useTranslation()
  const [days, setDays] = useState<1 | 7 | 30>(7)
  const { data, isPending } = useDeviceOverview(days)
  const locale = i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US'
  const bucketLabel = (iso: string) =>
    new Intl.DateTimeFormat(locale, data?.bucket === 'hour'
      ? { hour: '2-digit', minute: '2-digit' }
      : { month: '2-digit', day: '2-digit' }).format(new Date(iso))

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_devicehub.overviewPage.title')}
        description={t('plugin_devicehub.overviewPage.description')}
        actions={
          <>
            <Segmented
              ariaLabel={t('plugin_devicehub.overviewPage.range')}
              value={String(days)}
              onChange={(v) => setDays(Number(v) as 1 | 7 | 30)}
              options={[
                { value: '1', label: t('plugin_devicehub.history.h24') },
                { value: '7', label: t('plugin_devicehub.history.d7') },
                { value: '30', label: t('plugin_devicehub.history.d30') },
              ]}
            />
            <Button asChild variant="outline" size="sm">
              <Link to="/devicehub/monitor">{t('plugin_devicehub.overviewPage.viewMonitor')}</Link>
            </Button>
          </>
        }
      />

      {isPending || !data ? (
        <>
          <div className="grid gap-3 md:grid-cols-2 md:gap-4 xl:grid-cols-4">
            {Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-28 w-full" />)}
          </div>
          <div className="grid gap-4 lg:grid-cols-2">
            <Skeleton className="h-72 w-full" />
            <Skeleton className="h-72 w-full" />
          </div>
        </>
      ) : (
        <>
          {data.sampled ? (
            <p className="rounded-md bg-warning/10 px-3 py-2 text-xs text-warning">
              {t('plugin_devicehub.overviewPage.sampled')}
            </p>
          ) : null}
          <div className="grid gap-3 md:grid-cols-2 md:gap-4 xl:grid-cols-4">
            <StatCard
              tone="success"
              title={t('plugin_devicehub.overviewPage.onlineRate')}
              value={`${data.devices.onlineRate.toFixed(1)}%`}
              hint={`${data.devices.online} / ${data.devices.online + data.devices.offline}`}
              progress={data.devices.onlineRate}
              spark={data.onlineTrend.map((p) => p.onlineRate)}
              icon={<Wifi aria-hidden="true" />}
            />
            <StatCard
              tone="destructive"
              title={t('plugin_devicehub.overviewPage.offline')}
              value={data.devices.offline}
              hint={data.devices.disabled > 0
                ? t('plugin_devicehub.overviewPage.disabledHint', { count: data.devices.disabled })
                : undefined}
              icon={<WifiOff aria-hidden="true" />}
            />
            <StatCard
              tone="warning"
              title={t('plugin_devicehub.overviewPage.unsettled')}
              value={data.alarms.unsettled.total}
              hint={t('plugin_devicehub.overviewPage.unsettledHint', {
                critical: data.alarms.unsettled.critical,
                warn: data.alarms.unsettled.warn,
              })}
              icon={<BellRing aria-hidden="true" />}
            />
            <StatCard
              tone="chart-2"
              title={t('plugin_devicehub.overviewPage.windowAlarms')}
              value={data.alarms.window.total}
              hint={t('plugin_devicehub.overviewPage.days', { days })}
              icon={<Activity aria-hidden="true" />}
            />
          </div>

          <div className="grid gap-4 lg:grid-cols-2">
            <ChartCard
              title={t('plugin_devicehub.overviewPage.onlineTrend')}
              summary={{
                value: `${data.devices.onlineRate.toFixed(1)}%`,
                label: t('plugin_devicehub.overviewPage.onlineRate'),
                hint: t('plugin_devicehub.overviewPage.days', { days }),
              }}
            >
              <TrendChart
                data={data.onlineTrend.map((p) => ({ label: bucketLabel(p.ts), rate: p.onlineRate }))}
                xKey="label"
                type="area"
                switchable={false}
                height={240}
                series={[{ key: 'rate', label: t('plugin_devicehub.overviewPage.onlineRate'), color: 'hsl(var(--success))' }]}
              />
            </ChartCard>
            <ChartCard
              title={t('plugin_devicehub.overviewPage.alarmTrend')}
              summary={{
                value: data.alarms.window.total,
                label: t('plugin_devicehub.overviewPage.windowAlarms'),
                hint: t('plugin_devicehub.overviewPage.days', { days }),
              }}
            >
              <TrendChart
                data={data.alarmTrend.map((p) => ({ label: bucketLabel(p.ts), critical: p.critical, warn: p.warn }))}
                xKey="label"
                type="bar"
                switchable={false}
                height={240}
                series={[
                  { key: 'critical', label: t('plugin_devicehub.alarm.level.critical'), color: 'hsl(var(--destructive))' },
                  { key: 'warn', label: t('plugin_devicehub.alarm.level.warn'), color: 'hsl(var(--warning))' },
                ]}
              />
            </ChartCard>
          </div>

          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader className="flex flex-row items-center justify-between gap-2 space-y-0 pb-2">
                <CardTitle className="text-base font-medium">{t('plugin_devicehub.overviewPage.topDevices')}</CardTitle>
                <Button asChild variant="ghost" size="sm">
                  <Link to="/devicehub/monitor">{t('plugin_devicehub.overviewPage.viewMonitor')}</Link>
                </Button>
              </CardHeader>
              <CardContent className="space-y-1">
                {data.topDevices.length === 0 ? (
                  <EmptyState title={t('plugin_devicehub.overviewPage.topEmpty')} className="py-6" />
                ) : (
                  data.topDevices.map((d) => (
                    <Link
                      key={d.deviceNo}
                      to={`/devicehub/monitor?device=${encodeURIComponent(d.deviceNo)}&tab=alarms`}
                      className="flex items-start justify-between gap-3 rounded-md p-2 transition-colors hover:bg-muted/50"
                    >
                      <span className="min-w-0">
                        <span className="flex flex-wrap items-center gap-1.5">
                          <span className="truncate text-sm font-medium">{d.name}</span>
                          <Badge variant={statusVariant(d.status)}>{t(`plugin_devicehub.status.${d.status}`)}</Badge>
                        </span>
                        <span className="mt-0.5 block truncate text-xs text-muted-foreground">
                          {d.lastError || d.deviceNo}
                        </span>
                        <span className="mt-1 block text-xs text-muted-foreground">
                          {t('plugin_devicehub.overviewPage.topCounts', {
                            count: d.unsettled,
                            window: d.alarmCount,
                          })}
                        </span>
                      </span>
                      <span className="shrink-0 text-end">
                        <span className="num block text-lg font-semibold">{d.unsettled}</span>
                        <span className="text-xs text-muted-foreground">{t('plugin_devicehub.overviewPage.unsettledShort')}</span>
                      </span>
                    </Link>
                  ))
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between gap-2 space-y-0 pb-2">
                <CardTitle className="text-base font-medium">{t('plugin_devicehub.overviewPage.recentAlarms')}</CardTitle>
                <Button asChild variant="ghost" size="sm">
                  <Link to="/devicehub/monitor?tab=alarms">{t('plugin_devicehub.overviewPage.viewAll')}</Link>
                </Button>
              </CardHeader>
              <CardContent className="space-y-1">
                {data.recentAlarms.length === 0 ? (
                  <EmptyState title={t('plugin_devicehub.overviewPage.recentEmpty')} className="py-6" />
                ) : (
                  data.recentAlarms.map((a) => (
                    <Link
                      key={a.id}
                      to={`/devicehub/monitor?device=${encodeURIComponent(a.deviceNo)}&tab=alarms`}
                      className="block rounded-md p-2 transition-colors hover:bg-muted/50"
                    >
                      <span className="flex flex-wrap items-center gap-1.5">
                        <Badge variant={levelVariant(a.level)}>{t(`plugin_devicehub.alarm.level.${a.level}`)}</Badge>
                        <Badge variant={alarmStatusVariant(a.status)}>{t(`plugin_devicehub.alarm.status.${a.status}`)}</Badge>
                        <span className="min-w-0 flex-1 truncate text-sm">{a.message}</span>
                      </span>
                      <span className="mt-1 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                        <span className="truncate">{a.deviceName || a.deviceNo}</span>
                        <span>·</span>
                        <span title={formatTime(a.createdAt, locale)}>{formatAgo(a.createdAt, locale)}</span>
                        {a.ackByName ? <span>· {t('plugin_devicehub.alarm.ackedBy', { name: a.ackByName })}</span> : null}
                      </span>
                    </Link>
                  ))
                )}
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </PageContainer>
  )
}
