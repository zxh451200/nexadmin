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
import { RemoteSelect } from './RemoteSelect'
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
  'aria-label'?: string
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
  'aria-label': ariaLabel,
}: ImageUrlFieldProps) {
  const { t } = useTranslation()
  const { data: cfg } = useUploadConfig()
  const inputRef = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)
  const [open, setOpen] = useState(false)
  const [zoom, setZoom] = useState(false)

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

  return (
    <div className="flex items-center gap-3">
      {/* 卡在左、URL 行在右：上传卡不再孤零零吊在左下角，右侧一行正好吃掉整行宽度 */}
      {value ? (
        <div className="group relative shrink-0">
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
              className={cn('rounded-md border border-border bg-muted/40 object-contain p-1', previewClassName)}
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
      ) : (
        <button
          type="button"
          disabled={disabled || uploading}
          onClick={() => (crop ? setOpen(true) : inputRef.current?.click())}
          aria-label={ariaLabel ?? title}
          className={cn(
            'flex shrink-0 items-center justify-center gap-1 rounded-md border border-dashed text-xs text-muted-foreground transition-colors hover:border-primary/50 hover:bg-muted/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50',
            previewClassName,
          )}
        >
          {uploading ? (
            <Loader2 aria-hidden="true" className="size-4 animate-spin" />
          ) : (
            <Upload aria-hidden="true" className="size-4" />
          )}
          {uploading ? t('uploader.uploading') : t('common.upload')}
        </button>
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <div className="flex items-center gap-2">
          <Input
            value={value}
            disabled={disabled}
            onChange={(e) => onChange(e.target.value)}
            placeholder={placeholder}
            inputMode="url"
            spellCheck={false}
            className="min-w-0 flex-1"
            aria-label={ariaLabel ?? title}
          />
          {/* 文件库下拉：已传过的图片直接选，不用手写 URL（关键词可搜、按上传时间倒序） */}
          <RemoteSelect
            url="/files"
            params={{ category: 'image' }}
            valueKey="url"
            labelKey="name"
            value={value}
            onChange={(v) => onChange(String(v ?? ''))}
            allowClear={false}
            placeholder={t('uploader.pickFromLibrary')}
            disabled={disabled}
            className="w-36 shrink-0 sm:w-44"
          />
          {value ? (
            <Button type="button" variant="ghost" className="shrink-0 px-3" disabled={disabled} onClick={() => onChange('')}>
              {t('common.clear')}
            </Button>
          ) : null}
        </div>
        {hint && !crop ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
      </div>
      {value ? (
        <Lightbox
          open={zoom}
          onOpenChange={setZoom}
          slides={[{ src: value, alt: ariaLabel ?? title }]}
        />
      ) : null}
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
