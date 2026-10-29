import type { ReactNode } from 'react'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/framework/ui/dialog'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
} from '@/framework/ui/drawer'
import { cn } from '@/lib/utils'

export interface ResponsiveDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  children: ReactNode
  /** 固定在底部的操作区 */
  footer?: ReactNode
  /** 桌面弹窗宽度 */
  size?: 'sm' | 'md' | 'lg' | 'xl'
  className?: string
}

const sizeClass = {
  sm: 'md:max-w-sm',
  md: 'md:max-w-lg',
  lg: 'md:max-w-2xl',
  xl: 'md:max-w-4xl',
} as const

/** 桌面(>=md)为居中 Dialog，手机为底部 Drawer；header 与 footer 固定，中间内容滚动 */
export function ResponsiveDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  size = 'md',
  className,
}: ResponsiveDialogProps) {
  const isMobile = useIsMobile()

  if (isMobile) {
    return (
      <Drawer open={open} onOpenChange={onOpenChange}>
        <DrawerContent className="flex max-h-[92dvh] flex-col">
          <DrawerHeader className="text-start">
            <DrawerTitle>{title}</DrawerTitle>
            {description ? (
              <DrawerDescription>{description}</DrawerDescription>
            ) : (
              <DrawerDescription className="sr-only">{title}</DrawerDescription>
            )}
          </DrawerHeader>
          <div className={cn('min-h-0 flex-1 overflow-y-auto px-4 pb-4', className)}>{children}</div>
          {footer ? (
            <div className="flex shrink-0 flex-col-reverse gap-2 border-t border-border bg-background px-4 pt-3 pb-[max(1rem,env(safe-area-inset-bottom))] [&>button]:w-full">
              {footer}
            </div>
          ) : null}
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* 正文自己带滚动区，外层不能再滚一次，否则弹窗右侧会出现两条滚动条 */}
      <DialogContent className={cn('flex max-h-[85dvh] flex-col gap-0 p-0 !overflow-hidden', sizeClass[size])}>
        <DialogHeader className="shrink-0 p-6 pb-4 text-start">
          <DialogTitle>{title}</DialogTitle>
          {description ? (
            <DialogDescription>{description}</DialogDescription>
          ) : (
            <DialogDescription className="sr-only">{title}</DialogDescription>
          )}
        </DialogHeader>
        <div className={cn('min-h-0 flex-1 overflow-y-auto px-6 pb-6', className)}>{children}</div>
        {footer ? (
          <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border px-6 py-4">{footer}</div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
