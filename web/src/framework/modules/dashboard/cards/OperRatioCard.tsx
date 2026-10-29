import { useState } from 'react'
import { Cell, Legend, Pie, PieChart, Sector, Tooltip } from 'recharts'
import type { PieSectorDataItem } from 'recharts/types/polar/Pie'
import { useTranslation } from 'react-i18next'
import { ChartBox } from '@/framework/components/ChartBox'
import { ChartCard } from '@/framework/components/ChartCard'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

export default function OperRatioCard() {
  const { t } = useTranslation()
  const { data } = useDashboardStats()
  // Recharts 默认只弹 Tooltip，扇区本身不动；hover 时把那一片推出来一圈。
  const [active, setActive] = useState<number>()
  return (
    <ChartCard title={t('dashboard.operRatio')}>
      {data ? (
        <ChartBox height={260}>
          {({ width, height }) => (
            <PieChart width={width} height={height}>
              <Pie
                data={data.operTypes.map((o) => ({ name: t(`dashboard.oper.${o.type}`, { defaultValue: o.type }), value: o.value }))}
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
                {data.operTypes.map((o, i) => (
                  <Cell key={o.type} fill={`hsl(var(--chart-${(i % 5) + 1}))`} />
                ))}
              </Pie>
              <Tooltip
                contentStyle={{ background: 'hsl(var(--card))', border: '1px solid hsl(var(--border))', borderRadius: 8, color: 'hsl(var(--foreground))', fontSize: 12 }}
                itemStyle={{ color: 'hsl(var(--foreground))' }}
              />
              <Legend wrapperStyle={{ fontSize: 12 }} />
            </PieChart>
          )}
        </ChartBox>
      ) : (
        <Skeleton className="h-64 w-full" />
      )}
    </ChartCard>
  )
}
