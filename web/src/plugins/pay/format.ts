export function formatTime(iso: string | null | undefined, lang: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? ''
    : new Intl.DateTimeFormat(lang, {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      }).format(d)
}

const moneyFormatter = new Intl.NumberFormat(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const moneyFormatter0 = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 })

/** ISO 4217 零小数位币种：渠道侧 minor 就是主单位（1 JPY = 1 minor），÷100 显示会差 100 倍。 */
const ZERO_DECIMAL_CURRENCIES = new Set(['JPY', 'KRW', 'VND', 'CLP', 'ISK', 'XAF', 'XOF', 'XPF'])

/** 币种的小数位（0 或 2）：录入端「元 → minor」换算也用它，与显示同一条规则。 */
export function currencyDecimals(currency: string): number {
  return ZERO_DECIMAL_CURRENCIES.has(currency.toUpperCase()) ? 0 : 2
}

/** 用户输入的主单位金额 → minor（录入端统一走这里，别再手写 ×100）。 */
export function toMinor(amount: number, currency: string): number {
  const d = currencyDecimals(currency)
  return Math.round(amount * 10 ** d)
}

/** 只有金额、没有币种时使用（对账/结算等跨单汇总场景）——默认按两位小数处理。 */
export function formatAmount(minor: number): string {
  return moneyFormatter.format(minor / 100)
}

export function formatMoney(minor: number, currency: string): string {
  const f = currencyDecimals(currency) === 0 ? moneyFormatter0 : moneyFormatter
  return `${currency} ${f.format(minor)}`
}

export function formatBp(bp: number): string {
  return `${(bp / 100).toFixed(2)}%`
}

export function payStatusVariant(s: string): 'success' | 'destructive' | 'info' | 'secondary' {
  if (s === 'succeeded') return 'success'
  if (s === 'failed') return 'destructive'
  if (s === 'pending' || s === 'refunding') return 'info'
  return 'secondary'
}

export function refundStatusVariant(s: string): 'success' | 'destructive' | 'info' | 'secondary' {
  if (s === 'succeeded') return 'success'
  if (s === 'pending') return 'info'
  return 'destructive'
}
