import { useState } from 'react'
import { VideoIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { FilePickerDialog, PickFromLibraryButton } from './FilePickerDialog'
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
  /** true = 输入行在上、预览/上传卡在下（长表单里先看到输入再看到视频）；默认横排（卡在左、输入在右） */
  previewBelow?: boolean
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
  previewBelow = false,
  'aria-label': ariaLabel,
}: VideoUrlFieldProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [zoom, setZoom] = useState(false)
  const [pickOpen, setPickOpen] = useState(false)

  // 卡片：有值=帧缩略（点开放大播放）。无值时不再渲染独立的虚线/按钮块——上传入口并入输入行行首
  const cardEl = value ? (
    <button
      type="button"
      disabled={disabled}
      onClick={() => setZoom(true)}
      aria-label={t('common.preview')}
      title={t('common.preview')}
      className={cn('block shrink-0 rounded-md focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed', previewBelow && 'self-start')}
    >
      <video
        src={value}
        preload="metadata"
        muted
        className="size-24 rounded-md border border-border bg-muted/40 object-cover"
      />
    </button>
  ) : null
  const uploadBtn = (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className="h-9 shrink-0"
      disabled={disabled}
      onClick={() => setOpen(true)}
      aria-label={ariaLabel ?? title}
    >
      <VideoIcon aria-hidden="true" className="size-4" />
      {t('common.upload')}
    </Button>
  )
  const inputEl = (
    <div className={previewBelow ? 'flex items-center gap-2' : 'flex min-w-0 flex-1 flex-col gap-2'}>
      <div className="flex min-w-0 flex-1 items-center gap-2">
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
        {!value ? uploadBtn : null}
        {/* 文件库弹窗：已传过的视频直接选（缩略图网格 + 关键词可搜、按上传时间倒序） */}
        <PickFromLibraryButton className="shrink-0" disabled={disabled} onClick={() => setPickOpen(true)} />
      </div>
      {value ? (
        <Button type="button" variant="ghost" className="shrink-0 px-3" disabled={disabled} onClick={() => onChange('')}>
          {t('common.clear')}
        </Button>
      ) : null}
      {previewBelow ? null : hint}
    </div>
  )
  const hintEl = hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null

  return (
    <div className={previewBelow ? 'flex flex-col gap-2' : 'flex items-center gap-3'}>
      {previewBelow ? (
        <>
          {/* 输入行在上、预览/上传卡在下：长表单里先填地址再看效果（商品视频的形态） */}
          {inputEl}
          {hintEl}
          {cardEl}
        </>
      ) : (
        <>
          {/* 卡在左、URL 行在右：对齐 ImageUrlField 的横向形态；卡片统一 96px 方形，与封面/Logo 同规格。
              有值时是帧缩略（去 controls），点击开弹层播放——96px 里塞播放条会挤成一坨 */}
          {cardEl}
          {inputEl}
        </>
      )}
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
      <FilePickerDialog
        open={pickOpen}
        onOpenChange={setPickOpen}
        kind="video"
        value={value ? [value] : []}
        onConfirm={(urls) => onChange(urls[0] ?? '')}
        disabled={disabled}
      />
    </div>
  )
}
