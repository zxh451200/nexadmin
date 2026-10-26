import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './tooltip'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-[color,background-color,box-shadow,border-color] duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0',
  {
    variants: {
      variant: {
        default: 'bg-brand-gradient text-primary-foreground shadow-sm hover:brightness-110 hover:shadow-glow',
        destructive: 'bg-destructive text-destructive-foreground hover:bg-destructive/90',
        outline: 'border border-input bg-background hover:bg-accent hover:text-accent-foreground',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
        ghost: 'hover:bg-accent hover:text-accent-foreground',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-10 px-4 py-2 md:h-9',
        sm: 'h-9 rounded-md px-3 md:h-8 md:text-xs',
        lg: 'h-12 rounded-md px-8 md:h-10',
        icon: 'h-10 w-10 md:h-9 md:w-9',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button'
    // 纯图标按钮：用 aria-label（或 title）自动显示悬停/聚焦提示，不用每个页面单独包 Tooltip。
    // 手动控制开关：只认鼠标悬停与键盘聚焦（:focus-visible），按下即关。交给 Radix 自动管理时，
    // 下拉菜单关闭会把焦点还给触发按钮并重新唤起提示，而指针已不在按钮上、永远等不到移出事件，
    // 提示就卡住不消失了。
    const hint = size === 'icon' ? (props['aria-label'] ?? props.title) : undefined
    const [open, setOpen] = React.useState(false)
    // 浏览器侧 setTimeout 返回 number；@types/node 在场时 ReturnType<typeof setTimeout> 会变成
    // NodeJS.Timeout，与 window.setTimeout 的返回值冲突，这里显式用 number。
    const timer = React.useRef<number>()
    const show = () => {
      window.clearTimeout(timer.current)
      timer.current = window.setTimeout(() => setOpen(true), 300)
    }
    const hide = () => {
      window.clearTimeout(timer.current)
      setOpen(false)
    }
    React.useEffect(() => () => window.clearTimeout(timer.current), [])
    if (hint) {
      const rest = { ...props } // 去掉原生 title，避免和自定义提示重复弹出
      delete rest.title
      return (
        <TooltipProvider delayDuration={300}>
          <Tooltip open={open}>
            <TooltipTrigger
              asChild
              onPointerEnter={show}
              onPointerLeave={hide}
              onPointerDown={hide}
              onFocus={(e) => {
                if (e.currentTarget.matches(':focus-visible')) show()
              }}
              onBlur={hide}
              onKeyDown={(e) => {
                if (e.key === 'Escape') hide()
              }}
            >
              <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...rest} />
            </TooltipTrigger>
            <TooltipContent>{hint}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      )
    }
    return <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...props} />
  },
)
Button.displayName = 'Button'

export { Button, buttonVariants }
