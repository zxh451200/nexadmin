import { useState } from 'react'
import { Cell, Legend, Pie, PieChart, Sector, Tooltip } from 'recharts'
import type { PieSectorDataItem } from 'recharts/types/polar/Pie'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'
import { Can } from '@/framework/auth/Can'
import { ChartBox } from '@/framework/components/ChartBox'
import { ChartCard, type ChartCardRange } from '@/framework/components/ChartCard'
import { useLegendToggle } from '@/framework/components/useLegendToggle'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats, type TrendDays } from '../api'

const DAYS: TrendDays[] = [7, 14, 30]
const RANGE_KEYS = ['d7', 'd14', 'd30'] as const

export default function OperRatioCard() {
  const { t } = useTranslation()
  const [days, setDays] = useState<TrendDays>(30)
  const { data } = useDashboardStats(days)
  // 图例点击切显隐；饼图各扇区共用 dataKey，按名称取 key，并把隐藏项置 0 而不是删掉数据（否则图例点不回来）。
  const sliceNames = (data?.operTypes ?? []).map((o) => t(`dashboard.oper.${o.type}`, { defaultValue: o.type }))
  const { hidden, formatter } = useLegendToggle((value) => String(value ?? ''), sliceNames)
  const slices = (data?.operTypes ?? []).map((o, i) => ({
    name: t(`dashboard.oper.${o.type}`, { defaultValue: o.type }),
    value: hidden.has(t(`dashboard.oper.${o.type}`, { defaultValue: o.type })) ? 0 : o.value,
    color: `hsl(var(--chart-${(i % 5) + 1}))`,
  }))
  const ranges: ChartCardRange[] = DAYS.map((d, i) => ({ value: String(d), label: t(`dashboard.trendRange.${RANGE_KEYS[i]}`) }))
  return (
    <ChartCard
      title={t('dashboard.operRatio')}
      ranges={ranges}
      range={String(days)}
      onRangeChange={(v) => setDays(Number(v) as TrendDays)}
      // 操作日志是这份占比的明细去处：入口与报表页同一权限口径（operLog:list）
      actions={
        <Can perm="operLog:list">
          <Link
            to="/monitor/oper-log"
            className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            {t('dashboard.viewReport')}
            <ArrowRight className="size-3.5" aria-hidden="true" />
          </Link>
        </Can>
      }
      className="flex h-full flex-col"
    >
      <div className="flex-1">
        {data ? (
          <ChartBox height={260}>
            {({ width, height }) => (
              <PieChart width={width} height={height}>
                {/* recharts 3：hover 激活由组件内部状态驱动，activeShape 自动生效 */}
                <Pie
                  data={slices}
                  dataKey="value"
                  nameKey="name"
                  innerRadius="55%"
                  outerRadius="80%"
                  paddingAngle={2}
                  stroke="hsl(var(--card))"
                  activeShape={(p: PieSectorDataItem) => <Sector {...p} outerRadius={Number(p.outerRadius) + 6} />}
                >
                  {slices.map((o) => (
                    <Cell key={o.name} fill={o.color} />
                  ))}
                </Pie>
                <Tooltip
                  cursor={false}
                  contentStyle={{ background: 'hsl(var(--popover))', border: '1px solid hsl(var(--border))', borderRadius: 8, color: 'hsl(var(--popover-foreground))', fontSize: 12 }}
                  itemStyle={{ color: 'hsl(var(--popover-foreground))' }}
                />
                {/* 图例置顶、左对齐，与趋势卡同款 */}
                <Legend
                  verticalAlign="top"
                  align="left"
                  iconType="circle"
                  iconSize={8}
                  wrapperStyle={{ fontSize: 12, paddingBottom: 8 }}
                  formatter={formatter}
                  inactiveColor="hsl(var(--muted-foreground) / 0.55)"
                />
              </PieChart>
            )}
          </ChartBox>
        ) : (
          <Skeleton className="h-64 w-full" />
        )}
      </div>
    </ChartCard>
  )
}
