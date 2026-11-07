import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/framework/ui/skeleton'
import { Card } from '@/framework/ui/card'
import { StatCard } from '@/framework/components/StatCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { RankBars } from '@/framework/components/RankBars'
import { useCrmStats, type CrmStats } from './api'
import { formatMoney } from './format'

/** 漏斗展示顺序：线索 → 资格 → 方案 → 谈判 → 赢单/输单（计数与金额共用同一把尺）。 */
const STAGE_ORDER = ['lead', 'qualified', 'proposal', 'negotiate', 'won', 'lost'] as const

/** 统计 Tab 的三个图表分区的通用骨架：与卡片网格同一高度档，加载时不塌陷。 */
function PanelSkeleton() {
  return (
    <>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-24 w-full" />
        ))}
      </div>
      <Skeleton className="h-72 w-full" />
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <Skeleton className="h-56 w-full" />
        <Skeleton className="h-56 w-full" />
      </div>
    </>
  )
}

/**
 * 统计面板：概览卡 + 建档趋势 + 来源分布 + 商机漏斗（阶段计数 × 金额）。
 * 数据范围与客户列表同源（后端按 owner 裁），公海单独计数不随范围收窄。
 */
export function CrmStatsPanel() {
  const { t } = useTranslation()
  const { data: stats, isLoading } = useCrmStats()
  const s: CrmStats | undefined = stats

  const statCards = [
    { label: t('plugin_crm.type.person'), value: s?.persons ?? 0, tone: 'primary' as const },
    { label: t('plugin_crm.type.company'), value: s?.companies ?? 0, tone: 'info' as const },
    { label: t('plugin_crm.status.active'), value: s?.active ?? 0, tone: 'success' as const },
    { label: t('plugin_crm.status.inactive'), value: s?.inactive ?? 0, tone: 'warning' as const },
    { label: t('plugin_crm.stats.pool'), value: s?.pool ?? 0, tone: 'chart-3' as const },
    { label: t('plugin_crm.stats.openDeals'), value: s?.openDeals ?? 0, tone: 'chart-2' as const },
  ]

  // 来源分布：后端键含 none（无锚点/手工录入），展示名统一走 i18n
  const sourceItems = Object.entries(s?.sources ?? {}).map(([key, value]) => ({
    name: t(`plugin_crm.source.${key}`, { defaultValue: t('plugin_crm.source.none') }),
    value,
  }))
  // 阶段按漏斗顺序展示，金额（amountMinor）为零的档不画空条
  const stageItems = STAGE_ORDER.map((stage) => ({ stage, value: s?.stages?.[stage] ?? 0 }))
  // 金额漏斗的形状是 stage → 币种 → 金额：先并出实际出现过的币种集合，每个币种一组条
  const currencies = [...new Set(Object.values(s?.stageAmounts ?? {}).flatMap((m) => Object.keys(m)))].sort()
  const amountBlocks = currencies
    .map((currency) => ({
      currency,
      items: STAGE_ORDER.filter((stage) => (s?.stageAmounts?.[stage]?.[currency] ?? 0) > 0).map((stage) => ({
        stage,
        value: s?.stageAmounts?.[stage]?.[currency] ?? 0,
      })),
    }))
    .filter((b) => b.items.length > 0)

  return (
    <div className="flex flex-col gap-4">
      {isLoading || !s ? (
        <PanelSkeleton />
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
            {statCards.map((c) => (
              <StatCard key={c.label} title={c.label} value={c.value} tone={c.tone} />
            ))}
          </div>
          <Card className="p-3">
            <TrendChart
              data={s.createdDaily.map((d) => ({ date: d.date, count: d.count }))}
              xKey="date"
              type="bar"
              switchable={false}
              height={220}
              series={[{ key: 'count', label: t('plugin_crm.stats.createdSeries') }]}
            />
          </Card>
          <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
            <Card className="p-3">
              <h3 className="mb-2 text-sm font-semibold text-foreground">{t('plugin_crm.stats.bySource')}</h3>
              {sourceItems.length ? (
                <RankBars items={sourceItems} />
              ) : (
                <p className="py-4 text-center text-xs text-muted-foreground">{t('plugin_crm.empty')}</p>
              )}
            </Card>
            <Card className="p-3">
              <h3 className="mb-2 text-sm font-semibold text-foreground">{t('plugin_crm.stats.byStage')}</h3>
              {stageItems.some((i) => i.value > 0) ? (
                <>
                  <RankBars items={stageItems.map((i) => ({ name: t(`plugin_crm.stage.${i.stage}`), value: i.value }))} />
                  {amountBlocks.map(({ currency, items }) => (
                    <div key={currency}>
                      <div className="mt-3 mb-1 text-xs font-medium text-muted-foreground">
                        {t('plugin_crm.stats.byStageAmount')} · {currency}
                      </div>
                      <RankBars
                        items={items.map((i) => ({ name: t(`plugin_crm.stage.${i.stage}`), value: i.value }))}
                        formatValue={(v) => formatMoney(v, currency)}
                      />
                    </div>
                  ))}
                </>
              ) : (
                <p className="py-4 text-center text-xs text-muted-foreground">{t('plugin_crm.empty')}</p>
              )}
            </Card>
          </div>
        </>
      )}
    </div>
  )
}
