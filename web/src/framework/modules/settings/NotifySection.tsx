import { useEffect, useMemo, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2, Pencil, Plus, Send, Trash2 } from 'lucide-react'
import { useFieldArray, useForm, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Form } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Info } from 'lucide-react'
import { ROBOT_TYPES, useTestSection, type AllSettings, type RobotSettings } from './api'
import { SelectField, SwitchField, TextField, type AnyControl } from './fields'
import { SectionCard } from './SectionCard'

const robotSchema = z.object({
  name: z.string().trim().min(1).max(32).regex(/^[A-Za-z0-9_\-\u4e00-\u9fa5]+$/),
  type: z.enum(ROBOT_TYPES),
  url: z.string().trim().min(1).max(1000).refine((value) => {
    try {
      return new URL(value).protocol === 'https:'
    } catch {
      return false
    }
  }),
  secretKey: z.string(),
  secretKeyRef: z.string().optional(),
  markdown: z.boolean(),
})

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
  robots: z.array(robotSchema).max(20),
  accountMail: z.boolean(),
})

type RobotFormValues = z.infer<typeof robotSchema>

const emptyRobot: RobotFormValues = { name: '', type: 'dingtalk', url: '', secretKey: '', markdown: false }

/** 「发送测试」：把当前表单值（含未保存的修改）连同渠道交给后端真实发送一条 */
function useSendTest(form: UseFormReturn<any>) { // eslint-disable-line @typescript-eslint/no-explicit-any
  const { t } = useTranslation()
  const test = useTestSection()
  const run = async (channel: string, to?: string, robots?: RobotSettings[]) => {
    try {
      const r = await test.mutateAsync({ section: 'notify', body: { ...form.getValues(), ...(robots ? { robots } : {}), channel, to } })
      if (r.ok) toast.success(t('settings.test.ok'))
      else toast.error(t('settings.test.failed', { reason: t(`settings.test.reason.${r.reason ?? 'unknown'}`) }))
    } catch {
      // 失败原因由 http 拦截器统一弹出；业务失败（r.ok=false）在上面按 reason 展示
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
            {pending ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Send aria-hidden="true" />}
            {t('settings.notify.sendTest')}
          </Button>
        </Can>
      </div>
    </FormGridFull>
  )
}

interface RobotDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  robot: RobotFormValues | null
  robots: RobotFormValues[]
  editingIndex: number | null
  form: UseFormReturn<any> // eslint-disable-line @typescript-eslint/no-explicit-any
  onSubmit: (values: RobotFormValues) => void
}

function RobotFormDialog({ open, onOpenChange, robot, robots, editingIndex, form: notifyForm, onSubmit }: RobotDialogProps) {
  const { t } = useTranslation()
  const { run, pending } = useSendTest(notifyForm)
  const schema = useMemo(
    () =>
      robotSchema.refine(
        (value) => robots.findIndex((item) => item.name === value.name) === -1 || robots.findIndex((item) => item.name === value.name) === editingIndex,
        t('settings.notify.robotNameDuplicate'),
      ),
    [robots, editingIndex, t],
  )
  const form = useForm<RobotFormValues>({ resolver: zodResolver(schema), defaultValues: robot ?? emptyRobot })
  const fieldControl = form.control as unknown as AnyControl
  const type = form.watch('type')

  useEffect(() => {
    if (open) form.reset(robot ?? emptyRobot)
  }, [open, robot, form])

  const submit = form.handleSubmit(onSubmit)
  const sendTest = async () => {
    if (!(await form.trigger())) return
    const values = form.getValues()
    const nextRobots = robots.map((item, index) => (index === editingIndex ? values : item))
    if (editingIndex === null) nextRobots.push(values)
    await run(`robot:${values.name}`, undefined, nextRobots)
  }

  return (
    <ResponsiveDialog
      side="right"
      open={open}
      onOpenChange={onOpenChange}
      title={editingIndex === null ? t('settings.notify.addRobot') : t('settings.notify.editRobot')}
      description={t('settings.notify.robotDialogHint')}
      footer={
        <>
          <Button type="button" variant="outline" disabled={pending} onClick={() => void sendTest()}>
            {pending ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Send aria-hidden="true" />}
            {t('settings.notify.sendTest')}
          </Button>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="settings-robot-form">
            {editingIndex === null ? t('settings.notify.addToRobotList') : t('settings.notify.applyRobot')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id="settings-robot-form" onSubmit={(event) => { event.stopPropagation(); void submit(event) }}>
          <FormGrid>
            <TextField control={fieldControl} name="name" label={t('settings.notify.robotName')} description={t('settings.notify.robotNameHint')} />
            <SelectField
              control={fieldControl}
              name="type"
              label={t('settings.notify.robotType')}
              options={ROBOT_TYPES.map((key) => ({ value: key, label: t(`settings.notify.type.${key}`) }))}
            />
            <TextField control={fieldControl} name="url" label="Webhook URL" type="url" inputMode="url" placeholder="https://" />
            {type !== 'wecom' ? (
              <TextField
                control={fieldControl}
                name="secretKey"
                label={t('settings.notify.robotSecret')}
                type="password"
                description={t('settings.notify.robotSecretHint')}
              />
            ) : null}
            <FormGridFull>
              <SwitchField control={form.control} name="markdown" label={t('settings.notify.markdown')} bare />
            </FormGridFull>
          </FormGrid>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}

function Robots({ form }: { form: UseFormReturn<any> }) { // eslint-disable-line @typescript-eslint/no-explicit-any
  const { t } = useTranslation()
  const { isPlatform } = usePermission()
  const confirm = useConfirm()
  const { fields, append, remove, update } = useFieldArray({ control: form.control, name: 'robots' })
  const robots = (form.watch('robots') ?? []) as RobotFormValues[]
  const [open, setOpen] = useState(false)
  const [editingIndex, setEditingIndex] = useState<number | null>(null)

  const openEditor = (index: number | null) => {
    setEditingIndex(index)
    setOpen(true)
  }

  const removeRobot = async (index: number) => {
    const robot = robots[index]
    if (!robot) return
    if (!(await confirm({
      destructive: true,
      title: t('settings.notify.removeRobot'),
      description: t('settings.notify.removeRobotConfirm', { name: robot.name }),
    }))) return
    remove(index)
  }

  return (
    <section className="flex flex-col gap-3" aria-labelledby="notify-robots-title">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <h3 id="notify-robots-title" className="text-sm font-medium">{t('settings.notify.robots')}</h3>
          <Badge variant="secondary" className="num">{robots.length}</Badge>
        </div>
        <Can perm="settings:update">
          <Button type="button" variant="outline" size="sm" disabled={!isPlatform || robots.length >= 20} onClick={() => openEditor(null)}>
            <Plus className="me-1 h-4 w-4" aria-hidden="true" />
            {t('settings.notify.addRobot')}
          </Button>
        </Can>
      </div>
      <p className="text-xs text-muted-foreground">{t('settings.notify.robotsHint')}</p>
      {robots.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border px-4 py-6 text-center text-sm text-muted-foreground">
          {t('settings.notify.noRobots')}
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-border">
          {robots.map((robot, index) => (
            <div key={fields[index]?.id ?? index} className={`flex items-center gap-3 px-3 py-3 ${index < robots.length - 1 ? 'border-b border-border' : ''}`}>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-foreground" title={robot.name}>{robot.name || t('settings.notify.unnamedRobot')}</p>
                <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <Badge variant="secondary">{t(`settings.notify.type.${robot.type}`)}</Badge>
                  <span>{robot.markdown ? t('settings.notify.markdownOn') : t('settings.notify.markdownOff')}</span>
                </div>
              </div>
              <Can perm="settings:update">
                <div className="flex shrink-0 items-center gap-1">
                  <Button type="button" variant="ghost" size="icon" disabled={!isPlatform} aria-label={t('settings.notify.editRobotNamed', { name: robot.name })} onClick={() => openEditor(index)}>
                    <Pencil className="size-4" aria-hidden="true" />
                  </Button>
                  <Button type="button" variant="ghost" size="icon" disabled={!isPlatform} aria-label={t('settings.notify.removeRobotNamed', { name: robot.name })} className="text-muted-foreground hover:bg-destructive/10 hover:text-destructive" onClick={() => void removeRobot(index)}>
                    <Trash2 className="size-4" aria-hidden="true" />
                  </Button>
                </div>
              </Can>
            </div>
          ))}
        </div>
      )}
      <RobotFormDialog
        open={open}
        onOpenChange={setOpen}
        robot={editingIndex === null ? null : robots[editingIndex] ?? null}
        robots={robots}
        editingIndex={editingIndex}
        form={form}
        onSubmit={(values) => {
          if (editingIndex === null) append(values)
          else update(editingIndex, values)
          setOpen(false)
        }}
      />
    </section>
  )
}

export function NotifySection({ value }: { value: AllSettings['notify'] }) {
  const { t } = useTranslation()
  const initialValue = useMemo(
    () => ({ ...value, robots: value.robots.map((robot) => ({ ...robot, secretKeyRef: robot.secretKeyRef ?? robot.name })) }),
    [value],
  )
  return (
    <SectionCard section="notify" value={initialValue} schema={notifySchema}>
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
