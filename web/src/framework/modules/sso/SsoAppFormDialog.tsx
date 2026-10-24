import { useEffect, useMemo } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Uploader, type UploadedFile } from '@/framework/components/Uploader'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import { isHttpUrl, isValidRedirectUri, useSaveSsoApp, type SsoApp, type SsoAppCreated, type SsoAppForm } from './api'

const splitLines = (text: string) =>
  text
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean)

function buildSchema(t: (k: string, o?: Record<string, unknown>) => string) {
  return z.object({
    name: z.string().trim().min(1, t('common.required')).max(50, t('sso.form.nameTooLong')),
    redirectUris: z.string().superRefine((v, ctx) => {
      const lines = splitLines(v)
      if (lines.length === 0) {
        ctx.addIssue({ code: 'custom', message: t('sso.form.redirectRequired') })
        return
      }
      const bad = lines.find((l) => !isValidRedirectUri(l))
      if (bad) ctx.addIssue({ code: 'custom', message: t('sso.form.redirectInvalid', { uri: bad }) })
    }),
    logo: z.string().trim().refine((v) => !v || v.startsWith('/') || isHttpUrl(v), t('sso.form.urlInvalid')),
    homeUrl: z.string().trim().refine((v) => !v || isHttpUrl(v), t('sso.form.urlInvalid')),
    description: z.string().trim().max(200, t('sso.form.descTooLong')),
    enabled: z.boolean(),
    roleIds: z.array(z.string()),
    allowOrgRead: z.boolean(),
  })
}

type FormValues = z.infer<ReturnType<typeof buildSchema>>

const emptyValues: FormValues = {
  name: '',
  redirectUris: '',
  logo: '',
  homeUrl: '',
  description: '',
  enabled: true,
  roleIds: [],
  allowOrgRead: false,
}

const toValues = (a?: SsoApp | null): FormValues =>
  a
    ? {
        name: a.name,
        redirectUris: a.redirectUris.join('\n'),
        logo: a.logo ?? '',
        homeUrl: a.homeUrl ?? '',
        description: a.description ?? '',
        enabled: a.enabled,
        roleIds: a.roleIds ?? [],
        allowOrgRead: a.allowOrgRead ?? false,
      }
    : emptyValues

const logoFiles = (url: string): UploadedFile[] => (url ? [{ id: 'logo', url, name: 'logo', size: 0, type: 'image/*' }] : [])

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: SsoApp | null
  /** 创建成功（含只显示一次的 clientSecret） */
  onCreated: (app: SsoAppCreated) => void
}

export function SsoAppFormDialog({ open, onOpenChange, editing, onCreated }: Props) {
  const { t } = useTranslation()
  const save = useSaveSsoApp()
  const schema = useMemo(() => buildSchema(t), [t])
  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: emptyValues })

  useEffect(() => {
    if (open) form.reset(toValues(editing))
  }, [open, editing, form])

  const onSubmit = form.handleSubmit(async (v) => {
    const data: SsoAppForm = { ...v, redirectUris: splitLines(v.redirectUris) }
    try {
      const res = await save.mutateAsync({ id: editing?.id, data })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
      if (!editing && typeof (res as Partial<SsoAppCreated>).clientSecret === 'string') onCreated(res as SsoAppCreated)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  })

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={editing ? t('sso.form.editTitle') : t('sso.form.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="sso-app-form" disabled={save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id="sso-app-form" onSubmit={onSubmit} showRequiredHint>
          <FormGrid>
            <FormGridFull>
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sso.fields.name')}</FormLabel>
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
                name="redirectUris"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sso.fields.redirectUris')}</FormLabel>
                    <FormControl>
                      <Textarea {...field} rows={4} spellCheck={false} placeholder="https://app.example.com/callback" />
                    </FormControl>
                    <FormDescription>{t('sso.form.redirectHint')}</FormDescription>
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
                    <FormLabel>{t('sso.fields.roles')}</FormLabel>
                    <FormControl>
                      <RemoteSelect url="/roles/options" multiple value={field.value} onChange={field.onChange} placeholder={t('sso.allUsers')} />
                    </FormControl>
                    <FormDescription>{t('sso.form.rolesHint')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="logo"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sso.fields.logo')}</FormLabel>
                    <FormControl>
                      <Uploader
                        kind="image"
                        visibility="public"
                        crop={{ aspect: 1 }}
                        value={logoFiles(field.value)}
                        onChange={(files) => field.onChange(files[0]?.url ?? '')}
                      />
                    </FormControl>
                    <Input
                      value={field.value}
                      onChange={(e) => field.onChange(e.target.value)}
                      onBlur={field.onBlur}
                      inputMode="url"
                      placeholder={t('sso.form.logoUrlPlaceholder')}
                      aria-label={t('sso.form.logoUrl')}
                    />
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="homeUrl"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sso.fields.homeUrl')}</FormLabel>
                    <FormControl>
                      <Input {...field} inputMode="url" placeholder="https://app.example.com" />
                    </FormControl>
                    <FormDescription>{t('sso.form.homeUrlHint')}</FormDescription>
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
                    <FormLabel>{t('sso.fields.description')}</FormLabel>
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
                name="enabled"
                render={({ field }) => (
                  <FormItem className="flex min-h-11 items-center justify-between gap-3 rounded-md border border-border p-3">
                    <FormLabel className="m-0">{t('sso.fields.enabled')}</FormLabel>
                    <FormControl>
                      <Switch checked={field.value} onCheckedChange={field.onChange} />
                    </FormControl>
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="allowOrgRead"
                render={({ field }) => (
                  <FormItem className="flex flex-col gap-1 rounded-md border border-border p-3">
                    <div className="flex min-h-11 items-center justify-between gap-3">
                      <FormLabel className="m-0">{t('sso.fields.allowOrgRead')}</FormLabel>
                      <FormControl>
                        <Switch checked={field.value} onCheckedChange={field.onChange} />
                      </FormControl>
                    </div>
                    <FormDescription>{t('sso.form.allowOrgReadHint')}</FormDescription>
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
