import { useState } from 'react'
import { Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { cn } from '@/lib/utils'
import { ResponsiveDialog } from './ResponsiveDialog'
import { Uploader, type UploadedFile } from './Uploader'
import type { CropConfig } from './ImageCropDialog'

export interface ImageUrlFieldProps {
  value: string
  onChange: (url: string) => void
  /** 上传弹窗标题，如「上传 Logo」 */
  title: string
  /** 上传弹窗里的说明 */
  hint?: string
  placeholder?: string
  crop?: CropConfig
  disabled?: boolean
  /** 预览框尺寸类，默认正方形——logo 一律完整显示，不裁切 */
  previewClassName?: string
  'aria-label'?: string
}

/**
 * 单图字段的统一形态：URL 输入 + 上传按钮（弹窗内裁剪）+ 预览 + 清除。
 * 官网封面、SSO 应用 Logo、站点 Logo 共用同一套交互，避免「上传框旁边再挂一个裸输入框」。
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
  const [open, setOpen] = useState(false)

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
        <Button type="button" variant="outline" size="sm" className="shrink-0" disabled={disabled} onClick={() => setOpen(true)}>
          <Upload aria-hidden="true" />
          {t('common.upload')}
        </Button>
        {value ? (
          <Button type="button" variant="ghost" size="sm" className="shrink-0" disabled={disabled} onClick={() => onChange('')}>
            {t('common.clear')}
          </Button>
        ) : null}
      </div>
      {value ? <img src={value} alt="" loading="lazy" className={cn('rounded-md border border-border bg-muted/40 object-contain p-1', previewClassName)} /> : null}
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
    </div>
  )
}
