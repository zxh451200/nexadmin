import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const badgeBase =
  'inline-flex items-center rounded-md border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:ring-offset-background'

/** 状态色调：与 StatCard 同一套令牌，颜色一律由 tokens.css 的 [data-tone] + .tone-chip 给出 */
export type BadgeTone =
  | 'chart-1' | 'chart-2' | 'chart-3' | 'chart-4' | 'chart-5'
  | 'success' | 'warning' | 'info' | 'destructive'

const badgeVariants = cva(badgeBase, {
  variants: {
    variant: {
      default: 'border-transparent bg-primary text-primary-foreground',
      secondary: 'border-transparent bg-secondary text-secondary-foreground',
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
