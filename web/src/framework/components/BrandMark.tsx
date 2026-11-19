import { cn } from '@/lib/utils'

interface BrandMarkProps {
  /** 站点设置上传的 logo 地址；未配置时渲染默认标 */
  logoUrl?: string
  /** 仅作 img 的 alt 文案 */
  name?: string
  /** 外层尺寸/圆角等（含底座） */
  className?: string
  /** 内部核心标的尺寸 */
  iconClassName?: string
}

/** 默认标的图形本体（viewBox 512）：六边形容器 + 闪电能量核 + 轨道弧与卫星点。
    画法与 web/public/icon.svg 静态镜像一致——改这里时同步镜像（见组件注释）。 */
export function CoreMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 512 512" fill="none" aria-hidden="true" className={className}>
      {/* 轨道弧 + 卫星点：在轨运行的动感，只扫一段避免撞进六边形 */}
      <path
        d="M433.5 153.5A205 205 0 0 1 401 401"
        stroke="currentColor"
        strokeOpacity={0.45}
        strokeWidth={18}
        strokeLinecap="round"
      />
      <circle cx="401" cy="401" r="20" fill="currentColor" />
      {/* 六边形容器：能量单元外壳 */}
      <path
        d="M256 108 384.2 182 384.2 330 256 404 127.8 330 127.8 182Z"
        stroke="currentColor"
        strokeWidth={26}
        strokeLinejoin="round"
      />
      {/* 闪电能量核（与官网主题 favicon 同一枚 bolt 的形状语言） */}
      <path d="M16 6l4 10h-4l3 10-9-12h5l-3-8z" fill="currentColor" transform="translate(121 112) scale(9)" />
    </svg>
  )
}

/** 品牌标识唯一来源：优先站点设置（系统管理 → 参数设置）上传的 logo；
    未配置时渲染默认标——品牌渐变底座 + 能量核心标（六边形 + 闪电 + 轨道）。
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
      <CoreMark className={cn('size-[68%]', iconClassName)} />
    </span>
  )
}
