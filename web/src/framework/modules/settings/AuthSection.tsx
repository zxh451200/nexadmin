import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { FormGrid, FormGridFull } from '@/framework/components/FormLayout'
import { PROVIDER_KEYS, type AllSettings } from './api'
import { CheckField, SelectField, SwitchField, TextField, TextareaField } from './fields'
import { SectionCard } from './SectionCard'

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
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-2 text-sm font-medium text-foreground">{t('settings.auth.basicMethods')}</legend>
            <div className="flex flex-wrap gap-x-6 gap-y-2">
              <CheckField control={form.control} name="account" label={t('settings.auth.account')} />
              <CheckField control={form.control} name="phone" label={t('settings.auth.phone')} />
            </div>
          </fieldset>

          <div className="flex flex-col gap-2">
            <h3 className="text-sm font-medium text-foreground">{t('settings.auth.thirdParty')}</h3>
            {/* 平铺列表 + 分隔线：外层已经有卡片，再给每一行套边框会变成「卡片套卡片」 */}
            <div className="divide-y divide-border border-y border-border">
              {PROVIDER_KEYS.map((key) => {
                const enabled = form.watch(`providers.${key}.enabled`)
                return (
                  <div key={key} className="flex flex-col gap-3 py-3">
                    <SwitchField control={form.control} name={`providers.${key}.enabled`} label={t(`settings.auth.providers.${key}`)} bare />
                    {enabled ? (
                      <div className="rounded-md bg-muted/40 p-3">
                        <FormGrid>
                          <TextField control={form.control} name={`providers.${key}.clientId`} label="Client ID" />
                          <TextField control={form.control} name={`providers.${key}.clientSecret`} label="Client Secret" type="password" />
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
                    ) : null}
                  </div>
                )
              })}
            </div>
          </div>

          <FormGrid>
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
        </>
      )}
    </SectionCard>
  )
}
