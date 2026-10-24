import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/framework/ui/dialog'
import { Uploader } from '@/framework/components/Uploader'
import type { UploadedFile } from '@/framework/upload/types'

export interface MediaDialogProps {
  kind: 'image' | 'video' | null
  onClose: () => void
  onInsert: (kind: 'image' | 'video', file: UploadedFile) => void
}

/** 插入图片/视频：内置 Uploader（图片为 public） */
export function MediaDialog({ kind, onClose, onInsert }: MediaDialogProps) {
  const { t } = useTranslation()
  const [files, setFiles] = useState<UploadedFile[]>([])
  return (
    <Dialog open={!!kind} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="w-[calc(100vw-1.5rem)] max-w-md">
        <DialogHeader className="text-start">
          <DialogTitle>{kind === 'video' ? t('editor.insertVideo') : t('editor.insertImage')}</DialogTitle>
          <DialogDescription>{t('editor.mediaHint')}</DialogDescription>
        </DialogHeader>
        {kind && (
          <Uploader
            kind={kind}
            visibility={kind === 'image' ? 'public' : 'private'}
            value={files}
            onChange={(v) => {
              const last = v[v.length - 1]
              setFiles([])
              if (last) onInsert(kind, last)
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
