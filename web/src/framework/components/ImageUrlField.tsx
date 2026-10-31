import { useRef, useState } from 'react'
import { Loader2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { cn } from '@/lib/utils'
import { precheckFile, useUploadConfig } from '@/framework/upload/precheck'
import { uploadFile } from '@/framework/upload/uploadClient'
import { toWebp, webpName } from '@/framework/upload/toWebp'
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
 * 单图字段的统一形态：URL 输入 + 上传按钮 + 预览 + 清除。
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
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Input
          value={value}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          inputMode="url"
          spellCheck={false}
          aria-label={ariaLabel ?? title}
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="shrink-0"
          disabled={disabled || uploading}
          onClick={() => (crop ? setOpen(true) : inputRef.current?.click())}
        >
          {uploading ? <Loader2 aria-hidden="true" className="animate-spin" /> : <Upload aria-hidden="true" />}
          {uploading ? t('uploader.uploading') : t('common.upload')}
        </Button>
        {value ? (
          <Button type="button" variant="ghost" size="sm" className="shrink-0" disabled={disabled} onClick={() => onChange('')}>
            {t('common.clear')}
          </Button>
        ) : null}
      </div>
      {hint && !crop ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
      {value ? <img src={value} alt="" loading="lazy" className={cn('rounded-md border border-border bg-muted/40 object-contain p-1', previewClassName)} /> : null}
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
