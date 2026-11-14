import { useMemo, type ReactNode } from 'react'
import { KeyRound, Smartphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { UseFormReturn } from 'react-hook-form'
import { z } from 'zod'
import { oauthRedirectUri } from '@/framework/auth/oauth'
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
  onEnable,
  panel,
}: {
  control: AnyControl
  name: string
  icon: ReactNode
  enabled: boolean
  label: ReactNode
  description?: ReactNode
  /** 开关打开时的联动（例如把回调地址填成当前站点） */
  onEnable?: () => void
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
        <SwitchField
          control={control}
          name={name}
          bare
          label={label}
          description={description}
          onCheckedChange={(on) => {
            if (on) onEnable?.()
          }}
          className="min-w-0 flex-1"
        />
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
  /** 每行一个完整 URL；与开放平台登记的回调地址精确一致 */
  redirectUris: z.string(),
  /** 仅企业微信 */
  agentId: z.string().trim().optional(),
  /** 仅 OIDC */
  issuer: z.string().trim().optional(),
})

// 开启的 provider 必须当场填全，否则服务端也会拒绝（config.auth.provider_incomplete）：
// 提前在表单里说清楚，比保存后只收到一句报错好。
function authSchema(required: string) {
  return z
    .object({
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
    .superRefine((v, ctx) => {
      const issue = (key: string, field: string) =>
        ctx.addIssue({ code: 'custom', path: ['providers', key, field], message: required })
      for (const key of PROVIDER_KEYS) {
        const p = v.providers[key]
        if (!p.enabled) continue
        if (!p.clientId) issue(key, 'clientId')
        if (!p.redirectUris.trim()) issue(key, 'redirectUris')
        if (key === 'wecom' && !p.agentId) issue(key, 'agentId')
        if (key === 'oidc' && !p.issuer) issue(key, 'issuer')
      }
    })
}

export function AuthSection({ value }: { value: AllSettings['auth'] }) {
  const { t } = useTranslation()
  const schema = useMemo(() => authSchema(t('settings.auth.required')), [t])
  return (
    <SectionCard section="auth" value={value} schema={schema}>
      {(form) => <AuthFields form={form} />}
    </SectionCard>
  )
}

// 表单本身跨多个 provider 动态字段，控制面类型放宽（与 SectionCard 的 children 签名一致）
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AuthForm = UseFormReturn<any>

function AuthFields({ form }: { form: AuthForm }) {
  const { t } = useTranslation()
  // 打开开关时把回调地址填成当前站点的默认值（授权完成后前端回跳页），管理员按需改成对外域名。
  // 只在「打开」这一下补空值：已经填过（含 config.yaml 回填、或用户自己改过）就保持原样。
  const fillRedirect = (key: string) => {
    const cur = form.getValues(`providers.${key}.redirectUris`)
    if (!String(cur ?? '').trim()) {
      form.setValue(`providers.${key}.redirectUris`, oauthRedirectUri(key), { shouldValidate: false })
    }
  }

  return (
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
                onEnable={() => fillRedirect(key)}
                panel={
                  enabled ? (
                    <div className={cn('rounded-lg border border-border/60 bg-muted/50 p-3', INDENT)}>
                      <FormGrid cols={2}>
                        <TextField
                          control={form.control}
                          name={`providers.${key}.clientId`}
                          label={t(`settings.auth.credentials.${key}.id`)}
                        />
                        <TextField
                          control={form.control}
                          name={`providers.${key}.clientSecret`}
                          label={t(`settings.auth.credentials.${key}.secret`)}
                          type="password"
                          description={t('settings.auth.fields.secretHint')}
                        />
                        {key === 'wecom' ? (
                          <FormGridFull>
                            <TextField
                              control={form.control}
                              name="providers.wecom.agentId"
                              label={t('settings.auth.fields.agentId')}
                            />
                          </FormGridFull>
                        ) : null}
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
                        <FormGridFull>
                          <TextareaField
                            control={form.control}
                            name={`providers.${key}.redirectUris`}
                            label={t('settings.auth.fields.redirectUris')}
                            description={t('settings.auth.fields.redirectUrisHint', { url: oauthRedirectUri(key) })}
                            placeholder={oauthRedirectUri(key)}
                          />
                        </FormGridFull>
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
  )
}
