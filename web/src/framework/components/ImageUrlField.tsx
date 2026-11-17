import { useRef, useState } from 'react'
import { Loader2, Trash2, Upload, ZoomIn } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { cn } from '@/lib/utils'
import { precheckFile, useUploadConfig } from '@/framework/upload/precheck'
import { uploadFile } from '@/framework/upload/uploadClient'
import { toWebp, webpName } from '@/framework/upload/toWebp'
import { Lightbox } from '@/framework/upload/Lightbox'
import { FilePickerDialog, PickFromLibraryButton } from './FilePickerDialog'
import { ResponsiveDialog } from './ResponsiveDialog'
import { Uploader, type UploadedFile } from './Uploader'
import type { CropConfig } from './ImageCropDialog'

export interface ImageUrlFieldProps {
  value: string
  onChange: (url: string) => void
  /** 需要裁剪时作为裁剪弹窗标题，如「上传 Logo」 */
  title: string
  /** 字段说明：不裁剪时直接挂在输入框下方，裁剪时放进弹窗 */
  hint?: string
  placeholder?: string
  /** 传了才走「弹窗 → 选图 → 裁剪」；不传则点上传直接开系统选图框 */
  crop?: CropConfig
  disabled?: boolean
  /** 预览框尺寸类，默认正方形——logo 一律完整显示，不裁切 */
  previewClassName?: string
  /** true = 输入行在上、预览/上传卡在下（长表单里先看到输入再看到图）；默认横排（卡在左、输入在右） */
  previewBelow?: boolean
  'aria-label'?: string
  /** 行内校验失败时标红输入框（提示文案由调用方在字段下方自绘） */
  'aria-invalid'?: boolean
}

/**
 * 单图字段的统一形态：URL 输入 + 上传/预览卡 + 清除。
 * 站点 Logo、租户 Logo 点「上传」直接开系统选图框，选完即传即回填；
 * 只有配了 crop 的字段（SSO 应用 Logo）才弹窗，因为裁剪要在弹窗里画。
 */
export function ImageUrlField({
  value,
  onChange,
  title,
  hint,
  placeholder,
  crop,
  disabled,
  previewClassName = 'size-24',
  previewBelow = false,
  'aria-label': ariaLabel,
  'aria-invalid': ariaInvalid,
}: ImageUrlFieldProps) {
  const { t } = useTranslation()
  const { data: cfg } = useUploadConfig()
  const inputRef = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)
  const [open, setOpen] = useState(false)
  const [zoom, setZoom] = useState(false)
  const [pickOpen, setPickOpen] = useState(false)

  const pick = async (file: File | undefined) => {
    if (!file) return
    const err = precheckFile(file, 'image', cfg)
    if (err) {
      toast.error(
        err.code === 'size'
          ? t('uploader.errSize', { name: file.name, limit: err.limitMb })
          : err.code === 'type'
            ? t('uploader.errType', { name: file.name })
            : t('uploader.errKind', { name: file.name, kind: t('uploader.kind_image') }),
      )
      return
    }
    setUploading(true)
    try {
      const up = await uploadFile(file, { kind: 'image', visibility: 'public' })
      // 与 Uploader 同口径：原图落库后补一份 WebP 副本，失败只影响省流量，不影响可用性
      if (cfg?.imageWebp) {
        const webp = await toWebp(file).catch(() => null)
        if (webp && webp.size < up.size) {
          await uploadFile(new File([webp], webpName(file.name), { type: 'image/webp' }), {
            kind: 'image',
            visibility: 'public',
            variantOf: up.id,
          }).catch(() => null)
        }
      }
      onChange(up.url)
    } catch {
      toast.error(t('uploader.failed'))
    } finally {
      setUploading(false)
    }
  }

  // 卡片：有值=预览（点开放大，悬停浮现清除）。无值时不再渲染独立的虚线/按钮块——上传入口并入输入行行首
  const cardEl = value ? (
    <div className={cn('group relative shrink-0', previewBelow && 'self-start')}>
      <button
        type="button"
        disabled={disabled}
        onClick={() => setZoom(true)}
        aria-label={t('common.preview')}
        title={t('common.preview')}
        className="block rounded-md focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed"
      >
        <img
          src={value}
          alt=""
          loading="lazy"
          className={cn('rounded-md border border-border bg-muted object-contain p-1', previewClassName)}
          style={{
            // 细棋盘格垫底：透明 PNG/SVG、暗色 logo 在深色主题下不再与底色融成一块
            backgroundImage:
              'repeating-conic-gradient(hsl(var(--muted-foreground) / 0.14) 0% 25%, transparent 0% 50%)',
            backgroundSize: '10px 10px',
          }}
        />
      </button>
      {/* 悬停/聚焦时浮现操作；触屏没有 hover，仍走卡片点开放大 + 行内「清除」按钮 */}
      <div className="pointer-events-none absolute inset-0 flex items-center justify-center gap-1 rounded-md bg-black/50 opacity-0 transition-opacity group-focus-within:pointer-events-auto group-focus-within:opacity-100 group-hover:pointer-events-auto group-hover:opacity-100">
        <Button
          type="button"
          size="icon"
          variant="ghost"
          className="size-7 text-white hover:bg-white/20 hover:text-white"
          disabled={disabled}
          onClick={() => setZoom(true)}
          aria-label={t('common.preview')}
          title={t('common.preview')}
        >
          <ZoomIn aria-hidden="true" />
        </Button>
        <Button
          type="button"
          size="icon"
          variant="ghost"
          className="size-7 text-white hover:bg-white/20 hover:text-white"
          disabled={disabled}
          onClick={() => onChange('')}
          aria-label={t('common.clear')}
          title={t('common.clear')}
        >
          <Trash2 aria-hidden="true" />
        </Button>
      </div>
    </div>
  ) : null
  const uploadBtn = (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className="h-9 shrink-0"
      disabled={disabled || uploading}
      onClick={() => (crop ? setOpen(true) : inputRef.current?.click())}
      aria-label={ariaLabel ?? title}
    >
      {uploading ? <Loader2 aria-hidden="true" className="size-4 animate-spin" /> : <Upload aria-hidden="true" className="size-4" />}
      {uploading ? t('uploader.uploading') : t('common.upload')}
    </Button>
  )
  const hintEl = hint && !crop ? <p className="text-xs text-muted-foreground">{hint}</p> : null
  // 一行吃下所有元素：[预览] [输入框] [清除/上传] [从文件库选择]。
  // 之前「清除」竖排在输入列下方被 flex-col 拉成整行宽的居中大按钮，预览也掉出输入行——一行字段裂成三截。
  const inputEl = (
    <div className="flex min-w-0 flex-1 items-center gap-2">
      <Input
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        inputMode="url"
        spellCheck={false}
        className="min-w-0 flex-1"
        aria-label={ariaLabel ?? title}
        aria-invalid={ariaInvalid}
      />
      {/* 上传始终在行内：有值时也要能直接换图（先清除再上传会让预览闪没）；
          清除只在有值时出现，紧挨文件库入口 */}
      {uploadBtn}
      {value ? (
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-9 shrink-0"
          disabled={disabled}
          onClick={() => onChange('')}
          aria-label={t('common.clear')}
          title={t('common.clear')}
        >
          <Trash2 aria-hidden="true" />
        </Button>
      ) : null}
      {/* 文件库弹窗：已传过的图片直接选（缩略图网格 + 关键词可搜、按上传时间倒序） */}
      <PickFromLibraryButton className="shrink-0" disabled={disabled} onClick={() => setPickOpen(true)} />
    </div>
  )

  return (
    <div className="flex flex-col gap-2">
      {previewBelow ? (
        <>
          {/* 输入行在上、预览卡在下：长表单里先填地址再看效果（商品封面/视频的形态） */}
          {inputEl}
          {hintEl}
          {cardEl}
        </>
      ) : (
        <>
          {/* 卡在左、URL 行在右：上传卡不再孤零零吊在左下角，右侧一行正好吃掉整行宽度 */}
          <div className="flex items-center gap-3">
            {cardEl}
            {inputEl}
          </div>
          {hintEl}
        </>
      )}
      {value ? (
        <Lightbox
          open={zoom}
          onOpenChange={setZoom}
          slides={[{ src: value, alt: ariaLabel ?? title }]}
        />
      ) : null}
      {/* 文件库弹窗不随 value 门控：空字段正是「从文件库选一张」的主场景 */}
      <FilePickerDialog
        open={pickOpen}
        onOpenChange={setPickOpen}
        kind="image"
        value={value ? [value] : []}
        onConfirm={(urls) => onChange(urls[0] ?? '')}
        disabled={disabled}
      />
      <input
        ref={inputRef}
        type="file"
        hidden
        accept="image/*"
        onChange={(e) => {
          const file = e.target.files?.[0]
          e.target.value = ''
          void pick(file)
        }}
      />
      {crop ? (
        <ResponsiveDialog open={open} onOpenChange={setOpen} size="md" title={title} description={hint}>
          <Uploader
            kind="image"
            visibility="public"
            crop={crop}
            value={[]}
            onChange={(files: UploadedFile[]) => {
              const last = files[files.length - 1]
              if (!last) return
              onChange(last.url)
              setOpen(false)
            }}
          />
        </ResponsiveDialog>
      ) : null}
    </div>
  )
}
