import { useEffect, useMemo } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Can } from '@/framework/auth/Can'
import { AiWriteButton } from '@/framework/components/AiWriteButton'
import { DateTimePicker } from '@/framework/components/DateTimePicker'
import { DictSelect } from '@/framework/components/DictSelect'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchField } from '@/framework/components/SwitchField'
import { RichTextEditor } from '@/framework/components/RichTextEditor'
import { TreeSelect } from '@/framework/components/TreeSelect'
import { Uploader } from '@/framework/components/Uploader'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useSaveNotice, type Notice, type NoticeForm, type NoticeScope } from './api'
import { resolveUserLabel } from '../user/api'

const SCOPES: NoticeScope[] = ['all', 'role', 'dept', 'user']

function buildSchema(t: (k: string) => string) {
  return z
    .object({
      title: z.string().trim().min(1, t('common.required')).max(100, t('notice.titleTooLong')),
      type: z.string().min(1, t('common.required')),
      level: z.string().min(1, t('common.required')),
      content: z.string().min(1, t('notice.contentRequired')),
      scope: z.enum(['all', 'role', 'dept', 'user']),
      targetIds: z.array(z.string()),
      pinned: z.boolean(),
      popup: z.boolean(),
      platform: z.boolean(),
      publishAt: z.string(),
      expireAt: z.string(),
      attachments: z.array(
        z.object({
          id: z.string(),
          url: z.string(),
          name: z.string(),
          size: z.number(),
          type: z.string(),
          posterUrl: z.string().optional(),
        }),
      ),
    })
    .superRefine((v, ctx) => {
      if (v.scope !== 'all' && v.targetIds.length === 0) {
        ctx.addIssue({ code: 'custom', path: ['targetIds'], message: t('notice.targetRequired') })
      }
      if (v.publishAt && v.expireAt && new Date(v.expireAt) <= new Date(v.publishAt)) {
        ctx.addIssue({ code: 'custom', path: ['expireAt'], message: t('notice.expireAfterPublish') })
      }
    })
}

type FormValues = z.infer<ReturnType<typeof buildSchema>>

const emptyValues: FormValues = {
  title: '',
  type: 'notice',
  level: 'normal',
  content: '',
  scope: 'all',
  targetIds: [],
  pinned: false,
  popup: false,
  platform: false,
  publishAt: '',
  expireAt: '',
  attachments: [],
}

const toValues = (n?: Notice | null): FormValues =>
  n
    ? {
        title: n.title,
        type: n.type,
        level: n.level,
        content: n.content,
        scope: n.scope,
        targetIds: n.targetIds,
        pinned: n.pinned,
        popup: n.popup,
        platform: n.platform,
        publishAt: n.publishAt ?? '',
        expireAt: n.expireAt ?? '',
        attachments: n.attachments ?? [],
      }
    : emptyValues

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: Notice | null
}

export function NoticeFormDialog({ open, onOpenChange, editing }: Props) {
  const { t } = useTranslation()
  const save = useSaveNotice()
  const schema = useMemo(() => buildSchema(t), [t])
  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: emptyValues })
  const scope = useWatch({ control: form.control, name: 'scope' })

  useEffect(() => {
    if (open) form.reset(toValues(editing))
  }, [open, editing, form])

  const onSubmit = form.handleSubmit(async (v) => {
    const data: NoticeForm = {
      ...v,
      targetIds: v.scope === 'all' ? [] : v.targetIds,
      publishAt: v.publishAt || null,
      expireAt: v.expireAt || null,
    }
    try {
      await save.mutateAsync({ id: editing?.id, data })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch (e) {
      // 受限操作者发布对象越权时，后端返回明确文案（已本地化），直接透传
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  })

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="xl"
      title={editing ? t('notice.editTitle') : t('notice.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="notice-form" disabled={save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id="notice-form" onSubmit={onSubmit} showRequiredHint>
          <FormGrid>
            <FormGridFull>
              <FormField
                control={form.control}
                name="title"
                render={({ field }) => (
                  <FormItem>
                    <div className="flex items-center justify-between gap-2">
                      <FormLabel>{t('notice.fields.title')}</FormLabel>
                      <div className="flex items-center gap-2">
                        <AiWriteButton
                          target="title"
                          format="text"
                          getText={() => form.getValues('title')}
                          onResult={(text) => form.setValue('title', text.slice(0, 100), { shouldDirty: true, shouldValidate: true })}
                        />
                      </div>
                    </div>
                    <FormControl>
                      <Input {...field} maxLength={100} placeholder={t('common.pleaseInput')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormField
              control={form.control}
              name="type"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('notice.fields.type')}</FormLabel>
                  <FormControl>
                    <DictSelect dict="notice_type" value={field.value} onChange={field.onChange} allowClear={false} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="level"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('notice.fields.level')}</FormLabel>
                  <FormControl>
                    <DictSelect dict="notice_level" value={field.value} onChange={field.onChange} allowClear={false} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormGridFull>
              <FormField
                control={form.control}
                name="content"
                render={({ field }) => (
                  <FormItem>
                    <div className="flex items-center justify-between gap-2">
                      <FormLabel>{t('notice.fields.content')}</FormLabel>
                      <div className="flex items-center gap-2">
                        <AiWriteButton
                          target="body"
                          format="html"
                          getText={() => form.getValues('content')}
                          getBrief={() => form.getValues('title')}
                          onResult={(html) => form.setValue('content', html, { shouldDirty: true, shouldValidate: true })}
                        />
                      </div>
                    </div>
                    <FormControl>
                      <RichTextEditor value={field.value} onChange={field.onChange} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormGridFull>
              <FormGrid cols={3}>
                <FormField
                  control={form.control}
                  name="scope"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('notice.fields.scope')}</FormLabel>
                      <Select
                        value={field.value}
                        onValueChange={(v) => {
                          field.onChange(v)
                          form.setValue('targetIds', [], { shouldValidate: false })
                        }}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {SCOPES.map((s) => (
                            <SelectItem key={s} value={s}>
                              {t(`notice.scope.${s}`)}
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
                  name="publishAt"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('notice.fields.publishAt')}</FormLabel>
                      <FormControl>
                        <DateTimePicker value={field.value} onChange={field.onChange} />
                      </FormControl>
                      <FormDescription>{t('notice.hints.publishAt')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name="expireAt"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('notice.fields.expireAt')}</FormLabel>
                      <FormControl>
                        <DateTimePicker value={field.value} onChange={field.onChange} />
                      </FormControl>
                      <FormDescription>{t('notice.hints.expireAt')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </FormGrid>
            </FormGridFull>
            {scope !== 'all' ? (
              <FormGridFull>
                <FormField
                  control={form.control}
                  name="targetIds"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t(`notice.targets.${scope}`)}</FormLabel>
                      <FormControl>
                        {scope === 'role' ? (
                          <RemoteSelect url="/roles/options" multiple value={field.value} onChange={field.onChange} />
                        ) : scope === 'dept' ? (
                          <TreeSelect url="/depts/tree" multiple value={field.value} onChange={field.onChange} />
                        ) : (
                          <RemoteSelect url="/users" labelKey="nickname" multiple value={field.value} onChange={field.onChange} resolveLabel={resolveUserLabel} />
                        )}
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </FormGridFull>
            ) : null}
            <FormGridFull>
              <div className="divide-y divide-border rounded-lg border border-border">
                <SwitchField control={form.control} name="pinned" label={t('notice.fields.pinned')} bare className="p-3" />
                <SwitchField control={form.control} name="popup" label={t('notice.fields.popup')} description={t('notice.hints.popup')} bare className="p-3" />
                <Can perm="notice:platform">
                  <SwitchField control={form.control} name="platform" label={t('notice.fields.platform')} description={t('notice.hints.platform')} bare className="p-3" />
                </Can>
              </div>
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="attachments"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('notice.fields.attachments')}</FormLabel>
                    <FormControl>
                      <Uploader kind="file" multiple maxCount={9} value={field.value} onChange={field.onChange} />
                    </FormControl>
                    <FormMessage />
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
