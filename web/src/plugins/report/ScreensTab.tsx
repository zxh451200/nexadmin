import { useState } from 'react'
import { Monitor, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDeleteScreen, useScreenList, type Screen, type ScreenBrief } from './api'
import { ScreenDialog } from './ScreenDialog'
import { builtinText } from './builtinText'

export function ScreensTab() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const confirm = useConfirm()
  const { data: screens = [], isLoading } = useScreenList()
  const remove = useDeleteScreen()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Screen | null>(null)

  const onEdit = async (s: ScreenBrief) => {
    try {
      setEditing(await api.get<Screen>(`/plugins/report/screens/${s.id}`))
      setOpen(true)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  const onDelete = async (s: ScreenBrief) => {
    if (!(await confirm({ destructive: true, description: t('plugin_report.screen.deleteConfirm', { name: builtinText(t, s.name) }) }))) return
    try {
      await remove.mutateAsync(s.id)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <Can perm="report:screen">
        <div className="flex justify-end">
          <Button
            onClick={() => {
              setEditing(null)
              setOpen(true)
            }}
          >
            <Plus aria-hidden="true" />
            {t('plugin_report.screen.create')}
          </Button>
        </div>
      </Can>
      {isLoading ? <Skeleton className="h-32 w-full" /> : null}
      {!isLoading && screens.length === 0 ? <EmptyState title={t('plugin_report.screen.empty')} description={t('plugin_report.screen.emptyHint')} /> : null}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {screens.map((s) => (
          <Card key={s.id}>
            <CardHeader className="space-y-1 pb-2">
              <CardTitle className="flex items-center gap-2 text-base">
                <Monitor className="size-4 text-primary" aria-hidden="true" />
                <span className="truncate">{builtinText(t, s.name)}</span>
              </CardTitle>
              <p className="line-clamp-2 min-h-8 text-xs text-muted-foreground">{s.remark || t('plugin_report.screen.noRemark')}</p>
            </CardHeader>
            <CardContent className="flex flex-wrap items-center gap-2">
              <Button size="sm" onClick={() => navigate(`/monitor/report-screen?id=${s.id}`)}>
                {t('plugin_report.screen.open')}
              </Button>
              <Can perm="report:screen">
                <div className="ms-auto flex items-center gap-1">
                  <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => void onEdit(s)}>
                    <Pencil aria-hidden="true" />
                  </Button>
                  <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(s)}>
                    <Trash2 aria-hidden="true" />
                  </Button>
                </div>
              </Can>
            </CardContent>
          </Card>
        ))}
      </div>
      <ScreenDialog open={open} onOpenChange={setOpen} editing={editing} />
    </div>
  )
}
