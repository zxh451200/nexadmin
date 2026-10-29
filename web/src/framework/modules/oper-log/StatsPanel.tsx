import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Activity, AlertTriangle, Gauge, Timer } from 'lucide-react'
import { ChartCard } from '@/framework/components/ChartCard'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { StatCard } from '@/framework/components/StatCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { Skeleton } from '@/framework/ui/skeleton'
import { RankBars } from '@/framework/components/RankBars'
import { useOperStats, type StatsFilter } from './api'

const RANGES = ['7d', '30d', '90d', 'all'] as const
type RangeKey = (typeof RANGES)[number]

/** 本地日期，不用 toISOString：那会把边界按 UTC 平移一天 */
const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

/**
 * 操作日志统计面板：KPI + 按天趋势 + 时段分布 + 三张 Top-N。
 *
 * 时间窗自己管，不跟列表筛选联动：列表筛的是"看哪几行"，统计看的是"这段时间的趋势"，
 * 共用一套条件时用户分不清 KPI 算的是当前筛选还是全局。
 */
export function OperStatsPanel() {
  const { t } = useTranslation()
  const [range, setRange] = useState<RangeKey>('7d')
  const filter = useMemo<StatsFilter>(() => {
    if (range === 'all') return {}
    const days = Number(range.slice(0, -1))
    const start = new Date()
    start.setDate(start.getDate() - days + 1)
    return { startDate: ymd(start), endDate: ymd(new Date()) }
  }, [range])
  const { data, isPending } = useOperStats(filter)

  if (isPending || !data) {
    return (
      <div className="grid gap-4 md:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-28 w-full" />)}
      </div>
    )
  }

  const failRate = data.total > 0 ? (data.failures / data.total) * 100 : 0
  const slowest = data.slowest[0]
  const hours = data.hours.map((h) => ({ hour: `${String(h.hour).padStart(2, '0')}:00`, value: h.value }))
  // TrendChart 收的是宽松记录数组，这里显式转一份，不把业务接口类型透传给图表
  const days: Record<string, unknown>[] = data.days.map((d) => ({ date: d.date, total: d.total, failures: d.failures }))

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <SimpleSelect
          aria-label={t('operLog.stats.rangeLabel')}
          className="w-32"
          value={range}
          onChange={(v) => setRange(v as RangeKey)}
          options={RANGES.map((r) => ({ value: r, label: t(`operLog.stats.ranges.${r}`) }))}
        />
      </div>
      <div className="grid gap-3 md:grid-cols-2 md:gap-4 xl:grid-cols-4">
        <StatCard tone="chart-1" title={t('operLog.stats.total')} value={data.total} icon={<Activity aria-hidden="true" />} />
        <StatCard
          tone="destructive"
          title={t('operLog.stats.failures')}
          value={data.failures}
          hint={`${failRate.toFixed(1)}%`}
          icon={<AlertTriangle aria-hidden="true" />}
        />
        <StatCard tone="chart-2" title={t('operLog.stats.p95')} value={data.latency.p95} hint="ms" icon={<Timer aria-hidden="true" />} />
        <StatCard
          tone="chart-4"
          title={t('operLog.stats.slowest')}
          value={slowest ? slowest.avgMs : 0}
          hint="ms"
          icon={<Gauge aria-hidden="true" />}
        />
      </div>

      {data.sampled ? (
        <p className="text-xs text-muted-foreground">{t('operLog.stats.sampled')}</p>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-2">
        <ChartCard title={t('operLog.stats.daily')}>
          <TrendChart
            data={days}
            xKey="date"
            type="area"
            switchable
            series={[
              { key: 'total', label: t('operLog.stats.total') },
              { key: 'failures', label: t('operLog.stats.failures') },
            ]}
          />
        </ChartCard>
        <ChartCard title={t('operLog.stats.hourly')}>
          <TrendChart data={hours} xKey="hour" type="bar" switchable={false} series={[{ key: 'value', label: t('operLog.stats.total') }]} />
        </ChartCard>
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <ChartCard title={t('operLog.stats.topModules')}>
          <div data-tone="chart-1">
            <RankBars items={data.topModules} />
          </div>
        </ChartCard>
        <ChartCard title={t('operLog.stats.topTypes')}>
          <div data-tone="chart-2">
            <RankBars items={data.topTypes} />
          </div>
        </ChartCard>
        <ChartCard title={t('operLog.stats.topOperators')}>
          <div data-tone="chart-5">
            <RankBars items={data.topOperators} />
          </div>
        </ChartCard>
      </div>

      <ChartCard title={t('operLog.stats.slowestUrls')}>
        <div data-tone="warning">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th className="font-medium">{t('operLog.stats.col.url')}</th>
                <th className="font-medium">{t('operLog.stats.col.method')}</th>
                <th className="text-right font-medium">{t('operLog.stats.col.count')}</th>
                <th className="text-right font-medium">{t('operLog.stats.col.avgMs')}</th>
              </tr>
            </thead>
            <tbody>
              {data.slowest.map((u) => (
                <tr key={`${u.method} ${u.url}`} className="border-t border-border/60">
                  <td className="max-w-0 truncate py-1.5 pr-3" title={u.url}>{u.url}</td>
                  <td className="py-1.5 pr-3 text-muted-foreground">{u.method}</td>
                  <td className="num py-1.5 text-right">{u.count.toLocaleString()}</td>
                  <td className="num py-1.5 text-right">{u.avgMs.toLocaleString()} ms</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </ChartCard>
    </div>
  )
}
