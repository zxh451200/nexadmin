import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { BadgePercent, CircleDollarSign, Receipt, RefreshCw, TrendingUp } from 'lucide-react'
import { ChartCard } from '@/framework/components/ChartCard'
import { RankBars } from '@/framework/components/RankBars'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { StatCard } from '@/framework/components/StatCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { useChannels, usePayStats, type PayStatsFilter } from './api'

// 后端窗口上限 182 天（statsMaxDays），「近半年」是能如实兑现的最大档位，
// 不要再放一个名不副实的「全部」。
const RANGES = ['7d', '30d', '90d', '180d'] as const
type RangeKey = (typeof RANGES)[number]
const ALL_CHANNELS = '__all'

/** 本地日期，不用 toISOString：那会把边界按 UTC 平移一天 */
const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

/** 分 → 元。金额始终带币种显示，绝不把不同币种加在一起。 */
const money = (minor: number) => (minor / 100).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })

/**
 * 支付统计面板：成交概览 + 按天笔数 + 分币种金额表 + 渠道成交 + 结构排行。
 *
 * 时间窗自己管，不跟列表筛选联动：列表筛的是"看哪几笔"，统计看的是"这段时间的成色"，
 * 共用一套条件时用户分不清 KPI 算的是当前筛选还是全局。
 */
export function PayStatsPanel() {
  const { t } = useTranslation()
  const [range, setRange] = useState<RangeKey>('7d')
  const [channel, setChannel] = useState(ALL_CHANNELS)
  const { data: channels = [] } = useChannels()
  const filter = useMemo<PayStatsFilter>(() => {
    const days = Number(range.slice(0, -1))
    const start = new Date()
    start.setDate(start.getDate() - days + 1)
    return {
      startDate: ymd(start),
      endDate: ymd(new Date()),
      channelCode: channel === ALL_CHANNELS ? undefined : channel,
    }
  }, [range, channel])
  const { data, isFetching, isError, refetch } = usePayStats(filter)

  if (!data) {
    if (isError) {
      return (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border px-4 py-12 text-sm">
          <p className="text-muted-foreground">{t('plugin_pay.stats.loadFailed')}</p>
          <Button variant="outline" onClick={() => void refetch()}>
            {t('plugin_pay.stats.retry')}
          </Button>
        </div>
      )
    }
    return (
      <div className="grid gap-3 md:grid-cols-2 md:gap-4 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-28 w-full" />)}
      </div>
    )
  }

  const successRate = data.orders > 0 ? (data.paid / data.orders) * 100 : 0
  const days: Record<string, unknown>[] = data.days.map((d) => ({ date: d.date, orders: d.orders, paid: d.paid }))
  // 主币种按成交额取，不按字典序：多币种时 KPI 不能默认显示排在最前面的那个币种。
  const primary = [...data.byCurrency].sort((a, b) => b.gmvMinor - a.gmvMinor || b.orders - a.orders)[0]
  const byStatus = data.byStatus.map((s) => ({ name: t(`plugin_pay.status.${s.name}`, { defaultValue: s.name }), value: s.value }))
  const byMethod = data.byMethod.map((m) => ({ name: t(`plugin_pay.method.${m.name}`, { defaultValue: m.name }), value: m.value }))
  // 后端用 "-" 表示「失败但没记原因」，直接上屏会像一类业务原因。
  const failReasons = data.failReasons.map((r) => ({ ...r, name: r.name === '-' ? t('plugin_pay.stats.noReason') : r.name }))

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-medium">{t('plugin_pay.stats.rangeLabel')}</p>
          <p className="num text-xs text-muted-foreground">
            {data.from} ~ {data.to} · {t('plugin_pay.stats.orders')} {data.orders.toLocaleString()}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <SimpleSelect
            aria-label={t('plugin_pay.stats.channel')}
            className="w-40"
            value={channel}
            onChange={setChannel}
            options={[
              { value: ALL_CHANNELS, label: t('plugin_pay.all') },
              ...channels.map((c) => ({ value: c.code, label: c.name })),
            ]}
          />
          <SimpleSelect
            aria-label={t('plugin_pay.stats.rangeLabel')}
            className="w-32"
            value={range}
            onChange={(v) => setRange(v as RangeKey)}
            options={RANGES.map((r) => ({ value: r, label: t(`plugin_pay.stats.ranges.${r}`) }))}
          />
          <Button
            variant="outline"
            size="icon"
            aria-label={t('plugin_pay.stats.refresh')}
            disabled={isFetching}
            onClick={() => void refetch()}
          >
            <RefreshCw aria-hidden="true" className={isFetching ? 'animate-spin' : undefined} />
          </Button>
        </div>
      </div>

      {isError ? <p className="text-xs text-destructive">{t('plugin_pay.stats.stale')}</p> : null}

      <div className="grid gap-3 md:grid-cols-2 md:gap-4 xl:grid-cols-4">
        <StatCard
          tone="chart-1"
          title={t('plugin_pay.stats.gmv')}
          value={primary ? money(primary.gmvMinor) : 0}
          hint={primary?.currency}
          icon={<CircleDollarSign aria-hidden="true" />}
        />
        <StatCard
          tone="chart-2"
          title={t('plugin_pay.stats.paid')}
          value={data.paid}
          hint={`${successRate.toFixed(1)}% ${t('plugin_pay.stats.successRate')}`}
          icon={<TrendingUp aria-hidden="true" />}
        />
        <StatCard
          tone="success"
          title={t('plugin_pay.stats.net')}
          value={primary ? money(primary.netMinor) : 0}
          hint={primary ? `${primary.currency} · ${t('plugin_pay.stats.fee')} ${money(primary.feeMinor)}` : undefined}
          icon={<Receipt aria-hidden="true" />}
        />
        <StatCard
          tone="warning"
          title={t('plugin_pay.stats.refundRate')}
          value={primary ? `${primary.refundRate}%` : '0%'}
          hint={primary ? `${money(primary.refundedMinor)} ${primary.currency}` : undefined}
          icon={<BadgePercent aria-hidden="true" />}
        />
      </div>

      {data.byCurrency.length > 1 ? (
        <p className="text-xs text-muted-foreground">{t('plugin_pay.stats.multiCurrency')}</p>
      ) : null}
      {data.sampled ? <p className="text-xs text-muted-foreground">{t('plugin_pay.stats.sampled')}</p> : null}

      {data.orders === 0 ? (
        <div className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
          {t('plugin_pay.stats.emptyInRange')}
        </div>
      ) : (
        <>
          <div className="grid items-start gap-4 lg:grid-cols-2">
            <ChartCard title={t('plugin_pay.stats.daily')}>
              <TrendChart
                data={days}
                xKey="date"
                type="area"
                series={[
                  { key: 'orders', label: t('plugin_pay.stats.orders') },
                  { key: 'paid', label: t('plugin_pay.stats.paid') },
                ]}
              />
            </ChartCard>
            <div className="flex min-w-0 flex-col gap-4">
              <ChartCard title={t('plugin_pay.stats.byCurrency')}>
                <div className="overflow-x-auto">
                  <table className="w-full text-xs">
                    <thead>
                      <tr className="text-left text-muted-foreground">
                        <th className="font-medium">{t('plugin_pay.stats.currency')}</th>
                        <th className="text-right font-medium">{t('plugin_pay.stats.gmv')}</th>
                        <th className="text-right font-medium">{t('plugin_pay.stats.refunded')}</th>
                        <th className="text-right font-medium">{t('plugin_pay.stats.fee')}</th>
                        <th className="text-right font-medium">{t('plugin_pay.stats.net')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.byCurrency.map((c) => (
                        <tr key={c.currency} className="border-t border-border/60">
                          <td className="py-1.5">{c.currency}</td>
                          <td className="num py-1.5 text-right">{money(c.gmvMinor)}</td>
                          <td className="num py-1.5 text-right">{money(c.refundedMinor)}</td>
                          <td className="num py-1.5 text-right">{money(c.feeMinor)}</td>
                          <td className="num py-1.5 text-right font-medium">{money(c.netMinor)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </ChartCard>
              <ChartCard title={t('plugin_pay.stats.byChannel')}>
                <div className="overflow-x-auto">
                  <table className="w-full text-xs">
                    <thead>
                      <tr className="text-left text-muted-foreground">
                        <th className="font-medium">{t('plugin_pay.stats.channel')}</th>
                        <th className="text-right font-medium">{t('plugin_pay.stats.paid')}</th>
                        <th className="text-right font-medium">{t('plugin_pay.stats.gmv')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.byChannel.map((c) => (
                        <tr key={`${c.channel}-${c.currency}`} className="border-t border-border/60">
                          <td className="max-w-0 truncate py-1.5 pr-2" title={c.channel}>{c.channel}</td>
                          <td className="num py-1.5 text-right">{c.paid.toLocaleString()}</td>
                          <td className="num py-1.5 text-right">{money(c.gmvMinor)} {c.currency}</td>
                        </tr>
                      ))}
                      {data.byChannel.length === 0 ? (
                        <tr>
                          <td colSpan={3} className="py-6 text-center text-muted-foreground">{t('plugin_pay.empty')}</td>
                        </tr>
                      ) : null}
                    </tbody>
                  </table>
                </div>
              </ChartCard>
            </div>
          </div>

          <div className="grid items-start gap-4 lg:grid-cols-3">
            <ChartCard title={t('plugin_pay.stats.byScene')}>
              <div data-tone="chart-2">
                <RankBars items={data.byScene} emptyText={t('plugin_pay.empty')} />
              </div>
            </ChartCard>
            <ChartCard title={t('plugin_pay.stats.failReasons')}>
              <div data-tone="destructive">
                <RankBars items={failReasons} emptyText={t('plugin_pay.stats.noFailures')} />
              </div>
            </ChartCard>
            <ChartCard title={t('plugin_pay.stats.byStatus')}>
              <div data-tone="chart-1">
                <RankBars items={byStatus} emptyText={t('plugin_pay.empty')} />
              </div>
            </ChartCard>
          </div>

          <div className="grid items-start gap-4 lg:grid-cols-2">
            <ChartCard title={t('plugin_pay.stats.byMethod')}>
              <div data-tone="chart-3">
                <RankBars items={byMethod} emptyText={t('plugin_pay.empty')} />
              </div>
            </ChartCard>
            <ChartCard title={t('plugin_pay.stats.byBizType')}>
              <div data-tone="chart-4">
                <RankBars items={data.byBizType} emptyText={t('plugin_pay.empty')} />
              </div>
            </ChartCard>
          </div>
        </>
      )}
    </div>
  )
}
