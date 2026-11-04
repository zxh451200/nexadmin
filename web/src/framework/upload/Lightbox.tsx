import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import BaseLightbox from 'yet-another-react-lightbox'
import Counter from 'yet-another-react-lightbox/plugins/counter'
import Rotate from 'yet-another-react-lightbox/plugins/rotate'
import Zoom from 'yet-another-react-lightbox/plugins/zoom'
import 'yet-another-react-lightbox/styles.css'
import 'yet-another-react-lightbox/plugins/counter.css'

export interface LightboxSlide {
  src: string
  alt: string
}

export interface LightboxProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 一组图；只有一张时不显示翻页与计数 */
  slides: LightboxSlide[]
  /** 打开时定位到第几张 */
  index?: number
  /** 可选的信息条（如宽高/大小/格式）：内容要随翻页变化，配合 onSlideView 使用 */
  caption?: ReactNode
  /** 当前显示第几张（含打开时的第一张）：库自己管翻页，调用方只能从这里取回位置 */
  onSlideView?: (index: number) => void
}

/** 图片大图预览：缩放（滚轮/双击/捏合）、拖拽平移、90° 旋转、Esc 关闭；多图时左右翻页。 */
export function Lightbox({ open, onOpenChange, slides, index = 0, caption, onSlideView }: LightboxProps) {
  const { t } = useTranslation()
  if (!open || slides.length === 0) return null
  const grouped = slides.length > 1
  return (
    <BaseLightbox
      open
      index={index}
      close={() => onOpenChange(false)}
      slides={slides}
      plugins={grouped ? [Zoom, Rotate, Counter] : [Zoom, Rotate]}
      zoom={{ scrollToZoom: true, maxZoomPixelRatio: 4 }}
      rotate={{ buttons: 'both' }}
      // 库的翻页状态是自己的，view 回调（含打开时那一次）把位置交还给调用方，
      // 否则底部信息条会一直停在打开时那张图上。
      on={onSlideView ? { view: ({ index: seen }) => onSlideView(seen) } : undefined}
      // 库会读 styles.root 拼 CSS 变量：整块传 undefined 会让渲染崩，所以这里恒给对象
      styles={{
        navigationPrev: { display: grouped ? undefined : 'none' },
        navigationNext: { display: grouped ? undefined : 'none' },
        container: caption ? { backgroundColor: 'hsl(var(--background) / 0.92)' } : undefined,
      }}
      labels={{
        Lightbox: slides[index]?.alt ?? t('common.preview'),
        Close: t('common.close'),
        Previous: t('lightbox.prev'),
        Next: t('lightbox.next'),
        'Zoom in': t('lightbox.zoomIn'),
        'Zoom out': t('lightbox.zoomOut'),
        'Rotate clockwise': t('lightbox.rotateCw'),
        'Rotate counterclockwise': t('lightbox.rotateCcw'),
        // 单花括号是 lightbox 自己填充的占位符，不是 i18next 的 {{...}}，别改
        '{index} of {total}': t('lightbox.counter'),
      }}
      render={
        caption
          ? {
              slideFooter: () => (
                <div className="flex justify-center px-4 pb-2">
                  <div className="w-full max-w-xl rounded-lg border border-border bg-card/95 p-3 shadow-lg backdrop-blur">
                    {caption}
                  </div>
                </div>
              ),
            }
          : undefined
      }
    />
  )
}
