import { useMemo, useState } from 'react'
import { Activity, AlertTriangle, Timer } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ChartCard } from '@/framework/components/ChartCard'
import { MetricHistory } from '@/framework/components/MetricHistory'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { StatCard } from '@/framework/components/StatCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { useLiveMetrics } from '@/framework/hooks/use-live-metrics'
import { Badge } from '@/framework/ui/badge'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Skeleton } from '@/framework/ui/skeleton'
import { SORT_KEYS, TOP_OPTIONS, type ApiSnapshot, type ApiStat, type SortKey } from './api'

const INTERVALS = ['3000', '10000', '30000', '0'] as const

export default function ApiMonitorPage() {
  const { t, i18n } = useTranslation()
  const [interval, setIntervalMs] = useState<string>('3000')
  const [top, setTop] = useState<string>('5')
  const [sortBy, setSortBy] = useState<SortKey>('qps')
  const { latest, history, isLoading } = useLiveMetrics<ApiSnapshot>('/monitor/api', Number(interval), 30)

  const points = useMemo(
    () => history.map((h) => ({ time: new Date(h.ts).toLocaleTimeString(i18n.language, { hour12: false }), qps: h.qps })),
    [history, i18n.language],
  )

  const rows = useMemo(
    () => (latest?.apis ?? []).slice().sort((a, b) => b[sortBy] - a[sortBy]).slice(0, Number(top)),
    [latest, sortBy, top],
  )

  const columns: Column<ApiStat>[] = [
    {
      key: 'api',
      title: t('apiMonitor.col.api'),
      mobile: 'title',
      render: (r) => (
        <span className="flex items-center gap-2">
          <Badge variant="secondary">{r.method}</Badge>
          <span className="num">{r.path}</span>
        </span>
      ),
    },
    { key: 'qps', title: t('apiMonitor.col.qps'), align: 'end', render: (r) => <span className="num">{r.qps}</span> },
    { key: 'avgMs', title: t('apiMonitor.col.avgMs'), align: 'end', render: (r) => <span className="num">{r.avgMs}</span> },
    {
      key: 'errorRate',
      title: t('apiMonitor.col.errorRate'),
      align: 'end',
      render: (r) => <span className={r.errorRate >= 2 ? 'num text-destructive' : 'num'}>{r.errorRate.toFixed(2)}%</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader
        title={t('apiMonitor.title')}
        description={t('apiMonitor.description')}
        actions={
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">{t('apiMonitor.refresh.label')}</span>
            <Select value={interval} onValueChange={setIntervalMs}>
              <SelectTrigger className="w-32" aria-label={t('apiMonitor.refresh.label')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {INTERVALS.map((v) => (
                  <SelectItem key={v} value={v}>
                    {t(`apiMonitor.refresh.v${v}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        }
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3 md:gap-4">
        {isLoading || !latest ? (
          Array.from({ length: 3 }, (_, i) => <Skeleton key={i} className="h-32 w-full" />)
        ) : (
          <>
            <StatCard tone="chart-1"
              title={t('apiMonitor.card.qps')}
              value={latest.qps}
              hint="req/s"
              spark={history.map((h) => h.qps)}
              icon={<Activity aria-hidden="true" />}
            />
            <StatCard tone="warning" title={t('apiMonitor.card.avgMs')} value={latest.avgMs} hint="ms" icon={<Timer aria-hidden="true" />} />
            <StatCard tone="destructive"
              title={t('apiMonitor.card.errorRate')}
              value={`${latest.errorRate.toFixed(2)}%`}
              progress={Math.min(100, latest.errorRate * 10)}
              icon={<AlertTriangle aria-hidden="true" />}
            />
          </>
        )}
      </div>

      <ChartCard title={t('apiMonitor.chart.qps')}>
        <TrendChart
          data={points}
          xKey="time"
          type="area"
          switchable={false}
          height={220}
          series={[{ key: 'qps', label: t('apiMonitor.series.qps') }]}
        />
      </ChartCard>

      <MetricHistory
        kind="api"
        title={t('monitorHistory.title')}
        labels={{
          qps: t('apiMonitor.card.qps'),
          avgMs: t('apiMonitor.card.avgMs'),
          errorRate: t('apiMonitor.card.errorRate'),
        }}
      />

      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <p className="text-base font-medium">{t('apiMonitor.top.title')}</p>
          {/* 常驻选择器不是筛选框：值一直在，标签不能靠占位符传达，所以内联在控件左侧 */}
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="am-sort" className="text-xs font-normal text-muted-foreground">
                {t('apiMonitor.top.sortBy')}
              </Label>
              <Select value={sortBy} onValueChange={(v) => setSortBy(v as SortKey)}>
                <SelectTrigger id="am-sort" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SORT_KEYS.map((k) => (
                    <SelectItem key={k} value={k}>
                      {t(`apiMonitor.sort.${k}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex items-center gap-1.5">
              <Label htmlFor="am-top" className="text-xs font-normal text-muted-foreground">
                {t('apiMonitor.top.count')}
              </Label>
              <Select value={top} onValueChange={setTop}>
                <SelectTrigger id="am-top" className="w-28">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {TOP_OPTIONS.map((n) => (
                    <SelectItem key={n} value={n}>
                      {t(`apiMonitor.top.n${n}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        </div>
        <DataTable<ApiStat> columns={columns} data={rows} rowKey={(r) => `${r.method} ${r.path}`} loading={!latest} pagination={false} />
      </div>
    </PageContainer>
  )
}
