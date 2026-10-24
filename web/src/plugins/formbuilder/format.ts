import type { ClosedReason, FormStatus } from './api'

export function formatTime(iso: string | null | undefined, lang: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? ''
    : new Intl.DateTimeFormat(lang, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(d)
}

export function statusVariant(s: FormStatus): 'success' | 'secondary' | 'warning' {
  if (s === 'published') return 'success'
  if (s === 'offline') return 'warning'
  return 'secondary'
}

export function closedVariant(c: ClosedReason): 'success' | 'warning' | 'destructive' | 'secondary' {
  if (c === 'submitted') return 'success'
  if (c === 'expired') return 'destructive'
  if (c === 'full') return 'warning'
  return 'secondary'
}
