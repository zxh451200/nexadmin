import { useState } from 'react'
import { VideoIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { RemoteSelect } from './RemoteSelect'
import { ResponsiveDialog } from './ResponsiveDialog'
import { Uploader, type UploadedFile } from './Uploader'

export interface VideoUrlFieldProps {
  value: string
  onChange: (url: string) => void
  /** 上传弹窗标题 */
  title: string
  /** 字段说明，挂在输入框下方 */
  hint?: string
  placeholder?: string
  disabled?: boolean
  'aria-label'?: string
}

/**
 * 单视频字段的统一形态：URL 输入 + 上传/预览卡 + 清除（对齐 ImageUrlField 的收敛后形态）。
 * 上传走 Uploader 的 video 通道：断点续传 + 自动截海报帧，大文件也有进度可看；
 * 预览卡直接内联播放，清除入口在输入框旁。
 */
export function VideoUrlField({
  value,
  onChange,
  title,
  hint,
  placeholder,
  disabled,
  'aria-label': ariaLabel,
}: VideoUrlFieldProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [zoom, setZoom] = useState(false)

  return (
    <div className="flex items-center gap-3">
      {/* 卡在左、URL 行在右：对齐 ImageUrlField 的横向形态；卡片统一 96px 方形，与封面/Logo 同规格。
          有值时是帧缩略（去 controls），点击开弹层播放——96px 里塞播放条会挤成一坨 */}
      {value ? (
        <button
          type="button"
          disabled={disabled}
          onClick={() => setZoom(true)}
          aria-label={t('common.preview')}
          title={t('common.preview')}
          className="block shrink-0 rounded-md focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed"
        >
          <video
            src={value}
            preload="metadata"
            muted
            className="size-24 rounded-md border border-border bg-muted/40 object-cover"
          />
        </button>
      ) : (
        <button
          type="button"
          disabled={disabled}
          onClick={() => setOpen(true)}
          aria-label={ariaLabel ?? title}
          className="flex size-24 shrink-0 flex-col items-center justify-center gap-1 rounded-md border border-dashed text-xs text-muted-foreground transition-colors hover:border-primary/50 hover:bg-muted/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
        >
          <VideoIcon aria-hidden="true" className="size-5" />
          {t('uploader.add_video')}
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
            className="min-w-0 flex-1 font-mono"
            aria-label={ariaLabel ?? title}
          />
          {/* 文件库下拉：已传过的视频直接选，不用手写 URL（关键词可搜、按上传时间倒序） */}
          <RemoteSelect
            url="/files"
            params={{ category: 'video' }}
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
        {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
      </div>
      {/* 点缩略放大播放：与 ImageUrlField 点图放大同一交互 */}
      <ResponsiveDialog open={zoom} onOpenChange={setZoom} size="md" title={title}>
        <video src={value} controls autoPlay className="max-h-[70vh] w-full rounded-md bg-black" />
      </ResponsiveDialog>
      <ResponsiveDialog open={open} onOpenChange={setOpen} size="md" title={title} description={hint}>
        <Uploader
          kind="video"
          visibility="public"
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
