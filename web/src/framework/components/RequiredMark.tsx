import { cn } from '@/lib/utils'

/**
 * 必填星号：全站统一的红色星标。手写 label 的表单用它标记必填项；
 * react-hook-form 表单（ui/form.tsx 的 FormLabel）会按 schema 自动渲染同款星号，
 * 两处的类名保持一致（ms-0.5 text-destructive），改样式要两边同步。
 */
export function RequiredMark({ className }: { className?: string }) {
  return (
    <span aria-hidden="true" className={cn('ms-0.5 text-destructive', className)}>
      *
    </span>
  )
}
