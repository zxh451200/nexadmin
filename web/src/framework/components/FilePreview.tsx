import { useState } from 'react'
import { Download, File as FileIcon, FileArchive, FileAudio, FileText, FileVideo } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Lightbox } from '@/framework/upload/Lightbox'
import { formatSize } from '@/framework/upload/precheck'
import type { UploadedFile } from '@/framework/upload/types'

export function fileCategory(f: Pick<UploadedFile, 'type' | 'name'>): 'image' | 'video' | 'file' {
  const t = f.type?.toLowerCase() ?? ''
  const n = f.name.toLowerCase()
  if (t.startsWith('image/') || /\.(png|jpe?g|gif|webp|svg|bmp)$/.test(n)) return 'image'
  if (t.startsWith('video/') || /\.(mp4|webm|mov|m4v)$/.test(n)) return 'video'
  return 'file'
}

function IconFor({ f }: { f: UploadedFile }) {
  const t = f.type ?? ''
  const cls = 'size-6 shrink-0 text-muted-foreground'
  if (t.startsWith('audio/')) return <FileAudio className={cls} aria-hidden="true" />
  if (t.startsWith('video/')) return <FileVideo className={cls} aria-hidden="true" />
  if (/zip|rar|7z|tar|gzip/.test(t)) return <FileArchive className={cls} aria-hidden="true" />
  if (t.startsWith('text/') || /pdf|word|excel|sheet|presentation|document/.test(t)) return <FileText className={cls} aria-hidden="true" />
  return <FileIcon className={cls} aria-hidden="true" />
}

export interface FilePreviewProps {
  file: UploadedFile
  className?: string
  /** 叠加在预览上的操作区（如替换/删除按钮） */
  children?: React.ReactNode
}

/** 单个已上传文件的回显：图片缩略图(点击放大)、视频 <video controls> 加封面、其他文件图标+名称 */
export function FilePreview({ file, className, children }: FilePreviewProps) {
  const { t } = useTranslation()
  const [zoom, setZoom] = useState(false)
  const cat = fileCategory(file)

  if (cat === 'image') {
    return (
      <div className={cn('group relative overflow-hidden rounded-md border border-border bg-muted', className)}>
        <button type="button" aria-label={t('common.preview')} className="block size-full" onClick={() => setZoom(true)}>
          <img src={file.url} alt={file.name} className="aspect-square size-full object-cover" loading="lazy" />
        </button>
        <Lightbox open={zoom} onOpenChange={setZoom} src={file.url} alt={file.name} />
        {children}
      </div>
    )
  }

  if (cat === 'video') {
    return (
      <div className={cn('relative overflow-hidden rounded-md border border-border bg-muted', className)}>
        <video src={file.url} poster={file.posterUrl} controls preload="metadata" playsInline className="aspect-video size-full bg-muted object-cover" />
        {children}
      </div>
    )
  }

  return (
    <div className={cn('relative flex min-h-14 items-center gap-3 rounded-md border border-border bg-card p-3', className)}>
      <IconFor f={file} />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{file.name}</p>
        <p className="num text-xs text-muted-foreground">{formatSize(file.size)}</p>
      </div>
      <a
        href={file.url}
        download={file.name}
        target="_blank"
        rel="noreferrer"
        aria-label={t('common.download')}
        className="inline-flex size-11 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground md:size-8"
      >
        <Download className="size-4" aria-hidden="true" />
      </a>
      {children}
    </div>
  )
}
