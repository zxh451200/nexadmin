import { AlertCircle, RefreshCw, RotateCcw, Trash2, X } from 'lucide-react'
import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Progress } from '@/framework/ui/progress'
import { FilePreview, fileCategory } from '@/framework/components/FilePreview'
import { formatSize } from './precheck'
import { TileActions } from './TileActions'
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
  const { listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: file.id, disabled: !sortable })
  // 被拖图块自身不做补间，否则跟手慢半拍
  const style = { transform: CSS.Transform.toString(transform), transition: isDragging ? undefined : transition }
  // 整块图就是拖动抓手：不再单独放一个「六点方块」——它看着像菜单按钮，还得多按一次才拖。
  // 鼠标靠 distance:5 区分「点开大图」与「拖动」；触屏靠长按，所以容器不能加 touch-none，否则页面滑不动。
  // 不给容器 role="button"：里面已经有点图放大、替换、删除三个按钮，再套一层按钮语义只会更乱。
  const dragProps = sortable ? { ...listeners, tabIndex: 0, 'aria-label': t('uploader.drag') } : {}

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
      <TileActions
        actions={[
          { key: 'replace', label: t('common.replace'), icon: <RefreshCw className="size-4" aria-hidden="true" />, onSelect: onReplace },
          { key: 'remove', label: t('common.remove'), icon: <Trash2 className="size-4" aria-hidden="true" />, onSelect: onRemove, destructive: true },
        ]}
      />
    )

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...dragProps}
      className={cn(
        'flex items-center gap-1 outline-hidden',
        tileSize(cat),
        cat !== 'file' && 'block',
        sortable && 'cursor-grab active:cursor-grabbing focus-visible:ring-2 focus-visible:ring-ring',
        isDragging && 'z-20 cursor-grabbing opacity-80 shadow-lg',
      )}
    >
      <FilePreview file={file} className={cn(cat === 'file' ? 'min-w-0 flex-1' : 'size-full')}>
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
