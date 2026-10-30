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

/** 只有金额、没有币种时使用（对账/结算等跨单汇总场景）。 */
export function formatAmount(minor: number): string {
  return moneyFormatter.format(minor / 100)
}

export function formatMoney(minor: number, currency: string): string {
  return `${currency} ${formatAmount(minor)}`
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
