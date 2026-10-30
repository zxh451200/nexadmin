import { useEffect, useMemo } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { JsonInput } from '@/framework/components/JsonInput'
import { isJsonObjectText } from '@/framework/components/json'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchField } from '@/framework/components/SwitchField'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useHandlers, useSaveJob, type CronJob, type CronJobForm } from './api'
import { CRON_PRESETS, CUSTOM_PRESET, parseCron, presetKeyOf } from './cron'
import { formatTime } from './format'

function buildSchema(t: (k: string) => string) {
  return z.object({
    name: z.string().trim().min(1, t('plugin_cron.errors.nameRequired')).max(100, t('plugin_cron.errors.nameTooLong')),
    cronExpr: z.string().superRefine((v, ctx) => {
      const r = parseCron(v)
      if (!r.ok) ctx.addIssue({ code: 'custom', message: t(`plugin_cron.errors.${r.code}`) })
    }),
    handler: z.string().min(1, t('common.required')),
    params: z.string().refine(isJsonObjectText, t('plugin_cron.errors.paramsJson')),
    enabled: z.boolean(),
    retryCount: z.number().int().min(0, t('plugin_cron.errors.retryRange')).max(10, t('plugin_cron.errors.retryRange')),
  })
}

type FormValues = z.infer<ReturnType<typeof buildSchema>>

const emptyValues: FormValues = { name: '', cronExpr: '*/5 * * * *', handler: '', params: '', enabled: true, retryCount: 0 }

const toValues = (j?: CronJob | null): FormValues =>
  j ? { name: j.name, cronExpr: j.cronExpr, handler: j.handler, params: j.params, enabled: j.enabled, retryCount: j.retryCount } : emptyValues

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: CronJob | null
}

export function JobFormDialog({ open, onOpenChange, editing }: Props) {
  const { t, i18n } = useTranslation()
  const save = useSaveJob()
  const { data: handlers = [] } = useHandlers()
  const schema = useMemo(() => buildSchema(t), [t])
  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: emptyValues })
  const expr = useWatch({ control: form.control, name: 'cronExpr' })
  const handler = useWatch({ control: form.control, name: 'handler' })

  useEffect(() => {
    if (open) form.reset(toValues(editing))
  }, [open, editing, form])

  const preview = useMemo(() => {
    const r = parseCron(expr ?? '')
    return r.ok ? r.schedule.nextRuns(new Date(), 5) : null
  }, [expr])

  const onSubmit = form.handleSubmit(async (v) => {
    const data: CronJobForm = { ...v, name: v.name.trim(), params: v.params.trim() }
    try {
      await save.mutateAsync({ id: editing?.id, data })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  })

  const handlerLabel = (name: string, description: string) => t(`plugin_cron.handlers.${name}`, { defaultValue: description || name })

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={editing ? t('plugin_cron.editTitle') : t('plugin_cron.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="cron-job-form" disabled={save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id="cron-job-form" onSubmit={onSubmit} showRequiredHint>
          <FormGrid>
            <FormGridFull>
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('plugin_cron.fields.name')}</FormLabel>
                    <FormControl>
                      <Input {...field} maxLength={100} placeholder={t('common.pleaseInput')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>

            <FormGridFull>
              <FormField
                control={form.control}
                name="cronExpr"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('plugin_cron.fields.cronExpr')}</FormLabel>
                    <div className="flex flex-col gap-2 sm:flex-row">
                      <FormControl>
                        <Input {...field} className="font-mono sm:flex-1" spellCheck={false} autoComplete="off" placeholder="*/5 * * * *" />
                      </FormControl>
                      <Select
                        value={presetKeyOf(field.value)}
                        onValueChange={(k) => {
                          const p = CRON_PRESETS.find((x) => x.key === k)
                          if (p) form.setValue('cronExpr', p.expr, { shouldDirty: true, shouldValidate: true })
                        }}
                      >
                        <SelectTrigger className="sm:w-48" aria-label={t('plugin_cron.presets.label')}>
                          <SelectValue placeholder={t('plugin_cron.presets.label')} />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value={CUSTOM_PRESET} disabled>
                            {t('plugin_cron.presets.custom')}
                          </SelectItem>
                          {CRON_PRESETS.map((p) => (
                            <SelectItem key={p.key} value={p.key}>
                              {t(`plugin_cron.presets.${p.key}`)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    <FormDescription>{t('plugin_cron.hints.cronExpr')}</FormDescription>
                    <FormMessage />
                    <div className="rounded-md border border-border bg-muted/40 p-3 text-sm" aria-live="polite">
                      <p className="mb-1 text-xs font-medium text-muted-foreground">{t('plugin_cron.preview.title')}</p>
                      {preview && preview.length > 0 ? (
                        <ul className="num grid gap-0.5 sm:grid-cols-2">
                          {preview.map((d) => (
                            <li key={d.getTime()} className="text-foreground">
                              {formatTime(d.toISOString(), i18n.language)}
                            </li>
                          ))}
                        </ul>
                      ) : (
                        <p className="text-muted-foreground">{preview ? t('plugin_cron.preview.none') : '-'}</p>
                      )}
                    </div>
                  </FormItem>
                )}
              />
            </FormGridFull>

            <FormField
              control={form.control}
              name="handler"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('plugin_cron.fields.handler')}</FormLabel>
                  <Select
                    value={field.value}
                    onValueChange={(v) => {
                      field.onChange(v)
                      if (!form.getValues('params').trim()) {
                        form.setValue('params', t(`plugin_cron.paramsExample.${v}`, { defaultValue: '' }), { shouldDirty: true })
                      }
                    }}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder={t('common.pleaseSelect')} />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {handlers.map((h) => (
                        <SelectItem key={h.name} value={h.name}>
                          {handlerLabel(h.name, h.description)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="retryCount"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('plugin_cron.fields.retryCount')}</FormLabel>
                  <FormControl>
                    <Input
                      type="number"
                      inputMode="numeric"
                      min={0}
                      max={10}
                      value={field.value}
                      onChange={(e) => field.onChange(e.target.value === '' ? 0 : Number(e.target.value))}
                    />
                  </FormControl>
                  <FormDescription>{t('plugin_cron.hints.retryCount')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormGridFull>
              <FormField
                control={form.control}
                name="params"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('plugin_cron.fields.params')}</FormLabel>
                    <FormControl>
                      <JsonInput {...field} optional rows={3} placeholder="{}" />
                    </FormControl>
                    <FormDescription>
                      {handler ? t(`plugin_cron.hints.${handler}`, { defaultValue: t('plugin_cron.hints.params') }) : t('plugin_cron.hints.params')}
                    </FormDescription>
                  </FormItem>
                )}
              />
            </FormGridFull>

            <FormGridFull>
              <SwitchField control={form.control} name="enabled" label={t('plugin_cron.fields.enabled')} />
            </FormGridFull>
          </FormGrid>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
