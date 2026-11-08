import { useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { Download, Link2, Loader2, Upload } from 'lucide-react'
import { useFieldArray, useForm, useWatch, type Control, type FieldErrors, type FieldPath } from 'react-hook-form'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
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

// 预置键与后端 authnContextPresets 一一对应；custom 时存逗号分隔的 URI 列表。
const AUTHN_PRESETS = ['', 'passwordProtectedTransport', 'password', 'mfa', 'sfa'] as const
const validContextURI = (value: string) => /^(urn:[^\s,]+|https?:\/\/[^\s,]+)$/i.test(value)

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
      idpEntityId: z
        .string()
        .trim()
        .max(500, t('plugin_saml.errors.entityInvalid'))
        .refine((v) => !hasControl(v), t('plugin_saml.errors.entityInvalid')),
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
      forceAuthn: z.boolean(),
      requireSignedResponseForCBC: z.boolean(),
      rejectUnknownConditions: z.boolean(),
      requireAuthnContext: z.boolean(),
      authnContextPreset: z.enum(['', 'passwordProtectedTransport', 'password', 'mfa', 'sfa', 'custom']),
      authnContextCustom: z
        .string()
        .trim()
        .max(500, t('plugin_saml.errors.authnContextInvalid'))
        .refine((v) => !hasControl(v), t('plugin_saml.errors.authnContextInvalid')),
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
      if (v.authnContextPreset === 'custom') {
        const refs = v.authnContextCustom.split(',').map((s) => s.trim()).filter(Boolean)
        if (refs.length === 0) {
          ctx.addIssue({ code: 'custom', path: ['authnContextCustom'], message: t('plugin_saml.errors.authnContextRequired') })
        } else if (refs.length > 10) {
          ctx.addIssue({ code: 'custom', path: ['authnContextCustom'], message: t('plugin_saml.errors.authnContextCount') })
        } else if (refs.some((r) => !validContextURI(r))) {
          ctx.addIssue({ code: 'custom', path: ['authnContextCustom'], message: t('plugin_saml.errors.authnContextInvalid') })
        } else if (new Set(refs).size !== refs.length) {
          ctx.addIssue({ code: 'custom', path: ['authnContextCustom'], message: t('plugin_saml.errors.authnContextDuplicate') })
        }
      }
      const resolvedContext = v.authnContextPreset === 'custom' ? v.authnContextCustom.trim() : v.authnContextPreset
      if (v.requireAuthnContext && !resolvedContext) {
        ctx.addIssue({ code: 'custom', path: ['authnContextPreset'], message: t('plugin_saml.errors.authnContextRequired') })
      }
    })
}

type FormValues = z.infer<ReturnType<typeof buildSchema>>

const empty: FormValues = {
  name: '',
  enabled: false,
  idpMetadata: '',
  idpMetadataUrl: '',
  idpEntityId: '',
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
  forceAuthn: false,
  requireSignedResponseForCBC: false,
  rejectUnknownConditions: false,
  requireAuthnContext: false,
  authnContextPreset: '',
  authnContextCustom: '',
}

function toValues(s?: SamlSource | null): FormValues {
  if (!s) return empty
  return {
    name: s.name,
    enabled: s.enabled,
    idpMetadata: s.idpMetadata,
    idpMetadataUrl: s.idpMetadataUrl ?? '',
    idpEntityId: s.idpEntityId ?? '',
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
    forceAuthn: s.forceAuthn ?? false,
    requireSignedResponseForCBC: s.requireSignedResponseForCBC ?? false,
    rejectUnknownConditions: s.rejectUnknownConditions ?? false,
    requireAuthnContext: s.requireAuthnContext ?? false,
    ...toAuthnContextForm(s.authnContextClassRef ?? ''),
  }
}

function toAuthnContextForm(value: string): Pick<FormValues, 'authnContextPreset' | 'authnContextCustom'> {
  if (!value) return { authnContextPreset: '', authnContextCustom: '' }
  if ((AUTHN_PRESETS as readonly string[]).includes(value)) {
    return { authnContextPreset: value as FormValues['authnContextPreset'], authnContextCustom: '' }
  }
  return { authnContextPreset: 'custom', authnContextCustom: value }
}

function toPayload(v: FormValues): SourceForm {
  const { authnContextPreset, authnContextCustom, ...rest } = v
  return {
    ...rest,
    attrUsername: v.attrUsername || 'uid',
    attrEmail: v.attrEmail || 'mail',
    attrNickname: v.attrNickname || 'displayName',
    groupRoleMappings: v.groupRoleMappings.map((m) => ({ externalGroup: m.externalGroup, roleCodes: splitRoles(m.roleCodes) })),
    authnContextClassRef: authnContextPreset === 'custom' ? authnContextCustom.trim() : authnContextPreset,
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

type TabKey = 'basic' | 'account' | 'advanced'

// TabsContent 会卸载非活动面板，字段自身的 FormMessage 被隐藏，靠 Trigger 上的圆点提示哪个 Tab 有错。
const BASIC_ERROR_KEYS = ['name', 'idpMetadata', 'idpMetadataUrl', 'idpEntityId']
const ACCOUNT_ERROR_KEYS = [
  'attrUsername',
  'attrEmail',
  'attrNickname',
  'attrGroups',
  'attrPhone',
  'defaultRole',
  'requireMappedGroup',
  'groupRoleMappings',
]
const ADVANCED_ERROR_KEYS = ['nameIdFormat', 'authnContextPreset', 'authnContextCustom', 'requireAuthnContext']

export default function SourceFormDialog({ open, onOpenChange, editing }: Props) {
  const { t } = useTranslation()
  const save = useSaveSource()
  const schema = useMemo(() => buildSchema(t), [t])
  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: empty })
  const mappings = useFieldArray({ control: form.control, name: 'groupRoleMappings' })
  const autoCreate = useWatch({ control: form.control, name: 'autoCreate' })
  const idpMetadata = useWatch({ control: form.control, name: 'idpMetadata' })
  const idpMetadataUrl = useWatch({ control: form.control, name: 'idpMetadataUrl' })
  const idpEntityId = useWatch({ control: form.control, name: 'idpEntityId' })
  const authnContextPreset = useWatch({ control: form.control, name: 'authnContextPreset' })
  const [tab, setTab] = useState<TabKey>('basic')
  const [preview, setPreview] = useState<IdPInfo | null>(null)
  const [fetching, setFetching] = useState<'url' | 'xml' | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const candidates = preview?.candidates ?? []
  const shown = candidates.find((c) => c.entityId === idpEntityId) ?? preview
  const errors = form.formState.errors
  // 默认读渲染期的 formState.errors（供 Trigger 圆点）；提交失败时用 RHF 回传的最新 errors，避免闭包里的旧值。
  const hasError = (keys: string[], source: Record<string, unknown> = errors as Record<string, unknown>) =>
    keys.some((k) => Boolean(source[k]))

  useEffect(() => {
    if (!open) return
    form.reset(toValues(editing))
    setPreview(editing?.idp && !editing.idp.error ? editing.idp : null)
    setTab('basic')
  }, [open, editing, form])

  const onValid = async (v: FormValues) => {
    if (v.enabled && (preview?.candidates?.length ?? 0) > 1 && !v.idpEntityId) {
      // idpEntityId 属于基础 Tab，先切过去再报错，否则用户看不到提示。
      setTab('basic')
      form.setError('idpEntityId', { message: t('plugin_saml.errors.entityRequired') })
      return
    }
    try {
      await save.mutateAsync({ id: editing?.id, data: toPayload(v) })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onInvalid = (invalid: FieldErrors<FormValues>) => {
    const source = invalid as Record<string, unknown>
    if (hasError(BASIC_ERROR_KEYS, source)) setTab('basic')
    else if (hasError(ACCOUNT_ERROR_KEYS, source)) setTab('account')
    else if (hasError(ADVANCED_ERROR_KEYS, source)) setTab('advanced')
  }

  const onSubmit = form.handleSubmit(onValid, onInvalid)

  const runInspect = async (mode: 'url' | 'xml') => {
    setFetching(mode)
    try {
      const input =
        mode === 'url'
          ? { url: form.getValues('idpMetadataUrl').trim() }
          : { xml: form.getValues('idpMetadata').trim() }
      const r = await inspectMetadata(input)
      if (mode === 'url') form.setValue('idpMetadata', r.metadata, { shouldValidate: true, shouldDirty: true })
      const candidates = r.idp.candidates ?? []
      const current = form.getValues('idpEntityId').trim()
      const keep = candidates.some((c) => c.entityId === current) ? current : candidates.length === 1 ? candidates[0].entityId : ''
      form.setValue('idpEntityId', keep, { shouldValidate: true, shouldDirty: true })
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

  const onPickFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    if (file.size > 1024 * 1024) {
      toast.error(t('plugin_saml.errors.fileTooLarge'))
      return
    }
    try {
      form.setValue('idpMetadata', await file.text(), { shouldValidate: true, shouldDirty: true })
      await runInspect('xml')
    } catch {
      toast.error(t('plugin_saml.errors.fileReadFailed'))
    }
  }

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
          <Tabs value={tab} onValueChange={(v) => setTab(v as TabKey)}>
            <TabsList>
              <TabsTrigger value="basic" aria-invalid={hasError(BASIC_ERROR_KEYS) || undefined}>
                {t('plugin_saml.tabs.basic')}
                {hasError(BASIC_ERROR_KEYS) ? (
                  <span className="size-1.5 rounded-full bg-destructive" aria-hidden="true" />
                ) : null}
              </TabsTrigger>
              <TabsTrigger value="account" aria-invalid={hasError(ACCOUNT_ERROR_KEYS) || undefined}>
                {t('plugin_saml.tabs.account')}
                {hasError(ACCOUNT_ERROR_KEYS) ? (
                  <span className="size-1.5 rounded-full bg-destructive" aria-hidden="true" />
                ) : null}
              </TabsTrigger>
              <TabsTrigger value="advanced" aria-invalid={hasError(ADVANCED_ERROR_KEYS) || undefined}>
                {t('plugin_saml.tabs.advanced')}
                {hasError(ADVANCED_ERROR_KEYS) ? (
                  <span className="size-1.5 rounded-full bg-destructive" aria-hidden="true" />
                ) : null}
              </TabsTrigger>
            </TabsList>

            <TabsContent value="basic">
              <FormGrid cols={2}>
                <TextField control={c} name="name" label={p('name')} placeholder={t('common.pleaseInput')} />
                <SwitchField control={c} name="enabled" label={p('enabled')} description={h('enabled')} />

                <FormGridFull>
                  <div className="space-y-3 rounded-md bg-muted/40 p-3">
                    <TextField
                      control={c}
                      name="idpMetadataUrl"
                      label={p('idpMetadataUrl')}
                      hint={h('idpMetadataUrl')}
                      mono
                      placeholder="https://idp.example.com/metadata"
                    />
                    <div className="flex flex-wrap gap-2">
                      <input
                        ref={fileRef}
                        type="file"
                        accept=".xml,application/xml,text/xml"
                        className="hidden"
                        aria-label={t('plugin_saml.btn.importFile')}
                        onChange={(e) => void onPickFile(e)}
                      />
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={!!fetching}
                        onClick={() => fileRef.current?.click()}
                      >
                        <Upload aria-hidden="true" />
                        {t('plugin_saml.btn.importFile')}
                      </Button>
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
                    {candidates.length > 1 ? (
                      <FormField
                        control={c}
                        name="idpEntityId"
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{p('idpEntityId')}</FormLabel>
                            <Select
                              value={field.value || undefined}
                              onValueChange={(v) => {
                                field.onChange(v)
                                const cand = candidates.find((x) => x.entityId === v)
                                if (cand) setPreview((prev) => (prev ? { ...prev, ...cand } : prev))
                              }}
                            >
                              <FormControl>
                                <SelectTrigger>
                                  <SelectValue placeholder={t('plugin_saml.preview.selectEntity')} />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent>
                                {candidates.map((cand) => (
                                  <SelectItem key={cand.entityId} value={cand.entityId}>
                                    {cand.entityId}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                            <FormDescription>{h('idpEntityId')}</FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    ) : null}
                    {preview ? (
                      <div className="space-y-1 rounded-md border border-border bg-background p-3 text-xs">
                        <div className="font-medium text-foreground">{t('plugin_saml.preview.title')}</div>
                        {shown?.entityId ? (
                          <>
                            <div className="break-all">
                              {t('plugin_saml.preview.entityId')}: <code>{shown.entityId}</code>
                            </div>
                            <div className="break-all">
                              {t('plugin_saml.preview.sso')}: <code>{shown.ssoUrl || '—'}</code>
                              {shown.ssoBinding ? ` (${t(`plugin_saml.binding.${shown.ssoBinding}`)})` : ''}
                            </div>
                            <div className="break-all">
                              {t('plugin_saml.fields.cert')}: {shown.certCount > 0 ? fmtDate(shown.certNotAfter) : t('plugin_saml.preview.noCert')}
                            </div>
                          </>
                        ) : (
                          <p className="text-muted-foreground">{t('plugin_saml.preview.selectEntity')}</p>
                        )}
                      </div>
                    ) : null}
                  </div>
                </FormGridFull>
              </FormGrid>
            </TabsContent>

            <TabsContent value="account">
              <FormGrid cols={2}>
                <TextField control={c} name="attrUsername" label={p('attrUsername')} hint={h('attrUsername')} mono placeholder="uid" />
                <TextField control={c} name="attrEmail" label={p('attrEmail')} mono placeholder="mail" />
                <TextField control={c} name="attrNickname" label={p('attrNickname')} mono placeholder="displayName" />
                <TextField control={c} name="attrGroups" label={p('attrGroups')} hint={h('attrGroups')} mono placeholder="groups" />
                <TextField control={c} name="attrPhone" label={p('attrPhone')} mono placeholder="telephoneNumber" />

                <FormGridFull>
                  <div className="space-y-3 rounded-md bg-muted/40 p-3">
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
                  </div>
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
              </FormGrid>
            </TabsContent>

            <TabsContent value="advanced">
              <FormGrid cols={1}>
                <FormGridFull>
                  <div className="space-y-3 rounded-md bg-muted/40 p-3">
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
                    <SwitchField control={c} name="forceAuthn" label={p('forceAuthn')} description={h('forceAuthn')} bare />
                    <SwitchField
                      control={c}
                      name="requireSignedResponseForCBC"
                      label={p('requireSignedResponseForCBC')}
                      description={h('requireSignedResponseForCBC')}
                      bare
                    />
                    <SwitchField
                      control={c}
                      name="rejectUnknownConditions"
                      label={p('rejectUnknownConditions')}
                      description={h('rejectUnknownConditions')}
                      bare
                    />
                    <FormField
                      control={c}
                      name="authnContextPreset"
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{p('authnContext')}</FormLabel>
                          <Select value={field.value || 'none'} onValueChange={(v) => field.onChange(v === 'none' ? '' : v)}>
                            <FormControl>
                              <SelectTrigger>
                                <SelectValue />
                              </SelectTrigger>
                            </FormControl>
                            <SelectContent>
                              <SelectItem value="none">{t('plugin_saml.authnContext.none')}</SelectItem>
                              <SelectItem value="passwordProtectedTransport">
                                {t('plugin_saml.authnContext.passwordProtectedTransport')}
                              </SelectItem>
                              <SelectItem value="password">{t('plugin_saml.authnContext.password')}</SelectItem>
                              <SelectItem value="mfa">{t('plugin_saml.authnContext.mfa')}</SelectItem>
                              <SelectItem value="sfa">{t('plugin_saml.authnContext.sfa')}</SelectItem>
                              <SelectItem value="custom">{t('plugin_saml.authnContext.custom')}</SelectItem>
                            </SelectContent>
                          </Select>
                          <FormDescription>{h('authnContext')}</FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    {authnContextPreset === 'custom' ? (
                      <TextField
                        control={c}
                        name="authnContextCustom"
                        label={p('authnContextCustom')}
                        hint={h('authnContextCustom')}
                        mono
                        placeholder="urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport"
                      />
                    ) : null}
                    <SwitchField
                      control={c}
                      name="requireAuthnContext"
                      label={p('requireAuthnContext')}
                      description={h('requireAuthnContext')}
                      bare
                    />
                  </div>
                </FormGridFull>
              </FormGrid>
            </TabsContent>
          </Tabs>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
