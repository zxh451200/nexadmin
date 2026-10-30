import { useState } from 'react'
import { Loader2, Plus, Send, Trash2 } from 'lucide-react'
import { useFieldArray, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { FormGrid, FormGridFull } from '@/framework/components/FormLayout'
import { Can } from '@/framework/auth/Can'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Info } from 'lucide-react'
import { ROBOT_TYPES, useTestSection, type AllSettings } from './api'
import { SelectField, SwitchField, TextField } from './fields'
import { SectionCard } from './SectionCard'

const notifySchema = z.object({
  mail: z.object({
    host: z.string().trim().max(255),
    port: z.number().int().min(1).max(65535),
    security: z.enum(['none', 'starttls', 'ssl']),
    username: z.string().trim().max(255),
    password: z.string(),
    fromName: z.string().trim().max(100),
    fromAddr: z.string().trim().max(255),
  }),
  robots: z
    .array(
      z.object({
        name: z.string().trim().regex(/^[A-Za-z0-9_\-\u4e00-\u9fa5]{1,32}$/),
        type: z.enum(ROBOT_TYPES),
        url: z.string().trim().min(1).max(1000),
        secretKey: z.string(),
        markdown: z.boolean(),
      }),
    )
    .max(20),
  accountMail: z.boolean(),
})

/** 「发送测试」：把当前表单值（含未保存的修改）连同渠道一起交给后端真实发送一条 */
function useSendTest(form: UseFormReturn<any>) { // eslint-disable-line @typescript-eslint/no-explicit-any
  const { t } = useTranslation()
  const test = useTestSection()
  const run = async (channel: string, to?: string) => {
    try {
      const r = await test.mutateAsync({ section: 'notify', body: { ...form.getValues(), channel, to } })
      if (r.ok) toast.success(t('settings.test.ok'))
      else toast.error(t('settings.test.failed', { reason: t(`settings.test.reason.${r.reason ?? 'unknown'}`) }))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }
  return { run, pending: test.isPending }
}

function MailTest({ form }: { form: UseFormReturn<any> }) { // eslint-disable-line @typescript-eslint/no-explicit-any
  const { t } = useTranslation()
  const [to, setTo] = useState('')
  const { run, pending } = useSendTest(form)
  return (
    <FormGridFull>
      <Label htmlFor="notify-mail-to">{t('settings.notify.testTo')}</Label>
      <div className="mt-2 flex flex-col gap-2 sm:flex-row">
        <Input id="notify-mail-to" type="email" value={to} onChange={(e) => setTo(e.target.value)} placeholder="you@example.com" />
        <Can perm="settings:update">
          <Button type="button" variant="outline" className="sm:w-auto" disabled={pending} onClick={() => void run('mail', to.trim())}>
            {pending ? <Loader2 className="me-2 h-4 w-4 animate-spin" aria-hidden="true" /> : <Send className="me-2 h-4 w-4" aria-hidden="true" />}
            {t('settings.notify.sendTest')}
          </Button>
        </Can>
      </div>
    </FormGridFull>
  )
}

function Robots({ form }: { form: UseFormReturn<any> }) { // eslint-disable-line @typescript-eslint/no-explicit-any
  const { t } = useTranslation()
  const { fields, append, remove } = useFieldArray({ control: form.control, name: 'robots' })
  const { run, pending } = useSendTest(form)
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">{t('settings.notify.robots')}</h3>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => append({ name: '', type: 'dingtalk', url: '', secretKey: '', markdown: false })}
        >
          <Plus className="me-1 h-4 w-4" aria-hidden="true" />
          {t('settings.notify.addRobot')}
        </Button>
      </div>
      {fields.length === 0 ? <p className="text-sm text-muted-foreground">{t('settings.notify.noRobots')}</p> : null}
      {fields.map((f, i) => (
        <div key={f.id} className="flex flex-col gap-3 rounded-lg bg-muted/40 p-3">
          <FormGrid>
            <TextField control={form.control} name={`robots.${i}.name`} label={t('settings.notify.robotName')} />
            <SelectField
              control={form.control}
              name={`robots.${i}.type`}
              label={t('settings.notify.robotType')}
              options={ROBOT_TYPES.map((k) => ({ value: k, label: t(`settings.notify.type.${k}`) }))}
            />
            <TextField control={form.control} name={`robots.${i}.url`} label="Webhook URL" type="url" inputMode="url" placeholder="https://" />
            <TextField
              control={form.control}
              name={`robots.${i}.secretKey`}
              label={t('settings.notify.robotSecret')}
              type="password"
              description={t('settings.notify.robotSecretHint')}
            />
          </FormGrid>
          <SwitchField control={form.control} name={`robots.${i}.markdown`} label={t('settings.notify.markdown')} bare />
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button type="button" variant="ghost" onClick={() => remove(i)}>
              <Trash2 className="me-2 h-4 w-4" aria-hidden="true" />
              {t('settings.notify.removeRobot')}
            </Button>
            <Can perm="settings:update">
              <Button
                type="button"
                variant="outline"
                disabled={pending}
                onClick={() => void run(`robot:${form.getValues(`robots.${i}.name`) as string}`)}
              >
                <Send className="me-2 h-4 w-4" aria-hidden="true" />
                {t('settings.notify.sendTest')}
              </Button>
            </Can>
          </div>
        </div>
      ))}
    </div>
  )
}

export function NotifySection({ value }: { value: AllSettings['notify'] }) {
  const { t } = useTranslation()
  return (
    <SectionCard section="notify" value={value} schema={notifySchema}>
      {(form) => (
        <>
          <Alert variant="info">
            <Info aria-hidden="true" />
            <AlertDescription>{t('settings.notify.hint')}</AlertDescription>
          </Alert>
          <h3 className="text-sm font-medium">{t('settings.notify.mail')}</h3>
          <FormGrid>
            <TextField control={form.control} name="mail.host" label="SMTP Host" placeholder="smtp.example.com" />
            <TextField control={form.control} name="mail.port" label={t('settings.notify.port')} type="number" inputMode="numeric" />
            <SelectField
              control={form.control}
              name="mail.security"
              label={t('settings.notify.security')}
              options={[
                { value: 'none', label: t('settings.notify.securityNone') },
                { value: 'starttls', label: 'STARTTLS' },
                { value: 'ssl', label: 'SSL/TLS (465)' },
              ]}
            />
            <TextField control={form.control} name="mail.username" label={t('settings.notify.username')} />
            <TextField control={form.control} name="mail.password" label={t('settings.notify.password')} type="password" />
            <TextField control={form.control} name="mail.fromName" label={t('settings.notify.fromName')} />
            <TextField control={form.control} name="mail.fromAddr" label={t('settings.notify.fromAddr')} placeholder="noreply@example.com" />
            <MailTest form={form} />
          </FormGrid>
          <Robots form={form} />
          <SwitchField
            control={form.control}
            name="accountMail"
            label={t('settings.notify.accountMail')}
            description={t('settings.notify.accountMailHint')}
          />
        </>
      )}
    </SectionCard>
  )
}
