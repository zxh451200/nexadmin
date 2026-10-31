import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CircleDashed, Inbox, ShieldAlert, UserRoundX } from 'lucide-react'
import { ChartCard } from '@/framework/components/ChartCard'
import { RankBars } from '@/framework/components/RankBars'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { StatCard } from '@/framework/components/StatCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { Skeleton } from '@/framework/ui/skeleton'
import { useTicketStats, type TicketStatsFilter } from './api'

const RANGES = ['7d', '30d', '90d', 'all'] as const
type RangeKey = (typeof RANGES)[number]

/** 本地日期，不用 toISOString：那会把边界按 UTC 平移一天 */
const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

const minutes = (v: number) => (v >= 60 ? `${(v / 60).toFixed(1)}h` : `${v}m`)

/**
 * 工单统计面板：队列结构 + 新建 vs 已解决 + 按优先级 SLA 达成 + 坐席/业务量排行。
 *
 * 时间窗自己管，不跟列表筛选联动：列表筛的是"处理哪几张"，统计看的是"这段时间的队列成色"，
 * 共用一套条件时用户分不清 KPI 算的是当前筛选还是全局。
 */
export function TicketStatsPanel() {
  const { t } = useTranslation()
  const [range, setRange] = useState<RangeKey>('7d')
  const filter = useMemo<TicketStatsFilter>(() => {
    if (range === 'all') return {}
    const days = Number(range.slice(0, -1))
    const start = new Date()
    start.setDate(start.getDate() - days + 1)
    return { startDate: ymd(start), endDate: ymd(new Date()) }
  }, [range])
  const { data, isPending } = useTicketStats(filter)

  if (isPending || !data) {
    return (
      <div className="grid gap-3 md:grid-cols-2 md:gap-4 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-28 w-full" />)}
      </div>
    )
  }

  const pending = data.open + data.processing
  const days: Record<string, unknown>[] = data.days.map((d) => ({ date: d.date, created: d.created, resolved: d.resolved }))

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <SimpleSelect
          aria-label={t('plugin_ticket.stats.rangeLabel')}
          className="w-32"
          value={range}
          onChange={(v) => setRange(v as RangeKey)}
          options={RANGES.map((r) => ({ value: r, label: t(`plugin_ticket.stats.ranges.${r}`) }))}
        />
      </div>
      <div className="grid gap-3 md:grid-cols-2 md:gap-4 xl:grid-cols-4">
        <StatCard tone="chart-1" title={t('plugin_ticket.stats.total')} value={data.total} icon={<Inbox aria-hidden="true" />} />
        <StatCard
          tone="chart-2"
          title={t('plugin_ticket.stats.pending')}
          value={pending}
          hint={`${data.pending} ${t('plugin_ticket.stats.awaitingCustomer')}`}
          icon={<CircleDashed aria-hidden="true" />}
        />
        <StatCard tone="warning" title={t('plugin_ticket.stats.unassigned')} value={data.unassigned} icon={<UserRoundX aria-hidden="true" />} />
        <StatCard tone="destructive" title={t('plugin_ticket.stats.overdue')} value={data.overdue} icon={<ShieldAlert aria-hidden="true" />} />
      </div>

      {data.sampled ? <p className="text-xs text-muted-foreground">{t('plugin_ticket.stats.sampled')}</p> : null}

      <div className="grid gap-4 lg:grid-cols-2">
        <ChartCard title={t('plugin_ticket.stats.daily')}>
          <TrendChart
            data={days}
            xKey="date"
            type="line"
            series={[
              { key: 'created', label: t('plugin_ticket.stats.created') },
              { key: 'resolved', label: t('plugin_ticket.stats.resolved') },
            ]}
          />
        </ChartCard>
        <ChartCard title={t('plugin_ticket.stats.byAssignee')}>
          <div data-tone="chart-5">
            <RankBars items={data.byAssignee} />
          </div>
        </ChartCard>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <ChartCard title={t('plugin_ticket.stats.firstResponse')}>
          <div className="grid grid-cols-3 gap-3 py-4 text-center">
            <div>
              <p className="num text-2xl font-semibold">{minutes(data.firstResponse.p50Minutes)}</p>
              <p className="mt-1 text-xs text-muted-foreground">P50</p>
            </div>
            <div>
              <p className="num text-2xl font-semibold">{minutes(data.firstResponse.p90Minutes)}</p>
              <p className="mt-1 text-xs text-muted-foreground">P90</p>
            </div>
            <div>
              <p className="num text-2xl font-semibold">{data.firstResponse.noResponse.toLocaleString()}</p>
              <p className="mt-1 text-xs text-muted-foreground">{t('plugin_ticket.stats.noResponse')}</p>
            </div>
          </div>
        </ChartCard>
        <ChartCard title={t('plugin_ticket.stats.resolution')}>
          <div className="grid grid-cols-3 gap-3 py-4 text-center">
            <div>
              <p className="num text-2xl font-semibold">{minutes(data.resolution.p50Minutes)}</p>
              <p className="mt-1 text-xs text-muted-foreground">P50</p>
            </div>
            <div>
              <p className="num text-2xl font-semibold">{minutes(data.resolution.p90Minutes)}</p>
              <p className="mt-1 text-xs text-muted-foreground">P90</p>
            </div>
            <div>
              <p className="num text-2xl font-semibold">{data.resolution.resolved.toLocaleString()}</p>
              <p className="mt-1 text-xs text-muted-foreground">{t('plugin_ticket.stats.resolvedCount')}</p>
            </div>
          </div>
        </ChartCard>
      </div>

      <ChartCard title={t('plugin_ticket.stats.sla')}>
        <div className="space-y-4">
          <div>
            <p className="mb-1 text-xs font-medium text-muted-foreground">{t('plugin_ticket.stats.slaFirst')}</p>
            <table className="w-full text-xs">
              <thead>
                <tr className="text-left text-muted-foreground">
                  <th className="font-medium">{t('plugin_ticket.stats.priority')}</th>
                  <th className="text-right font-medium">{t('plugin_ticket.stats.target')}</th>
                  <th className="text-right font-medium">{t('plugin_ticket.stats.total')}</th>
                  <th className="text-right font-medium">{t('plugin_ticket.stats.breached')}</th>
                  <th className="text-right font-medium">{t('plugin_ticket.stats.breachRate')}</th>
                </tr>
              </thead>
              <tbody>
                {data.slaByPriority.map((r) => (
                  <tr key={r.priority} className="border-t border-border/60">
                    <td className="py-1.5">{r.priority}</td>
                    <td className="num py-1.5 text-right">{minutes(r.targetMinutes)}</td>
                    <td className="num py-1.5 text-right">{r.total.toLocaleString()}</td>
                    <td className="num py-1.5 text-right">{r.breached.toLocaleString()}</td>
                    <td className={`num py-1.5 text-right ${r.breachRate > 0 ? 'text-destructive' : 'text-muted-foreground'}`}>{r.breachRate}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div>
            <p className="mb-1 text-xs font-medium text-muted-foreground">{t('plugin_ticket.stats.slaResolution')}</p>
            <table className="w-full text-xs">
              <thead>
                <tr className="text-left text-muted-foreground">
                  <th className="font-medium">{t('plugin_ticket.stats.priority')}</th>
                  <th className="text-right font-medium">{t('plugin_ticket.stats.target')}</th>
                  <th className="text-right font-medium">{t('plugin_ticket.stats.breached')}</th>
                  <th className="text-right font-medium">{t('plugin_ticket.stats.breachRate')}</th>
                </tr>
              </thead>
              <tbody>
                {data.slaByPriority.map((r) => (
                  <tr key={r.priority} className="border-t border-border/60">
                    <td className="py-1.5">{r.priority}</td>
                    <td className="num py-1.5 text-right">{minutes(r.resolutionTargetMinutes)}</td>
                    <td className="num py-1.5 text-right">{r.resolutionBreached.toLocaleString()}</td>
                    <td className={`num py-1.5 text-right ${r.resolutionBreachRate > 0 ? 'text-destructive' : 'text-muted-foreground'}`}>{r.resolutionBreachRate}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-2">
        <ChartCard title={t('plugin_ticket.stats.satisfaction')}>
          <div className="flex items-baseline gap-2 py-2">
            <p className="num text-3xl font-semibold">{data.satisfaction.avgScore.toFixed(1)}</p>
            <span className="text-xs text-muted-foreground">
              / 5 · {data.satisfaction.rated.toLocaleString()} {t('plugin_ticket.stats.ratedCount')}
            </span>
          </div>
          <div data-tone="chart-4">
            <RankBars items={data.satisfaction.byScore} />
          </div>
        </ChartCard>
        <ChartCard title={t('plugin_ticket.stats.byBizType')}>
          <div data-tone="chart-3">
            <RankBars items={data.byBizType} />
          </div>
        </ChartCard>
      </div>
    </div>
  )
}
