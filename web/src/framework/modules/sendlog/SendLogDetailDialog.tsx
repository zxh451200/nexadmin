import { useTranslation } from 'react-i18next'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { fmtDateTime } from '../oper-log/format'
import { useSendLog, type SendLog } from './api'
import { STATUS_VARIANT } from './status'

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 break-all text-sm text-foreground">{children}</dd>
    </div>
  )
}

interface Props {
  log: SendLog | null
  onClose: () => void
}

/** 发送详情：桌面是居中弹窗，手机是底部抽屉（ResponsiveDialog）。列表行先显示，详情接口返回后覆盖。 */
export function SendLogDetailDrawer({ log, onClose }: Props) {
  const { t, i18n } = useTranslation()
  const { data } = useSendLog(log?.id ?? null)
  const row = data ?? log
  return (
    <ResponsiveDialog
      open={!!log}
      onOpenChange={(o) => {
        if (!o) onClose()
      }}
      title={t('sendlog.detail.title')}
      description={row ? `${t(`sendlog.channel.${row.channel}`)} · ${row.recipient || '-'}` : undefined}
      size="lg"
      footer={
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {row ? (
        <div className="flex flex-col gap-4">
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={t('sendlog.col.channel')}>{t(`sendlog.channel.${row.channel}`)}</Field>
            <Field label={t('sendlog.col.scene')}>{t(`sendlog.scene.${row.scene}`, { defaultValue: row.scene })}</Field>
            <Field label={t('sendlog.col.recipient')}>
              <span className="num">{row.recipient || '-'}</span>
            </Field>
            <Field label={t('sendlog.col.status')}>
              <Badge variant={STATUS_VARIANT[row.status]}>{t(`sendlog.status.${row.status}`)}</Badge>
            </Field>
            <div className="sm:col-span-2">
              <Field label={t('sendlog.col.subject')}>{row.subject || '-'}</Field>
            </div>
            <Field label={t('sendlog.col.duration')}>
              <span className="num">{row.durationMs} ms</span>
            </Field>
            <Field label={t('sendlog.col.time')}>{fmtDateTime(row.createdAt, i18n.language)}</Field>
            <div className="sm:col-span-2">
              <Field label={t('sendlog.detail.id')}>
                <span className="num">{row.id}</span>
              </Field>
            </div>
          </dl>
          {row.error ? (
            <section className="rounded-md border border-destructive/40 bg-destructive/10 p-3">
              <h3 className="mb-2 text-sm font-medium text-destructive">{t('sendlog.col.error')}</h3>
              <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all font-mono text-xs text-destructive">
                {row.error}
              </pre>
            </section>
          ) : null}
          <p className="text-xs text-muted-foreground">{t('sendlog.detail.notice')}</p>
        </div>
      ) : null}
    </ResponsiveDialog>
  )
}
