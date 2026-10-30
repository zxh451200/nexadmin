import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'

import { Button } from '@/framework/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/framework/ui/dialog'

/**
 * 编辑器表单的字段 schema / 语言清单未就绪时的占位。
 * 之前这里只有一个标题为「加载中…」的空弹窗，点「新建」先看到一块空白卡片；
 * 现在给出转圈与说明，请求失败时给重试，而不是永远停在那里。
 */
export function EditorSchemaGate({
  onOpenChange,
  error,
  onRetry,
}: {
  onOpenChange: (open: boolean) => void
  error: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader className="text-start">
          <DialogTitle>
            {error ? t('plugin_website.editor.loadFailed') : t('plugin_website.editor.preparing')}
          </DialogTitle>
          <DialogDescription>
            {error ? t('plugin_website.editor.loadFailedHint') : t('plugin_website.editor.preparingHint')}
          </DialogDescription>
        </DialogHeader>
        <div className="flex items-center justify-center py-2">
          {error ? (
            <Button variant="outline" size="sm" onClick={onRetry}>
              {t('plugin_website.editor.retry')}
            </Button>
          ) : (
            <Loader2 className="size-5 animate-spin text-muted-foreground" aria-hidden="true" />
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
