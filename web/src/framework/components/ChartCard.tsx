import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { cn } from '@/lib/utils'

export interface ChartCardProps {
  title: React.ReactNode
  children: React.ReactNode
  /** 标题右侧操作区 */
  actions?: React.ReactNode
  className?: string
}

export function ChartCard({ title, children, actions, className }: ChartCardProps) {
  return (
    <Card className={cn('transition-shadow hover:glow motion-reduce:transition-none', className)}>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0 pb-2">
        <CardTitle className="text-base font-medium">{title}</CardTitle>
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  )
}
