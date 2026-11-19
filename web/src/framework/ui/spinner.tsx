import { Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

/**
 * 加载指示器（对齐 shadcn/ui 2025-10 的 Spinner）：按钮 pending、行内加载等
 * 小面积场景用这个，骨架屏仍用 Skeleton——大块内容占位与小动作转圈是两种语言。
 * size 走 Tailwind 尺寸类；默认 size-4 与按钮图标同宽，塞进按钮不撑行高。
 */
export function Spinner({ className, ...props }: React.ComponentProps<typeof Loader2>) {
  return <Loader2 aria-hidden="true" className={cn('size-4 animate-spin', className)} {...props} />
}
