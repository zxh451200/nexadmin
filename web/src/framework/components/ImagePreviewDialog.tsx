import { useTranslation } from 'react-i18next'
import { ResponsiveDialog } from './ResponsiveDialog'

export interface ImagePreviewDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  src: string
  alt?: string
}

/**
 * Logo/图标类小图的「点开预览」：居中弹窗放大到舒服的尺寸。
 * 不用全屏灯箱（Lightbox）——为一个几十字的小图标盖住整个视口，视觉上像误触；
 * 照片、附件这类真要看细节的大图仍走 Lightbox（缩放/平移/翻页）。
 */
export function ImagePreviewDialog({ open, onOpenChange, src, alt = '' }: ImagePreviewDialogProps) {
  const { t } = useTranslation()
  return (
    <ResponsiveDialog open={open} onOpenChange={onOpenChange} size="sm" title={t('common.preview')}>
      <div className="flex items-center justify-center p-4">
        <img
          src={src}
          alt={alt}
          className="max-h-[50vh] max-w-full rounded-md border border-border bg-muted/40 object-contain"
        />
      </div>
    </ResponsiveDialog>
  )
}
