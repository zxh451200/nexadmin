import { useTranslation } from 'react-i18next'
import { DictTag } from '@/framework/components/DictTag'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import type { OperLog } from './api'
import { fmtDateTime, pretty } from './format'

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 break-all text-sm text-foreground">{children}</dd>
    </div>
  )
}

function Block({ label, text, danger }: { label: string; text: string; danger?: boolean }) {
  const { t } = useTranslation()
  return (
    <section
      className={
        danger
          ? 'rounded-md border border-destructive/40 bg-destructive/10 p-3'
          : 'rounded-md border border-border bg-muted p-3'
      }
    >
      <h3 className={danger ? 'mb-2 text-sm font-medium text-destructive' : 'mb-2 text-sm font-medium text-foreground'}>
        {label}
      </h3>
      <pre
        className={
          'max-h-64 overflow-auto whitespace-pre-wrap break-all font-mono text-xs ' +
          (danger ? 'text-destructive' : 'text-foreground')
        }
      >
        {text || t('operLog.detail.empty')}
      </pre>
    </section>
  )
}

interface Props {
  log: OperLog | null
  onClose: () => void
}

export function OperLogDetailDialog({ log, onClose }: Props) {
  const { t, i18n } = useTranslation()
  return (
    <ResponsiveDialog
      open={!!log}
      onOpenChange={(o) => {
        if (!o) onClose()
      }}
      title={t('operLog.detail.title')}
      description={log ? `${log.module} · ${log.operator}` : undefined}
      size="lg"
      footer={
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {log ? (
        <div className="flex flex-col gap-4">
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={t('operLog.col.module')}>{log.module}</Field>
            <Field label={t('operLog.col.type')}>
              <DictTag dict="oper_type" value={log.type} />
            </Field>
            <Field label={t('operLog.col.operator')}>{log.operator}</Field>
            <Field label={t('operLog.col.status')}>
              <DictTag dict="login_status" value={log.status} />
            </Field>
            <Field label={t('operLog.col.ip')}>
              <span className="num">{log.ip}</span>
            </Field>
            <Field label={t('operLog.detail.location')}>{log.location || '-'}</Field>
            <Field label={t('operLog.detail.method')}>
              <span className="num">{log.method}</span>
            </Field>
            <Field label={t('operLog.col.cost')}>
              <span className="num">{log.costMs} ms</span>
            </Field>
            <Field label={t('operLog.col.time')}>{fmtDateTime(log.createdAt, i18n.language)}</Field>
            <div className="sm:col-span-2">
              <Field label={t('operLog.detail.url')}>
                <span className="num">{log.url}</span>
              </Field>
            </div>
          </dl>
          <Block label={t('operLog.detail.params')} text={pretty(log.params)} />
          <Block label={t('operLog.detail.response')} text={pretty(log.response)} />
          {log.errorStack ? <Block danger label={t('operLog.detail.error')} text={log.errorStack} /> : null}
        </div>
      ) : null}
    </ResponsiveDialog>
  )
}
