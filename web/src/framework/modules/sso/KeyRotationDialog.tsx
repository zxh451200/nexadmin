import { KeyRound, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { useRotateSsoKeys, useSsoKeys } from './api'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** 签名密钥轮换：列出 JWKS 里的 key，并提供带二次确认的“立即轮换” */
export function KeyRotationDialog({ open, onOpenChange }: Props) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data, isLoading } = useSsoKeys(open)
  const rotate = useRotateSsoKeys()

  const onRotate = async () => {
    if (!(await confirm({ destructive: true, title: t('sso.keys.rotate'), description: t('sso.keys.confirm') }))) return
    try {
      await rotate.mutateAsync()
      toast.success(t('sso.keys.rotated'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="md"
      title={t('sso.keys.title')}
      description={t('sso.keys.description')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.close')}
          </Button>
          <Can perm="sso:edit">
            <Button type="button" onClick={onRotate} disabled={rotate.isPending}>
              <RefreshCw className="me-2 h-4 w-4" aria-hidden="true" />
              {rotate.isPending ? t('common.loading') : t('sso.keys.rotate')}
            </Button>
          </Can>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Alert>
          <KeyRound aria-hidden="true" />
          <AlertDescription>{t('sso.keys.notice')}</AlertDescription>
        </Alert>
        {isLoading ? (
          <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {(data ?? []).map((k) => (
              <li key={k.kid} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-3">
                <div className="flex flex-col gap-0.5">
                  <code className="font-mono text-sm">{k.kid}</code>
                  <span className="num text-xs text-muted-foreground">
                    {t('sso.keys.createdAt')}: {new Date(k.createdAt).toLocaleString()}
                    {k.retiresAt ? ` · ${t('sso.keys.retiresAt')}: ${new Date(k.retiresAt).toLocaleString()}` : ''}
                  </span>
                </div>
                <Badge variant={k.status === 'active' ? 'success' : 'secondary'}>{t(`sso.keys.status.${k.status}`)}</Badge>
              </li>
            ))}
          </ul>
        )}
      </div>
    </ResponsiveDialog>
  )
}
