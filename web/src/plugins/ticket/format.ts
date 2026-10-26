export function formatTime(iso: string | null | undefined, lang: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? ''
    : new Intl.DateTimeFormat(lang, {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      }).format(d)
}

export function statusVariant(s: string): 'success' | 'destructive' | 'info' | 'secondary' {
  if (s === 'processing') return 'info'
  if (s === 'resolved') return 'success'
  if (s === 'open') return 'destructive'
  return 'secondary'
}

export function priorityVariant(p: string): 'success' | 'destructive' | 'info' | 'secondary' {
  if (p === 'urgent') return 'destructive'
  if (p === 'high') return 'info'
  if (p === 'low') return 'secondary'
  return 'success'
}
