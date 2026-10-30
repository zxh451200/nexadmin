import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { KeyRound, LogIn, ShieldAlert, Users } from 'lucide-react'
import { ChartCard } from '@/framework/components/ChartCard'
import { RankBars } from '@/framework/components/RankBars'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { StatCard } from '@/framework/components/StatCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { Skeleton } from '@/framework/ui/skeleton'
import { useLoginStats, type LoginStatsFilter } from './api'

const RANGES = ['7d', '30d', '90d', 'all'] as const
type RangeKey = (typeof RANGES)[number]

/** 本地日期，不用 toISOString：那会把边界按 UTC 平移一天 */
const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

/**
 * 登录日志统计面板：KPI + 按天趋势 + 时段分布 + 失败原因/登录方式/浏览器 Top-N。
 *
 * 时间窗自己管，不跟列表的筛选联动——列表筛的是"看哪几行"，统计看的是"这段时间的趋势"，
 * 两者共用一套条件时，用户分不清 KPI 说的是当前筛选还是全局。
 */
export function LoginStatsPanel() {
  const { t } = useTranslation()
  const [range, setRange] = useState<RangeKey>('7d')
  const filter = useMemo<LoginStatsFilter>(() => {
    if (range === 'all') return {}
    const days = Number(range.slice(0, -1))
    const start = new Date()
    start.setDate(start.getDate() - days + 1)
    return { startDate: ymd(start), endDate: ymd(new Date()) }
  }, [range])
  const { data, isPending } = useLoginStats(filter)

  if (isPending || !data) {
    return (
      <div className="grid gap-4 md:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-28 w-full" />)}
      </div>
    )
  }

  const failRate = data.total > 0 ? (data.failures / data.total) * 100 : 0
  // Top-N 的名字是后端返回的 login_type / fail_reason 原值，上屏前换成字典里的说法
  const typeItems = data.types.map((x) => ({ ...x, name: t(`loginLog.type.${x.name}`, { defaultValue: x.name }) }))
  const reasonItems = data.failReasons.map((x) => ({ ...x, name: t(`loginLog.reason.${x.name}`, { defaultValue: x.name }) }))
  const hours = data.hours.map((h) => ({ hour: `${String(h.hour).padStart(2, '0')}:00`, value: h.value }))
  // TrendChart 收的是宽松记录数组，这里显式转一份，不把业务接口类型透传给图表
  const days: Record<string, unknown>[] = data.days.map((d) => ({ date: d.date, total: d.total, failures: d.failures }))

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <SimpleSelect
          aria-label={t('loginLog.stats.rangeLabel')}
          className="w-32"
          value={range}
          onChange={(v) => setRange(v as RangeKey)}
          options={RANGES.map((r) => ({ value: r, label: t(`loginLog.stats.ranges.${r}`) }))}
        />
      </div>
      <div className="grid gap-3 md:grid-cols-2 md:gap-4 xl:grid-cols-4">
        <StatCard tone="chart-1" title={t('loginLog.stats.total')} value={data.total} icon={<LogIn aria-hidden="true" />} />
        <StatCard
          tone="destructive"
          title={t('loginLog.stats.failures')}
          value={data.failures}
          hint={`${failRate.toFixed(1)}%`}
          icon={<ShieldAlert aria-hidden="true" />}
        />
        <StatCard tone="chart-2" title={t('loginLog.stats.types')} value={data.types.length} icon={<Users aria-hidden="true" />} />
        <StatCard tone="warning" title={t('loginLog.stats.reasons')} value={data.failReasons.length} icon={<KeyRound aria-hidden="true" />} />
      </div>

      {data.sampled ? <p className="text-xs text-muted-foreground">{t('loginLog.stats.sampled')}</p> : null}

      <div className="grid gap-4 lg:grid-cols-2">
        <ChartCard title={t('loginLog.stats.daily')}>
          <TrendChart
            data={days}
            xKey="date"
            type="line"
            switchable
            series={[
              { key: 'total', label: t('loginLog.stats.total') },
              { key: 'failures', label: t('loginLog.stats.failures') },
            ]}
          />
        </ChartCard>
        <ChartCard title={t('loginLog.stats.hourly')}>
          <TrendChart data={hours} xKey="hour" type="bar" switchable={false} series={[{ key: 'value', label: t('loginLog.stats.total') }]} />
        </ChartCard>
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <ChartCard title={t('loginLog.stats.topReasons')}>
          <div data-tone="destructive">
            <RankBars items={reasonItems} emptyText={t('loginLog.stats.noFailures')} />
          </div>
        </ChartCard>
        <ChartCard title={t('loginLog.stats.topTypes')}>
          <div data-tone="chart-2">
            <RankBars items={typeItems} />
          </div>
        </ChartCard>
        <ChartCard title={t('loginLog.stats.topBrowsers')}>
          <div data-tone="chart-4">
            <RankBars items={data.browsers} />
          </div>
        </ChartCard>
      </div>
    </div>
  )
}
