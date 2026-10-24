import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { RadioGroup, RadioGroupItem } from '@/framework/ui/radio-group'
import { usePolicy, useSavePolicy, type PolicyMode } from './api'

const MODES: PolicyMode[] = ['optional', 'all', 'roles']

export function PolicyTab() {
  const { t } = useTranslation()
  const { data } = usePolicy()
  const save = useSavePolicy()
  const [mode, setMode] = useState<PolicyMode>('optional')
  const [roles, setRoles] = useState('')
  const [days, setDays] = useState(7)

  useEffect(() => {
    if (data) {
      setMode(data.mode)
      setRoles(data.roles.join(', '))
      setDays(data.graceDays)
    }
  }, [data])

  const onSave = async () => {
    try {
      await save.mutateAsync({
        mode,
        roles: mode === 'roles' ? roles.split(',').map((r) => r.trim()).filter(Boolean) : [],
        graceDays: days,
      })
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('plugin_security.policy.title')}</CardTitle>
        <CardDescription>{t('plugin_security.policy.desc')}</CardDescription>
      </CardHeader>
      <CardContent className="flex max-w-xl flex-col gap-4">
        <RadioGroup value={mode} onValueChange={(v) => setMode(v as PolicyMode)} className="gap-3" aria-label={t('plugin_security.policy.title')}>
          {MODES.map((m) => (
            <div key={m} className="flex items-start gap-3">
              <RadioGroupItem value={m} id={`sec-mode-${m}`} className="mt-1" />
              <Label htmlFor={`sec-mode-${m}`} className="flex flex-col gap-0.5 font-normal">
                <span className="font-medium text-foreground">{t(`plugin_security.policy.mode.${m}`)}</span>
                <span className="text-sm text-muted-foreground">{t(`plugin_security.policy.modeDesc.${m}`)}</span>
              </Label>
            </div>
          ))}
        </RadioGroup>
        {mode === 'roles' ? (
          <div className="flex flex-col gap-2">
            <Label htmlFor="sec-roles">{t('plugin_security.policy.roles')}</Label>
            <Input id="sec-roles" value={roles} onChange={(e) => setRoles(e.target.value)} placeholder="admin, ops" spellCheck={false} />
            <p className="text-sm text-muted-foreground">{t('plugin_security.policy.rolesHint')}</p>
          </div>
        ) : null}
        {mode !== 'optional' ? (
          <div className="flex flex-col gap-2">
            <Label htmlFor="sec-grace">{t('plugin_security.policy.graceDays')}</Label>
            <Input
              id="sec-grace"
              type="number"
              inputMode="numeric"
              min={1}
              max={90}
              className="w-32"
              value={days}
              onChange={(e) => setDays(Number(e.target.value))}
            />
            <Alert>
              <AlertDescription>{t('plugin_security.policy.graceHint')}</AlertDescription>
            </Alert>
          </div>
        ) : null}
        <Can perm="security:policy">
          <div>
            <Button disabled={save.isPending} onClick={() => void onSave()}>
              {t('common.save')}
            </Button>
          </div>
        </Can>
      </CardContent>
    </Card>
  )
}
