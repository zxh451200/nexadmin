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

export function formatMoney(minor: number, currency: string): string {
  return `${currency} ${(minor / 100).toFixed(2)}`
}

export function payStatusVariant(s: string): 'success' | 'destructive' | 'info' | 'secondary' {
  if (s === 'paid') return 'success'
  if (s === 'failed') return 'destructive'
  if (s === 'pending' || s === 'refunding') return 'info'
  return 'secondary'
}

export function refundStatusVariant(s: string): 'success' | 'destructive' | 'info' | 'secondary' {
  if (s === 'succeeded') return 'success'
  if (s === 'pending') return 'info'
  return 'destructive'
}
