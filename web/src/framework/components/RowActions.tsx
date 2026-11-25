import { Fragment, type ReactNode } from 'react'
import { MoreHorizontal } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { Button } from '@/framework/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/framework/ui/dropdown-menu'
import { cn } from '@/lib/utils'

export interface RowActionItem {
  key: string
  label: string
  icon: ReactNode
  onSelect?: () => void
  href?: string
  external?: boolean
  disabled?: boolean
  destructive?: boolean
}

function ActionButton({ action }: { action: RowActionItem }) {
  const className = cn('size-11 md:size-8', action.destructive && 'text-destructive')
  const icon = <span className="flex size-4 items-center justify-center [&_svg]:size-4">{action.icon}</span>
  if (action.href) {
    return (
      <Button asChild variant="ghost" size="icon" className={className} aria-label={action.label} title={action.label} disabled={action.disabled}>
        <a href={action.href} target={action.external ? '_blank' : undefined} rel={action.external ? 'noopener noreferrer' : undefined} onClick={(e) => e.stopPropagation()}>
          {icon}
        </a>
      </Button>
    )
  }
  return (
    <Button type="button" variant="ghost" size="icon" className={className} aria-label={action.label} title={action.label} disabled={action.disabled} onClick={(e) => { e.stopPropagation(); action.onSelect?.() }}>
      {icon}
    </Button>
  )
}

export function RowActions({ actions, primaryKey }: { actions: RowActionItem[]; primaryKey?: string }) {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const primary = actions.find((action) => action.key === primaryKey)
  const secondary = primary ? actions.filter((action) => action !== primary) : actions

  if (!isMobile) return <>{actions.map((action) => <ActionButton key={action.key} action={action} />)}</>

  return (
    <>
      {primary ? <ActionButton action={primary} /> : null}
      {secondary.length > 0 ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button type="button" variant="ghost" size="icon" className="size-11 md:size-8" aria-label={t('common.more')} title={t('common.more')} onClick={(e) => e.stopPropagation()}>
              <MoreHorizontal className="size-4" aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
            {secondary.map((action, index) => (
              <Fragment key={action.key}>
                {action.destructive && index > 0 ? <DropdownMenuSeparator /> : null}
                {action.href ? (
                  <DropdownMenuItem asChild disabled={action.disabled} className={cn('min-h-11 md:min-h-8', action.destructive && 'text-destructive focus:text-destructive')}>
                    <a href={action.href} target={action.external ? '_blank' : undefined} rel={action.external ? 'noopener noreferrer' : undefined}>
                      <span className="flex size-4 items-center justify-center [&_svg]:size-4">{action.icon}</span>
                      {action.label}
                    </a>
                  </DropdownMenuItem>
                ) : (
                  <DropdownMenuItem disabled={action.disabled} onSelect={action.onSelect} className={cn('min-h-11 md:min-h-8', action.destructive && 'text-destructive focus:text-destructive')}>
                    <span className="flex size-4 items-center justify-center [&_svg]:size-4">{action.icon}</span>
                    {action.label}
                  </DropdownMenuItem>
                )}
              </Fragment>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
    </>
  )
}
