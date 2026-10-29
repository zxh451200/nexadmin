import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/framework/ui/dialog'
import { Uploader } from '@/framework/components/Uploader'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import type { UploadedFile } from '@/framework/upload/types'

export interface MediaDialogProps {
  kind: 'image' | 'video' | null
  onClose: () => void
  onInsert: (kind: 'image' | 'video', file: UploadedFile) => void
}

/** 插入图片/视频：本地上传（图片 public）或粘贴外部视频直链（如 Cloudflare Stream 的 MP4 下载地址）。 */
export function MediaDialog({ kind, onClose, onInsert }: MediaDialogProps) {
  const { t } = useTranslation()
  const [files, setFiles] = useState<UploadedFile[]>([])
  const [url, setUrl] = useState('')

  const insertUrl = () => {
    const src = url.trim()
    if (!/^https?:\/\//i.test(src)) {
      toast.error(t('editor.urlInvalid'))
      return
    }
    // 只写 src：外部地址没有 poster，交给浏览器首帧。
    onInsert('video', { id: '', url: src, name: src.split('/').pop() || 'video', size: 0, type: 'video/mp4' })
    setUrl('')
  }

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
        {kind === 'video' ? (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="editor-video-url" className="text-xs text-muted-foreground">
              {t('editor.videoUrl')}
            </Label>
            <div className="flex gap-2">
              <Input
                id="editor-video-url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://customer-xxx.cloudflarestream.com/<uid>/downloads/default.mp4"
                className="font-mono text-xs"
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    insertUrl()
                  }
                }}
              />
              <Button type="button" variant="outline" onClick={insertUrl} disabled={!url.trim()}>
                {t('editor.insertUrl')}
              </Button>
            </div>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
