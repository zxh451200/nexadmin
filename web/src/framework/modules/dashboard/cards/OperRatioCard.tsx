import { useState } from 'react'
import { Cell, Legend, Pie, PieChart, Sector, Tooltip } from 'recharts'
import type { PieSectorDataItem } from 'recharts/types/polar/Pie'
import { useTranslation } from 'react-i18next'
import { ChartBox } from '@/framework/components/ChartBox'
import { ChartCard } from '@/framework/components/ChartCard'
import { useLegendToggle } from '@/framework/components/useLegendToggle'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

export default function OperRatioCard() {
  const { t } = useTranslation()
  const { data } = useDashboardStats()
  // Recharts 默认只弹 Tooltip，扇区本身不动；hover 时把那一片推出来一圈。
  const [active, setActive] = useState<number>()
  // 图例点击切显隐；饼图各扇区共用 dataKey，按名称取 key，并把隐藏项置 0 而不是删掉数据（否则图例点不回来）。
  const sliceNames = (data?.operTypes ?? []).map((o) => t(`dashboard.oper.${o.type}`, { defaultValue: o.type }))
  const { hidden, formatter } = useLegendToggle((value) => String(value ?? ''), sliceNames)
  const slices = (data?.operTypes ?? []).map((o, i) => ({
    name: t(`dashboard.oper.${o.type}`, { defaultValue: o.type }),
    value: hidden.has(t(`dashboard.oper.${o.type}`, { defaultValue: o.type })) ? 0 : o.value,
    color: `hsl(var(--chart-${(i % 5) + 1}))`,
  }))
  return (
    <ChartCard title={t('dashboard.operRatio')}>
      {data ? (
        <ChartBox height={260}>
          {({ width, height }) => (
            <PieChart width={width} height={height}>
              <Pie
                data={slices}
                dataKey="value"
                nameKey="name"
                innerRadius="55%"
                outerRadius="80%"
                paddingAngle={2}
                stroke="hsl(var(--card))"
                activeIndex={active}
                activeShape={(p: PieSectorDataItem) => <Sector {...p} outerRadius={Number(p.outerRadius) + 6} />}
                onMouseEnter={(_, i) => setActive(i)}
                onMouseLeave={() => setActive(undefined)}
              >
                {slices.map((o) => (
                  <Cell key={o.name} fill={o.color} />
                ))}
              </Pie>
              <Tooltip
                contentStyle={{ background: 'hsl(var(--card))', border: '1px solid hsl(var(--border))', borderRadius: 8, color: 'hsl(var(--foreground))', fontSize: 12 }}
                itemStyle={{ color: 'hsl(var(--foreground))' }}
              />
              <Legend wrapperStyle={{ fontSize: 12 }} formatter={formatter} />
            </PieChart>
          )}
        </ChartBox>
      ) : (
        <Skeleton className="h-64 w-full" />
      )}
    </ChartCard>
  )
}
