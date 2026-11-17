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
  /**
   * 桌面呈现位置：center（默认）= 居中弹窗；right = 右侧抽屉。
   * 全站约定：实体的新建/编辑长表单（多字段、多分区、要滚动）用 right；
   * 短表单、详情查看、选择器、确认框、登录流程用默认 center。
   * 手机端不受影响：两种形态都是底部抽屉。
   */
  side?: 'center' | 'right'
  /** 高度固定为 85dvh（手机 92dvh），正文占满余下空间；多页签表单切换时弹窗不再变高变矮 */
  fixedHeight?: boolean
  className?: string
  /** 透传到桌面 DialogContent 外壳的类名（覆盖居中/定位）；正文类名请用 className */
  contentClassName?: string
}

const sizeClass = {
  sm: 'md:max-w-sm',
  md: 'md:max-w-lg',
  lg: 'md:max-w-2xl',
  xl: 'md:max-w-4xl',
} as const

// 右侧抽屉的桌面宽度：与居中弹窗的 size 档位一一对应。
const drawerWidthClass = {
  sm: 'w-[min(92vw,26rem)]',
  md: 'w-[min(92vw,34rem)]',
  lg: 'w-[min(92vw,44rem)]',
  xl: 'w-[min(92vw,52rem)]',
} as const

/** 桌面(>=md)为居中 Dialog（side="right" 时为右侧抽屉），手机为底部 Drawer；header 与 footer 固定，中间内容滚动 */
export function ResponsiveDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  size = 'md',
  side = 'center',
  fixedHeight,
  className,
  contentClassName,
}: ResponsiveDialogProps) {
  const isMobile = useIsMobile()
  // 右侧抽屉只发生在桌面；手机上两种形态统一走底部抽屉（方向不能带 right）。
  const useRight = !isMobile && side === 'right'

  if (isMobile || useRight) {
    return (
      <Drawer open={open} onOpenChange={onOpenChange} direction={useRight ? 'right' : undefined}>
        <DrawerContent
          side={useRight ? 'right' : undefined}
          className={cn(
            useRight ? drawerWidthClass[size] : cn('flex flex-col', fixedHeight ? 'h-[92dvh]' : 'max-h-[92dvh]'),
          )}
        >
          <DrawerHeader className={cn('text-start', useRight && 'shrink-0 border-b border-border dark:border-white/10')}>
            <DrawerTitle>{title}</DrawerTitle>
            {description ? (
              <DrawerDescription>{description}</DrawerDescription>
            ) : (
              <DrawerDescription className="sr-only">{title}</DrawerDescription>
            )}
          </DrawerHeader>
          <div
            className={cn(
              'min-h-0 flex-1 px-4 pb-4',
              useRight ? 'pt-4 overflow-y-auto' : fixedHeight ? 'overflow-hidden' : 'overflow-y-auto',
              className,
            )}
          >
            {children}
          </div>
          {footer ? (
            <div
              className={cn(
                'flex shrink-0 flex-col-reverse gap-2 border-t border-border px-4 pt-3 dark:border-white/10',
                useRight
                  ? 'items-end pb-4'
                  : 'pb-[max(1rem,env(safe-area-inset-bottom))] [&>button]:w-full',
              )}
            >
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
      <DialogContent
        className={cn(
          'flex flex-col gap-0 p-0 overflow-hidden!',
          fixedHeight ? 'h-[85dvh]' : 'max-h-[85dvh]',
          sizeClass[size],
          contentClassName,
        )}
      >
        {/* × 是 DialogContent 绝对定位的（inset-e-4 top-4，占 16-48px）：头部 pt-4 + 标题行 min-h-8
            让标题与 × 同高同起点（图标居中对齐标题），不再悬在标题与描述之间的半空；
            pe-12 给 × 留位，长标题/描述不再从按钮下面穿过 */}
        <DialogHeader className="shrink-0 border-b border-border px-4 pt-4 pb-3 pe-12 text-start dark:border-white/10">
          <DialogTitle className="flex min-h-8 items-center">{title}</DialogTitle>
          {description ? (
            <DialogDescription>{description}</DialogDescription>
          ) : (
            <DialogDescription className="sr-only">{title}</DialogDescription>
          )}
        </DialogHeader>
        <div className={cn('min-h-0 flex-1 px-4 pt-4 pb-4', fixedHeight ? 'overflow-hidden' : 'overflow-y-auto', className)}>
          {children}
        </div>
        {footer ? (
          <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border px-4 py-2.5 dark:border-white/10">{footer}</div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
