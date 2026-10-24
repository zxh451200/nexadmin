import { TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { buildEndpoints } from './api'
import { CopyButton } from './CopyButton'

export interface SecretTarget {
  name: string
  clientId: string
  /** 仅创建 / 重置后有值；纯查看接入信息时为空 */
  clientSecret?: string
}

function Row({ label, value, mono = true }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      <div className="flex items-center gap-1 rounded-md border border-border bg-muted/40 ps-3">
        <code className={`min-w-0 flex-1 break-all py-2 text-sm text-foreground ${mono ? 'font-mono' : ''}`}>{value}</code>
        <CopyButton text={value} label={`${label}`} />
      </div>
    </div>
  )
}

interface Props {
  target: SecretTarget | null
  onOpenChange: (open: boolean) => void
}

export function SecretDialog({ target, onOpenChange }: Props) {
  const { t } = useTranslation()
  const ep = buildEndpoints(window.location.origin)

  return (
    <ResponsiveDialog
      open={!!target}
      onOpenChange={onOpenChange}
      size="lg"
      title={target?.clientSecret ? t('sso.secret.title') : t('sso.access.title')}
      description={target?.name}
      footer={
        <Button type="button" onClick={() => onOpenChange(false)}>
          {target?.clientSecret ? t('sso.secret.saved') : t('common.close')}
        </Button>
      }
    >
      {target ? (
        <div className="flex flex-col gap-5">
          {target.clientSecret ? (
            <>
              <div
                role="alert"
                className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm text-foreground"
              >
                <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" aria-hidden="true" />
                <span>{t('sso.secret.onceHint')}</span>
              </div>
              <Row label={t('sso.fields.clientSecret')} value={target.clientSecret} />
            </>
          ) : null}
          <Row label={t('sso.fields.clientId')} value={target.clientId} />

          <section className="flex flex-col gap-3">
            <div className="border-b border-border pb-2">
              <h3 className="text-sm font-semibold text-foreground">{t('sso.access.section')}</h3>
              <p className="mt-1 text-xs text-muted-foreground">{t('sso.access.hint')}</p>
            </div>
            <Row label={t('sso.access.issuer')} value={ep.issuer} />
            <Row label={t('sso.access.discovery')} value={ep.discovery} />
            <Row label={t('sso.access.authorization')} value={ep.authorization} />
            <Row label={t('sso.access.token')} value={ep.token} />
          </section>
        </div>
      ) : null}
    </ResponsiveDialog>
  )
}
