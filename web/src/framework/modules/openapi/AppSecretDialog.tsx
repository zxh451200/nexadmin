import { useTranslation } from 'react-i18next'
import { buildSignExample, type OpenApp } from './api'
import { CodeBlock, RevealDialog } from './RevealDialog'

export interface AppSecretTarget {
  app: Pick<OpenApp, 'name' | 'appKey'>
  /** 仅创建 / 重置后有值；纯查看接入说明时为空 */
  appSecret?: string
}

interface Props {
  target: AppSecretTarget | null
  onOpenChange: (open: boolean) => void
}

/** 应用密钥一次性展示 + 签名调用说明 */
export function AppSecretDialog({ target, onOpenChange }: Props) {
  const { t } = useTranslation()
  const headers = ['X-App-Key', 'X-Timestamp', 'X-Nonce', 'X-Signature']

  return (
    <RevealDialog
      open={!!target}
      onOpenChange={onOpenChange}
      title={target?.appSecret ? t('openapi.apps.secret.title') : t('openapi.apps.guide.title')}
      description={target?.app.name}
      warning={target?.appSecret ? t('openapi.apps.secret.onceHint') : undefined}
      closeText={target?.appSecret ? t('openapi.apps.secret.saved') : t('common.close')}
      fields={[
        { label: t('openapi.apps.fields.appKey'), value: target?.app.appKey ?? '' },
        ...(target?.appSecret ? [{ label: t('openapi.apps.fields.appSecret'), value: target.appSecret }] : []),
      ]}
    >
      <section className="flex flex-col gap-3">
        <div className="border-b border-border pb-2">
          <h3 className="text-sm font-semibold text-foreground">{t('openapi.apps.guide.section')}</h3>
          <p className="mt-1 text-xs text-muted-foreground">{t('openapi.apps.guide.hint')}</p>
        </div>
        <ul className="flex flex-col gap-1.5 text-sm text-foreground">
          {headers.map((h) => (
            <li key={h} className="flex flex-col gap-0.5 sm:flex-row sm:items-baseline sm:gap-2">
              <code className="shrink-0 font-mono text-xs">{h}</code>
              <span className="text-xs text-muted-foreground">{t(`openapi.apps.guide.header.${h}`)}</span>
            </li>
          ))}
        </ul>
        <p className="text-xs text-muted-foreground">{t('openapi.apps.guide.stringToSign')}</p>
        <code className="block break-all rounded-md border border-border bg-muted/40 p-3 font-mono text-xs text-foreground">
          METHOD\npath\nquery\nsha256hex(body)\ntimestamp\nnonce
        </code>
        <p className="text-xs text-muted-foreground">{t('openapi.apps.guide.algorithm')}</p>
        <p className="text-xs text-muted-foreground">{t('openapi.apps.guide.window')}</p>
        <h4 className="text-sm font-medium text-foreground">{t('openapi.apps.guide.example')}</h4>
        <CodeBlock code={buildSignExample(window.location.origin, target?.app.appKey ?? '')} label={t('openapi.apps.guide.copyExample')} />
      </section>
    </RevealDialog>
  )
}
