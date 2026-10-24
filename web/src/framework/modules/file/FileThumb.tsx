import { File as FileIcon, FileArchive, FileAudio, FileText, Play } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { FileItem } from './api'

function DocIcon({ file, className }: { file: FileItem; className?: string }) {
  if (file.type.startsWith('audio/')) return <FileAudio className={className} aria-hidden="true" />
  if (/zip|rar|7z|tar|gzip/.test(file.type)) return <FileArchive className={className} aria-hidden="true" />
  if (file.category === 'doc') return <FileText className={className} aria-hidden="true" />
  return <FileIcon className={className} aria-hidden="true" />
}

/** 缩略图：图片原图 / 视频封面+播放标 / 文件图标 */
export function FileThumb({ file, className, iconClassName }: { file: FileItem; className?: string; iconClassName?: string }) {
  if (file.category === 'image' && file.url) {
    return <img src={file.url} alt={file.name} loading="lazy" className={cn('size-full object-cover', className)} />
  }
  if (file.category === 'video') {
    return (
      <div className={cn('relative size-full bg-muted', className)}>
        {file.posterUrl ? <img src={file.posterUrl} alt={file.name} loading="lazy" className="size-full object-cover" /> : null}
        <span className="absolute inset-0 flex items-center justify-center">
          <span className="flex size-10 items-center justify-center rounded-full bg-background/70 text-foreground backdrop-blur-sm">
            <Play className="size-5" aria-hidden="true" />
          </span>
        </span>
      </div>
    )
  }
  return (
    <div className={cn('flex size-full items-center justify-center bg-muted text-muted-foreground', className)}>
      <DocIcon file={file} className={cn('size-1/3 min-h-6 min-w-6', iconClassName)} />
    </div>
  )
}
