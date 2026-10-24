import { useEffect, useMemo } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import {
  eventGroup,
  isValidWebhookUrl,
  useOpenEvents,
  useSaveWebhook,
  type OpenEvent,
  type Webhook,
  type WebhookCreated,
  type WebhookForm,
} from './api'

function buildSchema(t: (k: string, o?: Record<string, unknown>) => string) {
  return z.object({
    name: z.string().trim().min(1, t('common.required')).max(50, t('openapi.webhooks.form.nameTooLong')),
    url: z.string().trim().min(1, t('common.required')).refine(isValidWebhookUrl, t('openapi.webhooks.form.urlInvalid')),
    events: z.array(z.string()).min(1, t('openapi.webhooks.form.eventsRequired')),
    enabled: z.boolean(),
  })
}

type FormValues = z.infer<ReturnType<typeof buildSchema>>

const emptyValues: FormValues = { name: '', url: '', events: [], enabled: true }

const toValues = (w?: Webhook | null): FormValues =>
  w ? { name: w.name, url: w.url, events: w.events, enabled: w.enabled } : emptyValues

/** 按前缀（user./dept./role./notice.）分组的事件多选 */
function EventPicker({ events, value, onChange }: { events: OpenEvent[]; value: string[]; onChange: (v: string[]) => void }) {
  const { t } = useTranslation()
  const groups = useMemo(() => {
    const map = new Map<string, OpenEvent[]>()
    for (const e of events) map.set(eventGroup(e.name), [...(map.get(eventGroup(e.name)) ?? []), e])
    return [...map.entries()]
  }, [events])

  const toggle = (name: string, on: boolean) => onChange(on ? [...value, name] : value.filter((v) => v !== name))
  const toggleGroup = (list: OpenEvent[], on: boolean) => {
    const names = list.map((e) => e.name)
    onChange(on ? [...new Set([...value, ...names])] : value.filter((v) => !names.includes(v)))
  }

  return (
    <div className="flex max-h-72 flex-col gap-4 overflow-y-auto rounded-md border border-border p-3">
      {groups.map(([group, list]) => {
        const picked = list.filter((e) => value.includes(e.name)).length
        const all = picked === list.length
        return (
          <fieldset key={group} className="flex flex-col gap-1">
            <legend className="sr-only">{t(`openapi.eventGroup.${group}`, { defaultValue: group })}</legend>
            <label className="flex min-h-11 items-center gap-2 text-sm font-medium text-foreground md:min-h-8">
              <Checkbox
                checked={all ? true : picked > 0 ? 'indeterminate' : false}
                onCheckedChange={(c) => toggleGroup(list, c === true)}
                aria-label={t(`openapi.eventGroup.${group}`, { defaultValue: group })}
              />
              {t(`openapi.eventGroup.${group}`, { defaultValue: group })}
              <span className="num text-xs font-normal text-muted-foreground">
                {picked}/{list.length}
              </span>
            </label>
            <div className="flex flex-col ps-6">
              {list.map((e) => (
                <label key={e.name} className="flex min-h-11 items-start gap-2 py-1.5 text-sm md:min-h-8">
                  <Checkbox className="mt-0.5" checked={value.includes(e.name)} onCheckedChange={(c) => toggle(e.name, c === true)} />
                  <span className="flex min-w-0 flex-col">
                    <code className="font-mono text-xs text-foreground">{e.name}</code>
                    <span className="text-xs text-muted-foreground">{e.description}</span>
                  </span>
                </label>
              ))}
            </div>
          </fieldset>
        )
      })}
    </div>
  )
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: Webhook | null
  /** 创建成功（含只显示一次的 secret） */
  onCreated: (w: WebhookCreated) => void
}

export function WebhookFormDialog({ open, onOpenChange, editing, onCreated }: Props) {
  const { t } = useTranslation()
  const save = useSaveWebhook()
  const { data: events } = useOpenEvents()
  const schema = useMemo(() => buildSchema(t), [t])
  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: emptyValues })

  useEffect(() => {
    if (open) form.reset(toValues(editing))
  }, [open, editing, form])

  const onSubmit = form.handleSubmit(async (v) => {
    const data: WebhookForm = { ...v, url: v.url.trim() }
    try {
      const res = await save.mutateAsync({ id: editing?.id, data })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
      if (!editing && typeof (res as Partial<WebhookCreated>).secret === 'string') onCreated(res as WebhookCreated)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  })

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={editing ? t('openapi.webhooks.form.editTitle') : t('openapi.webhooks.form.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="openapi-webhook-form" disabled={save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id="openapi-webhook-form" onSubmit={onSubmit} showRequiredHint>
          <FormGrid>
            <FormGridFull>
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('openapi.webhooks.fields.name')}</FormLabel>
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
                name="url"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('openapi.webhooks.fields.url')}</FormLabel>
                    <FormControl>
                      <Input {...field} inputMode="url" spellCheck={false} placeholder="https://example.com/webhook" />
                    </FormControl>
                    <FormDescription>{t('openapi.webhooks.form.urlHint')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="events"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('openapi.webhooks.fields.events')}</FormLabel>
                    <FormControl>
                      <EventPicker events={events ?? []} value={field.value} onChange={field.onChange} />
                    </FormControl>
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
                    <FormLabel className="m-0">{t('openapi.webhooks.fields.enabled')}</FormLabel>
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
