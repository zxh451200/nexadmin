import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Textarea } from '@/framework/ui/textarea'
import { useImportLicense, type PluginInfo } from './api'

const MAX_BYTES = 256 * 1024

interface Props {
  plugin: PluginInfo | null
  onOpenChange: (open: boolean) => void
  /** 预填的授权文本（市场安装后自动弹导时使用） */
  initialContent?: string
}

/** 外层按 plugin 切换 key，使每次打开都是干净状态 */
export function LicenseDialog({ plugin, onOpenChange, initialContent }: Props) {
  return (
    <LicenseDialogBody
      key={plugin?.id ?? ''}
      plugin={plugin}
      onOpenChange={onOpenChange}
      initialContent={initialContent}
    />
  )
}

function LicenseDialogBody({ plugin, onOpenChange, initialContent }: Props) {
  const { t } = useTranslation()
  const [content, setContent] = useState(initialContent ?? '')
  const [fileName, setFileName] = useState('')
  const mutation = useImportLicense()

  const onFile = async (file: File | undefined) => {
    if (!file) return
    if (file.size > MAX_BYTES) {
      toast.error(t('pluginMgr.license.tooLarge'))
      return
    }
    setContent(await file.text())
    setFileName(file.name)
  }

  const submit = async () => {
    if (!plugin || !content.trim()) return
    try {
      const res = await mutation.mutateAsync({ id: plugin.id, content })
      toast.success(res.needRestart ? t('pluginMgr.license.needRestart') : t('pluginMgr.license.effective'))
      onOpenChange(false)
    } catch {
      // 错误提示由请求层统一弹出
    }
  }

  return (
    <ResponsiveDialog
      open={!!plugin}
      onOpenChange={onOpenChange}
      title={t('pluginMgr.license.title', { name: plugin?.name ?? '' })}
      description={t('pluginMgr.license.desc')}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={submit} disabled={!content.trim() || mutation.isPending}>
            {t('common.import')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="plugin-license-file">{t('pluginMgr.license.file')}</Label>
          <Input id="plugin-license-file" type="file" accept=".lic,.txt,.json,text/plain" onChange={(e) => void onFile(e.target.files?.[0])} />
          {fileName ? <p className="truncate text-xs text-muted-foreground">{fileName}</p> : null}
        </div>
        <div className="space-y-2">
          <Label htmlFor="plugin-license-text">{t('pluginMgr.license.paste')}</Label>
          <Textarea
            id="plugin-license-text"
            rows={8}
            className="num break-all"
            value={content}
            placeholder={t('pluginMgr.license.placeholder')}
            onChange={(e) => {
              setContent(e.target.value)
              setFileName('')
            }}
          />
        </div>
      </div>
    </ResponsiveDialog>
  )
}
