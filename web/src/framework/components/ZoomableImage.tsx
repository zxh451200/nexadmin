import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { ImagePreviewDialog } from './ImagePreviewDialog'

export interface ZoomableImageProps {
  src: string
  alt?: string
  /** 缩略图尺寸与形状，如 `h-9 w-9` */
  className?: string
  /** 无障碍名称，默认「预览」 */
  title?: string
}

/**
 * 展示型缩略图 + 点开预览：给不是表单字段、只是给人看一眼的图片用（Logo/Favicon/二维码等小图）。
 * 点开是居中弹窗，不做全屏灯箱——为一个小图标盖住整个视口，视觉上像误触。
 * 图片地址是人工填的，取不到时整块不占位，避免留一个点不动的空按钮。
 */
export function ZoomableImage({ src, alt = '', className, title }: ZoomableImageProps) {
  const { t } = useTranslation()
  const [zoom, setZoom] = useState(false)
  // 只记住"这个地址挂了"：换成新地址要重新尝试加载
  const [brokenSrc, setBrokenSrc] = useState<string | null>(null)
  if (brokenSrc === src) return null
  const label = title ?? t('common.preview')
  return (
    <>
      <button
        type="button"
        onClick={() => setZoom(true)}
        aria-label={label}
        title={label}
        className="block shrink-0 cursor-zoom-in rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <img
          src={src}
          alt={alt}
          loading="lazy"
          onError={() => setBrokenSrc(src)}
          className={cn('rounded-md border border-border bg-muted/40 object-contain', className)}
        />
      </button>
      <ImagePreviewDialog open={zoom} onOpenChange={setZoom} src={src} alt={alt || label} />
    </>
  )
}
