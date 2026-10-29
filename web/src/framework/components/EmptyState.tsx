import type { ReactNode } from 'react'
import { Inbox } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

export interface EmptyStateProps {
  icon?: ReactNode
  title?: ReactNode
  description?: ReactNode
  action?: ReactNode
  className?: string
}

export function EmptyState({ icon, title, description, action, className }: EmptyStateProps) {
  const { t } = useTranslation()
  return (
    <div
      className={cn('flex flex-col items-center justify-center gap-2 px-4 py-12 text-center', className)}
      role="status"
    >
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted text-muted-foreground">
        {icon ?? <Inbox className="h-6 w-6" aria-hidden="true" />}
      </div>
      <div className="text-sm font-medium text-foreground">{title ?? t('common.empty')}</div>
      {description ? <div className="max-w-sm text-sm text-muted-foreground">{description}</div> : null}
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  )
}
