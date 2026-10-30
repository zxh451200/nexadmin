import { useEffect, useMemo } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useFieldArray, useForm, useWatch, type Control, type FieldPath } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchField as SharedSwitchField } from '@/framework/components/SwitchField'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import { useSaveSource, type CasConfig, type IdentitySource, type LdapConfig, type SourceForm } from './api'

const byteLength = (value: string) => new TextEncoder().encode(value).length
const hasControl = (value: string) => Array.from(value).some((c) => c.charCodeAt(0) < 32 || c.charCodeAt(0) === 127)
const validRole = (value: string) => byteLength(value) <= 64 && !hasControl(value) && value.toLowerCase() !== 'super'
const roleCodes = (value: string) => value.split('\n')

function buildSchema(t: (k: string) => string) {
  const req = z.string().trim().min(1, t('common.required'))
  return z
    .object({
      type: z.enum(['ldap', 'cas']),
      name: z.string().trim().min(1, t('plugin_identity.errors.nameRequired')).max(100, t('plugin_identity.errors.nameTooLong')),
      enabled: z.boolean(),
      // LDAP
      url: z.string().trim(),
      startTls: z.boolean(),
      allowPlaintext: z.boolean(),
      bindDn: z.string().trim(),
      bindPassword: z.string(),
      baseDn: z.string().trim(),
      userFilter: z.string().trim(),
      syncEnabled: z.boolean(),
      syncFilter: z.string().trim(),
      syncSubjectAttr: z.string(),
      syncDeactivate: z.boolean(),
      // CAS
      serverUrl: z.string().trim(),
      version: z.enum(['2', '3']),
      serviceBaseUrl: z.string().trim(),
      // 共用
      caCert: z.string(),
      attrUsername: z.string().trim(),
      attrNickname: z.string().trim(),
      attrEmail: z.string().trim(),
      attrPhone: z.string().trim(),
      attrGroups: z.string().trim().refine((v) => !v || /^[A-Za-z][A-Za-z0-9-]{0,63}$/.test(v), t('plugin_identity.errors.attribute')),
      groupRoleMappings: z.array(z.object({
        // 外部组按原值匹配，不能 trim 或按逗号拆分 DN。
        externalGroup: z.string().refine((v) => !!v.trim() && byteLength(v) <= 2048 && !hasControl(v), t('plugin_identity.errors.mappingGroup')),
        roleCodes: z.string().refine((v) => {
          const codes = roleCodes(v)
          return codes.length <= 100 && new Set(codes).size === codes.length &&
            codes.every((code) => !!code && code === code.trim() && validRole(code))
        }, t('plugin_identity.errors.mappingRole')),
      }).passthrough()).max(100, t('plugin_identity.errors.mappingCount')),
      requireMappedGroup: z.boolean(),
      timeoutSec: z.number().int().min(1, t('plugin_identity.errors.timeout')).max(30, t('plugin_identity.errors.timeout')),
      matchExisting: z.boolean(),
      autoCreate: z.boolean(),
      defaultRole: z.string().trim().refine(validRole, t('plugin_identity.errors.mappingRole')),
    })
    .superRefine((v, ctx) => {
      const need = (path: keyof typeof v, ok: boolean, msg: string) => {
        if (!ok) ctx.addIssue({ code: 'custom', path: [path], message: msg })
      }
      need('groupRoleMappings', !v.requireMappedGroup || v.groupRoleMappings.length > 0, t('plugin_identity.errors.mappingRequired'))
      need('groupRoleMappings', v.groupRoleMappings.reduce((sum, m) => sum + roleCodes(m.roleCodes).length, 0) <= 1000, t('plugin_identity.errors.mappingTotal'))
      const groups = new Set<string>()
      v.groupRoleMappings.forEach((m, index) => {
        if (groups.has(m.externalGroup)) {
          ctx.addIssue({ code: 'custom', path: ['groupRoleMappings', index, 'externalGroup'], message: t('plugin_identity.errors.mappingDuplicate') })
        }
        groups.add(m.externalGroup)
      })
      if (v.type === 'ldap') {
        need('url', /^ldaps?:\/\/[^/?#@\s]+$/i.test(v.url), t('plugin_identity.errors.ldapUrl'))
        // 服务端同样拒绝：ldap:// 且不开 StartTLS 时口令明文传输，必须由管理员显式确认。
        if (/^ldap:\/\//i.test(v.url) && !v.startTls) {
          need('allowPlaintext', v.allowPlaintext, t('plugin_identity.errors.plaintextRequired'))
        }
        need('baseDn', req.safeParse(v.baseDn).success, t('common.required'))
        need('userFilter', v.userFilter.includes('{username}'), t('plugin_identity.errors.filter'))
        if (v.syncEnabled) {
          need('syncFilter', !!v.syncFilter && !v.syncFilter.includes('{username}'), t('plugin_identity.sync.filterError'))
          need('syncSubjectAttr', ['entryUUID', 'objectGUID'].includes(v.syncSubjectAttr), t('common.required'))
        }
      } else {
        need('serverUrl', /^https?:\/\/[^/?#@\s]+(\/[^?#]*)?$/i.test(v.serverUrl), t('plugin_identity.errors.httpUrl'))
        need('serviceBaseUrl', /^https?:\/\/[^/?#@\s]+\/?$/i.test(v.serviceBaseUrl), t('plugin_identity.errors.origin'))
      }
    })
}

type FormValues = z.infer<ReturnType<typeof buildSchema>>

const empty: FormValues = {
  type: 'ldap', name: '', enabled: true,
  url: 'ldaps://', startTls: false, allowPlaintext: false, bindDn: '', bindPassword: '', baseDn: '', userFilter: '(&(objectClass=person)(uid={username}))',
  serverUrl: 'https://', version: '3', serviceBaseUrl: 'https://',
  caCert: '', attrUsername: 'uid', attrNickname: '', attrEmail: '', attrPhone: '', timeoutSec: 10,
  matchExisting: true, autoCreate: false, defaultRole: '',
  attrGroups: '', groupRoleMappings: [], requireMappedGroup: false,
  syncEnabled: false, syncFilter: '(objectClass=person)', syncSubjectAttr: 'entryUUID', syncDeactivate: false,
}

function toValues(s?: IdentitySource | null): FormValues {
  if (!s?.config) return empty
  const c = s.config as LdapConfig & CasConfig
  return {
    ...empty, type: s.type, name: s.name, enabled: s.enabled,
    url: c.url ?? empty.url, startTls: !!c.startTls, allowPlaintext: !!c.allowPlaintext, bindDn: c.bindDn ?? '', bindPassword: '', baseDn: c.baseDn ?? '', userFilter: c.userFilter ?? empty.userFilter,
    serverUrl: c.serverUrl ?? empty.serverUrl, version: c.version ?? '3', serviceBaseUrl: c.serviceBaseUrl ?? empty.serviceBaseUrl,
    caCert: c.caCert ?? '', attrUsername: c.attrUsername ?? '', attrNickname: c.attrNickname ?? '', attrEmail: c.attrEmail ?? '', attrPhone: c.attrPhone ?? '',
    timeoutSec: c.timeoutSec || 10, matchExisting: !!c.matchExisting, autoCreate: !!c.autoCreate, defaultRole: c.defaultRole ?? '',
    attrGroups: c.attrGroups ?? '', requireMappedGroup: !!c.requireMappedGroup,
    groupRoleMappings: (c.groupRoleMappings ?? []).map((m) => ({ ...m, roleCodes: m.roleCodes.join('\n') })),
    syncEnabled: c.directorySync?.enabled ?? false,
    syncFilter: c.directorySync?.filter ?? empty.syncFilter,
    syncSubjectAttr: c.directorySync?.subjectAttr ?? empty.syncSubjectAttr,
    syncDeactivate: !!c.directorySync?.deactivateMissing,
  }
}

function toPayload(v: FormValues, previous?: IdentitySource | null): SourceForm {
  // 保留表单未暴露的配置；密码与服务端回显标记不参与透传。
  const original: Record<string, unknown> = { ...(previous?.type === v.type ? previous.config : null) }
  delete original.bindPassword
  delete original.hasBindPassword
  const common = {
    ...original,
    caCert: v.caCert.trim() || undefined, attrUsername: v.attrUsername || undefined, attrNickname: v.attrNickname || undefined,
    attrEmail: v.attrEmail || undefined, attrPhone: v.attrPhone || undefined, timeoutSec: v.timeoutSec,
    matchExisting: v.matchExisting, autoCreate: v.autoCreate, defaultRole: v.defaultRole || undefined,
    attrGroups: v.attrGroups || (v.type === 'ldap' ? 'memberOf' : 'groups'), requireMappedGroup: v.requireMappedGroup,
    groupRoleMappings: v.groupRoleMappings.map((m) => ({ ...m, roleCodes: roleCodes(m.roleCodes) })),
  }
  if (v.type === 'ldap') {
    const config: LdapConfig = {
      ...common, url: v.url, startTls: v.startTls, bindDn: v.bindDn || undefined, bindPassword: v.bindPassword || undefined,
      // 只在真的会明文传输时才带上确认标记；ldaps / StartTLS 下服务端也会把它清掉。
      allowPlaintext: v.allowPlaintext && !v.startTls && /^ldap:\/\//i.test(v.url) ? true : undefined,
      baseDn: v.baseDn, userFilter: v.userFilter, attrUsername: v.attrUsername || 'uid',
      ...(v.syncEnabled || original.directorySync ? {
        directorySync: {
          ...(previous?.type === 'ldap' ? (previous.config as LdapConfig | null)?.directorySync : undefined),
          enabled: v.syncEnabled, filter: v.syncFilter, subjectAttr: v.syncSubjectAttr,
          deactivateMissing: v.syncDeactivate || undefined,
        },
      } : {}),
    }
    return { type: 'ldap', name: v.name, enabled: v.enabled, config }
  }
  const config: CasConfig = { ...common, serverUrl: v.serverUrl, version: v.version, serviceBaseUrl: v.serviceBaseUrl }
  return { type: 'cas', name: v.name, enabled: v.enabled, config }
}

interface TextProps {
  control: Control<FormValues>
  name: FieldPath<FormValues>
  label: string
  hint?: string
  type?: string
  mono?: boolean
  placeholder?: string
  autoComplete?: string
}

function TextField({ control, name, label, hint, type, mono, placeholder, autoComplete }: TextProps) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input
              type={type}
              className={mono ? 'font-mono' : undefined}
              spellCheck={false}
              autoComplete={autoComplete ?? 'off'}
              placeholder={placeholder}
              name={field.name}
              ref={field.ref}
              onBlur={field.onBlur}
              value={String(field.value ?? '')}
              onChange={field.onChange}
            />
          </FormControl>
          {hint ? <FormDescription>{hint}</FormDescription> : null}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function SwitchField({ control, name, label, hint, bare }: { control: Control<FormValues>; name: 'startTls' | 'allowPlaintext' | 'matchExisting' | 'autoCreate' | 'enabled' | 'requireMappedGroup' | 'syncEnabled' | 'syncDeactivate'; label: string; hint?: string; bare?: boolean }) {
  return <SharedSwitchField control={control} name={name} label={label} description={hint} bare={bare} />
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: IdentitySource | null
}

export function SourceFormDialog({ open, onOpenChange, editing }: Props) {
  const { t } = useTranslation()
  const save = useSaveSource()
  const schema = useMemo(() => buildSchema(t), [t])
  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: empty })
  const mappings = useFieldArray({ control: form.control, name: 'groupRoleMappings' })
  const type = useWatch({ control: form.control, name: 'type' })
  const autoCreate = useWatch({ control: form.control, name: 'autoCreate' })
  const url = useWatch({ control: form.control, name: 'url' })
  const startTls = useWatch({ control: form.control, name: 'startTls' })
  const syncEnabled = useWatch({ control: form.control, name: 'syncEnabled' })
  // 只有 ldap:// 且不启用 StartTLS 才会明文传口令，这时才需要管理员确认。
  const cleartext = /^ldap:\/\//i.test(url ?? '') && !startTls
  const hasPassword = !!(editing?.config as LdapConfig | null | undefined)?.hasBindPassword
  const c = form.control

  useEffect(() => {
    if (open) form.reset(toValues(editing))
  }, [open, editing, form])

  const onSubmit = form.handleSubmit(async (v) => {
    try {
      await save.mutateAsync({ id: editing?.id, data: toPayload(v, editing) })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  })

  const p = (k: string) => t(`plugin_identity.fields.${k}`)
  const h = (k: string) => t(`plugin_identity.hints.${k}`)

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={editing ? t('plugin_identity.editTitle') : t('plugin_identity.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="identity-source-form" disabled={save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id="identity-source-form" onSubmit={onSubmit} showRequiredHint>
          <FormGrid>
            <FormField
              control={c}
              name="type"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{p('type')}</FormLabel>
                  <Select value={field.value} onValueChange={field.onChange} disabled={!!editing}>
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value="ldap">{t('plugin_identity.type.ldap')}</SelectItem>
                      <SelectItem value="cas">{t('plugin_identity.type.cas')}</SelectItem>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />
            <TextField control={c} name="name" label={p('name')} placeholder={t('common.pleaseInput')} />
            {type === 'ldap' ? (
              <>
                <FormGridFull>
                  <TextField control={c} name="url" label={p('url')} hint={h('url')} mono placeholder="ldaps://ldap.example.com:636" />
                </FormGridFull>
                <FormGridFull>
                  <SwitchField control={c} name="startTls" label={p('startTls')} hint={h('startTls')} />
                </FormGridFull>
                {cleartext ? (
                  <FormGridFull>
                    <SwitchField control={c} name="allowPlaintext" label={p('allowPlaintext')} hint={h('allowPlaintext')} />
                  </FormGridFull>
                ) : null}
                <TextField control={c} name="bindDn" label={p('bindDn')} hint={h('bindDn')} mono />
                <TextField
                  control={c}
                  name="bindPassword"
                  type="password"
                  label={p('bindPassword')}
                  hint={editing && hasPassword ? h('bindPasswordKeep') : h('bindPassword')}
                  autoComplete="new-password"
                  placeholder={editing && hasPassword ? '••••••••' : undefined}
                />
                <TextField control={c} name="baseDn" label={p('baseDn')} mono />
                <TextField control={c} name="userFilter" label={p('userFilter')} hint={h('userFilter')} mono />
                <FormGridFull>
                  <fieldset className="space-y-3 rounded-md bg-muted/40 p-3">
                    <legend className="px-1 text-sm font-medium">{t('plugin_identity.sync.title')}</legend>
                    <p className="text-sm text-muted-foreground">{t('plugin_identity.sync.explanation')}</p>
                    <SwitchField control={c} name="syncEnabled" label={t('plugin_identity.sync.enable')} bare />
                    <TextField control={c} name="syncFilter" label={t('plugin_identity.sync.filter')} hint={t('plugin_identity.sync.filterHint')} mono />
                    <FormField control={c} name="syncSubjectAttr" render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('plugin_identity.sync.subjectAttr')}</FormLabel>
                        <Select value={field.value} onValueChange={field.onChange}>
                          <FormControl><SelectTrigger><SelectValue /></SelectTrigger></FormControl>
                          <SelectContent>
                            <SelectItem value="entryUUID">entryUUID</SelectItem>
                            <SelectItem value="objectGUID">objectGUID</SelectItem>
                          </SelectContent>
                        </Select>
                        <FormDescription>{t('plugin_identity.sync.subjectHint')}</FormDescription>
                        <FormMessage />
                      </FormItem>
                    )} />
                    {syncEnabled ? (
                      <SwitchField control={c} name="syncDeactivate" label={t('plugin_identity.sync.deactivateMissing')} hint={t('plugin_identity.sync.deactivateHint')} bare />
                    ) : null}
                  </fieldset>
                </FormGridFull>
              </>
            ) : (
              <>
                <TextField control={c} name="serverUrl" label={p('serverUrl')} hint={h('serverUrl')} mono placeholder="https://cas.example.com/cas" />
                <FormField
                  control={c}
                  name="version"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{p('version')}</FormLabel>
                      <Select value={field.value} onValueChange={field.onChange}>
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectItem value="3">CAS 3.0</SelectItem>
                          <SelectItem value="2">CAS 2.0</SelectItem>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormGridFull>
                  <TextField control={c} name="serviceBaseUrl" label={p('serviceBaseUrl')} hint={h('serviceBaseUrl')} mono placeholder="https://admin.example.com" />
                </FormGridFull>
              </>
            )}
            <TextField control={c} name="attrUsername" label={p('attrUsername')} hint={type === 'cas' ? h('casUsername') : undefined} mono />
            <TextField control={c} name="attrNickname" label={p('attrNickname')} mono />
            <TextField control={c} name="attrEmail" label={p('attrEmail')} mono />
            <TextField control={c} name="attrPhone" label={p('attrPhone')} mono />
            <FormGridFull>
              <TextField control={c} name="attrGroups" label={p('attrGroups')} hint={h('attrGroups')} mono placeholder={type === 'ldap' ? 'memberOf' : 'groups'} />
            </FormGridFull>
            <FormGridFull>
              <fieldset className="space-y-3 rounded-md bg-muted/40 p-3">
                <legend className="px-1 text-sm font-medium">{p('groupRoleMappings')}</legend>
                <p className="text-sm text-muted-foreground">{h('groupRoleMappings')}</p>
                {mappings.fields.map((mapping, index) => (
                  <div key={mapping.id} className="grid gap-3 rounded-md bg-background p-3 sm:grid-cols-2">
                    <TextField control={c} name={`groupRoleMappings.${index}.externalGroup`} label={p('group')} mono />
                    <FormField
                      control={c}
                      name={`groupRoleMappings.${index}.roleCodes`}
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{p('role')}</FormLabel>
                          <FormControl>
                            <Textarea {...field} rows={2} className="font-mono" spellCheck={false} />
                          </FormControl>
                          <FormDescription>{h('roleCodes')}</FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <Button type="button" variant="outline" className="justify-self-start" onClick={() => mappings.remove(index)}>
                      {t('plugin_identity.btn.removeMapping')} {index + 1}
                    </Button>
                  </div>
                ))}
                <Button type="button" variant="outline" disabled={mappings.fields.length >= 100} onClick={() => mappings.append({ externalGroup: '', roleCodes: '' })}>
                  {t('plugin_identity.btn.addMapping')}
                </Button>
                {form.formState.errors.groupRoleMappings?.root?.message || form.formState.errors.groupRoleMappings?.message ? (
                  <p role="alert" className="text-sm font-medium text-destructive">
                    {form.formState.errors.groupRoleMappings?.root?.message ?? form.formState.errors.groupRoleMappings?.message}
                  </p>
                ) : null}
              </fieldset>
            </FormGridFull>
            <FormGridFull>
              <SwitchField control={c} name="requireMappedGroup" label={p('requireMappedGroup')} hint={h('requireMappedGroup')} />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={c}
                name="caCert"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{p('caCert')}</FormLabel>
                    <FormControl>
                      <Textarea {...field} rows={3} className="font-mono text-xs" spellCheck={false} placeholder="-----BEGIN CERTIFICATE-----" />
                    </FormControl>
                    <FormDescription>{h('caCert')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            <FormField
              control={c}
              name="timeoutSec"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{p('timeoutSec')}</FormLabel>
                  <FormControl>
                    <Input type="number" inputMode="numeric" min={1} max={30} value={field.value} onChange={(e) => field.onChange(Number(e.target.value) || 0)} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <TextField control={c} name="defaultRole" label={p('defaultRole')} hint={autoCreate ? h('defaultRole') : h('defaultRoleOff')} />
            <FormGridFull>
              <SwitchField control={c} name="matchExisting" label={p('matchExisting')} hint={h('matchExisting')} />
            </FormGridFull>
            <FormGridFull>
              <SwitchField control={c} name="autoCreate" label={p('autoCreate')} hint={h('autoCreate')} />
            </FormGridFull>
            <FormGridFull>
              <SwitchField control={c} name="enabled" label={p('enabled')} />
            </FormGridFull>
          </FormGrid>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
