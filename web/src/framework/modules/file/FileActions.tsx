import { useState } from 'react'
import { Copy, Eye, FolderInput, Globe, Lock, MoreHorizontal, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { FilePreview } from '@/framework/components/FilePreview'
import { Lightbox } from '@/framework/upload/Lightbox'
import { Button } from '@/framework/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/framework/ui/dropdown-menu'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { cn } from '@/lib/utils'
import { FileThumb } from './FileThumb'
import { useFileGroups, type FileItem } from './api'

export function fileLink(f: FileItem): string {
  if (!f.url || f.url.startsWith('data:')) return f.url ? `${window.location.origin}/files/${f.id}/${encodeURIComponent(f.name)}` : ''
  return f.url.startsWith('http') || f.url.startsWith('blob:') ? f.url : `${window.location.origin}${f.url}`
}

export async function copyLink(f: FileItem, t: (k: string) => string) {
  const link = fileLink(f) || `${window.location.origin}/files/${f.id}/${encodeURIComponent(f.name)}`
  try {
    await navigator.clipboard.writeText(link)
    toast.success(t('fileMgr.copied'))
  } catch {
    toast.error(t('fileMgr.copyFailed'))
  }
}

/** 预览：图片放大，视频播放，其他显示文件卡片 */
export function FilePreviewDialog({ file, onClose }: { file: FileItem | null; onClose: () => void }) {
  const { t } = useTranslation()
  if (!file) return null
  if (file.category === 'image' && file.url) {
    return <Lightbox open onOpenChange={(o) => !o && onClose()} src={file.url} alt={file.name} />
  }
  const playable = file.category === 'video' && !!file.url
  return (
    <ResponsiveDialog open onOpenChange={(o) => !o && onClose()} title={file.name} size="lg">
      {playable || file.category !== 'video' ? (
        <FilePreview file={{ id: file.id, url: file.url, name: file.name, size: file.size, type: file.type, posterUrl: file.posterUrl }} />
      ) : (
        <div className="space-y-2">
          <div className="aspect-video overflow-hidden rounded-md border border-border">
            <FileThumb file={file} />
          </div>
          <p className="text-sm text-muted-foreground">{t('fileMgr.noVideoSource')}</p>
        </div>
      )}
    </ResponsiveDialog>
  )
}

export function MoveDialog({ open, onOpenChange, count, onConfirm }: { open: boolean; onOpenChange: (o: boolean) => void; count: number; onConfirm: (groupId: string) => void }) {
  const { t } = useTranslation()
  const { data: groups = [] } = useFileGroups()
  const [target, setTarget] = useState('none')
  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('fileMgr.moveTo')}
      description={t('fileMgr.selectedCount', { count })}
      size="sm"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => onConfirm(target === 'none' ? '' : target)}>{t('common.confirm')}</Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label>{t('fileMgr.moveTo')}</Label>
        <Select value={target} onValueChange={setTarget}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="none">{t('fileMgr.ungrouped')}</SelectItem>
            {groups.map((g) => (
              <SelectItem key={g.id} value={g.id}>
                {g.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </ResponsiveDialog>
  )
}

interface MenuProps {
  file: FileItem
  onPreview: (f: FileItem) => void
  onToggleVisibility: (f: FileItem) => void
  onMove: (f: FileItem) => void
  onDelete: (f: FileItem) => void
  /** 缩略图上的浮层触发器需要自带底色，否则压在图片上看不清 */
  triggerClassName?: string
}

export function FileActionsMenu({ file, onPreview, onToggleVisibility, onMove, onDelete, triggerClassName }: MenuProps) {
  const { t } = useTranslation()
  const { has } = usePermission()
  const isPublic = file.visibility === 'public'
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t('common.more')} className={cn('size-11 md:size-8', triggerClassName)} onClick={(e) => e.stopPropagation()}>
          <MoreHorizontal className="size-4" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
        <DropdownMenuItem className="min-h-11 md:min-h-8" onSelect={() => onPreview(file)}>
          <Eye className="me-2 size-4" aria-hidden="true" />
          {t('common.preview')}
        </DropdownMenuItem>
        <DropdownMenuItem className="min-h-11 md:min-h-8" onSelect={() => void copyLink(file, t)}>
          <Copy className="me-2 size-4" aria-hidden="true" />
          {t('fileMgr.copyLink')}
        </DropdownMenuItem>
        {has('file:upload') && (
          <>
            <DropdownMenuItem className="min-h-11 md:min-h-8" onSelect={() => onToggleVisibility(file)}>
              {isPublic ? <Lock className="me-2 size-4" aria-hidden="true" /> : <Globe className="me-2 size-4" aria-hidden="true" />}
              {isPublic ? t('fileMgr.setPrivate') : t('fileMgr.setPublic')}
            </DropdownMenuItem>
            <DropdownMenuItem className="min-h-11 md:min-h-8" onSelect={() => onMove(file)}>
              <FolderInput className="me-2 size-4" aria-hidden="true" />
              {t('fileMgr.moveTo')}
            </DropdownMenuItem>
          </>
        )}
        <Can perm="file:delete">
          <DropdownMenuSeparator />
          <DropdownMenuItem className="min-h-11 text-destructive focus:text-destructive md:min-h-8" onSelect={() => onDelete(file)}>
            <Trash2 className="me-2 size-4" aria-hidden="true" />
            {t('common.delete')}
          </DropdownMenuItem>
        </Can>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
