import { useTranslation } from 'react-i18next'
import { Area, ComposedChart, CartesianGrid, Line, Tooltip, XAxis, YAxis } from 'recharts'
import { ChartBox } from '@/framework/components/ChartBox'
import { EmptyState } from '@/framework/components/EmptyState'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { cn } from '@/lib/utils'

/** 趋势图照抄模板 performance-overview 的画法：ComposedChart——主系列渐变面积（0.36→0.04）
    + 次系列细折线，monotone 平滑、无 dot、无 cursor、仅横向网格（0.5 透明度）、
    X 轴 minTickGap 48 防挤、tooltip 为日期标题 + 线形指示 + 右对齐粗体数值。

    曲线类型用 monotone 而不是 natural：natural 是自然三次样条，相邻数据点落差大时（比如节假日
    归零前后）会在两点之间冲出数据范围、掉到 0 线以下；而 Area 渲染带 clipPath（裁剪到绘图区），
    冲出基线的那一段直接被裁掉，看起来就是「线在低谷处被盖住了」。monotone 是保单调插值，
    曲线不会超出相邻点的值域，既能保住平滑感又不会掉出坐标区。

    Y 轴显式给 domain 留白（峰值 8% 顶部、底部 4%）：峰值不贴顶，0 值也抬离裁剪边缘，
    否则落在 0 上的折线（如询盘）会被裁掉一半、显得几乎看不见。 */

export interface TrendBarSeries {
  key: string
  label: string
  /** CSS 颜色值，如 hsl(var(--chart-2)) */
  color: string
}

export interface TrendAreaProps {
  data: Record<string, unknown>[]
  xKey: string
  series: TrendBarSeries[]
  height?: number
  className?: string
}

interface TooltipEntry {
  dataKey?: string | number
  value?: number | string
}

function TrendTooltipContent({
  active,
  payload,
  label,
  series,
  fmt,
}: {
  active?: boolean
  payload?: TooltipEntry[]
  label?: unknown
  series: TrendBarSeries[]
  fmt: (n: number) => string
}) {
  if (!active || !payload?.length) return null
  const rows = payload
    .map((p) => ({ s: series.find((x) => x.key === p.dataKey), v: Number(p.value) }))
    .filter((r): r is { s: TrendBarSeries; v: number } => Boolean(r.s) && Number.isFinite(r.v))
  if (!rows.length) return null
  return (
    <div className="w-44 rounded-md border bg-popover p-3 text-xs shadow-md">
      <p className="mb-2 font-semibold">{String(label ?? '')}</p>
      {rows.map(({ s, v }) => (
        <div key={s.key} className="flex items-center justify-between gap-3 py-0.5">
          <span className="flex min-w-0 items-center gap-2">
            <span className="h-3 w-0.5 shrink-0 rounded-full" style={{ background: s.color }} aria-hidden="true" />
            <span className="truncate text-muted-foreground">{s.label}</span>
          </span>
          <span className="num font-semibold">{fmt(v)}</span>
        </div>
      ))}
    </div>
  )
}

export function TrendArea({ data, xKey, series, height: heightProp = 280, className }: TrendAreaProps) {
  const { i18n } = useTranslation()
  const isMobile = useIsMobile()
  const height = isMobile ? Math.min(heightProp, 200) : heightProp
  const fmt = (n: number) => new Intl.NumberFormat(i18n.language).format(n)
  const [primary, ...rest] = series
  const uid = `${primary?.key ?? 's'}-fill`

  // Y 轴上下留白：所有系列的最大值决定顶部空间，底部留 4% 让 0 值离开裁剪边缘
  const peak = data.reduce((max, row) => {
    let m = max
    for (const s of series) {
      const v = Number(row[s.key])
      if (Number.isFinite(v) && v > m) m = v
    }
    return m
  }, 0)
  // peak 为 0（窗口内全归零）时给一个 [−0.04, 1] 的兜底区间，否则平坦的 0 线会压在下边缘上
  const yTop = peak > 0 ? peak * 1.08 : 1
  const yBottom = peak > 0 ? -peak * 0.04 : -0.04

  return (
    <div className={cn('w-full', className)}>
      {/* 图例置顶、右对齐（与模板 ChartLegend verticalAlign=top 同位） */}
      <div className="mb-3 flex flex-wrap items-center justify-end gap-4 text-xs text-muted-foreground">
        {series.map((s) => (
          <span key={s.key} className="flex items-center gap-1.5">
            <span className="h-3 w-0.5 rounded-full" style={{ background: s.color }} aria-hidden="true" />
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
            <ComposedChart data={data} width={width} height={h} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id={uid} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor={primary?.color} stopOpacity={0.36} />
                  <stop offset="95%" stopColor={primary?.color} stopOpacity={0.04} />
                </linearGradient>
              </defs>
              <CartesianGrid vertical={false} stroke="hsl(var(--border))" strokeOpacity={0.5} />
              <YAxis hide domain={[yBottom, yTop]} />
              <XAxis
                dataKey={xKey}
                tick={{ fontSize: 11, fill: 'hsl(var(--muted-foreground))' }}
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                minTickGap={48}
              />
              <Tooltip cursor={false} content={<TrendTooltipContent series={series} fmt={fmt} />} />
              {primary ? (
                <Area
                  dataKey={primary.key}
                  type="monotone"
                  stroke={primary.color}
                  strokeWidth={1.5}
                  fill={`url(#${uid})`}
                  fillOpacity={1}
                  dot={false}
                  activeDot={{ r: 3, strokeWidth: 0 }}
                />
              ) : null}
              {rest.map((s) => (
                <Line
                  key={s.key}
                  dataKey={s.key}
                  type="monotone"
                  stroke={s.color}
                  strokeWidth={1.3}
                  dot={false}
                  activeDot={{ r: 3, strokeWidth: 0 }}
                />
              ))}
            </ComposedChart>
          )}
        </ChartBox>
      )}
    </div>
  )
}
