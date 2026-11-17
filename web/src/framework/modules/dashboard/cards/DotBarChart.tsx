import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Bar, BarChart, CartesianGrid, Tooltip, XAxis, YAxis } from 'recharts'
import { ChartBox } from '@/framework/components/ChartBox'
import { EmptyState } from '@/framework/components/EmptyState'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { cn } from '@/lib/utils'

/** 图表形态照抄 shadcn 教育仪表盘的 AssignmentStatus：SVG 点阵纹理填充的分组柱状图，
    图例置顶、cursor=false、仅横向虚线网格、细描边圆角柱。颜色由调用方传 token（深色下灰阶化）。 */
export interface DotBarSeries {
  key: string
  label: string
  /** CSS 颜色值，如 hsl(var(--chart-2)) */
  color: string
}

export interface DotBarChartProps {
  data: Record<string, unknown>[]
  xKey: string
  series: DotBarSeries[]
  height?: number
  className?: string
}

interface TooltipEntry {
  dataKey?: string | number
  value?: number | string
}

function DotTooltipContent({
  active,
  payload,
  label,
  series,
  fmt,
}: {
  active?: boolean
  payload?: TooltipEntry[]
  label?: unknown
  series: DotBarSeries[]
  fmt: (n: number) => string
}) {
  if (!active || !payload?.length) return null
  const rows = payload
    .map((p) => ({ s: series.find((x) => x.key === p.dataKey), v: Number(p.value) }))
    .filter((r): r is { s: DotBarSeries; v: number } => Boolean(r.s) && Number.isFinite(r.v))
  if (!rows.length) return null
  return (
    <div className="min-w-28 rounded-md border bg-popover p-2.5 text-xs shadow-md">
      <p className="mb-1.5 text-muted-foreground">{String(label ?? '')}</p>
      {rows.map(({ s, v }) => (
        <div key={s.key} className="flex items-center justify-between gap-4 py-0.5">
          <span className="flex items-center gap-1.5">
            <span className="size-2.5 rounded-[2px]" style={{ background: s.color }} aria-hidden="true" />
            {s.label}
          </span>
          <span className="num font-medium">{fmt(v)}</span>
        </div>
      ))}
    </div>
  )
}

export function DotBarChart({ data, xKey, series, height: heightProp = 280, className }: DotBarChartProps) {
  const { i18n } = useTranslation()
  const isMobile = useIsMobile()
  const height = isMobile ? Math.min(heightProp, 200) : heightProp
  const uid = useId().replace(/:/g, '')
  const pid = (key: string) => `dot-${uid}-${key}`
  const fmt = (n: number) => new Intl.NumberFormat(i18n.language).format(n)

  return (
    <div className={cn('w-full', className)}>
      {/* 图例置顶、左对齐 */}
      <div className="mb-3 flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
        {series.map((s) => (
          <span key={s.key} className="flex items-center gap-1.5">
            <span className="size-2.5 rounded-[2px]" style={{ background: s.color }} aria-hidden="true" />
            {s.label}
          </span>
        ))}
      </div>
      {data.length === 0 ? (
        <div className="flex items-center justify-center" style={{ height }}>
          <EmptyState className="py-0" />
        </div>
      ) : (
      <ChartBox height={height}>
        {({ width, height: h }) => (
          <BarChart data={data} width={width} height={h} margin={{ top: 0, right: 0, bottom: 0, left: 0 }} barGap={4}>
            <defs>
              {/* 半透明底色 + 两粒圆点：模板的点阵纹理，柱体仍带 0.5px 细描边 */}
              {series.map((s) => (
                <pattern key={s.key} id={pid(s.key)} width={10} height={10} patternUnits="userSpaceOnUse">
                  <rect width={10} height={10} fill={s.color} fillOpacity={0.28} />
                  <circle cx={3} cy={3} r={1.6} fill={s.color} />
                  <circle cx={8} cy={8} r={1.6} fill={s.color} />
                </pattern>
              ))}
            </defs>
            <CartesianGrid vertical={false} strokeDasharray="4 4" stroke="hsl(var(--border))" />
            <XAxis
              dataKey={xKey}
              tick={{ fontSize: 11, fill: 'hsl(var(--muted-foreground))' }}
              tickLine={false}
              axisLine={false}
              tickMargin={10}
            />
            <YAxis
              allowDecimals={false}
              tick={{ fontSize: 11, fill: 'hsl(var(--muted-foreground))' }}
              tickLine={false}
              axisLine={false}
              width={40}
              tickFormatter={(v) =>
                new Intl.NumberFormat(i18n.language, { notation: 'compact', maximumFractionDigits: 1 }).format(Number(v))
              }
            />
            <Tooltip cursor={false} content={<DotTooltipContent series={series} fmt={fmt} />} />
            {series.map((s) => (
              <Bar
                key={s.key}
                dataKey={s.key}
                fill={`url(#${pid(s.key)})`}
                stroke={s.color}
                strokeWidth={0.5}
                strokeOpacity={0.5}
                radius={[4, 4, 0, 0]}
                maxBarSize={26}
              />
            ))}
          </BarChart>
        )}
      </ChartBox>
      )}
    </div>
  )
}
