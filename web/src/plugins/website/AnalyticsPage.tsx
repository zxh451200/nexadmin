import { useState } from 'react'
import { Cell, Pie, PieChart, Tooltip } from 'recharts'
import { Eye, Inbox, Monitor, Radio, Timer, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { TrendArea } from '@/framework/modules/dashboard/cards/TrendArea'
import { ChartBox } from '@/framework/components/ChartBox'
import { ChartCard, type ChartCardRange } from '@/framework/components/ChartCard'
import { StatCard } from '@/framework/components/StatCard'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Badge } from '@/framework/ui/badge'
import { Skeleton } from '@/framework/ui/skeleton'
import { useAnalytics, type AnalyticsDays, type AnalyticsDeviceRow, type AnalyticsRefRow } from './api'

// 访问统计：公开官网的第一方轻量盘——浏览 / 访客 / 跳出率 / 访问时长 / 询盘漏斗 +
// 实时在站 + 设备画像 + 入口页。对标 Plausible/Umami 的核心指标；Geo/IP 与 UTM 归因
// 有意不做：采集口径是匿名会话 cookie（不含 IP 与个人身份），见 plugins/website/analytics.go。
// 口径：bot 不采集，day 按站点时区归日；跳出率 = 只有一次浏览的会话占比。

const DAYS: AnalyticsDays[] = [7, 14, 30]
const RANGE_KEYS = ['d7', 'd14', 'd30'] as const

/** 流量来源徽章的色调：direct 是中性（默认入站），其余按语义色区分 */
function refTone(kind: AnalyticsRefRow['kind']) {
  switch (kind) {
    case 'search': return 'success' as const
    case 'social': return 'info' as const
    case 'external': return 'warning' as const
    case 'internal': return 'chart-2' as const
    default: return undefined
  }
}

export default function AnalyticsPage() {
  const { t, i18n } = useTranslation()
  const [days, setDays] = useState<AnalyticsDays>(7)
  const { data, isLoading } = useAnalytics(days)
  const fmt = (n: number) => new Intl.NumberFormat(i18n.language).format(n)
  // 设备/来源画像的 key 直接展示原文（Chrome/Windows/google.com 这类专名不翻译）；
  // unknown → other 的兜底文案走 i18n。
  const deviceKey = (key: string) => (key === 'other' ? t('plugin_website.analytics.deviceOther') : key)

  const ranges: ChartCardRange[] = DAYS.map((d, i) => ({
    value: String(d),
    label: t(`plugin_website.analytics.range.${RANGE_KEYS[i]}`),
  }))

  // 漏斗两级（浏览 → 询盘）：询盘柱按占浏览的比例，4% 保底可见；转化率 = 询盘 / 浏览
  const views = data?.views ?? 0
  const inquiries = data?.inquiries ?? 0
  const rate = views > 0 ? (inquiries / views) * 100 : 0
  const barW = (v: number) => (views > 0 ? Math.max((v / views) * 100, 4) : 0)

  // 访问时长 mm:ss（窗口均值，秒）；1 小时以上极罕见，也兜得住
  const dur = data?.avgDuration ?? 0
  const durText = `${Math.floor(dur / 60)}:${String(dur % 60).padStart(2, '0')}`

  const refRows = data?.referrers ?? []
  const refMax = Math.max(...refRows.map((r) => r.sessions), 1)
  const paths = data?.topPaths ?? []
  const pathMax = Math.max(...paths.map((p) => p.views), 1)
  const entries = data?.entryPages ?? []
  const entryMax = Math.max(...entries.map((e) => e.views), 1)

  const kindLabel = (kind: AnalyticsRefRow['kind']) => t(`plugin_website.analytics.refKind.${kind}`)
  const refColor = (kind: AnalyticsRefRow['kind']) => {
    const index = ['direct', 'search', 'social', 'external', 'internal'].indexOf(kind)
    return `hsl(var(--chart-${(index < 0 ? 0 : index) + 1}))`
  }
  const sourceRows = data?.refKindStats ?? []
  const sourceTotal = sourceRows.reduce((sum, row) => sum + row.sessions, 0)
  const sourceSlices = sourceRows.map((row) => ({
    name: kindLabel(row.key as AnalyticsRefRow['kind']),
    value: row.sessions,
    color: refColor(row.key as AnalyticsRefRow['kind']),
  }))

  const deviceStack = (title: string, rows: AnalyticsDeviceRow[]) => {
    const total = rows.reduce((sum, row) => sum + row.sessions, 0)
    if (rows.length === 0) return null
    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between gap-2">
          <p className="text-sm font-medium text-foreground">{title}</p>
          <span className="num text-xs text-muted-foreground">{fmt(total)}</span>
        </div>
        <div
          className="flex h-3 overflow-hidden rounded-full bg-muted"
          role="img"
          aria-label={`${title}: ${rows.map((row) => `${deviceKey(row.key)} ${fmt(row.sessions)}`).join(', ')}`}
        >
          {rows.map((row, index) => (
            <div
              key={row.key}
              className="h-full"
              style={{
                width: `${total > 0 ? (row.sessions / total) * 100 : 0}%`,
                backgroundColor: `hsl(var(--chart-${(index % 5) + 1}))`,
              }}
              title={`${deviceKey(row.key)}: ${fmt(row.sessions)}`}
            />
          ))}
        </div>
        <div className="grid grid-cols-2 gap-x-3 gap-y-2">
          {rows.map((row, index) => (
            <div key={row.key} className="flex min-w-0 items-center gap-1.5 text-xs">
              <span
                aria-hidden="true"
                className="size-2 shrink-0 rounded-full"
                style={{ backgroundColor: `hsl(var(--chart-${(index % 5) + 1}))` }}
              />
              <span className="min-w-0 flex-1 truncate" title={deviceKey(row.key)}>{deviceKey(row.key)}</span>
              <span className="num shrink-0 font-medium">{fmt(row.sessions)}</span>
            </div>
          ))}
        </div>
      </div>
    )
  }

  const pathList = (rows: typeof paths, max: number, tone: string) =>
    rows.map((p, i) => (
      <div key={`${p.path}-${i}`} className="space-y-1.5">
        <div className="flex items-center justify-between gap-2">
          <span className="num min-w-0 truncate text-sm" title={p.path}>{p.path}</span>
          <span className="num shrink-0 text-sm font-medium">{fmt(p.views)}</span>
        </div>
        <div className="h-1.5 overflow-hidden rounded-full bg-muted">
          <div className={`h-full rounded-full ${tone}`} style={{ width: `${(p.views / max) * 100}%` }} />
        </div>
      </div>
    ))

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_website.analytics.title')}
        description={<span className="block max-w-2xl leading-relaxed">{t('plugin_website.analytics.description')}</span>}
      />

      {/* KPI 行：浏览 / 访客 / 跳出率 / 访问时长 / 询盘 / 实时在站（近 5 分钟窗口，60s 轮询） */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6 md:gap-4">
        <StatCard
          title={t('plugin_website.analytics.kpi.views')}
          value={data?.views ?? 0}
          icon={<Eye aria-hidden="true" />}
          spark={data?.trend.map((p) => p.views)}
          tone="chart-1"
        />
        <StatCard
          title={t('plugin_website.analytics.kpi.sessions')}
          value={data?.sessions ?? 0}
          icon={<Users aria-hidden="true" />}
          spark={data?.trend.map((p) => p.sessions)}
          tone="chart-2"
        />
        <StatCard
          title={t('plugin_website.analytics.kpi.bounce')}
          value={`${(data?.bounceRate ?? 0).toFixed(1)}%`}
          hint={t('plugin_website.analytics.kpi.bounceHint')}
          icon={<Monitor aria-hidden="true" />}
          tone="chart-3"
        />
        <StatCard
          title={t('plugin_website.analytics.kpi.duration')}
          value={durText}
          hint={t('plugin_website.analytics.kpi.durationHint')}
          icon={<Timer aria-hidden="true" />}
          tone="chart-4"
        />
        <StatCard
          title={t('plugin_website.analytics.kpi.inquiries')}
          value={data?.inquiries ?? 0}
          icon={<Inbox aria-hidden="true" />}
          spark={data?.trend.map((p) => p.inquiries)}
          tone="chart-5"
        />
        <StatCard
          title={t('plugin_website.analytics.kpi.realtime')}
          value={data?.realtime ?? 0}
          hint={t('plugin_website.analytics.kpi.realtimeHint')}
          icon={<Radio aria-hidden="true" />}
          tone="success"
        />
      </div>

      {/* 流量趋势：浏览（面积）+ 访客/询盘（细线），窗口 7/14/30 切换 */}
      <ChartCard
        title={t('plugin_website.analytics.trend.title')}
        ranges={ranges}
        range={String(days)}
        onRangeChange={(v) => setDays(Number(v) as AnalyticsDays)}
        summary={
          data
            ? {
                value: fmt(views),
                label: t('plugin_website.analytics.trend.summaryViews'),
                hint: t('plugin_website.analytics.range.window', { days }),
              }
            : undefined
        }
      >
        {isLoading ? (
          <Skeleton className="h-72 w-full" />
        ) : (
          <TrendArea
            data={(data?.trend ?? []).map((p) => ({ date: p.day, views: p.views, sessions: p.sessions, inquiries: p.inquiries }))}
            xKey="date"
            series={[
              { key: 'views', label: t('plugin_website.analytics.trend.series.views'), color: 'hsl(var(--chart-1))' },
              { key: 'sessions', label: t('plugin_website.analytics.trend.series.sessions'), color: 'hsl(var(--chart-2))' },
              { key: 'inquiries', label: t('plugin_website.analytics.trend.series.inquiries'), color: 'hsl(var(--chart-5))' },
            ]}
          />
        )}
      </ChartCard>

      {/* 来源 / 设备 / 漏斗：三列同排（窄屏折行） */}
      <div className="grid grid-cols-1 gap-3 md:gap-4 xl:grid-cols-3">
        <Card className="flex flex-col transition-shadow hover:glow motion-reduce:transition-none">
          <CardHeader className="pb-2">
            <CardTitle className="text-base font-medium">{t('plugin_website.analytics.sources.title')}</CardTitle>
          </CardHeader>
          <CardContent className="flex-1 space-y-3">
            {isLoading ? (
              <Skeleton className="h-48 w-full" />
            ) : sourceTotal === 0 ? (
              <p className="py-8 text-center text-sm text-muted-foreground">{t('plugin_website.analytics.empty')}</p>
            ) : (
              <>
                <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                  <ChartBox height={168} className="shrink-0 sm:w-40">
                    {({ width, height }) => (
                      <div className="relative size-full">
                        <PieChart width={width} height={height}>
                          <Pie
                            data={sourceSlices}
                            dataKey="value"
                            nameKey="name"
                            innerRadius="58%"
                            outerRadius="82%"
                            paddingAngle={2}
                            stroke="hsl(var(--card))"
                          >
                            {sourceSlices.map((slice) => (
                              <Cell key={slice.name} fill={slice.color} />
                            ))}
                          </Pie>
                          <Tooltip
                            cursor={false}
                            formatter={(value) => fmt(Number(value))}
                            contentStyle={{ background: 'hsl(var(--popover))', border: '1px solid hsl(var(--border))', borderRadius: 8, color: 'hsl(var(--popover-foreground))', fontSize: 12 }}
                            itemStyle={{ color: 'hsl(var(--popover-foreground))' }}
                          />
                        </PieChart>
                        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
                          <span className="num text-lg font-semibold">{fmt(sourceTotal)}</span>
                          <span className="text-[11px] text-muted-foreground">{t('plugin_website.analytics.kpi.sessions')}</span>
                        </div>
                      </div>
                    )}
                  </ChartBox>
                  <div className="min-w-0 flex-1 space-y-2">
                    {sourceSlices.map((slice) => (
                      <div key={slice.name} className="flex items-center gap-2 text-sm">
                        <span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ backgroundColor: slice.color }} />
                        <span className="min-w-0 flex-1 truncate">{slice.name}</span>
                        <span className="num shrink-0 text-xs text-muted-foreground">
                          {sourceTotal > 0 ? Math.round((slice.value / sourceTotal) * 100) : 0}%
                        </span>
                        <span className="num w-10 shrink-0 text-end font-medium">{fmt(slice.value)}</span>
                      </div>
                    ))}
                  </div>
                </div>
                {refRows.length > 0 ? (
                  <div className="space-y-3 border-t border-border pt-3">
                    <p className="text-sm font-medium text-foreground">{t('plugin_website.analytics.sources.topDomains')}</p>
                    {refRows.map((r, i) => (
                      <div key={`${r.kind}-${r.host}-${i}`} className="space-y-1.5">
                        <div className="flex items-center justify-between gap-2">
                          <span className="flex min-w-0 items-center gap-2">
                            <Badge variant={refTone(r.kind) ? undefined : 'secondary'} tone={refTone(r.kind)}>
                              {kindLabel(r.kind)}
                            </Badge>
                            {r.host ? <span className="num truncate text-sm">{r.host}</span> : null}
                          </span>
                          <span className="num shrink-0 text-sm font-medium">{fmt(r.sessions)}</span>
                        </div>
                        <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                          <div className="h-full rounded-full bg-chart-2" style={{ width: `${(r.sessions / refMax) * 100}%` }} />
                        </div>
                      </div>
                    ))}
                  </div>
                ) : null}
              </>
            )}
          </CardContent>
        </Card>

        <Card className="flex flex-col transition-shadow hover:glow motion-reduce:transition-none">
          <CardHeader className="pb-2">
            <CardTitle className="text-base font-medium">{t('plugin_website.analytics.devices.title')}</CardTitle>
          </CardHeader>
          <CardContent className="flex-1 space-y-5">
            {isLoading ? (
              <Skeleton className="h-48 w-full" />
            ) : (data?.browsers?.length ?? 0) === 0 && (data?.oses?.length ?? 0) === 0 ? (
              <p className="py-8 text-center text-sm text-muted-foreground">{t('plugin_website.analytics.empty')}</p>
            ) : (
              <>
                {deviceStack(t('plugin_website.analytics.devices.browser'), data?.browsers ?? [])}
                {deviceStack(t('plugin_website.analytics.devices.os'), data?.oses ?? [])}
              </>
            )}
          </CardContent>
        </Card>

        <Card className="flex flex-col transition-shadow hover:glow motion-reduce:transition-none">
          <CardHeader className="pb-2">
            <CardTitle className="text-base font-medium">{t('plugin_website.analytics.funnel.title')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-1 flex-col justify-center gap-5">
            {isLoading ? (
              <Skeleton className="h-28 w-full" />
            ) : (
              <>
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2 text-sm">
                    <span className="text-muted-foreground">{t('plugin_website.analytics.funnel.views')}</span>
                    <span className="num font-semibold">{fmt(views)}</span>
                  </div>
                  <div className="h-3 overflow-hidden rounded-full bg-muted">
                    <div className="h-full rounded-full bg-chart-1" style={{ width: views > 0 ? '100%' : '0%' }} />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2 text-sm">
                    <span className="text-muted-foreground">{t('plugin_website.analytics.funnel.inquiries')}</span>
                    <span className="num font-semibold">{fmt(inquiries)}</span>
                  </div>
                  <div className="h-3 overflow-hidden rounded-full bg-muted">
                    <div className="h-full rounded-full bg-chart-5" style={{ width: `${barW(inquiries)}%` }} />
                  </div>
                </div>
                <p className="text-center text-sm text-muted-foreground">
                  {t('plugin_website.analytics.funnel.rate')}{' '}
                  <span className="num text-xl font-semibold text-foreground">{rate.toFixed(2)}%</span>
                </p>
              </>
            )}
          </CardContent>
        </Card>
      </div>

      {/* 热门页 / 入口页：两列同排（窄屏折行） */}
      <div className="grid grid-cols-1 gap-3 md:gap-4 xl:grid-cols-2">
        <Card className="flex flex-col transition-shadow hover:glow motion-reduce:transition-none">
          <CardHeader className="pb-2">
            <CardTitle className="text-base font-medium">{t('plugin_website.analytics.paths.title')}</CardTitle>
          </CardHeader>
          <CardContent className="flex-1 space-y-3">
            {isLoading ? (
              <Skeleton className="h-40 w-full" />
            ) : paths.length === 0 ? (
              <p className="py-8 text-center text-sm text-muted-foreground">{t('plugin_website.analytics.empty')}</p>
            ) : (
              pathList(paths, pathMax, 'bg-chart-1')
            )}
          </CardContent>
        </Card>

        <Card className="flex flex-col transition-shadow hover:glow motion-reduce:transition-none">
          <CardHeader className="pb-2">
            <CardTitle className="text-base font-medium">{t('plugin_website.analytics.entries.title')}</CardTitle>
          </CardHeader>
          <CardContent className="flex-1 space-y-3">
            {isLoading ? (
              <Skeleton className="h-40 w-full" />
            ) : entries.length === 0 ? (
              <p className="py-8 text-center text-sm text-muted-foreground">{t('plugin_website.analytics.empty')}</p>
            ) : (
              pathList(entries, entryMax, 'bg-chart-4')
            )}
          </CardContent>
        </Card>
      </div>
    </PageContainer>
  )
}
