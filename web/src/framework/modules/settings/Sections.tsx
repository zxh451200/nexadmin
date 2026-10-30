import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Info, TriangleAlert } from 'lucide-react'
import { Can } from '@/framework/auth/Can'
import { ImageUrlField } from '@/framework/components/ImageUrlField'
import { FormGrid, FormGridFull } from '@/framework/components/FormLayout'
import { useLanguages } from '@/framework/i18n/useLanguages'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/framework/ui/card'
import { FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { ATTEMPT_OPTIONS, IDLE_MINUTE_OPTIONS, LOCK_MINUTE_OPTIONS, SESSION_HOUR_OPTIONS, type AllSettings } from './api'
import { RadioField, SelectField, SwitchField, TextField } from './fields'
import { UploadLimitCard } from '@/framework/modules/config/UploadLimitCard'
import { SectionCard } from './SectionCard'

const str = z.string().trim()

const siteSchema = z.object({
  siteName: str.min(1).max(60),
  logoUrl: z.string(),
  copyright: str.max(200),
  defaultLanguage: z.string().min(1),
  watermark: z.boolean(),
})

export function SiteSection({ value }: { value: AllSettings['site'] }) {
  const { t } = useTranslation()
  const { languages } = useLanguages()
  return (
    <SectionCard section="site" value={value} schema={siteSchema}>
      {(form) => (
        <FormGrid>
          <TextField control={form.control} name="siteName" label={t('settings.site.siteName')} />
          <TextField control={form.control} name="copyright" label={t('settings.site.copyright')} />
          <SelectField
            control={form.control}
            name="defaultLanguage"
            label={t('settings.site.defaultLanguage')}
            options={languages.map((l) => ({ value: l.code, label: l.name }))}
          />
          <FormGridFull>
            <FormField
              control={form.control}
              name="logoUrl"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('settings.site.logo')}</FormLabel>
                  <ImageUrlField
                    value={field.value}
                    onChange={field.onChange}
                    title={t('settings.site.logoUpload')}
                    hint={t('settings.site.logoUploadHint')}
                    placeholder={t('settings.site.logoPlaceholder')}
                    previewClassName="h-16 w-40"
                  />
                  <FormMessage />
                </FormItem>
              )}
            />
          </FormGridFull>
          <FormGridFull>
            <SwitchField
              control={form.control}
              name="watermark"
              label={t('settings.site.watermark')}
              description={t('settings.site.watermarkHint')}
            />
          </FormGridFull>
        </FormGrid>
      )}
    </SectionCard>
  )
}

const securitySchema = z.object({
  maxAttempts: z.enum(ATTEMPT_OPTIONS),
  lockMinutes: z.enum(LOCK_MINUTE_OPTIONS),
  idleMinutes: z.enum(IDLE_MINUTE_OPTIONS),
  maxHours: z.enum(SESSION_HOUR_OPTIONS),
})

export function SecuritySection({ value }: { value: AllSettings['security'] }) {
  const { t } = useTranslation()
  return (
    <SectionCard section="security" value={value} schema={securitySchema}>
      {(form) => (
        <FormGrid>
          <SelectField
            control={form.control}
            name="maxAttempts"
            label={t('settings.security.maxAttempts')}
            description={t('settings.security.maxAttemptsHint')}
            options={ATTEMPT_OPTIONS.map((v) => ({ value: v, label: t('settings.security.times', { count: Number(v) }) }))}
          />
          <SelectField
            control={form.control}
            name="lockMinutes"
            label={t('settings.security.lockMinutes')}
            description={t('settings.security.lockMinutesHint')}
            options={LOCK_MINUTE_OPTIONS.map((v) => ({ value: v, label: t('settings.security.minutes', { count: Number(v) }) }))}
          />
          <SelectField
            control={form.control}
            name="idleMinutes"
            label={t('settings.security.idleMinutes')}
            description={t('settings.security.idleMinutesHint')}
            options={IDLE_MINUTE_OPTIONS.map((v) => ({
              value: v,
              label: v === '0' ? t('settings.security.unlimited') : t('settings.security.minutes', { count: Number(v) }),
            }))}
          />
          <SelectField
            control={form.control}
            name="maxHours"
            label={t('settings.security.maxHours')}
            description={t('settings.security.maxHoursHint')}
            options={SESSION_HOUR_OPTIONS.map((v) => ({
              value: v,
              label: v === '0' ? t('settings.security.unlimited') : t('settings.security.hours', { count: Number(v) }),
            }))}
          />
        </FormGrid>
      )}
    </SectionCard>
  )
}

const cacheSchema = z.object({
  type: z.enum(['memory', 'valkey']),
  address: z.string().trim(),
  password: z.string(),
})

export function CacheSection({ value }: { value: AllSettings['cache'] }) {
  const { t } = useTranslation()
  return (
    <SectionCard section="cache" value={value} schema={cacheSchema} testable>
      {(form) => (
        <>
          <RadioField
            control={form.control}
            name="type"
            label={t('settings.cache.type')}
            options={[
              { value: 'memory', label: t('settings.cache.memory') },
              { value: 'valkey', label: 'Valkey' },
            ]}
          />
          {form.watch('type') === 'valkey' ? (
            <FormGrid>
              <TextField control={form.control} name="address" label={t('settings.cache.address')} placeholder="127.0.0.1:6379" />
              <TextField control={form.control} name="password" label={t('settings.cache.password')} type="password" />
            </FormGrid>
          ) : null}
        </>
      )}
    </SectionCard>
  )
}

/**
 * 文件存储只读：后端只读 config/config.yaml 的 storage.*（向导 / `init --config` 写入），
 * 这里保存的值不会被任何地方读取，所以不提供编辑，免得“保存成功却不生效”。
 */
export function StorageSection({ value }: { value: AllSettings['storage'] }) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>{t('settings.storage.title')}</CardTitle>
          <CardDescription>{t('settings.storage.description')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Alert variant="info">
            <Info aria-hidden="true" />
            <AlertDescription>{t('settings.storage.readonlyHint')}</AlertDescription>
          </Alert>
          <p className="text-sm text-muted-foreground">
            {t('settings.storage.type')}：{value.type === 's3' ? t('settings.storage.s3') : t('settings.storage.local')}
          </p>
        </CardContent>
      </Card>
      {/* 上传限制是文件存储策略的一部分，和「参数」这种键值表不是一类东西，搬到这里更符合直觉 */}
      <Can perm="config:list">
        <UploadLimitCard />
      </Can>
    </div>
  )
}

const aiSchema = z.object({
  enabled: z.boolean(),
  baseUrl: z.string().trim(),
  apiKey: z.string(),
  model: z.string().trim(),
  timeoutSec: z.number().int().min(1).max(600),
  redact: z.boolean(),
})

export function AiSection({ value }: { value: AllSettings['ai'] }) {
  const { t } = useTranslation()
  return (
    <SectionCard section="ai" value={value} schema={aiSchema} testable>
      {(form) => (
        <>
          <Alert variant="info">
            <Info aria-hidden="true" />
            <AlertDescription>{t('settings.ai.hint')}</AlertDescription>
          </Alert>
          <SwitchField control={form.control} name="enabled" label={t('settings.ai.enabled')} />
          <SwitchField control={form.control} name="redact" label={t('settings.ai.redact')} description={t('settings.ai.redactHint')} />
          <FormGrid>
            <TextField control={form.control} name="baseUrl" label="Base URL" type="url" inputMode="url" />
            <TextField control={form.control} name="apiKey" label="API Key" type="password" />
            <TextField control={form.control} name="model" label={t('settings.ai.model')} />
            <TextField control={form.control} name="timeoutSec" label={t('settings.ai.timeoutSec')} type="number" inputMode="numeric" />
          </FormGrid>
        </>
      )}
    </SectionCard>
  )
}

const smsSchema = z.object({
  provider: z.enum(['none', 'aliyun', 'tencent', 'log']),
  accessKey: z.string().trim(),
  secretKey: z.string(),
  signName: z.string().trim(),
  templateCode: z.string().trim(),
})

export function SmsSection({ value }: { value: AllSettings['sms'] }) {
  const { t } = useTranslation()
  return (
    <SectionCard section="sms" value={value} schema={smsSchema} testable>
      {(form) => {
        const provider = form.watch('provider')
        return (
          <>
            <SelectField
              control={form.control}
              name="provider"
              label={t('settings.sms.provider')}
              options={[
                { value: 'none', label: t('settings.sms.none') },
                { value: 'aliyun', label: t('settings.sms.aliyun') },
                { value: 'tencent', label: t('settings.sms.tencent') },
                { value: 'log', label: t('settings.sms.log') },
              ]}
            />
            {provider === 'log' ? (
              <Alert variant="warning">
                <TriangleAlert aria-hidden="true" />
                <AlertDescription>{t('settings.sms.logWarning')}</AlertDescription>
              </Alert>
            ) : null}
            {provider === 'aliyun' || provider === 'tencent' ? (
              <FormGrid>
                <TextField control={form.control} name="accessKey" label="Access Key" />
                <TextField control={form.control} name="secretKey" label="Secret Key" type="password" />
                <TextField control={form.control} name="signName" label={t('settings.sms.signName')} />
                <TextField control={form.control} name="templateCode" label={t('settings.sms.templateCode')} />
              </FormGrid>
            ) : null}
          </>
        )
      }}
    </SectionCard>
  )
}
