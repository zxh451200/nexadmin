import type { ReactNode } from 'react'
import { TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/framework/ui/badge'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import type { ProviderCapabilities } from './api'
import {
  allowedHint,
  currenciesOf,
  methodsOf,
  supportsCurrency,
  supportsMethod,
  unsupportedCurrency,
  unsupportedMethod,
} from './capabilities'

/** 禁用/风险原因的行内说明：不用 tooltip（tooltip 在移动端和键盘路径上等于空转）。 */
export function CapabilityNote({ children, tone = 'muted' }: { children: ReactNode; tone?: 'muted' | 'warn' }) {
  if (!children) return null
  return (
    <p
      className={
        tone === 'warn'
          ? 'flex items-start gap-1.5 text-xs font-medium text-destructive'
          : 'flex items-start gap-1.5 text-xs text-muted-foreground'
      }
    >
      <TriangleAlert aria-hidden="true" className="mt-0.5 h-3.5 w-3.5 shrink-0" />
      <span className="min-w-0 wrap-break-word">{children}</span>
    </p>
  )
}

/** 一项能力的状态徽标：支持 / 不支持（渠道能力矩阵一览用）。 */
export function CapabilityChip({ label, supported }: { label: string; supported: boolean }) {
  return (
    <Badge variant={supported ? 'success' : 'outline'} className="font-normal">
      {label}
      {supported ? '' : ' ✕'}
    </Badge>
  )
}

/**
 * 渠道能力一览（只读）：四个动作 + 币种/支付方式。
 * 能力数据缺失时明确说「未提供」，不假装全都支持。
 */
export function CapabilitySummary({ caps, provider }: { caps: ProviderCapabilities | null; provider: string }) {
  const { t } = useTranslation()
  if (!caps) {
    return (
      <p className="text-xs text-muted-foreground">
        {t('plugin_pay.capabilities.unknown', { provider })}
      </p>
    )
  }
  const actions = [
    { key: 'refundQuery', label: t('plugin_pay.capabilities.actions.refundQuery'), on: caps.refundQuery },
    { key: 'auth', label: t('plugin_pay.capabilities.actions.auth'), on: caps.auth },
    { key: 'bill', label: t('plugin_pay.capabilities.actions.bill'), on: caps.bill },
    { key: 'payout', label: t('plugin_pay.capabilities.actions.payout'), on: caps.payout },
  ] as const
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-1.5">
        {actions.map((a) => (
          <CapabilityChip key={a.key} label={a.label} supported={a.on} />
        ))}
      </div>
      <dl className="grid grid-cols-1 gap-1 text-xs sm:grid-cols-2">
        <div className="min-w-0">
          <dt className="text-muted-foreground">{t('plugin_pay.capabilities.currencies')}</dt>
          <dd className="wrap-break-word text-foreground">{currenciesOf(caps).join(', ') || '-'}</dd>
        </div>
        <div className="min-w-0">
          <dt className="text-muted-foreground">{t('plugin_pay.capabilities.methods')}</dt>
          <dd className="wrap-break-word text-foreground">
            {methodsOf(caps)
              .map((m) => t(`plugin_pay.method.${m}`, { defaultValue: m }))
              .join(', ') || t('plugin_pay.capabilities.noMethod')}
          </dd>
        </div>
      </dl>
    </div>
  )
}

interface CapabilitySelectProps {
  label: ReactNode
  value: string
  onChange: (v: string) => void
  /** 下拉全集：所有渠道的能力并集，具体渠道不支持的项置灰 */
  options: { value: string; label: string }[]
  /** null = 能力未知（不禁用任何项） */
  caps: ProviderCapabilities | null
  provider: string
  kind: 'currency' | 'method'
  placeholder?: string
  disabled?: boolean
}

/**
 * 按渠道能力置灰的币种/支付方式下拉。
 * 不支持的选项保留在列表里但 disabled，并在下方给出行内原因——
 * 让用户看见「为什么不能选」，而不是选了之后被后端拒。
 */
export function CapabilitySelect({
  label,
  value,
  onChange,
  options,
  caps,
  provider,
  kind,
  placeholder,
  disabled,
}: CapabilitySelectProps) {
  const { t } = useTranslation()
  const isCurrency = kind === 'currency'
  const supported = (v: string) => (isCurrency ? supportsCurrency(caps, v) : supportsMethod(caps, v))
  const currentBlocked = !!value && caps !== null && !supported(value)
  const allowed = isCurrency ? currenciesOf(caps) : methodsOf(caps)
  const reason = currentBlocked
    ? isCurrency
      ? unsupportedCurrency(t, provider, value)
      : unsupportedMethod(t, provider, value)
    : ''

  return (
    <label className="flex flex-col gap-1 text-sm">
      {label}
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger aria-label={typeof label === 'string' ? label : undefined}>
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {options.map((o) => {
            const ok = supported(o.value)
            return (
              <SelectItem key={o.value} value={o.value} disabled={!ok}>
                <span className="flex flex-wrap items-center gap-1.5">
                  {o.label}
                  {!ok ? (
                    <span className="text-xs text-muted-foreground">{t('plugin_pay.capabilities.unsupportedSuffix')}</span>
                  ) : null}
                </span>
              </SelectItem>
            )
          })}
        </SelectContent>
      </Select>
      {currentBlocked ? <CapabilityNote tone="warn">{reason}</CapabilityNote> : null}
      {caps ? <p className="text-xs text-muted-foreground">{allowedHint(t, provider, allowed)}</p> : null}
    </label>
  )
}
