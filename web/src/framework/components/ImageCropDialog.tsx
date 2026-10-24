import { useCallback, useEffect, useRef, useState } from 'react'
import Cropper from 'cropperjs'
import 'cropperjs/dist/cropper.css'
import { RotateCw, ZoomIn, ZoomOut } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/framework/ui/dialog'

export type CropConfig = { aspect?: number; width?: number; height?: number } | 'free'

export interface ImageCropDialogProps {
  open: boolean
  /** 待裁剪的图片文件 */
  file: File | null
  crop: CropConfig
  onCancel: () => void
  /** 裁剪完成，返回浏览器 canvas 输出的新文件 */
  onConfirm: (file: File) => void
}

export function ImageCropDialog({ open, file, crop, onCancel, onConfirm }: ImageCropDialogProps) {
  const { t } = useTranslation()
  const cropperRef = useRef<Cropper | null>(null)
  const [src, setSrc] = useState<string>()
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!file || !open) return
    const url = URL.createObjectURL(file)
    setSrc(url)
    return () => URL.revokeObjectURL(url)
  }, [file, open])

  const aspect = crop === 'free' ? NaN : crop.aspect ?? (crop.width && crop.height ? crop.width / crop.height : NaN)

  // 图片挂载后初始化 cropper（Dialog 内容为 portal，需等 ref 就绪）
  const setImg = useCallback(
    (el: HTMLImageElement | null) => {
      cropperRef.current?.destroy()
      cropperRef.current = null
      if (!el) return
      const init = () => {
        cropperRef.current?.destroy()
        cropperRef.current = new Cropper(el, { aspectRatio: aspect, viewMode: 1, autoCropArea: 1, dragMode: 'move', background: false, responsive: true })
      }
      el.onload = init
      if (el.complete && el.naturalWidth) init()
    },
    [aspect],
  )

  useEffect(
    () => () => {
      cropperRef.current?.destroy()
    },
    [],
  )

  const apply = () => {
    const c = cropperRef.current
    if (!c || !file) return
    setBusy(true)
    const fixed = crop !== 'free' && crop.width && crop.height ? { width: crop.width, height: crop.height } : {}
    const canvas = c.getCroppedCanvas({ ...fixed, imageSmoothingQuality: 'high' })
    const mime = file.type === 'image/png' || file.type === 'image/webp' ? file.type : 'image/jpeg'
    canvas.toBlob(
      (blob) => {
        setBusy(false)
        if (!blob) return
        const ext = mime === 'image/png' ? 'png' : mime === 'image/webp' ? 'webp' : 'jpg'
        const base = file.name.replace(/\.[^.]+$/, '')
        onConfirm(new File([blob], `${base}.${ext}`, { type: mime }))
      },
      mime,
      0.92,
    )
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent className="max-h-[92dvh] w-[calc(100vw-1.5rem)] max-w-2xl overflow-y-auto">
        <DialogHeader className="text-start">
          <DialogTitle>{t('uploader.cropTitle')}</DialogTitle>
          <DialogDescription>
            {crop !== 'free' && crop.width && crop.height
              ? t('uploader.cropFixed', { width: crop.width, height: crop.height })
              : t('uploader.cropHint')}
          </DialogDescription>
        </DialogHeader>
        <div className="h-[50dvh] w-full overflow-hidden rounded-md bg-muted">
          {src && <img ref={setImg} src={src} alt={file?.name ?? ''} className="block max-w-full" />}
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" size="icon" aria-label={t('uploader.zoomIn')} onClick={() => cropperRef.current?.zoom(0.1)}>
            <ZoomIn className="size-4" aria-hidden="true" />
          </Button>
          <Button type="button" variant="outline" size="icon" aria-label={t('uploader.zoomOut')} onClick={() => cropperRef.current?.zoom(-0.1)}>
            <ZoomOut className="size-4" aria-hidden="true" />
          </Button>
          <Button type="button" variant="outline" size="icon" aria-label={t('uploader.rotate')} onClick={() => cropperRef.current?.rotate(90)}>
            <RotateCw className="size-4" aria-hidden="true" />
          </Button>
        </div>
        <DialogFooter className="gap-2">
          <Button type="button" variant="outline" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
          <Button type="button" disabled={busy} onClick={apply}>
            {t('common.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
