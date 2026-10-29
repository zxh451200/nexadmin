import { Toaster as Sonner } from 'sonner'
import { useTheme } from '@/framework/theme/ThemeProvider'

type ToasterProps = React.ComponentProps<typeof Sonner>

const Toaster = ({ ...props }: ToasterProps) => {
  const { resolved } = useTheme()
  return (
    <Sonner
      theme={resolved}
      className="toaster group"
      toastOptions={{
        classNames: {
          toast:
            'group toast group-[.toaster]:bg-popover group-[.toaster]:text-popover-foreground group-[.toaster]:border-border group-[.toaster]:shadow-lg group-[.toaster]:pe-9',
          // sonner 默认把关闭按钮挂在卡片左上角外侧（translate(-35%,-35%)），
          // 会压住左侧的警示图标和圆角；收进卡片内右上角，左右方向走逻辑属性以兼容 RTL。
          closeButton:
            'end-1.5 top-1.5 start-auto size-[18px] rounded-full border-transparent bg-transparent text-muted-foreground [transform:none] hover:bg-muted hover:text-foreground',
          description: 'group-[.toast]:text-muted-foreground',
          actionButton: 'group-[.toast]:bg-primary group-[.toast]:text-primary-foreground',
          cancelButton: 'group-[.toast]:bg-muted group-[.toast]:text-muted-foreground',
        },
      }}
      {...props}
    />
  )
}

export { Toaster }
