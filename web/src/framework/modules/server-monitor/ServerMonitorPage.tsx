import { useMemo, useState } from 'react'
import { Cpu, HardDrive, MemoryStick, Timer } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ChartCard } from '@/framework/components/ChartCard'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { StatCard } from '@/framework/components/StatCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { useLiveMetrics } from '@/framework/hooks/use-live-metrics'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Skeleton } from '@/framework/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import type { DiskInfo, ServerSnapshot } from './api'
import { ClientErrorsTab } from './ClientErrorsTab'
import { HealthCard } from './HealthCard'
import { MetricHistory } from '@/framework/components/MetricHistory'

const INTERVALS = ['3000', '10000', '30000', '0'] as const

function formatUptime(sec: number, d: string, h: string, m: string) {
  const days = Math.floor(sec / 86400)
  const hours = Math.floor((sec % 86400) / 3600)
  const mins = Math.floor((sec % 3600) / 60)
  return days > 0 ? `${days}${d} ${hours}${h}` : `${hours}${h} ${mins}${m}`
}

export default function ServerMonitorPage() {
  const { t, i18n } = useTranslation()
  const [interval, setIntervalMs] = useState<string>('3000')
  const { latest, history, isLoading } = useLiveMetrics<ServerSnapshot>('/monitor/server', Number(interval), 30)

  const points = useMemo(
    () =>
      history.map((h) => ({
        time: new Date(h.ts).toLocaleTimeString(i18n.language, { hour12: false }),
        cpu: Number(h.cpu.usage.toFixed(1)),
        mem: Number(((h.memory.usedGb / h.memory.totalGb) * 100).toFixed(1)),
        inKbps: Math.round(h.network.inKbps),
        outKbps: Math.round(h.network.outKbps),
        goroutines: h.runtime.goroutines,
        heap: Math.round(h.runtime.heapMb),
        gc: Number(h.runtime.gcPauseMs.toFixed(2)),
      })),
    [history, i18n.language],
  )

  const disks = latest?.disks ?? []
  const diskTotal = disks.reduce((s, d) => s + d.totalGb, 0)
  const diskUsed = disks.reduce((s, d) => s + d.usedGb, 0)

  const diskColumns: Column<DiskInfo>[] = [
    { key: 'mount', title: t('serverMonitor.disk.mount'), mobile: 'title', render: (d) => <span className="num">{d.mount}</span> },
    { key: 'total', title: t('serverMonitor.disk.total'), render: (d) => <span className="num">{d.totalGb} GB</span> },
    { key: 'used', title: t('serverMonitor.disk.used'), render: (d) => <span className="num">{d.usedGb.toFixed(1)} GB</span> },
    {
      key: 'usage',
      title: t('serverMonitor.disk.usage'),
      mobile: 'sub',
      render: (d) => {
        const pct = (d.usedGb / d.totalGb) * 100
        return (
          <div className="flex min-w-36 items-center gap-2">
            <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100}>
              <div className="h-full rounded-full bg-brand-gradient" style={{ width: `${pct}%` }} />
            </div>
            <span className="num w-12 text-end text-xs text-muted-foreground">{pct.toFixed(0)}%</span>
          </div>
        )
      },
    },
  ]

  const info: [string, string][] = latest
    ? [
        [t('serverMonitor.info.os'), latest.info.os],
        [t('serverMonitor.info.arch'), latest.info.arch],
        [t('serverMonitor.info.goVersion'), latest.info.goVersion],
        [t('serverMonitor.info.hostname'), latest.info.hostname],
        [t('serverMonitor.info.ip'), latest.info.ip],
        [t('serverMonitor.info.startedAt'), new Date(latest.info.startedAt).toLocaleString(i18n.language, { hour12: false })],
      ]
    : []

  return (
    <PageContainer>
      <PageHeader
        title={t('serverMonitor.title')}
        description={t('serverMonitor.description')}
        actions={
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">{t('serverMonitor.refresh.label')}</span>
            <Select value={interval} onValueChange={setIntervalMs}>
              <SelectTrigger className="w-32" aria-label={t('serverMonitor.refresh.label')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {INTERVALS.map((v) => (
                  <SelectItem key={v} value={v}>
                    {t(`serverMonitor.refresh.v${v}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        }
      />

      <Tabs defaultValue="metrics" className="flex flex-col gap-3 md:gap-5">
        <TabsList className="w-full flex-wrap overflow-visible sm:w-fit">
          <TabsTrigger value="metrics" className="flex-1 sm:flex-none">
            {t('serverMonitor.tabs.metrics')}
          </TabsTrigger>
          <TabsTrigger value="clientErrors" className="flex-1 sm:flex-none">
            {t('serverMonitor.tabs.clientErrors')}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="metrics" className="mt-0 flex flex-col gap-3 md:gap-5">
          <HealthCard />
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 md:gap-4 xl:grid-cols-4">
            {isLoading || !latest ? (
              Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-32 w-full" />)
            ) : (
              <>
                <StatCard tone="chart-1"
                  title={t('serverMonitor.card.cpu')}
                  value={`${latest.cpu.usage.toFixed(1)}%`}
                  hint={`${latest.cpu.cores} ${t('serverMonitor.cores')}`}
                  progress={latest.cpu.usage}
                  icon={<Cpu aria-hidden="true" />}
                />
                <StatCard tone="chart-2"
                  title={t('serverMonitor.card.memory')}
                  value={latest.memory.usedGb.toFixed(1)}
                  hint={`/ ${latest.memory.totalGb} GB`}
                  progress={(latest.memory.usedGb / latest.memory.totalGb) * 100}
                  icon={<MemoryStick aria-hidden="true" />}
                />
                <StatCard tone="chart-4"
                  title={t('serverMonitor.card.disk')}
                  value={diskUsed.toFixed(0)}
                  hint={`/ ${diskTotal.toFixed(0)} GB`}
                  progress={diskTotal ? (diskUsed / diskTotal) * 100 : 0}
                  icon={<HardDrive aria-hidden="true" />}
                />
                <StatCard tone="chart-5"
                  title={t('serverMonitor.card.uptime')}
                  value={formatUptime(latest.info.uptimeSec, t('serverMonitor.unit.d'), t('serverMonitor.unit.h'), t('serverMonitor.unit.m'))}
                  hint={`${t('serverMonitor.card.load')} ${latest.cpu.load1.toFixed(2)}`}
                  icon={<Timer aria-hidden="true" />}
                />
              </>
            )}
          </div>

          <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-2">
            <ChartCard title={t('serverMonitor.chart.cpuMem')}>
              <TrendChart
                data={points}
                xKey="time"
                type="area"
                switchable={false}
                height={240}
                series={[
                  { key: 'cpu', label: t('serverMonitor.series.cpu') },
                  { key: 'mem', label: t('serverMonitor.series.mem') },
                ]}
              />
            </ChartCard>
            <ChartCard title={t('serverMonitor.chart.network')}>
              <TrendChart
                data={points}
                xKey="time"
                type="line"
                switchable={false}
                height={240}
                series={[
                  { key: 'inKbps', label: t('serverMonitor.series.in') },
                  { key: 'outKbps', label: t('serverMonitor.series.out') },
                ]}
              />
            </ChartCard>
          </div>

          <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-3">
            <ChartCard title={t('serverMonitor.chart.goroutines')}>
              <TrendChart data={points} xKey="time" type="area" switchable={false} height={180} series={[{ key: 'goroutines', label: t('serverMonitor.series.goroutines') }]} />
            </ChartCard>
            <ChartCard title={t('serverMonitor.chart.heap')}>
              <TrendChart data={points} xKey="time" type="area" switchable={false} height={180} series={[{ key: 'heap', label: t('serverMonitor.series.heap'), color: 'hsl(var(--chart-2))' }]} />
            </ChartCard>
            <ChartCard title={t('serverMonitor.chart.gc')}>
              <TrendChart data={points} xKey="time" type="bar" switchable={false} height={180} series={[{ key: 'gc', label: t('serverMonitor.series.gc'), color: 'hsl(var(--chart-3))' }]} />
              {latest ? (
                <p className="num mt-2 text-xs text-muted-foreground">
                  {t('serverMonitor.gcCount')}: {latest.runtime.gcCount}
                </p>
              ) : null}
            </ChartCard>
          </div>

          <MetricHistory
            kind="server"
            title={t('monitorHistory.title')}
            labels={{
              cpuUsage: t('serverMonitor.series.cpu'),
              memUsedPct: t('serverMonitor.series.mem'),
              diskUsedPct: t('serverMonitor.card.disk'),
              load1: t('serverMonitor.card.load'),
              netInKbps: t('serverMonitor.series.in'),
              netOutKbps: t('serverMonitor.series.out'),
              goroutines: t('serverMonitor.series.goroutines'),
              heapMb: t('serverMonitor.series.heap'),
              gcPauseMs: t('serverMonitor.series.gc'),
            }}
          />

          <div className="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-3">
            <Card className="lg:col-span-1">
              <CardHeader className="pb-2">
                <CardTitle className="text-base font-medium">{t('serverMonitor.info.title')}</CardTitle>
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
            <Card className="lg:col-span-2">
              <CardHeader className="pb-2">
                <CardTitle className="text-base font-medium">{t('serverMonitor.disk.title')}</CardTitle>
              </CardHeader>
              <CardContent>
                <DataTable<DiskInfo> columns={diskColumns} data={disks} rowKey="mount" loading={!latest} pagination={false} />
              </CardContent>
            </Card>
          </div>
        </TabsContent>
        <TabsContent value="clientErrors" className="mt-0">
          <ClientErrorsTab />
        </TabsContent>
      </Tabs>
    </PageContainer>
  )
}
