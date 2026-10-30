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
  /** alt 只对图片有意义：默认取文件名，允许插入前改掉 */
  onInsert: (kind: 'image' | 'video', file: UploadedFile, alt?: string) => void
}

/**
 * 插入图片/视频：本地上传（图片 public）或粘贴外部视频直链（如 Cloudflare Stream 的 MP4 下载地址）。
 *
 * 图片上传后不立刻插入，先给出一个「描述（alt）」输入框（预填文件名）——
 * 正文里的图对读屏和搜索都有用，而 alt 只能在这一步顺手写，插进去之后就没有入口了。
 */
export function MediaDialog({ kind, onClose, onInsert }: MediaDialogProps) {
  const { t } = useTranslation()
  const [files, setFiles] = useState<UploadedFile[]>([])
  const [url, setUrl] = useState('')
  const [pending, setPending] = useState<UploadedFile | null>(null)
  const [alt, setAlt] = useState('')

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

  const reset = () => {
    setPending(null)
    setAlt('')
  }

  return (
    <Dialog open={!!kind} onOpenChange={(o) => { if (!o) { reset(); onClose() } }}>
      <DialogContent className="w-[calc(100vw-1.5rem)] max-w-md">
        <DialogHeader className="text-start">
          <DialogTitle>{kind === 'video' ? t('editor.insertVideo') : t('editor.insertImage')}</DialogTitle>
          <DialogDescription>{t('editor.mediaHint')}</DialogDescription>
        </DialogHeader>
        {kind && !pending && (
          <Uploader
            kind={kind}
            visibility={kind === 'image' ? 'public' : 'private'}
            value={files}
            onChange={(v) => {
              const last = v[v.length - 1]
              setFiles([])
              if (!last) return
              if (kind === 'video') {
                onInsert('video', last)
                return
              }
              setPending(last)
              setAlt(last.name.replace(/\.[^.]+$/, ''))
            }}
          />
        )}
        {kind === 'image' && pending ? (
          <div className="flex flex-col gap-2">
            <img src={pending.url} alt="" className="max-h-40 w-full rounded-md border object-contain" />
            <Label htmlFor="editor-image-alt" className="text-xs text-muted-foreground">
              {t('editor.imageAlt')}
            </Label>
            <div className="flex gap-2">
              <Input
                id="editor-image-alt"
                value={alt}
                maxLength={200}
                placeholder={t('editor.imageAltHint')}
                onChange={(e) => setAlt(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    onInsert('image', pending, alt.trim())
                    reset()
                  }
                }}
              />
              <Button type="button" onClick={() => { onInsert('image', pending, alt.trim()); reset() }}>
                {t('editor.insertImage')}
              </Button>
            </div>
            <Button type="button" variant="ghost" size="sm" className="self-start" onClick={reset}>
              {t('common.back')}
            </Button>
          </div>
        ) : null}
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
                placeholder={t('editor.videoUrlPlaceholder')}
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
