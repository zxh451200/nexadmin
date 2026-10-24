import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Uploader, type UploadedFile } from '@/framework/components/Uploader'
import { Button } from '@/framework/ui/button'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import type { UploadKind } from '@/framework/upload/types'
import { useFileGroups, useRegisterFiles, type FileVisibility } from './api'

interface Props {
  open: boolean
  onOpenChange: (o: boolean) => void
  /** 默认保存到的分组 id，空字符串为未分组 */
  defaultGroupId: string
}

const KINDS: UploadKind[] = ['image', 'video', 'file']

export function UploadDialog({ open, onOpenChange, defaultGroupId }: Props) {
  const { t } = useTranslation()
  const { data: groups = [] } = useFileGroups()
  const register = useRegisterFiles()
  const [kind, setKind] = useState<UploadKind>('image')
  const [visibility, setVisibility] = useState<FileVisibility>('private')
  const [groupId, setGroupId] = useState<string | null>(null)
  const [files, setFiles] = useState<UploadedFile[]>([])

  const group = groupId ?? (defaultGroupId || 'none')

  const reset = () => {
    setFiles([])
    setGroupId(null)
  }

  const done = async () => {
    if (files.length === 0) {
      toast.warning(t('fileMgr.uploadEmpty'))
      return
    }
    try {
      await register.mutateAsync({
        files: files.map((f) => ({ name: f.name, url: f.url, posterUrl: f.posterUrl, type: f.type, size: f.size })),
        groupId: group === 'none' ? '' : group,
        visibility,
      })
      toast.success(t('fileMgr.uploadSuccess', { count: files.length }))
      reset()
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(o) => {
        if (!o) reset()
        onOpenChange(o)
      }}
      title={t('fileMgr.upload')}
      description={t('fileMgr.uploadHint')}
      size="lg"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button disabled={register.isPending} onClick={() => void done()}>
            {t('fileMgr.uploadDone')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="grid gap-4 md:grid-cols-3">
          <div className="space-y-2">
            <Label>{t('fileMgr.uploadKind')}</Label>
            <Select
              value={kind}
              onValueChange={(v) => {
                setKind(v as UploadKind)
                setFiles([])
              }}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {t(`fileMgr.kind.${k}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label>{t('fileMgr.uploadTargetGroup')}</Label>
            <Select value={group} onValueChange={setGroupId}>
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
          <div className="space-y-2">
            <Label>{t('fileMgr.uploadVisibility')}</Label>
            <Select value={visibility} onValueChange={(v) => setVisibility(v as FileVisibility)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="private">{t('fileMgr.private')}</SelectItem>
                <SelectItem value="public">{t('fileMgr.public')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <Uploader key={kind} kind={kind} multiple maxCount={20} visibility={visibility} value={files} onChange={setFiles} />
      </div>
    </ResponsiveDialog>
  )
}
