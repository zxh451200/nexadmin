import { useTranslation } from 'react-i18next'
import { DateRangePicker } from '@/framework/components/DateRangePicker'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useFilterOptions, type FilterValues, type ReportFilter } from './api'
import { builtinText } from './builtinText'

const ALL = '__all'

function SelectFilter({ reportId, f, value, onChange }: { reportId: string; f: ReportFilter; value: unknown; onChange: (v: unknown) => void }) {
  const { t } = useTranslation()
  const { data: options = [] } = useFilterOptions(reportId, f.key)
  if (f.multiple) {
    const picked = Array.isArray(value) ? (value as (string | number)[]) : []
    return (
      <div role="group" aria-label={builtinText(t, f.label)} className="flex flex-wrap items-center gap-1.5">
        {options.map((o) => {
          const on = picked.includes(o)
          return (
            <Button key={String(o)} type="button" size="sm" variant={on ? 'default' : 'outline'} aria-pressed={on} onClick={() => onChange(on ? picked.filter((x) => x !== o) : [...picked, o])}>
              {String(o)}
            </Button>
          )
        })}
      </div>
    )
  }
  return (
    <Select value={typeof value === 'string' && value ? value : ALL} onValueChange={(v) => onChange(v === ALL ? '' : v)}>
      <SelectTrigger className="md:w-44" aria-label={builtinText(t, f.label)}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{t('plugin_report.report.allOption', { label: builtinText(t, f.label) })}</SelectItem>
        {options.map((o) => (
          <SelectItem key={String(o)} value={String(o)}>
            {String(o)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

interface Props {
  reportId: string
  filters: ReportFilter[]
  values: FilterValues
  onChange: (next: FilterValues) => void
}

/** 报表筛选器条：日期范围 / 下拉。取值只是「筛选器 key -> 值」，列与操作符由报表配置决定，服务端据此作用于绑定同一数据集的图表。 */
export function ReportFilters({ reportId, filters, values, onChange }: Props) {
  const { t } = useTranslation()
  if (filters.length === 0) return null
  const set = (k: string, v: unknown) => onChange({ ...values, [k]: v })
  return (
    <Card className="flex flex-wrap items-center gap-3 p-3">
      {filters.map((f) => (
        <div key={f.key} className="flex flex-wrap items-center gap-2">
          <span className="text-sm text-muted-foreground">{builtinText(t, f.label)}</span>
          {f.type === 'dateRange' ? (
            <DateRangePicker
              className="md:w-64"
              value={Array.isArray(values[f.key]) ? { from: (values[f.key] as string[])[0], to: (values[f.key] as string[])[1] } : null}
              onChange={(r) => set(f.key, r.from || r.to ? [r.from ?? '', r.to ?? ''] : undefined)}
            />
          ) : (
            <SelectFilter reportId={reportId} f={f} value={values[f.key]} onChange={(v) => set(f.key, v)} />
          )}
        </div>
      ))}
      <Button variant="ghost" size="sm" className="ms-auto" onClick={() => onChange({})}>
        {t('common.reset')}
      </Button>
    </Card>
  )
}
