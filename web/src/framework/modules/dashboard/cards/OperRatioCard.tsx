import { Cell, Legend, Pie, PieChart, Sector, Tooltip } from 'recharts'
import type { PieSectorDataItem } from 'recharts/types/polar/Pie'
import { useTranslation } from 'react-i18next'
import { ChartBox } from '@/framework/components/ChartBox'
import { useLegendToggle } from '@/framework/components/useLegendToggle'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardStats } from '../api'

export default function OperRatioCard() {
  const { t } = useTranslation()
  const { data } = useDashboardStats()
  // 图例点击切显隐；饼图各扇区共用 dataKey，按名称取 key，并把隐藏项置 0 而不是删掉数据（否则图例点不回来）。
  const sliceNames = (data?.operTypes ?? []).map((o) => t(`dashboard.oper.${o.type}`, { defaultValue: o.type }))
  const { hidden, formatter } = useLegendToggle((value) => String(value ?? ''), sliceNames)
  const slices = (data?.operTypes ?? []).map((o, i) => ({
    name: t(`dashboard.oper.${o.type}`, { defaultValue: o.type }),
    value: hidden.has(t(`dashboard.oper.${o.type}`, { defaultValue: o.type })) ? 0 : o.value,
    color: `hsl(var(--chart-${(i % 5) + 1}))`,
  }))
  return (
    <Card className="flex h-full flex-col transition-shadow hover:glow motion-reduce:transition-none">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">{t('dashboard.operRatio')}</CardTitle>
      </CardHeader>
      <CardContent className="flex-1">
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
                  inactiveColor="hsl(var(--muted-foreground))"
                />
              </PieChart>
            )}
          </ChartBox>
        ) : (
          <Skeleton className="h-64 w-full" />
        )}
      </CardContent>
    </Card>
  )
}
