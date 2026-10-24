import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useCleanSendLogs } from './api'

const OPTIONS = ['7', '30', '90', '180'] as const

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 清理成功后调用（页面据此回到第一页、清空选择） */
  onDone: () => void
}

/** 按天数清理发送日志。至少保留当天之前的 N 天，不提供“全部清空”（要清空请勾选后批量删除）。 */
export function SendLogCleanDialog({ open, onOpenChange, onDone }: Props) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const clean = useCleanSendLogs()
  const [days, setDays] = useState<string>('30')

  const submit = async () => {
    if (!(await confirm({ destructive: true, description: t('sendlog.clean.confirm') }))) return
    try {
      const res = await clean.mutateAsync(Number(days))
      onOpenChange(false)
      onDone()
      toast.success(t('sendlog.clean.done', { count: res.removed }))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('sendlog.clean.title')}
      description={t('sendlog.clean.description')}
      size="sm"
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" variant="destructive" disabled={clean.isPending} onClick={submit}>
            {t('common.confirm')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-1.5">
        <Label>{t('sendlog.clean.range')}</Label>
        <Select value={days} onValueChange={setDays}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {OPTIONS.map((d) => (
              <SelectItem key={d} value={d}>
                {t(`sendlog.clean.d${d}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </ResponsiveDialog>
  )
}
