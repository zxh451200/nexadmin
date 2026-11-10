import { useTranslation } from 'react-i18next'
import { Dialog, DialogContent, DialogTitle } from '@/framework/ui/dialog'
import { InlineViewer } from './InlineViewer'

export interface ImagePreviewDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  src: string
  alt?: string
}

/**
 * Logo/图标类小图的「点开预览」：居中小弹窗，内嵌 viewerjs（缩放/旋转/1:1/重置），不铺满视口。
 * 早期是居中小弹窗，后来换成过全屏灯箱；实际用下来一张小图标盖住整个屏幕太重了，
 * 2026-11-11 改回小弹窗并接入 viewerjs inline——看大图/多图细节仍走 Lightbox（文件管理等）。
 */
export function ImagePreviewDialog({ open, onOpenChange, src, alt = '' }: ImagePreviewDialogProps) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-fit max-w-[90vw] p-3">
        <DialogTitle className="sr-only">{alt || t('common.preview')}</DialogTitle>
        <InlineViewer
          src={src}
          alt={alt || t('common.preview')}
          className="h-[min(50dvh,420px)] w-[min(80vw,640px)] overflow-hidden rounded-md bg-muted/40"
        />
      </DialogContent>
    </Dialog>
  )
}
