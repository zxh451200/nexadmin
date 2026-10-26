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
  if (s === 'approved') return 'success'
  if (s === 'rejected') return 'destructive'
  if (s === 'running') return 'info'
  return 'secondary'
}

export function actionLabel(action: string): string {
  switch (action) {
    case 'start':
      return '发起'
    case 'approve':
      return '同意'
    case 'reject':
      return '驳回'
    case 'countersign':
      return '加签'
    case 'rollback':
      return '回退'
    case 'escalate':
      return '升级'
    case 'withdraw':
      return '撤回'
    default:
      return action
  }
}
