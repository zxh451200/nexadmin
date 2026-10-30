import { useEffect, useMemo } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchField } from '@/framework/components/SwitchField'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { cn } from '@/lib/utils'
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
    <div className="overflow-hidden rounded-lg border border-border">
      {groups.map(([group, list]) => {
        const label = t(`openapi.eventGroup.${group}`, { defaultValue: group })
        const picked = list.filter((e) => value.includes(e.name)).length
        const all = picked === list.length
        return (
          <fieldset key={group} className="min-w-0 border-b border-border last:border-b-0">
            <legend className="sr-only">{label}</legend>
            <div className="flex items-center gap-2 bg-muted/50 px-3 py-2">
              <Checkbox
                checked={all ? true : picked > 0 ? 'indeterminate' : false}
                onCheckedChange={(c) => toggleGroup(list, c === true)}
                aria-label={label}
              />
              <span className="text-sm font-medium text-foreground">{label}</span>
              <Badge variant="secondary" className="num ms-auto px-1.5 py-0 text-[11px] font-medium text-muted-foreground">
                {picked}/{list.length}
              </Badge>
            </div>
            <div className="grid grid-cols-1 gap-1 p-2 md:grid-cols-2">
              {list.map((e) => {
                const on = value.includes(e.name)
                return (
                  <label
                    key={e.name}
                    className={cn(
                      'flex min-h-11 cursor-pointer items-start gap-2.5 rounded-md border border-transparent px-2.5 py-1.5 transition-colors md:min-h-0',
                      on ? 'bg-primary/5' : 'hover:bg-accent/60',
                    )}
                  >
                    <Checkbox checked={on} onCheckedChange={(c) => toggle(e.name, c === true)} className="mt-0.5" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm text-foreground" title={e.description || e.name}>
                        {e.description || e.name}
                      </span>
                      {e.description ? (
                        <code className="block truncate font-mono text-[11px] text-muted-foreground">{e.name}</code>
                      ) : null}
                    </span>
                  </label>
                )
              })}
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
              <SwitchField control={form.control} name="enabled" label={t('openapi.webhooks.fields.enabled')} />
            </FormGridFull>
          </FormGrid>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
