import { useEffect, useMemo, useState } from 'react'
import { Bar, BarChart, CartesianGrid, Cell, Legend, Line, LineChart, Pie, PieChart, Sector, Tooltip, XAxis, YAxis } from 'recharts'
import type { PieSectorDataItem } from 'recharts/types/polar/Pie'
import { useTranslation } from 'react-i18next'
import { ChartBox } from '@/framework/components/ChartBox'
import { useLegendToggle } from '@/framework/components/useLegendToggle'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { Skeleton } from '@/framework/ui/skeleton'
import { cn } from '@/lib/utils'
import type { ChartConfig, ChartType, QueryResult } from './api'
import { colorAt, fmtCell, toCartesian, toMetric, toPie } from './chartData'
import { builtinText, columnText } from './builtinText'

export interface ChartViewProps {
  type: ChartType
  result?: QueryResult
  config: ChartConfig
  height: number
  loading?: boolean
  error?: boolean
  className?: string
  /** 指标图外部已有标题时关掉内部标签，避免标题重复 */
  showMetricLabel?: boolean
}

const axis = { fontSize: 12, fill: 'hsl(var(--muted-foreground))' }
const tooltipStyle = {
  background: 'hsl(var(--popover))',
  border: '1px solid hsl(var(--border))',
  borderRadius: 12,
  color: 'hsl(var(--foreground))',
  fontSize: 12,
}

type Row = Record<string, string | number | boolean | null>

function TableChart({ result, height }: { result: QueryResult; height: number }) {
  const { t, i18n } = useTranslation()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const columns: Column<Row>[] = result.columns.map((c, i) => ({
    key: c.key,
    title: columnText(t, result.source, c),
    align: c.kind === 'measure' ? 'end' : undefined,
    mobile: i === 0 ? 'title' : undefined,
    render: (r) => <span className={cn(c.type === 'number' && 'num')}>{fmtCell(r[c.key], i18n.language)}</span>,
  }))
  const rows = result.rows.slice((page - 1) * pageSize, page * pageSize)
  return (
    <div className="overflow-auto" style={{ maxHeight: Math.max(height, 160) }}>
      <DataTable
        bare
        columns={columns}
        data={rows}
        rowKey={(r) => String(result.rows.indexOf(r))}
        emptyText={t('plugin_report.empty')}
        pagination={{
          page,
          pageSize,
          total: result.rows.length,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
      />
      {result.truncated ? <p className="px-2 py-1 text-xs text-muted-foreground">{t('plugin_report.truncated')}</p> : null}
    </div>
  )
}

export function ChartView({ type, result, config, height, loading, error, className, showMetricLabel = true }: ChartViewProps) {
  const { t, i18n } = useTranslation()
  const compact = (n: number) => new Intl.NumberFormat(i18n.language, { notation: 'compact', maximumFractionDigits: 1 }).format(n)
  const cart = useMemo(() => (result && (type === 'bar' || type === 'line') ? toCartesian(result) : null), [result, type])
  const pie = useMemo(() => (result && type === 'pie' ? toPie(result) : []), [result, type])
  const metric = useMemo(() => (result && type === 'metric' ? toMetric(result) : null), [result, type])
  // Recharts 默认只弹 Tooltip；hover 时把被指到的那一片推出来一圈，否则图表"点不动"。
  const [activeSlice, setActiveSlice] = useState<number>()
  const cartKeys = useMemo(() => cart?.series.map((s) => s.key) ?? [], [cart])
  const pieNames = useMemo(() => pie.map((p) => p.name), [pie])
  const { hidden, formatter } = useLegendToggle(undefined, cartKeys)
  // 饼图按扇区名切显隐：三项共用 dataKey，只能拿图例文字当 key。
  // 注意不能把数据行删掉——图例条目也来自 data，删了就没法再点回来；置 0 即可隐藏扇区。
  const { hidden: hiddenSlices, formatter: sliceFormatter } = useLegendToggle((value) => String(value ?? ''), pieNames)
  // 单系列柱状图：点柱子按该分类筛选（其它柱淡出），再点一次或点标签上的 × 取消。
  const [picked, setPicked] = useState<string | null>(null)
  const visiblePie = useMemo(
    () => pie.map((p) => (hiddenSlices.has(p.name) ? { ...p, value: 0 } : p)),
    [pie, hiddenSlices],
  )

  useEffect(() => {
    setPicked(null)
    setActiveSlice(undefined)
  }, [result])

  if (loading && !result) return <Skeleton className={cn('w-full', className)} style={{ height }} />
  if (error && !result) {
    return (
      <p role="alert" className={cn('flex items-center justify-center text-sm text-destructive', className)} style={{ height }}>
        {t('plugin_report.loadFailed')}
      </p>
    )
  }
  if (!result || (result.rows.length === 0 && type !== 'metric')) {
    return (
      <div className={cn('flex items-center justify-center text-sm text-muted-foreground', className)} style={{ height }}>
        {t('plugin_report.empty')}
      </div>
    )
  }
  if (type === 'table') return <TableChart result={result} height={height} />
  // 度量列名可能是内置键（builtinText）也可能是白名单元数据里的列（fieldText），按结果列的 field/source 取字典
  const colText = (key: string, label: string) => {
    const c = result.columns.find((x) => x.key === key)
    return c ? columnText(t, result.source, c) : builtinText(t, label)
  }
  if (type === 'metric' && metric) {
    return (
      <div className={cn('flex flex-col items-center justify-center gap-1', className)} style={{ height }}>
        {showMetricLabel ? <span className="text-sm text-muted-foreground">{colText('m0', metric.label)}</span> : null}
        <span className="num text-4xl font-semibold tracking-tight" style={{ color: colorAt(config.colors, 0) }}>
          {metric.value == null ? '-' : new Intl.NumberFormat(i18n.language).format(metric.value)}
          {config.unit ? <span className="ms-1 text-base font-normal text-muted-foreground">{builtinText(t, config.unit)}</span> : null}
        </span>
      </div>
    )
  }
  const tip = {
    contentStyle: tooltipStyle,
    labelStyle: { color: 'hsl(var(--muted-foreground))' },
    itemStyle: { color: 'hsl(var(--foreground))' },
    formatter: (v: unknown) => (typeof v === 'number' ? new Intl.NumberFormat(i18n.language).format(v) : String(v ?? '')),
  }

  return (
    <div className={cn('relative', className)}>
      {picked && type === 'bar' ? (
        <button
          type="button"
          onClick={() => setPicked(null)}
          className="absolute end-1 top-0 z-10 flex items-center gap-1 rounded-full border border-border bg-card px-2 py-0.5 text-xs text-muted-foreground hover:text-foreground"
        >
          {t('plugin_report.filteredBy', { name: picked })}
          <span aria-hidden="true">×</span>
        </button>
      ) : null}
    <ChartBox height={height}>
      {({ width, height: h }) => {
        if (type === 'pie') {
          return (
            <PieChart width={width} height={h}>
              <Pie
                data={visiblePie}
                dataKey="value"
                nameKey="name"
                innerRadius="45%"
                outerRadius="75%"
                stroke="hsl(var(--card))"
                paddingAngle={2}
                activeIndex={activeSlice}
                activeShape={(p: PieSectorDataItem) => <Sector {...p} outerRadius={Number(p.outerRadius) + 6} />}
                onMouseEnter={(_, i) => setActiveSlice(i)}
                onMouseLeave={() => setActiveSlice(undefined)}
              >
                {visiblePie.map((p) => (
                  <Cell key={p.name} fill={colorAt(config.colors, pie.findIndex((o) => o.name === p.name))} />
                ))}
              </Pie>
              <Tooltip {...tip} />
              <Legend
                iconType="circle"
                iconSize={8}
                wrapperStyle={{ fontSize: 12 }}
                formatter={sliceFormatter}
                inactiveColor="hsl(var(--muted-foreground))"
              />
            </PieChart>
          )
        }
        const series = cart?.series ?? []
        const common = { data: cart?.data ?? [], width, height: h, margin: { top: 8, right: 8, bottom: 0, left: 0 } }
        const parts = (
          <>
            <CartesianGrid strokeDasharray="4 4" stroke="hsl(var(--border))" vertical={false} />
            <XAxis dataKey="x" tick={axis} tickLine={false} axisLine={false} tickMargin={8} />
            <YAxis tick={axis} tickLine={false} axisLine={false} width={44} tickFormatter={(v) => compact(Number(v))} />
            <Tooltip {...tip} cursor={{ fill: 'hsl(var(--muted) / 0.5)' }} />
            {series.length > 1 ? (
              <Legend
                iconType="circle"
                iconSize={8}
                wrapperStyle={{ fontSize: 12 }}
                formatter={formatter}
                inactiveColor="hsl(var(--muted-foreground))"
              />
            ) : null}
          </>
        )
        return type === 'bar' ? (
          <BarChart {...common}>
            {parts}
            {series.map((s, i) => {
              // 单系列柱状图：点柱子按该分类筛选（其它柱淡出），再点一次取消。
              const single = series.length === 1
              return (
                <Bar
                  key={s.key}
                  dataKey={s.key}
                  name={colText(s.key, s.label)}
                  fill={colorAt(config.colors, i)}
                  stackId={config.stacked ? 'a' : undefined}
                  radius={[6, 6, 0, 0]}
                  maxBarSize={32}
                  hide={hidden.has(s.key)}
                  className={single ? 'cursor-pointer' : undefined}
                  // Bar 的回调给的是这一根柱子的数值信息，分类名要从同一索引的原始数据里取
                  onClick={
                    single
                      ? (_: unknown, index: number) => {
                          const key = String(cart?.data?.[index]?.x ?? '')
                          setPicked((cur) => (cur === key ? null : key))
                        }
                      : undefined
                  }
                >
                  {single
                    ? (cart?.data ?? []).map((row) => (
                        <Cell key={String(row.x)} fill={colorAt(config.colors, i)} fillOpacity={picked && picked !== String(row.x) ? 0.25 : 1} />
                      ))
                    : null}
                </Bar>
              )
            })}
          </BarChart>
        ) : (
          <LineChart {...common}>
            {parts}
            {series.map((s, i) => (
              <Line key={s.key} dataKey={s.key} name={colText(s.key, s.label)} type="monotone" stroke={colorAt(config.colors, i)} strokeWidth={2} dot={false} hide={hidden.has(s.key)} />
            ))}
          </LineChart>
        )
      }}
      </ChartBox>
    </div>
  )
}
