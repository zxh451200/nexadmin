import { useState } from 'react'
import { Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTime } from './format'
import { useDisable, useEnable, useRegenerate, useSetup, useStatus, type SetupResult } from './api'
import { Alert, AlertDescription, AlertTitle } from '@/framework/ui/alert'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'

function useErrorText() {
  const { t } = useTranslation()
  return (e: unknown) => (e instanceof Error && e.message ? e.message : t('common.operateFailed'))
}

function CopyButton({ value, label }: { value: string; label: string }) {
  const { t } = useTranslation()
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      onClick={() => {
        void navigator.clipboard?.writeText(value).then(
          () => toast.success(t('plugin_security.copied')),
          () => toast.error(t('common.operateFailed')),
        )
      }}
    >
      <Copy aria-hidden="true" />
      {label}
    </Button>
  )
}

/** 个人中心：启用 / 关闭二次验证、恢复码。没有联网装二维码库，所以展示密钥与 otpauth 链接文本。 */
export function TwoFactorTab() {
  const { t, i18n } = useTranslation()
  const errText = useErrorText()
  const { data: st } = useStatus()
  const setup = useSetup()
  const enable = useEnable()
  const disable = useDisable()
  const regen = useRegenerate()
  const [setupData, setSetupData] = useState<SetupResult | null>(null)
  const [codes, setCodes] = useState<string[] | null>(null)
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)

  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast.success(t('common.operateSuccess'))
      setCode('')
    } catch (e) {
      toast.error(errText(e))
      setCode('')
    }
  }

  if (!st) return null

  return (
    <div className="flex flex-col gap-4">
      {st.required && !st.enabled ? (
        <Alert role="alert">
          <AlertTitle>{t('plugin_security.required.title')}</AlertTitle>
          <AlertDescription>
            {st.graceEndsAt
              ? t('plugin_security.required.grace', { time: formatTime(st.graceEndsAt, i18n.language) })
              : t('plugin_security.required.desc')}
          </AlertDescription>
        </Alert>
      ) : null}

      {codes ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('plugin_security.recovery.title')}</CardTitle>
            <CardDescription>{t('plugin_security.recovery.once')}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <ul className="grid grid-cols-1 gap-2 rounded-md border border-border bg-muted/40 p-3 font-mono text-sm sm:grid-cols-2">
              {codes.map((c) => (
                <li key={c} className="select-all">
                  {c}
                </li>
              ))}
            </ul>
            <div className="flex flex-wrap gap-2">
              <CopyButton value={codes.join('\n')} label={t('plugin_security.copy')} />
              <Button type="button" onClick={() => setCodes(null)}>
                {t('plugin_security.recovery.saved')}
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle>{t('plugin_security.twofa.title')}</CardTitle>
            <Badge variant={st.enabled ? 'success' : 'secondary'}>{st.enabled ? t('plugin_security.twofa.on') : t('plugin_security.twofa.off')}</Badge>
          </div>
          <CardDescription>{t('plugin_security.twofa.desc')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {!st.enabled && !setupData ? (
            <div>
              <Button
                disabled={setup.isPending}
                onClick={() => void run(async () => setSetupData(await setup.mutateAsync()))}
              >
                {t('plugin_security.twofa.start')}
              </Button>
            </div>
          ) : null}

          {!st.enabled && setupData ? (
            <div className="flex flex-col gap-4">
              <p className="text-sm text-muted-foreground">{t('plugin_security.twofa.noQr')}</p>
              <div className="flex flex-col gap-2">
                <Label htmlFor="sec-secret">{t('plugin_security.twofa.secret')}</Label>
                <div className="flex flex-wrap items-center gap-2">
                  <code id="sec-secret" className="select-all break-all rounded bg-muted px-2 py-1 font-mono text-sm">
                    {setupData.secret}
                  </code>
                  <CopyButton value={setupData.secret} label={t('plugin_security.copy')} />
                </div>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="sec-uri">{t('plugin_security.twofa.uri')}</Label>
                <div className="flex flex-wrap items-center gap-2">
                  <code id="sec-uri" className="select-all break-all rounded bg-muted px-2 py-1 font-mono text-xs">
                    {setupData.uri}
                  </code>
                  <CopyButton value={setupData.uri} label={t('plugin_security.copy')} />
                </div>
              </div>
              <div className="flex max-w-sm flex-col gap-2">
                <Label htmlFor="sec-enable-code">{t('plugin_security.twofa.code')}</Label>
                <Input
                  id="sec-enable-code"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={6}
                  placeholder={t('plugin_security.twofa.codePlaceholder')}
                />
                <div>
                  <Button
                    disabled={enable.isPending || !/^\d{6}$/.test(code)}
                    onClick={() =>
                      void run(async () => {
                        const r = await enable.mutateAsync(code)
                        setSetupData(null)
                        setCodes(r.recoveryCodes)
                      })
                    }
                  >
                    {t('plugin_security.twofa.confirmEnable')}
                  </Button>
                </div>
              </div>
            </div>
          ) : null}

          {st.enabled ? (
            <div className="flex max-w-sm flex-col gap-3">
              <p className="text-sm text-muted-foreground">{t('plugin_security.twofa.recoveryLeft', { count: st.recoveryLeft })}</p>
              <Label htmlFor="sec-manage-code">{recovery ? t('plugin_security.twofa.recoveryInput') : t('plugin_security.twofa.code')}</Label>
              <Input
                id="sec-manage-code"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                inputMode={recovery ? 'text' : 'numeric'}
                autoComplete="one-time-code"
                maxLength={recovery ? 64 : 6}
                placeholder={recovery ? t('plugin_security.twofa.recoveryPlaceholder') : t('plugin_security.twofa.codePlaceholder')}
              />
              <Button type="button" variant="link" className="h-auto self-start p-0" onClick={() => setRecovery((v) => !v)}>
                {recovery ? t('plugin_security.twofa.useCode') : t('plugin_security.twofa.useRecovery')}
              </Button>
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  disabled={regen.isPending || recovery || !/^\d{6}$/.test(code)}
                  onClick={() => void run(async () => setCodes((await regen.mutateAsync(code)).recoveryCodes))}
                >
                  {t('plugin_security.twofa.regen')}
                </Button>
                <Button
                  variant="destructive"
                  disabled={disable.isPending || !st.canDisable || code.trim().length < 6}
                  onClick={() => void run(() => disable.mutateAsync({ code: code.trim(), kind: recovery ? 'recovery' : undefined }))}
                >
                  {t('plugin_security.twofa.disable')}
                </Button>
              </div>
              {!st.canDisable ? <p className="text-sm text-muted-foreground">{t('plugin_security.twofa.cannotDisable')}</p> : null}
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  )
}
