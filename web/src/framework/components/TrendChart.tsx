import { useId, useState } from 'react'
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Line, LineChart, Tooltip, XAxis, YAxis, Legend } from 'recharts'
import { useTranslation } from 'react-i18next'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { cn } from '@/lib/utils'
import { ChartBox } from './ChartBox'
import { EmptyState } from './EmptyState'
import { Segmented } from './Segmented'
import { useLegendToggle } from './useLegendToggle'

export type TrendType = 'line' | 'area' | 'bar'

export interface TrendSeries {
  key: string
  label: string
  /** 默认取 hsl(var(--chart-n)) */
  color?: string
}

export interface TrendChartProps {
  data: Record<string, unknown>[]
  series: TrendSeries[]
  xKey: string
  type?: TrendType
  /** 是否显示折线/面积/柱状切换，默认显示 */
  switchable?: boolean
  height?: number
  className?: string
}

const TYPES: TrendType[] = ['line', 'area', 'bar']
const color = (i: number, c?: string) => c ?? `hsl(var(--chart-${(i % 5) + 1}))`
const axis = { fontSize: 12, fill: 'hsl(var(--muted-foreground))' }

// 玻璃质感的提示框，跟随明暗主题
const tooltipStyle = {
  background: 'hsl(var(--popover) / 0.92)',
  backdropFilter: 'blur(8px)',
  border: '1px solid hsl(var(--border))',
  borderRadius: 12,
  color: 'hsl(var(--foreground))',
  fontSize: 12,
  boxShadow: '0 12px 32px -12px hsl(232 40% 10% / 0.35)',
}

export function TrendChart({ data, series, xKey, type = 'line', switchable = true, height: heightProp = 280, className }: TrendChartProps) {
  const { t, i18n } = useTranslation()
  const isMobile = useIsMobile()
  const height = isMobile ? Math.min(heightProp, 200) : heightProp // 手机上图表矮一些，一屏能多看几张
  const [kind, setKind] = useState<TrendType>(type)
  const { hidden, formatter } = useLegendToggle(undefined, series.map((s) => s.key))
  const uid = useId().replace(/:/g, '')
  const gid = (i: number) => `tc-${uid}-${i}`

  // 大数字用紧凑格式（16000 → 1.6万 / 16K），避免纵轴文字被裁
  const compact = (n: number) => new Intl.NumberFormat(i18n.language, { notation: 'compact', maximumFractionDigits: 1 }).format(n)

  const common = { data, margin: { top: 8, right: 8, bottom: 0, left: 0 } }
  const parts = (
    <>
      <defs>
        {series.map((s, i) => (
          <linearGradient key={s.key} id={gid(i)} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color(i, s.color)} stopOpacity={kind === 'bar' ? 1 : 0.32} />
            <stop offset="100%" stopColor={color(i, s.color)} stopOpacity={kind === 'bar' ? 0.55 : 0} />
          </linearGradient>
        ))}
      </defs>
      <CartesianGrid strokeDasharray="4 4" stroke="hsl(var(--border))" strokeOpacity={0.7} vertical={false} />
      <XAxis dataKey={xKey} tick={axis} tickLine={false} axisLine={false} tickMargin={8} />
      <YAxis allowDecimals={false} tick={axis} tickLine={false} axisLine={false} width={44} tickFormatter={(v) => compact(Number(v))} />
      <Tooltip
        cursor={{ stroke: 'hsl(var(--primary))', strokeOpacity: 0.35, strokeDasharray: '4 4', fill: 'hsl(var(--muted) / 0.5)' }}
        contentStyle={tooltipStyle}
        labelStyle={{ color: 'hsl(var(--muted-foreground))', marginBottom: 4 }}
        itemStyle={{ color: 'hsl(var(--foreground))' }}
        formatter={(v: unknown) => (typeof v === 'number' ? new Intl.NumberFormat(i18n.language).format(v) : (v as React.ReactNode))}
      />
      {series.length > 1 && (
        <Legend
          iconType="circle"
          iconSize={8}
          wrapperStyle={{ fontSize: 12, paddingTop: 8 }}
          formatter={formatter}
          // 默认 #ccc 在白卡上等于隐形，关掉的系列得留得下可点回来的痕迹
          inactiveColor="hsl(var(--muted-foreground))"
        />
      )}
    </>
  )

  return (
    <div className={cn('w-full', className)}>
      {switchable && (
        <Segmented
          ariaLabel={t('charts.chartType')}
          className="mb-3 ms-auto"
          value={kind}
          onChange={(v) => setKind(v as TrendType)}
          options={TYPES.map((k) => ({ value: k, label: t(`charts.${k}`) }))}
        />
      )}
      {data.length === 0 ? (
        <div className="flex items-center justify-center" style={{ height }}>
          <EmptyState className="py-0" />
        </div>
      ) : (
        <ChartBox height={height}>
          {({ width, height: h }) =>
            kind === 'bar' ? (
              <BarChart {...common} width={width} height={h} barGap={4}>
                {parts}
                {series.map((s, i) => (
                  <Bar key={s.key} dataKey={s.key} name={s.label} fill={`url(#${gid(i)})`} radius={[6, 6, 0, 0]} maxBarSize={28} hide={hidden.has(s.key)} />
                ))}
              </BarChart>
            ) : kind === 'area' ? (
              <AreaChart {...common} width={width} height={h}>
                {parts}
                {series.map((s, i) => (
                  <Area
                    key={s.key}
                    type="monotone"
                    dataKey={s.key}
                    name={s.label}
                    stroke={color(i, s.color)}
                    fill={`url(#${gid(i)})`}
                    strokeWidth={2.5}
                    dot={false}
                    activeDot={{ r: 5, strokeWidth: 2, stroke: 'hsl(var(--card))' }}
                    hide={hidden.has(s.key)}
                  />
                ))}
              </AreaChart>
            ) : (
              <LineChart {...common} width={width} height={h}>
                {parts}
                {series.map((s, i) => (
                  <Line
                    key={s.key}
                    type="monotone"
                    dataKey={s.key}
                    name={s.label}
                    stroke={color(i, s.color)}
                    strokeWidth={2.5}
                    dot={false}
                    activeDot={{ r: 5, strokeWidth: 2, stroke: 'hsl(var(--card))' }}
                    hide={hidden.has(s.key)}
                  />
                ))}
              </LineChart>
            )
          }
        </ChartBox>
      )}
    </div>
  )
}
