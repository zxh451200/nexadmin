export function formatTime(iso: string | null | undefined, lang: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? ''
    : new Intl.DateTimeFormat(lang, { hour: '2-digit', minute: '2-digit', hour12: false }).format(d)
}

export function statusVariant(s: string): 'success' | 'secondary' {
  return s === 'open' ? 'success' : 'secondary'
}
