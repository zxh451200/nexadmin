import * as React from 'react'
import { Popover, PopoverContent, PopoverTrigger } from '@/framework/ui/popover'
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle, DrawerDescription } from '@/framework/ui/drawer'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { cn } from '@/lib/utils'

export interface PickerShellProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 触发器，必须能接收 ref 与点击（会以 asChild 方式包裹） */
  trigger: React.ReactNode
  /** 手机抽屉标题（无障碍必需） */
  title: string
  children: React.ReactNode
  /** 桌面 Popover 内容区 className */
  contentClassName?: string
  /** 桌面 Popover 对齐 */
  align?: 'start' | 'center' | 'end'
  disabled?: boolean
}

/** 桌面 Popover / 手机底部 Drawer 的统一弹出层 */
export function PickerShell({
  open,
  onOpenChange,
  trigger,
  title,
  children,
  contentClassName,
  align = 'start',
  disabled,
}: PickerShellProps) {
  const isMobile = useIsMobile()

  if (isMobile) {
    return (
      <>
        <span
          className="contents"
          onClick={() => {
            if (!disabled) onOpenChange(true)
          }}
        >
          {trigger}
        </span>
        <Drawer open={open} onOpenChange={onOpenChange}>
          <DrawerContent className="max-h-[85dvh]">
            <DrawerHeader className="text-start">
              <DrawerTitle>{title}</DrawerTitle>
              <DrawerDescription className="sr-only">{title}</DrawerDescription>
            </DrawerHeader>
            <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
              {children}
            </div>
          </DrawerContent>
        </Drawer>
      </>
    )
  }

  return (
    <Popover open={open} onOpenChange={(o) => !disabled && onOpenChange(o)}>
      <PopoverTrigger asChild disabled={disabled}>
        {trigger}
      </PopoverTrigger>
      <PopoverContent align={align} className={cn('w-[var(--radix-popover-trigger-width)] min-w-[8rem] p-0', contentClassName)}>
        {children}
      </PopoverContent>
    </Popover>
  )
}
