import { cn } from '@/lib/utils'

/**
 * 键盘按键徽标（对齐 shadcn/ui 2025-10 的 Kbd）：快捷键提示统一用这个，
 * 不再在 placeholder 文案里写「Shift+回车换行」这类口头描述。
 * 高度与 text-xs 输入配套；深浅主题都走 muted 语义令牌。
 */
export function Kbd({ className, ...props }: React.ComponentProps<'kbd'>) {
  return (
    <kbd
      className={cn(
        'inline-flex h-5 min-w-5 items-center justify-center gap-0.5 rounded border border-border bg-muted px-1 font-sans text-[10px] font-medium text-muted-foreground',
        className,
      )}
      {...props}
    />
  )
}
