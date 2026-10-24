import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import type { Filter, SourceCol } from './api'
import { formatFilterValue, opsFor, parseFilterValue } from './chartData'

interface Props {
  columns: SourceCol[]
  value: Filter[]
  onChange: (next: Filter[]) => void
  max?: number
}

/** 过滤条件编辑器：列、操作符都从白名单元数据里选，值是文本（in 用逗号、between 用 ~ 分隔）。没有任何自由 SQL 输入。 */
export function FilterEditor({ columns, value, onChange, max = 10 }: Props) {
  const { t } = useTranslation()
  const colOf = (name: string) => columns.find((c) => c.name === name)
  const set = (i: number, patch: Partial<Filter>) => onChange(value.map((f, j) => (j === i ? { ...f, ...patch } : f)))

  return (
    <div className="flex flex-col gap-2">
      {value.map((f, i) => {
        const col = colOf(f.column)
        const ops = col ? opsFor(col.type) : []
        return (
          <div key={i} className="grid grid-cols-1 gap-2 rounded-md border border-border p-2 md:grid-cols-[1fr_8rem_1fr_auto]">
            <Select
              value={f.column}
              onValueChange={(v) => {
                const c = colOf(v)
                set(i, { column: v, op: c ? opsFor(c.type)[0] : '=', value: '' })
              }}
            >
              <SelectTrigger aria-label={t('plugin_report.filter.column')}>
                <SelectValue placeholder={t('common.pleaseSelect')} />
              </SelectTrigger>
              <SelectContent>
                {columns.map((c) => (
                  <SelectItem key={c.name} value={c.name}>
                    {t(`plugin_report.field.${c.name}`, { defaultValue: c.label })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={f.op} onValueChange={(v) => set(i, { op: v as Filter['op'], value: '' })}>
              <SelectTrigger aria-label={t('plugin_report.filter.op')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ops.map((o) => (
                  <SelectItem key={o} value={o}>
                    {t(`plugin_report.op.${o}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              aria-label={t('plugin_report.filter.value')}
              placeholder={f.op === 'in' ? t('plugin_report.filter.inHint') : f.op === 'between' ? t('plugin_report.filter.betweenHint') : t('plugin_report.filter.value')}
              defaultValue={formatFilterValue(f.op, f.value)}
              key={`${f.column}-${f.op}`}
              onChange={(e) => col && set(i, { value: parseFilterValue(f.op, col.type, e.target.value) })}
            />
            <Button type="button" variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => onChange(value.filter((_, j) => j !== i))}>
              <Trash2 aria-hidden="true" />
            </Button>
          </div>
        )
      })}
      {value.length < max && columns.length > 0 ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-fit"
          onClick={() => onChange([...value, { column: columns[0].name, op: opsFor(columns[0].type)[0], value: '' }])}
        >
          <Plus aria-hidden="true" />
          {t('plugin_report.filter.add')}
        </Button>
      ) : null}
    </div>
  )
}
