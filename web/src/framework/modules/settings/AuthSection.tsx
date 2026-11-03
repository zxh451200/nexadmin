import type { ReactNode } from 'react'
import { KeyRound, Smartphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { ProviderIcon } from '@/framework/components/BrandIcon'
import { FormGrid, FormGridFull } from '@/framework/components/FormLayout'
import { cn } from '@/lib/utils'
import { PROVIDER_KEYS, type AllSettings } from './api'
import { SelectField, SwitchField, TextField, TextareaField, type AnyControl } from './fields'
import { SectionCard } from './SectionCard'

// 底座 size-9 + 间距 gap-3：展开的配置面板按同一宽度缩进，读起来才是「这个方式的下级」
const INDENT = 'md:ms-12'

/**
 * 一条登录方式：图标底座在左，名称与说明同列排布，开关贴右边缘。
 * 底座启用时点亮成品牌色——扫一眼左列就知道开了哪些，不用逐个去看右侧开关。
 */
function MethodRow({
  control,
  name,
  icon,
  enabled,
  label,
  description,
  panel,
}: {
  control: AnyControl
  name: string
  icon: ReactNode
  enabled: boolean
  label: ReactNode
  description?: ReactNode
  panel?: ReactNode
}) {
  return (
    <div className="flex flex-col gap-3 rounded-lg px-2 py-2 transition-colors hover:bg-muted/40">
      <div className="flex items-center gap-3">
        <span
          aria-hidden="true"
          className={cn(
            'flex size-9 shrink-0 items-center justify-center rounded-lg border transition-colors',
            enabled ? 'border-primary/30 bg-primary/10 text-primary' : 'border-border text-muted-foreground',
          )}
        >
          {icon}
        </span>
        <SwitchField control={control} name={name} bare label={label} description={description} className="min-w-0 flex-1" />
      </div>
      {panel}
    </div>
  )
}

/** 分组标题：说明跟在标题右侧；组与组之间只靠留白分开，不画通栏分隔线 */
function GroupLegend({ title, hint }: { title: string; hint?: string }) {
  return (
    <legend className="mb-1.5 text-sm font-semibold text-foreground">
      {title}
      {hint ? <span className="ms-2 text-xs font-normal text-muted-foreground">{hint}</span> : null}
    </legend>
  )
}

const providerSchema = z.object({
  enabled: z.boolean(),
  clientId: z.string().trim(),
  clientSecret: z.string(),
  issuer: z.string().trim().optional(),
})

const authSchema = z.object({
  account: z.boolean(),
  phone: z.boolean(),
  providers: z.object({
    wecom: providerSchema,
    dingtalk: providerSchema,
    feishu: providerSchema,
    google: providerSchema,
    github: providerSchema,
    oidc: providerSchema,
  }),
  matchBy: z.enum(['phone', 'email', 'employeeNo']),
  autoCreate: z.boolean(),
  allowedDomains: z.string(),
  proxy: z.string().trim(),
})

export function AuthSection({ value }: { value: AllSettings['auth'] }) {
  const { t } = useTranslation()
  return (
    <SectionCard section="auth" value={value} schema={authSchema}>
      {(form) => (
        <>
          <fieldset className="flex flex-col gap-1">
            <GroupLegend title={t('settings.auth.basicMethods')} />
            <div className="-mx-2">
              <MethodRow
                control={form.control}
                name="account"
                icon={<KeyRound className="size-[18px]" />}
                enabled={form.watch('account')}
                label={t('settings.auth.account')}
              />
              <MethodRow
                control={form.control}
                name="phone"
                icon={<Smartphone className="size-[18px]" />}
                enabled={form.watch('phone')}
                label={t('settings.auth.phone')}
              />
            </div>
          </fieldset>

          <fieldset className="flex flex-col gap-1">
            <GroupLegend title={t('settings.auth.thirdParty')} hint={t('settings.auth.thirdPartyHint')} />
            <div className="-mx-2">
              {PROVIDER_KEYS.map((key) => {
                const enabled = form.watch(`providers.${key}.enabled`)
                return (
                  <MethodRow
                    key={key}
                    control={form.control}
                    name={`providers.${key}.enabled`}
                    icon={<ProviderIcon provider={key} className="size-[18px]" />}
                    enabled={enabled}
                    label={t(`settings.auth.providers.${key}`)}
                    description={t(`settings.auth.providerHints.${key}`)}
                    panel={
                      enabled ? (
                        <div className={cn('rounded-lg border border-border/60 bg-muted/50 p-3', INDENT)}>
                          <FormGrid cols={2}>
                            <TextField control={form.control} name={`providers.${key}.clientId`} label="Client ID" />
                            <TextField
                              control={form.control}
                              name={`providers.${key}.clientSecret`}
                              label="Client Secret"
                              type="password"
                            />
                            {key === 'oidc' ? (
                              <FormGridFull>
                                <TextField
                                  control={form.control}
                                  name="providers.oidc.issuer"
                                  label="Issuer"
                                  type="url"
                                  inputMode="url"
                                  placeholder="https://idp.example.com"
                                />
                              </FormGridFull>
                            ) : null}
                          </FormGrid>
                        </div>
                      ) : null
                    }
                  />
                )
              })}
            </div>
          </fieldset>

          <fieldset className="flex flex-col gap-3">
            <GroupLegend title={t('settings.auth.matchLimit')} hint={t('settings.auth.matchLimitHint')} />
            <FormGrid cols={2}>
              <SelectField
                control={form.control}
                name="matchBy"
                label={t('settings.auth.matchBy')}
                description={t('settings.auth.matchByHint')}
                options={[
                  { value: 'phone', label: t('settings.auth.matchPhone') },
                  { value: 'email', label: t('settings.auth.matchEmail') },
                  { value: 'employeeNo', label: t('settings.auth.matchEmployeeNo') },
                ]}
              />
              <TextField
                control={form.control}
                name="proxy"
                label={t('settings.auth.proxy')}
                description={t('settings.auth.proxyHint')}
                type="url"
                inputMode="url"
                placeholder="http://proxy.example.com:8080"
              />
              <FormGridFull>
                <SwitchField
                  control={form.control}
                  name="autoCreate"
                  label={t('settings.auth.autoCreate')}
                  description={t('settings.auth.autoCreateHint')}
                />
              </FormGridFull>
              <FormGridFull>
                <TextareaField
                  control={form.control}
                  name="allowedDomains"
                  label={t('settings.auth.allowedDomains')}
                  description={t('settings.auth.allowedDomainsHint')}
                  placeholder="example.com, corp.example.com"
                />
              </FormGridFull>
            </FormGrid>
          </fieldset>
        </>
      )}
    </SectionCard>
  )
}
