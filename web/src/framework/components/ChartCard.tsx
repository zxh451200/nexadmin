import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Segmented } from '@/framework/components/Segmented'
import { cn } from '@/lib/utils'

export interface ChartCardRange {
  value: string
  label: string
}

export interface ChartCardSummary {
  /** 当前合计/总量，如「18,426」；数字或字符串均可 */
  value: React.ReactNode
  /** 总量的含义，如「客户总数」 */
  label?: React.ReactNode
  /** 右侧补充（如时间窗说明「近 30 天」） */
  hint?: React.ReactNode
}

export interface ChartCardProps {
  title: React.ReactNode
  children: React.ReactNode
  /** 标题右侧操作区 */
  actions?: React.ReactNode
  /** 图表上方的总量标注：大数字 + 含义（借鉴 analytics 模板「18,426 Customers」的画法） */
  summary?: ChartCardSummary
  /** 时间范围切换：给出即渲染在标题右侧（Segmented），数据由调用方按 range 自取 */
  ranges?: ChartCardRange[]
  range?: string
  onRangeChange?: (value: string) => void
  className?: string
}

export function ChartCard({ title, children, actions, summary, ranges, range, onRangeChange, className }: ChartCardProps) {
  const { t } = useTranslation()
  return (
    <Card className={cn('transition-shadow hover:glow motion-reduce:transition-none', className)}>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0 pb-2">
        <CardTitle className="text-base font-medium">{title}</CardTitle>
        <div className="flex items-center gap-2">
          {actions}
          {ranges?.length && onRangeChange ? (
            <Segmented
              ariaLabel={t('charts.range')}
              value={range ?? ranges[0]?.value}
              onChange={onRangeChange}
              options={ranges}
            />
          ) : null}
        </div>
      </CardHeader>
      {summary ? (
        // 大数字标注：value 是主角、label 收在旁边，hint（时间窗）靠右
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 px-6 pb-1">
          <p className="flex flex-wrap items-baseline gap-x-2">
            <span className="num text-2xl font-semibold leading-none tracking-tight">{summary.value}</span>
            {summary.label ? <span className="text-sm text-muted-foreground">{summary.label}</span> : null}
          </p>
          {summary.hint ? <span className="text-xs text-muted-foreground">{summary.hint}</span> : null}
        </div>
      ) : null}
      <CardContent className={summary ? 'pt-2' : undefined}>{children}</CardContent>
    </Card>
  )
}
