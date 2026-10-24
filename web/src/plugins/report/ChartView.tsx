import { useMemo, useState } from 'react'
import { Bar, BarChart, CartesianGrid, Cell, Legend, Line, LineChart, Pie, PieChart, Tooltip, XAxis, YAxis } from 'recharts'
import { useTranslation } from 'react-i18next'
import { ChartBox } from '@/framework/components/ChartBox'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { Skeleton } from '@/framework/ui/skeleton'
import { cn } from '@/lib/utils'
import type { ChartConfig, ChartType, QueryResult } from './api'
import { colorAt, fmtCell, toCartesian, toMetric, toPie } from './chartData'

export interface ChartViewProps {
  type: ChartType
  result?: QueryResult
  config: ChartConfig
  height: number
  loading?: boolean
  error?: boolean
  className?: string
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
    title: c.label,
    align: c.kind === 'measure' ? 'end' : undefined,
    mobile: i === 0 ? 'title' : undefined,
    render: (r) => <span className={cn(c.type === 'number' && 'num')}>{fmtCell(r[c.key], i18n.language)}</span>,
  }))
  const rows = result.rows.slice((page - 1) * pageSize, page * pageSize)
  return (
    <div className="overflow-auto" style={{ maxHeight: Math.max(height, 160) }}>
      <DataTable
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

export function ChartView({ type, result, config, height, loading, error, className }: ChartViewProps) {
  const { t, i18n } = useTranslation()
  const compact = (n: number) => new Intl.NumberFormat(i18n.language, { notation: 'compact', maximumFractionDigits: 1 }).format(n)
  const cart = useMemo(() => (result && (type === 'bar' || type === 'line') ? toCartesian(result) : null), [result, type])
  const pie = useMemo(() => (result && type === 'pie' ? toPie(result) : []), [result, type])
  const metric = useMemo(() => (result && type === 'metric' ? toMetric(result) : null), [result, type])

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
  if (type === 'metric' && metric) {
    return (
      <div className={cn('flex flex-col items-center justify-center gap-1', className)} style={{ height }}>
        <span className="text-sm text-muted-foreground">{metric.label}</span>
        <span className="num text-4xl font-semibold tracking-tight" style={{ color: colorAt(config.colors, 0) }}>
          {metric.value == null ? '-' : new Intl.NumberFormat(i18n.language).format(metric.value)}
          {config.unit ? <span className="ms-1 text-base font-normal text-muted-foreground">{config.unit}</span> : null}
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
    <ChartBox height={height} className={className}>
      {({ width, height: h }) => {
        if (type === 'pie') {
          return (
            <PieChart width={width} height={h}>
              <Pie data={pie} dataKey="value" nameKey="name" innerRadius="45%" outerRadius="75%" stroke="hsl(var(--card))" paddingAngle={2}>
                {pie.map((p, i) => (
                  <Cell key={p.name} fill={colorAt(config.colors, i)} />
                ))}
              </Pie>
              <Tooltip {...tip} />
              <Legend iconType="circle" iconSize={8} wrapperStyle={{ fontSize: 12 }} />
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
            {series.length > 1 ? <Legend iconType="circle" iconSize={8} wrapperStyle={{ fontSize: 12 }} /> : null}
          </>
        )
        return type === 'bar' ? (
          <BarChart {...common}>
            {parts}
            {series.map((s, i) => (
              <Bar key={s.key} dataKey={s.key} name={s.label} fill={colorAt(config.colors, i)} stackId={config.stacked ? 'a' : undefined} radius={[6, 6, 0, 0]} maxBarSize={32} />
            ))}
          </BarChart>
        ) : (
          <LineChart {...common}>
            {parts}
            {series.map((s, i) => (
              <Line key={s.key} dataKey={s.key} name={s.label} type="monotone" stroke={colorAt(config.colors, i)} strokeWidth={2} dot={false} />
            ))}
          </LineChart>
        )
      }}
    </ChartBox>
  )
}
