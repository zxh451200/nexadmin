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

export function statusVariant(s: string): 'success' | 'destructive' | 'info' | 'warning' | 'secondary' {
  if (s === 'processing') return 'info'
  if (s === 'pending') return 'warning'
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

// 队列行里的「圆点 + 灰字」只要颜色语义，不直接用 Badge variant：两处（状态/优先级）
// 都从同一张映射表取色，避免药丸版和圆点版对同一个状态给出不同的颜色。
const TONE_OF: Record<'success' | 'destructive' | 'info' | 'warning' | 'secondary', 'success' | 'destructive' | 'info' | 'warning' | 'neutral'> = {
  success: 'success',
  destructive: 'destructive',
  info: 'info',
  warning: 'warning',
  secondary: 'neutral',
}
export function statusTone(s: string) {
  return TONE_OF[statusVariant(s)]
}
export function priorityTone(p: string) {
  return TONE_OF[priorityVariant(p)]
}
