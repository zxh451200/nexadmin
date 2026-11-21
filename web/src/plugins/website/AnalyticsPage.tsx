import { useState } from 'react'
import { Eye, Inbox, Radio, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { TrendArea } from '@/framework/modules/dashboard/cards/TrendArea'
import { ChartCard, type ChartCardRange } from '@/framework/components/ChartCard'
import { StatCard } from '@/framework/components/StatCard'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Badge } from '@/framework/ui/badge'
import { Skeleton } from '@/framework/ui/skeleton'
import { useAnalytics, type AnalyticsDays, type AnalyticsRefRow } from './api'

// 访问统计：公开官网的第一方轻量盘——浏览 / 访客 / 询盘漏斗 + 实时在站。
// 口径（后端 plugins/website/analytics.go）：匿名会话 cookie 去重访客，bot 不采集，
// day 按站点时区归日；匿名统计不含 IP 与个人身份。

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

  const ranges: ChartCardRange[] = DAYS.map((d, i) => ({
    value: String(d),
    label: t(`plugin_website.analytics.range.${RANGE_KEYS[i]}`),
  }))

  // 漏斗两级（浏览 → 询盘）：询盘柱按占浏览的比例，4% 保底可见；转化率 = 询盘 / 浏览
  const views = data?.views ?? 0
  const inquiries = data?.inquiries ?? 0
  const rate = views > 0 ? (inquiries / views) * 100 : 0
  const barW = (v: number) => (views > 0 ? Math.max((v / views) * 100, 4) : 0)

  const refRows = data?.referrers ?? []
  const refMax = Math.max(...refRows.map((r) => r.sessions), 1)
  const paths = data?.topPaths ?? []
  const pathMax = Math.max(...paths.map((p) => p.views), 1)

  const kindLabel = (kind: AnalyticsRefRow['kind']) => t(`plugin_website.analytics.refKind.${kind}`)

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.analytics.title')} description={t('plugin_website.analytics.description')} />

      {/* KPI 行：浏览 / 访客 / 询盘带趋势 spark；实时访客单独一张（近 5 分钟窗口，60s 轮询） */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4 md:gap-4">
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
          title={t('plugin_website.analytics.kpi.inquiries')}
          value={data?.inquiries ?? 0}
          icon={<Inbox aria-hidden="true" />}
          spark={data?.trend.map((p) => p.inquiries)}
          tone="chart-4"
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
              { key: 'inquiries', label: t('plugin_website.analytics.trend.series.inquiries'), color: 'hsl(var(--chart-4))' },
            ]}
          />
        )}
      </ChartCard>

      {/* 来源 / 热门页 / 漏斗：三列同排（窄屏折行），份额条给出「谁在贡献流量」的量感 */}
      <div className="grid grid-cols-1 gap-3 md:gap-4 xl:grid-cols-3">
        <Card className="flex flex-col transition-shadow hover:glow motion-reduce:transition-none">
          <CardHeader className="pb-2">
            <CardTitle className="text-base font-medium">{t('plugin_website.analytics.sources.title')}</CardTitle>
          </CardHeader>
          <CardContent className="flex-1 space-y-3">
            {isLoading ? (
              <Skeleton className="h-40 w-full" />
            ) : refRows.length === 0 ? (
              <p className="py-8 text-center text-sm text-muted-foreground">{t('plugin_website.analytics.empty')}</p>
            ) : (
              refRows.map((r, i) => (
                <div key={`${r.kind}-${r.host}-${i}`} className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <span className="flex min-w-0 items-center gap-2">
                      <Badge variant={refTone(r.kind) ? undefined : 'secondary'} tone={refTone(r.kind)}>
                        {kindLabel(r.kind)}
                      </Badge>
                      {/* direct 无来源域：徽章即全部信息，不重复一遍分类名 */}
                      {r.host ? <span className="num truncate text-sm">{r.host}</span> : null}
                    </span>
                    <span className="num shrink-0 text-sm font-medium">{fmt(r.sessions)}</span>
                  </div>
                  <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                    <div className="h-full rounded-full bg-chart-2" style={{ width: `${(r.sessions / refMax) * 100}%` }} />
                  </div>
                </div>
              ))
            )}
          </CardContent>
        </Card>

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
              paths.map((p, i) => (
                <div key={`${p.path}-${i}`} className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <span className="num min-w-0 truncate text-sm" title={p.path}>{p.path}</span>
                    <span className="num shrink-0 text-sm font-medium">{fmt(p.views)}</span>
                  </div>
                  <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                    <div className="h-full rounded-full bg-chart-1" style={{ width: `${(p.views / pathMax) * 100}%` }} />
                  </div>
                </div>
              ))
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
                    <div className="h-full rounded-full bg-chart-4" style={{ width: `${barW(inquiries)}%` }} />
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
    </PageContainer>
  )
}
