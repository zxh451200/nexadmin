import type { AuditAction } from './api'

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

export function actionVariant(a: AuditAction): 'success' | 'info' | 'destructive' {
  if (a === 'create') return 'success'
  if (a === 'delete') return 'destructive'
  return 'info'
}

/** 把任意字段值转成可展示的文本；null 单独处理，不在这里转。 */
export function displayValue(v: unknown): string {
  if (typeof v === 'string') return v
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}
