import { useId } from 'react'
import { Area, AreaChart } from 'recharts'
import { ArrowDownRight, ArrowUpRight, Minus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent } from '@/framework/ui/card'
import { cn } from '@/lib/utils'
import { ChartBox } from './ChartBox'

/** 卡片色调：只允许语义令牌，颜色由 tokens.css 的 [data-tone] 给出。 */
export type StatTone =
  | 'chart-1' | 'chart-2' | 'chart-3' | 'chart-4' | 'chart-5'
  | 'success' | 'warning' | 'info' | 'destructive' | 'primary'

export interface StatCardProps {
  title: React.ReactNode
  value: number | string
  /** 数值后面的补充文字，如「/ 500 GB」 */
  hint?: React.ReactNode
  /** 同比/环比百分比，如 12.5 表示 +12.5%；正数为升（success），负数为降（destructive） */
  delta?: number
  deltaLabel?: string
  icon?: React.ReactNode
  /** 迷你趋势线数据（从旧到新） */
  spark?: number[]
  /** 0-100，显示一条进度条（如存储用量） */
  progress?: number
  /** 图标/顶线/进度条/趋势线用的色调，默认 primary */
  tone?: StatTone
  className?: string
}

export function StatCard({ title, value, hint, delta, deltaLabel, icon, spark, progress, tone = 'primary', className }: StatCardProps) {
  const { t, i18n } = useTranslation()
  const gid = useId().replace(/:/g, '')
  const shown = typeof value === 'number' ? new Intl.NumberFormat(i18n.language).format(value) : value
  const up = (delta ?? 0) > 0
  const down = (delta ?? 0) < 0
  const DeltaIcon = up ? ArrowUpRight : down ? ArrowDownRight : Minus

  return (
    <Card
      data-tone={tone}
      className={cn(
        'tone-rule group relative overflow-hidden pt-px transition-all duration-200 hover:-translate-y-0.5 hover:shadow-lg motion-reduce:transition-none motion-reduce:hover:translate-y-0',
        className,
      )}
    >
      <CardContent className="space-y-2 p-3 md:space-y-3 md:p-5">
        <div className="flex items-start justify-between gap-2 md:gap-3">
          <div className="min-w-0">
            <p className="truncate text-xs text-muted-foreground md:text-sm">{title}</p>
            <p className="mt-1 flex items-baseline gap-1.5">
              <span className="num truncate text-xl font-semibold tracking-tight md:text-3xl">{shown}</span>
              {hint ? <span className="num shrink-0 text-xs text-muted-foreground md:text-sm">{hint}</span> : null}
            </p>
          </div>
          {icon ? (
            <div className="tone-chip flex size-9 shrink-0 items-center justify-center rounded-xl shadow-sm transition-colors duration-200 group-hover:shadow-md md:size-11 [&_svg]:size-4 md:[&_svg]:size-5">
              {icon}
            </div>
          ) : null}
        </div>

        {progress !== undefined ? (
          <div
            className="h-1.5 overflow-hidden rounded-full bg-muted"
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(Math.min(100, Math.max(0, progress)))}
          >
            <div className="tone-bar h-full rounded-full" style={{ width: `${Math.min(100, Math.max(0, progress))}%` }} />
          </div>
        ) : null}

        {spark && spark.length > 1 ? (
          <div className="-mx-1" aria-hidden="true">
            <ChartBox height={40}>
              {({ width, height }) => (
                <AreaChart width={width} height={height} data={spark.map((v) => ({ v }))} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
                  <defs>
                    <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="hsl(var(--tone))" stopOpacity={0.35} />
                      <stop offset="100%" stopColor="hsl(var(--chart-1))" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <Area type="monotone" dataKey="v" stroke="hsl(var(--tone))" strokeWidth={2} fill={`url(#${gid})`} dot={false} isAnimationActive={false} />
                </AreaChart>
              )}
            </ChartBox>
          </div>
        ) : null}

        {delta !== undefined ? (
          <div className="flex items-center gap-2 text-xs">
            <span
              className={cn(
                'inline-flex items-center gap-0.5 rounded-full px-2 py-0.5 font-medium',
                up && 'bg-success/10 text-success',
                down && 'bg-destructive/10 text-destructive',
                !up && !down && 'bg-muted text-muted-foreground',
              )}
            >
              <DeltaIcon className="size-3.5" aria-hidden="true" />
              <span className="num">{`${up ? '+' : ''}${delta.toFixed(1)}%`}</span>
            </span>
            <span className="sr-only">{up ? t('charts.up') : down ? t('charts.down') : t('charts.flat')}</span>
            <span className="text-muted-foreground">{deltaLabel ?? t('charts.vsLast')}</span>
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
