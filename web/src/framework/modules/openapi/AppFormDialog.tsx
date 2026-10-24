import { useEffect, useMemo } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import { isValidIpOrCidr, splitLines, useSaveOpenApp, type OpenApp, type OpenAppCreated, type OpenAppForm } from './api'

function buildSchema(t: (k: string, o?: Record<string, unknown>) => string) {
  return z.object({
    name: z.string().trim().min(1, t('common.required')).max(50, t('openapi.apps.form.nameTooLong')),
    description: z.string().trim().max(200, t('openapi.apps.form.descTooLong')),
    roleIds: z.array(z.string()),
    enabled: z.boolean(),
    ipAllow: z.string().superRefine((v, ctx) => {
      const lines = splitLines(v)
      const bad = lines.find((l) => !isValidIpOrCidr(l))
      if (bad) ctx.addIssue({ code: 'custom', message: t('openapi.apps.form.ipInvalid', { ip: bad }) })
      else if (new Set(lines).size !== lines.length) ctx.addIssue({ code: 'custom', message: t('openapi.apps.form.ipDuplicated') })
    }),
  })
}

type FormValues = z.infer<ReturnType<typeof buildSchema>>

const emptyValues: FormValues = { name: '', description: '', roleIds: [], enabled: true, ipAllow: '' }

const toValues = (a?: OpenApp | null): FormValues =>
  a
    ? { name: a.name, description: a.description ?? '', roleIds: a.roleIds ?? [], enabled: a.enabled, ipAllow: (a.ipAllow ?? []).join('\n') }
    : emptyValues

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: OpenApp | null
  /** 创建成功（含只显示一次的 appSecret） */
  onCreated: (app: OpenAppCreated) => void
}

export function AppFormDialog({ open, onOpenChange, editing, onCreated }: Props) {
  const { t } = useTranslation()
  const save = useSaveOpenApp()
  const schema = useMemo(() => buildSchema(t), [t])
  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: emptyValues })

  useEffect(() => {
    if (open) form.reset(toValues(editing))
  }, [open, editing, form])

  const onSubmit = form.handleSubmit(async (v) => {
    const data: OpenAppForm = { ...v, ipAllow: splitLines(v.ipAllow) }
    try {
      const res = await save.mutateAsync({ id: editing?.id, data })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
      if (!editing && typeof (res as Partial<OpenAppCreated>).appSecret === 'string') onCreated(res as OpenAppCreated)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  })

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={editing ? t('openapi.apps.form.editTitle') : t('openapi.apps.form.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="openapi-app-form" disabled={save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id="openapi-app-form" onSubmit={onSubmit} showRequiredHint>
          <FormGrid>
            <FormGridFull>
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('openapi.apps.fields.name')}</FormLabel>
                    <FormControl>
                      <Input {...field} maxLength={50} placeholder={t('common.pleaseInput')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="description"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('openapi.apps.fields.description')}</FormLabel>
                    <FormControl>
                      <Textarea {...field} rows={2} maxLength={200} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="roleIds"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('openapi.apps.fields.roles')}</FormLabel>
                    <FormControl>
                      <RemoteSelect
                        url="/roles/options"
                        multiple
                        value={field.value}
                        onChange={field.onChange}
                        placeholder={t('openapi.apps.form.rolesPlaceholder')}
                      />
                    </FormControl>
                    <FormDescription>{t('openapi.apps.form.rolesHint')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="ipAllow"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('openapi.apps.fields.ipAllow')}</FormLabel>
                    <FormControl>
                      <Textarea {...field} rows={4} spellCheck={false} placeholder={'203.0.113.10\n10.0.0.0/8'} />
                    </FormControl>
                    <FormDescription>{t('openapi.apps.form.ipHint')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="enabled"
                render={({ field }) => (
                  <FormItem className="flex min-h-11 items-center justify-between gap-3 rounded-md border border-border p-3">
                    <FormLabel className="m-0">{t('openapi.apps.fields.enabled')}</FormLabel>
                    <FormControl>
                      <Switch checked={field.value} onCheckedChange={field.onChange} />
                    </FormControl>
                  </FormItem>
                )}
              />
            </FormGridFull>
          </FormGrid>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
