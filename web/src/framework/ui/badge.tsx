import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const badgeBase =
  // 11px/medium + 收一档内边距：徽章是辅助信息，不该和正文争字重；tone-chip 的同色淡底
  // 在 11px 下更经得起一行十几枚的排布（12px/semibold 会整列发黑）。
  'inline-flex items-center rounded-md border px-2 py-0.5 text-[11px] font-medium leading-4 transition-colors focus:outline-hidden focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:ring-offset-background'

/** 状态色调：与 StatCard 同一套令牌，颜色一律由 tokens.css 的 [data-tone] + .tone-chip 给出 */
export type BadgeTone =
  | 'chart-1' | 'chart-2' | 'chart-3' | 'chart-4' | 'chart-5'
  | 'success' | 'warning' | 'info' | 'destructive'

const badgeVariants = cva(badgeBase, {
  variants: {
    variant: {
      default: 'border-transparent bg-primary text-primary-foreground',
      // 中性徽章也要有描边：tone-chip 都有 1px 同色边，唯独它是透明边，
      // 同一列里「菜单(青) / 目录(中性)」会被读成「一个高亮一个禁用」，而不是两种平级取值。
      secondary: 'border-border bg-secondary text-secondary-foreground',
      destructive: 'tone-chip',
      success: 'tone-chip',
      warning: 'tone-chip',
      info: 'tone-chip',
      outline: 'text-foreground',
    },
  },
  defaultVariants: { variant: 'default' },
})

export interface BadgeProps extends React.HTMLAttributes<HTMLDivElement>, VariantProps<typeof badgeVariants> {
  /** 强制按色调渲染成状态 chip（如用 chart-2 区分中性/进行中状态）；优先于 variant 着色 */
  tone?: BadgeTone
}

function Badge({ className, variant, tone, ...props }: BadgeProps) {
  // 状态变体 / 显式 tone：走 data-tone + tone-chip（同色淡底 + 同色描边 + 同色文字），
  // 底色 alpha 0.14（暗色 0.2），文字用满强度色调色保证与卡底的对比度。
  const chipTone: BadgeTone | undefined =
    tone ??
    (variant === 'success' || variant === 'warning' || variant === 'info' || variant === 'destructive' ? variant : undefined)
  if (chipTone) {
    return <div data-tone={chipTone} className={cn(badgeBase, 'tone-chip', className)} {...props} />
  }
  return <div className={cn(badgeVariants({ variant }), className)} {...props} />
}

export { Badge, badgeVariants }
