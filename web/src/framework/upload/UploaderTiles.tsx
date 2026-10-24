import { AlertCircle, GripVertical, RefreshCw, RotateCcw, X } from 'lucide-react'
import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Progress } from '@/framework/ui/progress'
import { FilePreview, fileCategory } from '@/framework/components/FilePreview'
import { formatSize } from './precheck'
import type { UploadedFile } from './types'

export interface PendingItem {
  uid: string
  file: File
  previewUrl: string
  progress: number
  error?: string
  replaceId?: string
}

export const tileSize = (cat: 'image' | 'video' | 'file') =>
  cat === 'file' ? 'w-full' : cat === 'video' ? 'w-full max-w-xs sm:w-56' : 'size-28 md:size-32'

const barBtn =
  'inline-flex h-11 flex-1 items-center justify-center text-foreground hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:h-8'

export function FileTile({
  file,
  sortable,
  onReplace,
  onRemove,
}: {
  file: UploadedFile
  sortable: boolean
  onReplace: () => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const cat = fileCategory(file)
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: file.id, disabled: !sortable })
  const style = { transform: CSS.Transform.toString(transform), transition }

  const handle = sortable && (
    <button
      ref={setActivatorNodeRef}
      type="button"
      aria-label={t('uploader.drag')}
      className={cn(
        'inline-flex size-11 touch-none items-center justify-center rounded-md bg-background/80 text-muted-foreground backdrop-blur hover:text-foreground md:size-8',
        cat === 'file' ? 'shrink-0' : 'absolute start-1 top-1 z-10',
      )}
      {...attributes}
      {...listeners}
    >
      <GripVertical className="size-4" aria-hidden="true" />
    </button>
  )

  const actions =
    cat === 'file' ? (
      <div className="flex shrink-0 items-center">
        <button type="button" aria-label={t('common.replace')} className="inline-flex size-11 items-center justify-center rounded-md text-muted-foreground hover:bg-muted md:size-8" onClick={onReplace}>
          <RefreshCw className="size-4" aria-hidden="true" />
        </button>
        <button type="button" aria-label={t('common.remove')} className="inline-flex size-11 items-center justify-center rounded-md text-destructive hover:bg-muted md:size-8" onClick={onRemove}>
          <X className="size-4" aria-hidden="true" />
        </button>
      </div>
    ) : (
      <div className="absolute inset-x-0 bottom-0 z-10 flex divide-x divide-border border-t border-border bg-background/85 backdrop-blur rtl:divide-x-reverse">
        <button type="button" aria-label={t('common.replace')} className={barBtn} onClick={onReplace}>
          <RefreshCw className="size-4" aria-hidden="true" />
        </button>
        <button type="button" aria-label={t('common.remove')} className={cn(barBtn, 'text-destructive')} onClick={onRemove}>
          <X className="size-4" aria-hidden="true" />
        </button>
      </div>
    )

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={cn('flex items-center gap-1', tileSize(cat), cat !== 'file' && 'block', isDragging && 'z-20 opacity-80 shadow-lg')}
    >
      {cat === 'file' && handle}
      <FilePreview file={file} className={cn(cat === 'file' ? 'min-w-0 flex-1' : 'size-full', cat === 'video' && 'pb-11 md:pb-8')}>
        {cat !== 'file' && handle}
        {actions}
      </FilePreview>
    </div>
  )
}

export function PendingTile({
  item,
  kind,
  onCancel,
  onRetry,
}: {
  item: PendingItem
  kind: 'image' | 'video' | 'file'
  onCancel: () => void
  onRetry: () => void
}) {
  const { t } = useTranslation()
  const failed = !!item.error
  return (
    <div className={cn('relative overflow-hidden rounded-md border border-border bg-muted', tileSize(kind), kind === 'file' && 'bg-card')}>
      {kind === 'image' && <img src={item.previewUrl} alt={item.file.name} className="size-full object-cover opacity-60" />}
      {kind === 'video' && <video src={item.previewUrl} muted playsInline preload="metadata" className="aspect-video size-full object-cover opacity-60" />}
      <div className={cn('flex flex-col gap-1 p-2', kind !== 'file' && 'absolute inset-0 justify-end bg-background/50')}>
        <p className="truncate text-xs">{item.file.name}</p>
        {failed ? (
          <p className="flex items-center gap-1 text-xs text-destructive">
            <AlertCircle className="size-3 shrink-0" aria-hidden="true" />
            <span className="truncate">{t('uploader.failed')}</span>
          </p>
        ) : (
          <div className="flex items-center gap-2">
            <Progress value={item.progress} className="h-1.5" aria-label={t('uploader.uploading')} />
            <span className="num w-9 shrink-0 text-end text-xs">{item.progress}%</span>
          </div>
        )}
        {kind === 'file' && <p className="num text-xs text-muted-foreground">{formatSize(item.file.size)}</p>}
        <div className="flex">
          {failed && (
            <button type="button" aria-label={t('uploader.retry')} className="inline-flex size-11 items-center justify-center rounded-md hover:bg-muted md:size-8" onClick={onRetry}>
              <RotateCcw className="size-4" aria-hidden="true" />
            </button>
          )}
          <button type="button" aria-label={t('common.cancel')} className="inline-flex size-11 items-center justify-center rounded-md text-destructive hover:bg-muted md:size-8" onClick={onCancel}>
            <X className="size-4" aria-hidden="true" />
          </button>
        </div>
      </div>
    </div>
  )
}
