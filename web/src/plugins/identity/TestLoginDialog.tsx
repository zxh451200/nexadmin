import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { useTestLogin, type IdentitySource, type TestResult } from './api'

interface Props {
  source: IdentitySource | null
  onClose: () => void
}

/** 「测试用户登录」：走一遍真实的 LDAP 搜索 + 用户 bind，只显示映射结果，不建号、不签发令牌。 */
export function TestLoginDialog({ source, onClose }: Props) {
  const { t } = useTranslation()
  const test = useTestLogin()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [result, setResult] = useState<TestResult | null>(null)

  useEffect(() => {
    if (source) {
      setUsername('')
      setPassword('')
      setResult(null)
    }
  }, [source])

  const run = async () => {
    if (!source) return
    try {
      setResult(await test.mutateAsync({ id: source.id, username: username.trim(), password }))
    } catch {
      setResult({ ok: false, message: t('common.operateFailed') })
    }
    setPassword('') // 用完即清，不在界面里留着
  }

  const rows: [string, string | undefined][] = [
    ['dn', result?.dn],
    ['username', result?.username],
    ['nickname', result?.nickname],
    ['email', result?.email],
    ['phone', result?.phone],
  ]

  return (
    <ResponsiveDialog
      open={!!source}
      onOpenChange={(o) => !o && onClose()}
      size="md"
      title={t('plugin_identity.testLoginTitle', { name: source?.name ?? '' })}
      footer={
        <>
          <Button type="button" variant="outline" onClick={onClose}>
            {t('common.close')}
          </Button>
          <Button type="submit" form="identity-test-login" disabled={test.isPending || !username.trim() || !password}>
            {test.isPending ? t('common.loading') : t('plugin_identity.testLogin')}
          </Button>
        </>
      }
    >
      <form
        id="identity-test-login"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          void run()
        }}
      >
        <p className="text-sm text-muted-foreground">{t('plugin_identity.testLoginHint')}</p>
        <div className="flex flex-col gap-2">
          <Label htmlFor="identity-test-user">{t('plugin_identity.fields.testUsername')}</Label>
          <Input id="identity-test-user" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" spellCheck={false} maxLength={256} />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="identity-test-pass">{t('plugin_identity.fields.testPassword')}</Label>
          <Input id="identity-test-pass" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" maxLength={512} />
        </div>
        {result ? (
          <div className="rounded-md border border-border bg-muted/40 p-3 text-sm" role="status" aria-live="polite">
            <div className="mb-2 flex items-center gap-2">
              <Badge variant={result.ok ? 'success' : 'destructive'}>{result.ok ? t('plugin_identity.result.ok') : t('plugin_identity.result.fail')}</Badge>
              {result.stage ? <span className="text-xs text-muted-foreground">{t(`plugin_identity.stage.${result.stage}`)}</span> : null}
            </div>
            {result.ok ? (
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
                {rows
                  .filter(([, v]) => !!v)
                  .map(([k, v]) => (
                    <div key={k} className="contents">
                      <dt className="text-muted-foreground">{t(`plugin_identity.mapped.${k}`)}</dt>
                      <dd className="break-all font-mono text-xs text-foreground">{v}</dd>
                    </div>
                  ))}
              </dl>
            ) : (
              <p className="break-words text-foreground">{result.message}</p>
            )}
          </div>
        ) : null}
      </form>
    </ResponsiveDialog>
  )
}
