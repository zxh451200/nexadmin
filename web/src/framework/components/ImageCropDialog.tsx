import { useCallback, useEffect, useRef, useState } from 'react'
import Cropper from 'cropperjs'
import 'cropperjs/dist/cropper.css'
import { RotateCw, Undo2, ZoomIn, ZoomOut } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/framework/ui/dialog'
import { Input } from '@/framework/ui/input'
import { cn } from '@/lib/utils'

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

/** 图片元信息：原始尺寸、类型、文件大小 */
type CropMeta = { w: number; h: number; type: string; size: number }

/** 裁剪框实时状态（容器坐标 + 换算成原图像素的输出尺寸） */
type LiveBox = { x: number; y: number; w: number; h: number; contW: number; contH: number; outW: number; outH: number }

const fmtBytes = (n: number) => (n >= 1048576 ? `${(n / 1048576).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`)

/** 裁剪框（容器像素）换算成原图像素的倍率：旋转 90°/270° 后渲染宽对应原图高 */
const naturalScale = (c: Cropper) => {
  const img = c.getImageData()
  const rot = (((img.rotate ?? 0) % 360) + 360) % 360
  const natW = rot % 180 ? img.naturalHeight : img.naturalWidth
  return natW / c.getCanvasData().width
}

export function ImageCropDialog({ open, file, crop, onCancel, onConfirm }: ImageCropDialogProps) {
  const { t } = useTranslation()
  const cropperRef = useRef<Cropper | null>(null)
  const [src, setSrc] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [meta, setMeta] = useState<CropMeta | null>(null)
  const [live, setLive] = useState<LiveBox | null>(null)
  const [wText, setWText] = useState('')
  const [hText, setHText] = useState('')
  /** 正在编辑的输入框：拖动产生的 crop 事件不覆盖其文本 */
  const editing = useRef<'w' | 'h' | null>(null)
  /** 自由裁剪下可切换的快捷比例；null = 沿用配置（free 即不限比例） */
  const [aspectOverride, setAspectOverride] = useState<number | null>(null)

  useEffect(() => {
    if (!file || !open) return
    const url = URL.createObjectURL(file)
    setSrc(url)
    // 换图清掉上一张的尺寸残留，ready 同步前不闪旧数字
    setWText('')
    setHText('')
    return () => URL.revokeObjectURL(url)
  }, [file, open])

  const aspect = crop === 'free' ? NaN : crop.aspect ?? (crop.width && crop.height ? crop.width / crop.height : NaN)
  const fixedOut = crop !== 'free' && !!(crop.width && crop.height)
  /** 当前生效比例：快捷比例 chips 覆盖配置值（fixedOut 时比例锁死不响应 chips） */
  const curAspect = fixedOut ? aspect : (aspectOverride ?? aspect)

  /** 旋转 90° 时把裁剪框跟着图片一起变换。cropperjs v1 的 rotate 只转画布不动裁剪框，
   *  原地留下的框会悬在转好的图外/错位。映射：旧画布上的相对坐标 (u,v)，顺时针转 90°
   *  后落到新画布的 (1-v, u)，框宽高对调；锁比例的裁剪按映射框面积以原比例重取
   *  （旋转后宽高比对调，不重取会破坏比例锁）。 */
  const rotateCropBox = (c: Cropper, deg: 90 | -90) => {
    const C0 = c.getCanvasData()
    const B = c.getCropBoxData()
    c.rotate(deg)
    const C1 = c.getCanvasData()
    const relL = (B.left - C0.left) / C0.width
    const relT = (B.top - C0.top) / C0.height
    const relR = relL + B.width / C0.width
    const relB = relT + B.height / C0.height
    const box =
      deg === 90
        ? { l: 1 - relB, t: relL, r: 1 - relT, b: relR }
        : { l: relT, t: 1 - relR, r: relB, b: 1 - relL }
    let width = (box.r - box.l) * C1.width
    let height = (box.b - box.t) * C1.height
    let left = C1.left + box.l * C1.width
    let top = C1.top + box.t * C1.height
    if (!Number.isNaN(curAspect)) {
      const cx = left + width / 2
      const cy = top + height / 2
      width = Math.round(Math.sqrt(width * height * curAspect))
      height = Math.round(width / curAspect)
      left = cx - width / 2
      top = cy - height / 2
    }
    c.setCropBoxData({ left, top, width, height })
  }

  // 裁剪框变化（含拖动、就绪）→ 同步浮标与输入框
  const syncLive = useCallback((c: Cropper) => {
    const b = c.getCropBoxData()
    const cont = c.getContainerData()
    const f = naturalScale(c)
    const outW = Math.max(1, Math.round(b.width * f))
    const outH = Math.max(1, Math.round(b.height * f))
    setLive({ x: b.left, y: b.top, w: b.width, h: b.height, contW: cont.width, contH: cont.height, outW, outH })
    setWText((prev) => (editing.current === 'w' ? prev : String(outW)))
    setHText((prev) => (editing.current === 'h' ? prev : String(outH)))
  }, [])

  // 图片挂载后初始化 cropper（Dialog 内容为 portal，需等 ref 就绪）
  const setImg = useCallback(
    (el: HTMLImageElement | null) => {
      cropperRef.current?.destroy()
      cropperRef.current = null
      if (!el) return
      const init = () => {
        cropperRef.current?.destroy()
        const c = new Cropper(el, {
          aspectRatio: aspect,
          preview: '#crop-preview',
          viewMode: 1,
          autoCropArea: 1,
          dragMode: 'move',
          background: false,
          responsive: true,
          zoomOnWheel: true,
          // 就绪即同步宽高输入框：el 上的 ready 监听挂得太晚（构造期间就派发）会错过首帧，
          // 宽高框会一直空着直到用户拖动一次；官方 ready 回调是可靠时机
          ready: () => syncLive(c),
        })
        cropperRef.current = c
        setMeta({ w: el.naturalWidth, h: el.naturalHeight, type: file?.type ?? '', size: file?.size ?? 0 })
        const onCrop = () => syncLive(c)
        el.addEventListener('crop', onCrop)
      }
      el.onload = init
      if (el.complete && el.naturalWidth) init()
    },
    [aspect, file, syncLive],
  )

  useEffect(
    () => () => {
      cropperRef.current?.destroy()
    },
    [],
  )

  // 输入框提交（失焦 / 回车）：换算成容器坐标写回裁剪框，按比例与画布边界收拢
  const commitInput = (which: 'w' | 'h') => {
    editing.current = null
    const c = cropperRef.current
    if (!c) return
    const raw = which === 'w' ? wText : hText
    const v = Math.round(Number(raw))
    if (!Number.isFinite(v) || v <= 0) {
      syncLive(c)
      return
    }
    const f = naturalScale(c)
    const canvas = c.getCanvasData()
    const cur = c.getCropBoxData()
    const maxW = Math.floor(canvas.width * f)
    const maxH = Math.floor(canvas.height * f)
    let outW: number
    let outH: number
    if (curAspect) {
      outW = which === 'w' ? v : Math.round(v * curAspect)
      outW = Math.min(outW, maxW, Math.floor(maxH * curAspect))
      outH = Math.round(outW / curAspect)
    } else if (which === 'w') {
      outW = Math.min(v, maxW)
      outH = Math.min(Math.round(cur.height * f), maxH)
    } else {
      outH = Math.min(v, maxH)
      outW = Math.min(Math.round(cur.width * f), maxW)
    }
    c.setCropBoxData({ width: outW / f, height: outH / f })
  }

  // 浮标贴着裁剪框左上角，贴边时收进来；框太靠上则改挂框下方
  const badge = live && live.w > 0 && live.contW > 0 ? {
    left: Math.min(Math.max(4, live.x), Math.max(4, live.contW - 100)),
    top: live.y >= 28 ? live.y - 4 : Math.max(4, live.y + 4),
    above: live.y >= 28,
  } : null

  const apply = () => {
    const c = cropperRef.current
    if (!c || !file) return
    setBusy(true)
    const fixed = fixedOut && crop.width && crop.height ? { width: crop.width, height: crop.height } : {}
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

  const numberInputProps = (which: 'w' | 'h') => ({
    className: 'h-8 w-20 text-xs tabular-nums',
    type: 'number' as const,
    min: 1,
    value: which === 'w' ? wText : hText,
    // 输出尺寸固定时输入框只读：实时显示选框在原图上的尺寸（输出会缩放到固定尺寸）
    readOnly: fixedOut,
    title: fixedOut ? t('uploader.cropFixedInput') : undefined,
    onFocus: () => {
      editing.current = which
    },
    onChange: (e: React.ChangeEvent<HTMLInputElement>) => {
      const v = e.target.value
      if (which === 'w') setWText(v)
      else setHText(v)
    },
    onBlur: () => commitInput(which),
    onKeyDown: (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
    },
  })

  // 快捷比例 chips（自由裁剪专用；fixedOut 比例锁死不显示）
  const ratioChips: { v: number | null; label: string }[] = [
    { v: null, label: t('uploader.cropRatioFree') },
    { v: 1, label: '1:1' },
    { v: 4 / 3, label: '4:3' },
    { v: 3 / 4, label: '3:4' },
    { v: 16 / 9, label: '16:9' },
  ]
  const chipActive = (v: number | null) =>
    v === null ? Number.isNaN(curAspect) : Math.abs(curAspect - v) < 0.001
  const pickRatio = (v: number | null) => {
    setAspectOverride(v)
    const c = cropperRef.current
    if (c) c.setAspectRatio(v === null ? NaN : v)
  }
  // 实时预览窗的宽高比：固定输出用输出比例，自由裁剪跟随当前选框
  const previewAspect = fixedOut && crop.width && crop.height ? crop.width / crop.height : live && live.h > 0 ? live.w / live.h : 1

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent className="max-h-[92dvh] w-[calc(100vw-1.5rem)] max-w-4xl overflow-y-auto">
        <DialogHeader className="text-start">
          <DialogTitle>{t('uploader.cropTitle')}</DialogTitle>
          <DialogDescription>
            {fixedOut && crop.width && crop.height
              ? t('uploader.cropFixed', { width: crop.width, height: crop.height })
              : t('uploader.cropHint')}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="text-muted-foreground flex items-center gap-2 text-xs">
            {meta && (
              <>
                <span className="text-foreground font-medium tabular-nums">
                  {meta.w} × {meta.h}
                </span>
                <span>{meta.type.replace('image/', '').toUpperCase() || '—'}</span>
                <span>{fmtBytes(meta.size)}</span>
              </>
            )}
          </div>
          <div className="flex items-center gap-2">
            <label className="text-muted-foreground flex items-center gap-1.5 text-xs">
              {t('uploader.cropWidth')}
              <Input aria-label={t('uploader.cropWidth')} {...numberInputProps('w')} />
            </label>
            <label className="text-muted-foreground flex items-center gap-1.5 text-xs">
              {t('uploader.cropHeight')}
              <Input aria-label={t('uploader.cropHeight')} {...numberInputProps('h')} />
            </label>
          </div>
        </div>
        <div className="flex flex-col gap-3 lg:flex-row">
          {/* 双击图片 = 确认（与主按钮同效）；滚轮缩放由 cropper 的 zoomOnWheel 提供 */}
          <div
            className="relative h-[50dvh] w-full flex-1 overflow-hidden rounded-md border bg-muted"
            onDoubleClick={() => !busy && apply()}
          >
            {src && <img ref={setImg} src={src} alt={file?.name ?? ''} className="block max-w-full" />}
            {badge && live && (
              <div
                className="pointer-events-none absolute z-10 rounded-sm bg-black/70 px-1.5 py-0.5 text-[11px] leading-none text-white tabular-nums"
                style={{ left: badge.left, top: badge.top, transform: badge.above ? 'translateY(-100%)' : undefined }}
              >
                {live.outW} × {live.outH}
              </div>
            )}
          </div>
          <div className="flex w-full shrink-0 flex-col gap-3 lg:w-48">
            <div className="flex flex-col gap-1">
              <p className="text-xs text-muted-foreground">{t('uploader.previewLabel')}</p>
              {/* cropper 的 preview 会把裁剪框内的区域实时镜像到这里 */}
              <div
                id="crop-preview"
                className="w-full overflow-hidden rounded-md border bg-muted"
                style={{ aspectRatio: String(previewAspect || 1) }}
              />
              <p className="text-xs tabular-nums text-muted-foreground">
                {t('uploader.outputSize')}：
                <span className="text-foreground font-medium">
                  {fixedOut && crop.width && crop.height ? `${crop.width} × ${crop.height}` : live ? `${live.outW} × ${live.outH}` : '—'}
                </span>
              </p>
            </div>
            {!fixedOut && (
              <div className="flex flex-col gap-1">
                <p className="text-xs text-muted-foreground">{t('uploader.ratioLabel')}</p>
                <div className="flex flex-wrap gap-1.5">
                  {ratioChips.map((chip) => (
                    <button
                      key={chip.label}
                      type="button"
                      onClick={() => pickRatio(chip.v)}
                      className={cn(
                        'rounded-full border px-2.5 py-1 text-xs transition-colors focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring',
                        chipActive(chip.v)
                          ? 'border-primary bg-primary text-primary-foreground'
                          : 'border-border hover:bg-muted',
                      )}
                    >
                      {chip.label}
                    </button>
                  ))}
                </div>
              </div>
            )}
            <div className="flex items-center gap-2">
              <Button type="button" variant="outline" size="icon" aria-label={t('uploader.zoomIn')} onClick={() => cropperRef.current?.zoom(0.1)}>
                <ZoomIn className="size-4" aria-hidden="true" />
              </Button>
              <Button type="button" variant="outline" size="icon" aria-label={t('uploader.zoomOut')} onClick={() => cropperRef.current?.zoom(-0.1)}>
                <ZoomOut className="size-4" aria-hidden="true" />
              </Button>
              <Button type="button" variant="outline" size="icon" aria-label={t('uploader.rotate')} onClick={() => { const c = cropperRef.current; if (c) rotateCropBox(c, 90) }}>
                <RotateCw className="size-4" aria-hidden="true" />
              </Button>
              <Button type="button" variant="outline" size="icon" aria-label={t('uploader.cropReset')} onClick={() => { cropperRef.current?.reset(); setAspectOverride(null) }}>
                <Undo2 className="size-4" aria-hidden="true" />
              </Button>
            </div>
          </div>
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
