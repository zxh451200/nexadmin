import { useEffect, useMemo, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { Download, Link2, Loader2 } from 'lucide-react'
import { useFieldArray, useForm, useWatch, type Control, type FieldPath } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchField } from '@/framework/components/SwitchField'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import { inspectMetadata, useSaveSource, type IdPInfo, type SamlSource, type SourceForm } from './api'

const byteLength = (value: string) => new TextEncoder().encode(value).length
const hasControl = (value: string) => Array.from(value).some((c) => c.charCodeAt(0) < 32 || c.charCodeAt(0) === 127)
const validRole = (value: string) => byteLength(value) <= 64 && !hasControl(value) && value.toLowerCase() !== 'super'
const splitRoles = (value: string) => value.split('\n').map((s) => s.trim()).filter(Boolean)
const fmtDate = (v?: string) => {
  if (!v) return '—'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? v : d.toLocaleDateString()
}
const validAttr = (value: string) => byteLength(value) <= 255 && !hasControl(value) && !value.includes(' ')

function buildSchema(t: (k: string) => string) {
  return z
    .object({
      name: z.string().trim().min(1, t('plugin_saml.errors.nameRequired')).max(100, t('plugin_saml.errors.nameTooLong')),
      enabled: z.boolean(),
      idpMetadata: z.string().trim().min(1, t('plugin_saml.errors.metadataRequired')),
      idpMetadataUrl: z
        .string()
        .trim()
        .refine((v) => !v || /^https?:\/\/[^/?#@\s]+[^\s]*$/i.test(v), t('plugin_saml.errors.urlInvalid')),
      attrUsername: z.string().trim().refine(validAttr, t('plugin_saml.errors.attrInvalid')),
      attrEmail: z.string().trim().refine(validAttr, t('plugin_saml.errors.attrInvalid')),
      attrNickname: z.string().trim().refine(validAttr, t('plugin_saml.errors.attrInvalid')),
      attrGroups: z.string().trim().refine(validAttr, t('plugin_saml.errors.attrInvalid')),
      attrPhone: z.string().trim().refine(validAttr, t('plugin_saml.errors.attrInvalid')),
      autoCreate: z.boolean(),
      matchExisting: z.boolean(),
      defaultRole: z.string().trim().refine((v) => !v || validRole(v), t('plugin_saml.errors.defaultRole')),
      requireMappedGroup: z.boolean(),
      groupRoleMappings: z
        .array(
          z.object({
            // 组值按原值精确匹配，不能 trim 或改写。
            externalGroup: z
              .string()
              .refine((v) => !!v.trim() && byteLength(v) <= 2048 && !hasControl(v), t('plugin_saml.errors.mappingGroup')),
            roleCodes: z.string().refine((v) => {
              const codes = splitRoles(v)
              return (
                codes.length > 0 &&
                codes.length <= 100 &&
                new Set(codes).size === codes.length &&
                codes.every((code) => validRole(code))
              )
            }, t('plugin_saml.errors.mappingRole')),
          }),
        )
        .max(100, t('plugin_saml.errors.mappingCount')),
      allowIdpInitiated: z.boolean(),
      signAuthnRequest: z.boolean(),
      nameIdFormat: z.enum(['', 'persistent', 'transient', 'emailAddress', 'unspecified', 'entity']),
    })
    .superRefine((v, ctx) => {
      if (v.requireMappedGroup && v.groupRoleMappings.length === 0) {
        ctx.addIssue({ code: 'custom', path: ['groupRoleMappings'], message: t('plugin_saml.errors.mappingRequired') })
      }
      const total = v.groupRoleMappings.reduce((sum, m) => sum + splitRoles(m.roleCodes).length, 0)
      if (total > 1000) {
        ctx.addIssue({ code: 'custom', path: ['groupRoleMappings'], message: t('plugin_saml.errors.mappingTotal') })
      }
      const groups = new Set<string>()
      v.groupRoleMappings.forEach((m, index) => {
        if (groups.has(m.externalGroup)) {
          ctx.addIssue({ code: 'custom', path: ['groupRoleMappings', index, 'externalGroup'], message: t('plugin_saml.errors.mappingDuplicate') })
        }
        groups.add(m.externalGroup)
      })
    })
}

type FormValues = z.infer<ReturnType<typeof buildSchema>>

const empty: FormValues = {
  name: '',
  enabled: false,
  idpMetadata: '',
  idpMetadataUrl: '',
  attrUsername: 'uid',
  attrEmail: 'mail',
  attrNickname: 'displayName',
  attrGroups: '',
  attrPhone: '',
  autoCreate: false,
  matchExisting: false,
  defaultRole: '',
  requireMappedGroup: false,
  groupRoleMappings: [],
  allowIdpInitiated: false,
  signAuthnRequest: false,
  nameIdFormat: '',
}

function toValues(s?: SamlSource | null): FormValues {
  if (!s) return empty
  return {
    name: s.name,
    enabled: s.enabled,
    idpMetadata: s.idpMetadata,
    idpMetadataUrl: s.idpMetadataUrl ?? '',
    attrUsername: s.attrUsername,
    attrEmail: s.attrEmail,
    attrNickname: s.attrNickname,
    attrGroups: s.attrGroups ?? '',
    attrPhone: s.attrPhone ?? '',
    autoCreate: s.autoCreate,
    matchExisting: s.matchExisting,
    defaultRole: s.defaultRole ?? '',
    requireMappedGroup: s.requireMappedGroup,
    groupRoleMappings: (s.groupRoleMappings ?? []).map((m) => ({ externalGroup: m.externalGroup, roleCodes: m.roleCodes.join('\n') })),
    allowIdpInitiated: s.allowIdpInitiated,
    signAuthnRequest: s.signAuthnRequest,
    nameIdFormat: (s.nameIdFormat ?? '') as FormValues['nameIdFormat'],
  }
}

function toPayload(v: FormValues): SourceForm {
  return {
    ...v,
    attrUsername: v.attrUsername || 'uid',
    attrEmail: v.attrEmail || 'mail',
    attrNickname: v.attrNickname || 'displayName',
    groupRoleMappings: v.groupRoleMappings.map((m) => ({ externalGroup: m.externalGroup, roleCodes: splitRoles(m.roleCodes) })),
  }
}

interface TextProps {
  control: Control<FormValues>
  name: FieldPath<FormValues>
  label: string
  hint?: string
  placeholder?: string
  mono?: boolean
}

function TextField({ control, name, label, hint, placeholder, mono }: TextProps) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input
              className={mono ? 'font-mono' : undefined}
              spellCheck={false}
              autoComplete="off"
              placeholder={placeholder}
              value={String(field.value ?? '')}
              onChange={field.onChange}
              onBlur={field.onBlur}
              name={field.name}
              ref={field.ref}
            />
          </FormControl>
          {hint ? <FormDescription>{hint}</FormDescription> : null}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: SamlSource | null
}

export default function SourceFormDialog({ open, onOpenChange, editing }: Props) {
  const { t } = useTranslation()
  const save = useSaveSource()
  const schema = useMemo(() => buildSchema(t), [t])
  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: empty })
  const mappings = useFieldArray({ control: form.control, name: 'groupRoleMappings' })
  const autoCreate = useWatch({ control: form.control, name: 'autoCreate' })
  const idpMetadata = useWatch({ control: form.control, name: 'idpMetadata' })
  const idpMetadataUrl = useWatch({ control: form.control, name: 'idpMetadataUrl' })
  const [preview, setPreview] = useState<IdPInfo | null>(null)
  const [fetching, setFetching] = useState<'url' | 'xml' | null>(null)

  useEffect(() => {
    if (!open) return
    form.reset(toValues(editing))
    setPreview(editing?.idp && !editing.idp.error ? editing.idp : null)
  }, [open, editing, form])

  const onSubmit = form.handleSubmit(async (v) => {
    try {
      await save.mutateAsync({ id: editing?.id, data: toPayload(v) })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  })

  const runInspect = async (mode: 'url' | 'xml') => {
    setFetching(mode)
    try {
      const input =
        mode === 'url'
          ? { url: form.getValues('idpMetadataUrl').trim() }
          : { xml: form.getValues('idpMetadata').trim() }
      const r = await inspectMetadata(input)
      if (mode === 'url') form.setValue('idpMetadata', r.metadata, { shouldValidate: true, shouldDirty: true })
      setPreview(r.idp)
      toast.success(t('plugin_saml.preview.ok'))
    } catch (e) {
      setPreview(null)
      toast.error((e as Error).message)
    } finally {
      setFetching(null)
    }
  }

  const p = (k: string) => t(`plugin_saml.fields.${k}`)
  const h = (k: string) => t(`plugin_saml.hints.${k}`)
  const c = form.control

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={editing ? t('plugin_saml.edit') : t('plugin_saml.add')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="saml-source-form" disabled={save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id="saml-source-form" onSubmit={onSubmit} showRequiredHint>
          <FormGrid>
            <TextField control={c} name="name" label={p('name')} placeholder={t('common.pleaseInput')} />
            <SwitchField control={c} name="enabled" label={p('enabled')} description={h('enabled')} />

            <FormGridFull>
              <fieldset className="space-y-3 rounded-md bg-muted/40 p-3">
                <legend className="px-1 text-sm font-medium">{t('plugin_saml.sections.idp')}</legend>
                <TextField
                  control={c}
                  name="idpMetadataUrl"
                  label={p('idpMetadataUrl')}
                  hint={h('idpMetadataUrl')}
                  mono
                  placeholder="https://idp.example.com/metadata"
                />
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={!!fetching || !(idpMetadataUrl ?? '').trim()}
                    onClick={() => void runInspect('url')}
                  >
                    {fetching === 'url' ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Link2 aria-hidden="true" />}
                    {t('plugin_saml.btn.fetch')}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={!!fetching || !(idpMetadata ?? '').trim()}
                    onClick={() => void runInspect('xml')}
                  >
                    {fetching === 'xml' ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Download aria-hidden="true" />}
                    {t('plugin_saml.btn.parse')}
                  </Button>
                </div>
                <FormField
                  control={c}
                  name="idpMetadata"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{p('idpMetadata')}</FormLabel>
                      <FormControl>
                        <Textarea
                          {...field}
                          rows={6}
                          spellCheck={false}
                          className="font-mono text-xs"
                          placeholder="<EntityDescriptor …>"
                        />
                      </FormControl>
                      <FormDescription>{h('idpMetadata')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                {preview ? (
                  <div className="space-y-1 rounded-md border border-border bg-background p-3 text-xs">
                    <div className="font-medium text-foreground">{t('plugin_saml.preview.title')}</div>
                    <div className="break-all">
                      {t('plugin_saml.preview.entityId')}: <code>{preview.entityId}</code>
                    </div>
                    <div className="break-all">
                      {t('plugin_saml.preview.sso')}: <code>{preview.ssoUrl || '—'}</code>
                      {preview.ssoBinding ? ` (${t(`plugin_saml.binding.${preview.ssoBinding}`)})` : ''}
                    </div>
                    <div className="break-all">
                      {t('plugin_saml.fields.cert')}: {preview.certCount > 0 ? fmtDate(preview.certNotAfter) : t('plugin_saml.preview.noCert')}
                    </div>
                  </div>
                ) : null}
              </fieldset>
            </FormGridFull>

            <TextField control={c} name="attrUsername" label={p('attrUsername')} hint={h('attrUsername')} mono placeholder="uid" />
            <TextField control={c} name="attrEmail" label={p('attrEmail')} mono placeholder="mail" />
            <TextField control={c} name="attrNickname" label={p('attrNickname')} mono placeholder="displayName" />
            <TextField control={c} name="attrGroups" label={p('attrGroups')} hint={h('attrGroups')} mono placeholder="groups" />
            <TextField control={c} name="attrPhone" label={p('attrPhone')} mono placeholder="telephoneNumber" />

            <FormGridFull>
              <fieldset className="space-y-3 rounded-md bg-muted/40 p-3">
                <legend className="px-1 text-sm font-medium">{t('plugin_saml.sections.account')}</legend>
                <SwitchField control={c} name="autoCreate" label={p('autoCreate')} description={h('autoCreate')} bare />
                <SwitchField control={c} name="matchExisting" label={p('matchExisting')} description={h('matchExisting')} bare />
                <TextField
                  control={c}
                  name="defaultRole"
                  label={p('defaultRole')}
                  hint={autoCreate ? h('defaultRole') : h('defaultRoleOff')}
                  mono
                  placeholder="guest"
                />
              </fieldset>
            </FormGridFull>

            <FormGridFull>
              <fieldset className="space-y-3 rounded-md bg-muted/40 p-3">
                <legend className="px-1 text-sm font-medium">{p('groupRoleMappings')}</legend>
                <p className="text-sm text-muted-foreground">{h('groupRoleMappings')}</p>
                <SwitchField
                  control={c}
                  name="requireMappedGroup"
                  label={p('requireMappedGroup')}
                  description={h('requireMappedGroup')}
                  bare
                />
                {mappings.fields.map((mapping, index) => (
                  <div key={mapping.id} className="grid gap-3 rounded-md bg-background p-3 sm:grid-cols-2">
                    <TextField control={c} name={`groupRoleMappings.${index}.externalGroup`} label={p('group')} mono />
                    <FormField
                      control={c}
                      name={`groupRoleMappings.${index}.roleCodes`}
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{p('roles')}</FormLabel>
                          <FormControl>
                            <Textarea {...field} rows={2} className="font-mono" spellCheck={false} />
                          </FormControl>
                          <FormDescription>{h('roleCodes')}</FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <Button type="button" variant="outline" className="justify-self-start" onClick={() => mappings.remove(index)}>
                      {t('plugin_saml.btn.removeMapping')} {index + 1}
                    </Button>
                  </div>
                ))}
                <Button
                  type="button"
                  variant="outline"
                  disabled={mappings.fields.length >= 100}
                  onClick={() => mappings.append({ externalGroup: '', roleCodes: '' })}
                >
                  {t('plugin_saml.btn.addMapping')}
                </Button>
                {form.formState.errors.groupRoleMappings?.root?.message || form.formState.errors.groupRoleMappings?.message ? (
                  <p role="alert" className="text-sm font-medium text-destructive">
                    {form.formState.errors.groupRoleMappings?.root?.message ?? form.formState.errors.groupRoleMappings?.message}
                  </p>
                ) : null}
              </fieldset>
            </FormGridFull>

            <FormGridFull>
              <fieldset className="space-y-3 rounded-md bg-muted/40 p-3">
                <legend className="px-1 text-sm font-medium">{t('plugin_saml.sections.advanced')}</legend>
                <SwitchField
                  control={c}
                  name="allowIdpInitiated"
                  label={p('allowIdpInitiated')}
                  description={h('allowIdpInitiated')}
                  bare
                />
                <SwitchField
                  control={c}
                  name="signAuthnRequest"
                  label={p('signAuthnRequest')}
                  description={h('signAuthnRequest')}
                  bare
                />
                <FormField
                  control={c}
                  name="nameIdFormat"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{p('nameIdFormat')}</FormLabel>
                      <Select
                        value={field.value || 'default'}
                        onValueChange={(v) => field.onChange(v === 'default' ? '' : v)}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectItem value="default">{t('plugin_saml.nameId.default')}</SelectItem>
                          <SelectItem value="persistent">persistent</SelectItem>
                          <SelectItem value="transient">transient</SelectItem>
                          <SelectItem value="emailAddress">emailAddress</SelectItem>
                          <SelectItem value="unspecified">unspecified</SelectItem>
                          <SelectItem value="entity">entity</SelectItem>
                        </SelectContent>
                      </Select>
                      <FormDescription>{h('nameIdFormat')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </fieldset>
            </FormGridFull>
          </FormGrid>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
