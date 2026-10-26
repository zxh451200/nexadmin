import { Hexagon } from 'lucide-react'
import { cn } from '@/lib/utils'

interface BrandMarkProps {
  /** 站点设置上传的 logo 地址；未配置时渲染默认标 */
  logoUrl?: string
  /** 仅作 img 的 alt 文案 */
  name?: string
  /** 外层尺寸/圆角等（含底座） */
  className?: string
  /** 内部六边形的尺寸 */
  iconClassName?: string
}

/** 品牌标识唯一来源：优先站点设置（系统管理 → 参数设置）上传的 logo；
    未配置时渲染默认标——品牌渐变底座 + 白描线六边形。
    侧边栏 / 登录页 / 初始化向导等所有界面一律从这里取，保证全应用只有一个 logo 设计；
    favicon 与 PWA 图标（web/public/icon*.svg/png）是它的静态镜像，改默认标时需同步。 */
export function BrandMark({ logoUrl, name, className, iconClassName }: BrandMarkProps) {
  if (logoUrl) return <img src={logoUrl} alt={name || 'logo'} className={cn('shrink-0 object-contain', className)} />
  return (
    <span
      className={cn(
        'flex shrink-0 items-center justify-center rounded-lg bg-brand-gradient text-primary-foreground',
        className,
      )}
    >
      <Hexagon className={cn('size-[62%]', iconClassName)} aria-hidden="true" />
    </span>
  )
}
