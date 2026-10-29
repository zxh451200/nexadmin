import { useMemo, useState } from 'react'
import { Database, Gauge, KeyRound, Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ChartCard } from '@/framework/components/ChartCard'
import { MetricHistory } from '@/framework/components/MetricHistory'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { StatCard } from '@/framework/components/StatCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { useLiveMetrics } from '@/framework/hooks/use-live-metrics'
import { Badge } from '@/framework/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Skeleton } from '@/framework/ui/skeleton'
import type { CachePrefix, CacheSnapshot } from './api'

const INTERVALS = ['3000', '10000', '30000', '0'] as const

export default function CacheMonitorPage() {
  const { t, i18n } = useTranslation()
  const [interval, setIntervalMs] = useState<string>('3000')
  const { latest, history, isLoading } = useLiveMetrics<CacheSnapshot>('/monitor/cache', Number(interval), 30)
  const nf = useMemo(() => new Intl.NumberFormat(i18n.language), [i18n.language])

  const points = useMemo(
    () =>
      history.map((h) => ({
        time: new Date(h.ts).toLocaleTimeString(i18n.language, { hour12: false }),
        hit: Number(h.hitRate.toFixed(1)),
        qps: h.qps,
        mem: Math.round(h.usedMemoryMb),
      })),
    [history, i18n.language],
  )

  const prefixColumns: Column<CachePrefix>[] = [
    { key: 'prefix', title: t('cacheMonitor.prefix.name'), mobile: 'title', render: (p) => <span className="num">{p.prefix}</span> },
    { key: 'desc', title: t('cacheMonitor.prefix.desc'), mobile: 'sub', render: (p) => t(`cacheMonitor.prefix.d.${p.desc}`) },
    { key: 'keys', title: t('cacheMonitor.prefix.keys'), align: 'end', render: (p) => <span className="num">{nf.format(p.keys)}</span> },
    { key: 'ttl', title: t('cacheMonitor.prefix.ttl'), align: 'end', render: (p) => <span className="num">{p.ttl}</span> },
  ]

  const info: [string, string][] = latest
    ? [
        [t('cacheMonitor.info.driver'), latest.driver === 'valkey' ? 'Valkey' : t('cacheMonitor.info.memory')],
        [t('cacheMonitor.info.version'), latest.version],
        [t('cacheMonitor.info.uptime'), `${Math.floor(latest.uptimeSec / 86400)} ${t('cacheMonitor.info.days')}`],
        [t('cacheMonitor.info.connections'), String(latest.connections)],
        [t('cacheMonitor.info.maxMemory'), `${latest.maxMemoryMb} MB`],
      ]
    : []

  return (
    <PageContainer>
      <PageHeader
        title={t('cacheMonitor.title')}
        description={t('cacheMonitor.description')}
        actions={
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">{t('cacheMonitor.refresh.label')}</span>
            <Select value={interval} onValueChange={setIntervalMs}>
              <SelectTrigger className="w-32" aria-label={t('cacheMonitor.refresh.label')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {INTERVALS.map((v) => (
                  <SelectItem key={v} value={v}>
                    {t(`cacheMonitor.refresh.v${v}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        }
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 md:gap-4 xl:grid-cols-4">
        {isLoading || !latest ? (
          Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-32 w-full" />)
        ) : (
          <>
            <StatCard tone="chart-1"
              title={t('cacheMonitor.card.memory')}
              value={latest.usedMemoryMb.toFixed(0)}
              hint={`/ ${latest.maxMemoryMb} MB`}
              progress={(latest.usedMemoryMb / latest.maxMemoryMb) * 100}
              icon={<Database aria-hidden="true" />}
            />
            <StatCard tone="chart-2" title={t('cacheMonitor.card.keys')} value={latest.keys} icon={<KeyRound aria-hidden="true" />} />
            <StatCard tone="success"
              title={t('cacheMonitor.card.hitRate')}
              value={`${latest.hitRate.toFixed(1)}%`}
              progress={latest.hitRate}
              icon={<Gauge aria-hidden="true" />}
            />
            <StatCard tone="chart-5"
              title={t('cacheMonitor.card.qps')}
              value={latest.qps}
              hint="ops/s"
              spark={history.map((h) => h.qps)}
              icon={<Zap aria-hidden="true" />}
            />
          </>
        )}
      </div>

      <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-2">
        <ChartCard title={t('cacheMonitor.chart.hit')}>
          <TrendChart data={points} xKey="time" type="area" switchable={false} height={220} series={[{ key: 'hit', label: t('cacheMonitor.series.hit') }]} />
        </ChartCard>
        <ChartCard title={t('cacheMonitor.chart.qps')}>
          <TrendChart
            data={points}
            xKey="time"
            type="line"
            switchable={false}
            height={220}
            series={[{ key: 'qps', label: t('cacheMonitor.series.qps'), color: 'hsl(var(--chart-2))' }]}
          />
        </ChartCard>
      </div>

      <MetricHistory
        kind="cache"
        title={t('monitorHistory.title')}
        labels={{
          hitRate: t('cacheMonitor.series.hit'),
          qps: t('cacheMonitor.series.qps'),
          keys: t('cacheMonitor.card.keys'),
          usedMemoryMb: t('cacheMonitor.card.memory'),
          connections: t('cacheMonitor.info.connections'),
        }}
      />

      <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-3">
        <ChartCard title={t('cacheMonitor.chart.commands')} className="lg:col-span-2">
          <TrendChart
            data={(latest?.commands ?? []).map((c) => ({ name: c.name, count: c.count }))}
            xKey="name"
            type="bar"
            switchable={false}
            height={220}
            series={[{ key: 'count', label: t('cacheMonitor.series.count'), color: 'hsl(var(--chart-3))' }]}
          />
        </ChartCard>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center justify-between text-base font-medium">
              {t('cacheMonitor.info.title')}
              {latest ? <Badge variant="success">{t('cacheMonitor.info.running')}</Badge> : null}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {info.length === 0 ? (
              <Skeleton className="h-40 w-full" />
            ) : (
              <dl className="divide-y divide-border text-sm">
                {info.map(([k, v]) => (
                  <div key={k} className="flex items-center justify-between gap-4 py-2.5">
                    <dt className="text-muted-foreground">{k}</dt>
                    <dd className="num min-w-0 truncate text-end">{v}</dd>
                  </div>
                ))}
              </dl>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-base font-medium">{t('cacheMonitor.prefix.title')}</CardTitle>
        </CardHeader>
        <CardContent>
          <DataTable<CachePrefix> columns={prefixColumns} data={latest?.prefixes ?? []} rowKey="prefix" loading={!latest} pagination={false} />
        </CardContent>
      </Card>
    </PageContainer>
  )
}
