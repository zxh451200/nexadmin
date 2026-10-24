import { useTranslation } from 'react-i18next'
import type { FieldChange } from './api'
import { displayValue } from './format'

function Value({ v, tone }: { v: unknown; tone: 'old' | 'new' }) {
  const { t } = useTranslation()
  const cls =
    tone === 'old'
      ? 'border-destructive/30 bg-destructive/10 text-destructive'
      : 'border-success/30 bg-success/10 text-success'
  if (v === null || v === undefined) {
    return <span className="rounded-md border border-border bg-muted/40 px-2 py-1 text-xs italic text-muted-foreground">{t('plugin_audit.diff.empty')}</span>
  }
  const text = displayValue(v)
  return (
    <span className={`block whitespace-pre-wrap break-all rounded-md border px-2 py-1 font-mono text-xs ${cls}`}>
      {text === '' ? t('plugin_audit.diff.emptyString') : text}
    </span>
  )
}

/** 字段级对比：旧值红、新值绿（语义色 token，暗色主题同样可读）。 */
export function DiffView({ changes }: { changes: Record<string, FieldChange> }) {
  const { t } = useTranslation()
  const names = Object.keys(changes).sort()
  if (names.length === 0) return <p className="text-sm text-muted-foreground">{t('plugin_audit.diff.none')}</p>
  return (
    <div className="flex flex-col gap-2" role="table" aria-label={t('plugin_audit.diff.title')}>
      <div className="hidden grid-cols-[minmax(7rem,12rem)_1fr_1fr] gap-3 px-1 text-xs text-muted-foreground sm:grid" role="row">
        <span role="columnheader">{t('plugin_audit.diff.field')}</span>
        <span role="columnheader">{t('plugin_audit.diff.old')}</span>
        <span role="columnheader">{t('plugin_audit.diff.new')}</span>
      </div>
      {names.map((n) => (
        <div key={n} className="grid grid-cols-1 gap-1 rounded-md border border-border p-2 sm:grid-cols-[minmax(7rem,12rem)_1fr_1fr] sm:gap-3 sm:border-0 sm:p-1" role="row">
          <span className="break-all font-mono text-xs font-medium text-foreground" role="cell">{n}</span>
          <div role="cell">
            <span className="mb-0.5 block text-[11px] text-muted-foreground sm:hidden">{t('plugin_audit.diff.old')}</span>
            <Value v={changes[n].old} tone="old" />
          </div>
          <div role="cell">
            <span className="mb-0.5 block text-[11px] text-muted-foreground sm:hidden">{t('plugin_audit.diff.new')}</span>
            <Value v={changes[n].new} tone="new" />
          </div>
        </div>
      ))}
    </div>
  )
}
