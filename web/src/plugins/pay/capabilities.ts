import { useMemo } from 'react'
import { useCapabilities, useChannels, type PayProviderCapability, type ProviderCapabilities } from './api'

/**
 * 渠道能力矩阵的前端侧解析。
 *
 * 纪律：前端禁用只是体验，后端对同一个动作还会独立判一次（能力不支持时报
 * PAY_CAPABILITY_UNSUPPORTED）。因此「能力数据没取到」时一律放行——
 * 未知不等于不支持，据此禁用会把可用渠道一起锁死。
 */

/** 后端 capabilities 里币种为空＝只收 CNY（与 handler 的兜底口径一致）。 */
export const DEFAULT_CURRENCIES = ['CNY']

/** 界面有译名的支付方式取值；能力矩阵里出现其它取值时按原文显示。 */
export const KNOWN_METHODS = ['native', 'qr', 'page', 'wap', 'h5', 'jsapi', 'app', 'micropay']

/** 四个动作型能力（对应后端 Capabilities 的布尔位）。 */
export type CapabilityAction = 'refundQuery' | 'auth' | 'bill' | 'payout'

export type Translate = (key: string, options?: Record<string, unknown>) => string

export interface CapabilityIndex {
  byProvider: Record<string, ProviderCapabilities>
  /** 矩阵是否真的取回来了：false 时所有 supports* 一律返回 true（不做禁用） */
  known: boolean
  isLoading: boolean
  isError: boolean
}

export function indexCapabilities(list: PayProviderCapability[] | undefined): Record<string, ProviderCapabilities> {
  const byProvider: Record<string, ProviderCapabilities> = {}
  for (const row of list ?? []) {
    if (!row?.provider) continue
    byProvider[row.provider] = {
      currencies: row.capabilities?.currencies ?? [],
      methods: row.capabilities?.methods ?? [],
      refundQuery: !!row.capabilities?.refundQuery,
      auth: !!row.capabilities?.auth,
      bill: !!row.capabilities?.bill,
      payout: !!row.capabilities?.payout,
    }
  }
  return byProvider
}

/** 一次取回、按 provider 索引；渠道表单与各动作按钮共用同一份数据。 */
export function useCapabilityIndex(): CapabilityIndex {
  const { data, isLoading, isError } = useCapabilities()
  const byProvider = useMemo(() => indexCapabilities(data), [data])
  return { byProvider, known: !!data && data.length > 0, isLoading, isError }
}

/** 渠道实例列表 + 按编码查 provider，供各页签共用（避免每处重复 useChannels）。 */
export function useChannelDirectory() {
  const { data: channels = [], ...rest } = useChannels()
  const providerOf = useMemo(() => {
    const map = new Map<string, string>()
    channels.forEach((c) => map.set(c.code, c.provider))
    return (channelCode?: string) => (channelCode ? map.get(channelCode) ?? '' : '')
  }, [channels])
  return { channels, providerOf, ...rest }
}

/** 取某渠道类型的能力；矩阵里没有该类型（或未取到）返回 null＝未知。 */
export function capabilitiesOfProvider(index: CapabilityIndex, provider?: string): ProviderCapabilities | null {
  if (!index.known || !provider) return null
  return index.byProvider[provider] ?? null
}

/** 取某渠道实例（按渠道编码）的能力。 */
export function capabilitiesOfChannel(
  index: CapabilityIndex,
  providerOf: (channelCode?: string) => string,
  channelCode?: string,
): ProviderCapabilities | null {
  if (!channelCode) return null
  return capabilitiesOfProvider(index, providerOf(channelCode))
}

export function currenciesOf(caps: ProviderCapabilities | null): string[] {
  const list = caps?.currencies?.length ? caps.currencies : DEFAULT_CURRENCIES
  return [...new Set(list)]
}

export function methodsOf(caps: ProviderCapabilities | null): string[] {
  return [...new Set(caps?.methods ?? [])]
}

/** 下拉的全集：所有渠道能力的并集，落到具体渠道时不支持项置灰。 */
export function allCurrencies(index: CapabilityIndex): string[] {
  const set = new Set(DEFAULT_CURRENCIES)
  Object.values(index.byProvider).forEach((c) => c.currencies.forEach((x) => set.add(x)))
  return [...set].sort()
}

export function allMethods(index: CapabilityIndex): string[] {
  const set = new Set<string>(KNOWN_METHODS)
  Object.values(index.byProvider).forEach((c) => c.methods.forEach((x) => set.add(x)))
  return [...set]
}

export function supportsCurrency(caps: ProviderCapabilities | null, currency: string): boolean {
  if (!caps) return true
  return currenciesOf(caps).includes(currency.trim().toUpperCase())
}

export function supportsMethod(caps: ProviderCapabilities | null, method: string): boolean {
  if (!caps) return true
  return caps.methods.includes(method)
}

export function supportsAction(caps: ProviderCapabilities | null, action: CapabilityAction): boolean {
  if (!caps) return true
  return caps[action]
}

/** 当前渠道下第一个可用币种：切换渠道后把不合法的默认值收敛掉。 */
export function firstCurrency(caps: ProviderCapabilities | null): string {
  return currenciesOf(caps)[0] ?? 'CNY'
}

export function providerDisplayName(t: Translate, provider: string): string {
  return t(`plugin_pay.providers.${provider}`, { defaultValue: provider || '-' })
}

export function actionLabel(t: Translate, action: CapabilityAction): string {
  return t(`plugin_pay.capabilities.actions.${action}`)
}

/** 禁用原因文案：动作类——明确说「该渠道不支持」，并提示后端同样会拒。 */
export function unsupportedAction(t: Translate, provider: string, action: CapabilityAction): string {
  return t('plugin_pay.capabilities.providerUnsupported', {
    provider: providerDisplayName(t, provider),
    action: actionLabel(t, action),
  })
}

/** 禁用原因文案：币种类（可选值另由 allowedHint 给出）。 */
export function unsupportedCurrency(t: Translate, provider: string, currency: string): string {
  return t('plugin_pay.capabilities.unsupportedCurrency', {
    provider: providerDisplayName(t, provider),
    currency,
  })
}

/** 禁用原因文案：支付方式类。 */
export function unsupportedMethod(t: Translate, provider: string, method: string): string {
  return t('plugin_pay.capabilities.unsupportedMethod', {
    provider: providerDisplayName(t, provider),
    method: t(`plugin_pay.method.${method}`, { defaultValue: method }),
  })
}

/** 币种/支付方式的可选值提示（禁用项旁的可读原因）。 */
export function allowedHint(t: Translate, provider: string, options: string[]): string {
  return options.length
    ? t('plugin_pay.capabilities.allowedOnly', {
        provider: providerDisplayName(t, provider),
        options: options.join(', '),
      })
    : t('plugin_pay.capabilities.noneAdvertised', { provider: providerDisplayName(t, provider) })
}
