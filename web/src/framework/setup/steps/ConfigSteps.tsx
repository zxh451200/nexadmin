import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { useLanguages } from '@/framework/i18n/useLanguages'
import { type SetupValues } from '../schema'
import { SelectField, SwitchField, TextField, TimezoneField } from '../Fields'
import { OptionalProbe } from './OptionalProbe'

export function AdminStep() {
  const { t } = useTranslation()
  const { languages } = useLanguages()

  return (
    <div className="space-y-4">
      <div className="grid gap-4 md:grid-cols-2">
        <TextField name="admin.username" label={t('setup.admin.username')} autoComplete="username" />
        <TextField name="admin.siteName" label={t('setup.admin.siteName')} />
        <TextField name="admin.password" label={t('setup.admin.password')} type="password" autoComplete="new-password" />
        <TextField name="admin.confirm" label={t('setup.admin.confirm')} type="password" autoComplete="new-password" />
        <SelectField
          name="admin.language"
          label={t('setup.admin.language')}
          options={(languages.length ? languages : [{ code: 'zh-CN', name: '简体中文' }, { code: 'en-US', name: 'English' }]).map((l) => ({ value: l.code, label: l.name }))}
        />
        <TimezoneField name="admin.timezone" label={t('setup.admin.timezone')} />
      </div>
    </div>
  )
}

export function OptionalStep() {
  const { t } = useTranslation()
  const { control } = useFormContext<SetupValues>()
  const optional = useWatch({ control, name: 'optional' })
  return (
    <div className="space-y-6">
      <div className="space-y-4">
        <SelectField name="optional.storage" label={t('setup.optional.storageTitle')} options={[
          { value: 'local', label: t('setup.optional.local') },
          { value: 's3', label: t('setup.optional.s3') },
        ]} />
        {optional.storage === 's3' && (
          <div className="grid gap-4 md:grid-cols-2">
            <TextField name="optional.s3Endpoint" label={t('setup.optional.s3Endpoint')} />
            <TextField name="optional.s3Bucket" label={t('setup.optional.s3Bucket')} />
            <TextField name="optional.s3AccessKey" label={t('setup.optional.s3AccessKey')} autoComplete="off" />
            <TextField name="optional.s3SecretKey" label={t('setup.optional.s3SecretKey')} type="password" autoComplete="new-password" />
            <TextField name="optional.s3Region" label={t('setup.optional.s3Region')} />
            <TextField name="optional.s3Prefix" label={t('setup.optional.s3Prefix')} />
            <SwitchField name="optional.s3PathStyle" label={t('setup.optional.s3PathStyle')} />
            <div className="md:col-span-2">
              <OptionalProbe kind="s3" payload={{
                s3Endpoint: optional.s3Endpoint, s3Bucket: optional.s3Bucket,
                s3AccessKey: optional.s3AccessKey, s3SecretKey: optional.s3SecretKey,
                s3Region: optional.s3Region, s3Prefix: optional.s3Prefix, s3PathStyle: optional.s3PathStyle,
              }} />
            </div>
          </div>
        )}
      </div>
      <div className="space-y-4">
        <SwitchField name="optional.aiEnabled" label={t('setup.optional.aiEnable')} description={t('setup.optional.aiHint')} />
        {optional.aiEnabled && (
          <div className="grid gap-4 md:grid-cols-2">
            <TextField name="optional.aiBaseUrl" label={t('setup.optional.aiBaseUrl')} />
            <TextField name="optional.aiModel" label={t('setup.optional.aiModel')} />
            <TextField name="optional.aiApiKey" label={t('setup.optional.aiApiKey')} type="password" autoComplete="new-password" />
            <div className="md:col-span-2">
              <OptionalProbe kind="ai" payload={{ aiBaseUrl: optional.aiBaseUrl, aiApiKey: optional.aiApiKey, aiModel: optional.aiModel }} />
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export function ConfirmSummary() {
  const { t } = useTranslation()
  const { getValues } = useFormContext<SetupValues>()
  const v = getValues()
  const rows: [string, string][] = [
    [t('setup.db.type'), `${v.db.type} · ${v.db.host}:${v.db.port}`],
    [t('setup.db.database'), `${v.db.database}${v.db.autoCreate ? ` (${t('setup.db.autoCreate')})` : ''}`],
    [t('setup.admin.username'), v.admin.username],
    [t('setup.admin.siteName'), v.admin.siteName],
    [t('setup.admin.language'), `${v.admin.language} · ${v.admin.timezone}`],
    [t('setup.cache.type'), t(`setup.cache.${v.cache.type}`)],
    [t('setup.optional.storageTitle'), t(`setup.optional.${v.optional.storage}`)],
    [t('setup.optional.aiTitle'), v.optional.aiEnabled ? v.optional.aiModel : t('setup.optional.skipped')],
  ]
  return (
    <div className="space-y-3">
      <dl className="divide-y divide-border rounded-lg border border-border text-sm">
        {rows.map(([k, val]) => (
          <div key={k} className="flex flex-col gap-1 px-3 py-2 md:flex-row md:gap-4">
            <dt className="shrink-0 text-muted-foreground md:w-40">{k}</dt>
            <dd className="min-w-0 break-words">{val}</dd>
          </div>
        ))}
      </dl>
      <p className="text-xs text-muted-foreground">{t('setup.defaultsNote')}</p>
    </div>
  )
}
