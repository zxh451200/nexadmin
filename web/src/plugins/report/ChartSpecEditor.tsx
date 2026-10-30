import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { cn } from '@/lib/utils'
import type { Agg, Bucket, ChartConfig, ChartType, SourceCol } from './api'
import { aggsFor, colorVar, COLOR_TOKENS } from './chartData'
import { builtinText, fieldText } from './builtinText'
import { FilterEditor } from './FilterEditor'

interface Props {
  table?: string
  type: ChartType
  columns: SourceCol[]
  value: ChartConfig
  onChange: (next: ChartConfig) => void
}

const NONE = '__none'
const BUCKETS: Bucket[] = ['day', 'month', 'year']

export function ChartSpecEditor({ table, type, columns, value, onChange }: Props) {
  const { t } = useTranslation()
  const colOf = (n?: string) => columns.find((c) => c.name === n)
  const label = (c: SourceCol) => fieldText(t, table, c.name, c.label)
  const patch = (p: Partial<ChartConfig>) => onChange({ ...value, ...p })
  const maxDims = type === 'metric' ? 0 : type === 'pie' ? 1 : 2
  const maxMeas = type === 'metric' || type === 'pie' ? 1 : 6
  const outputs = [...value.dimensions.map((_, i) => `d${i}`), ...value.measures.map((_, i) => `m${i}`)]

  return (
    <div className="flex flex-col gap-5">
      {maxDims > 0 || type === 'table' ? (
        <section className="flex flex-col gap-2" aria-label={t('plugin_report.chart.dimensions')}>
          <Label>{t('plugin_report.chart.dimensions')}</Label>
          {value.dimensions.map((d, i) => {
            const col = colOf(d.column)
            return (
              <div key={i} className="grid grid-cols-[1fr_auto] gap-2 md:grid-cols-[1fr_9rem_auto]">
                <Select value={d.column} onValueChange={(v) => patch({ dimensions: value.dimensions.map((x, j) => (j === i ? { column: v } : x)) })}>
                  <SelectTrigger aria-label={t('plugin_report.chart.dimension')}>
                    <SelectValue placeholder={t('common.pleaseSelect')} />
                  </SelectTrigger>
                  <SelectContent>
                    {columns.map((c) => (
                      <SelectItem key={c.name} value={c.name}>
                        {label(c)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {col?.type === 'time' ? (
                  <Select
                    value={d.bucket || NONE}
                    onValueChange={(v) => patch({ dimensions: value.dimensions.map((x, j) => (j === i ? { ...x, bucket: v === NONE ? '' : (v as Bucket) } : x)) })}
                  >
                    <SelectTrigger aria-label={t('plugin_report.chart.bucket')}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={NONE}>{t('plugin_report.bucket.none')}</SelectItem>
                      {BUCKETS.map((b) => (
                        <SelectItem key={b} value={b}>
                          {t(`plugin_report.bucket.${b}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : (
                  <span className="hidden md:block" />
                )}
                <Button type="button" variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => patch({ dimensions: value.dimensions.filter((_, j) => j !== i) })}>
                  <Trash2 aria-hidden="true" />
                </Button>
              </div>
            )
          })}
          {value.dimensions.length < (type === 'table' ? 6 : maxDims) && columns.length > 0 ? (
            <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => patch({ dimensions: [...value.dimensions, { column: columns[0].name }] })}>
              <Plus aria-hidden="true" />
              {t('plugin_report.chart.addDimension')}
            </Button>
          ) : null}
        </section>
      ) : null}

      <section className="flex flex-col gap-2" aria-label={t('plugin_report.chart.measures')}>
        <Label>{t('plugin_report.chart.measures')}</Label>
        {value.measures.map((m, i) => {
          const col = colOf(m.column)
          const aggs = aggsFor(m.column ? col : undefined)
          const setM = (p: Partial<ChartConfig['measures'][number]>) => patch({ measures: value.measures.map((x, j) => (j === i ? { ...x, ...p } : x)) })
          return (
            <div key={i} className="grid grid-cols-[1fr_1fr_auto] gap-2 md:grid-cols-[8rem_1fr_auto]">
              <Select value={m.agg} onValueChange={(v) => setM({ agg: v as Agg })}>
                <SelectTrigger aria-label={t('plugin_report.chart.agg')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {aggs.map((a) => (
                    <SelectItem key={a} value={a}>
                      {t(`plugin_report.agg.${a}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select
                value={m.column || NONE}
                onValueChange={(v) => {
                  const c = v === NONE ? undefined : colOf(v)
                  setM({ column: v === NONE ? undefined : v, agg: aggsFor(c).includes(m.agg) ? m.agg : (aggsFor(c)[0] as Agg) })
                }}
              >
                <SelectTrigger aria-label={t('plugin_report.chart.measureColumn')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>{t('plugin_report.chart.allRows')}</SelectItem>
                  {columns.map((c) => (
                    <SelectItem key={c.name} value={c.name}>
                      {label(c)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button type="button" variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => patch({ measures: value.measures.filter((_, j) => j !== i) })}>
                <Trash2 aria-hidden="true" />
              </Button>
            </div>
          )
        })}
        {value.measures.length < maxMeas ? (
          <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => patch({ measures: [...value.measures, { agg: 'count' }] })}>
            <Plus aria-hidden="true" />
            {t('plugin_report.chart.addMeasure')}
          </Button>
        ) : null}
      </section>

      <section className="flex flex-col gap-2">
        <Label>{t('plugin_report.chart.filters')}</Label>
        <FilterEditor table={table} columns={columns} value={value.filters} onChange={(filters) => patch({ filters })} />
      </section>

      <section className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <div className="flex flex-col gap-2">
          <Label>{t('plugin_report.chart.orderBy')}</Label>
          <div className="grid grid-cols-[1fr_auto] items-center gap-2">
            <Select
              value={value.orderBy[0]?.ref ?? NONE}
              onValueChange={(v) => patch({ orderBy: v === NONE ? [] : [{ ref: v, desc: value.orderBy[0]?.desc }] })}
            >
              <SelectTrigger aria-label={t('plugin_report.chart.orderBy')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>{t('plugin_report.chart.orderDefault')}</SelectItem>
                {outputs.map((o) => (
                  <SelectItem key={o} value={o}>
                    {o.startsWith('d') ? t('plugin_report.chart.dimensionN', { n: Number(o.slice(1)) + 1 }) : t('plugin_report.chart.measureN', { n: Number(o.slice(1)) + 1 })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Switch
              checked={!!value.orderBy[0]?.desc}
              disabled={value.orderBy.length === 0}
              aria-label={t('plugin_report.chart.desc')}
              onCheckedChange={(v) => patch({ orderBy: [{ ref: value.orderBy[0].ref, desc: v }] })}
            />
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="chart-limit">{t('plugin_report.chart.limit')}</Label>
          <Input id="chart-limit" type="number" min={1} max={5000} value={value.limit} onChange={(e) => patch({ limit: Math.min(5000, Math.max(1, Number(e.target.value) || 1)) })} />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="chart-unit">{t('plugin_report.chart.unit')}</Label>
          <Input id="chart-unit" maxLength={16} value={builtinText(t, value.unit)} onChange={(e) => patch({ unit: e.target.value })} />
        </div>
      </section>

      <section className="flex flex-col gap-2">
        <Label>{t('plugin_report.chart.colors')}</Label>
        <div className="flex flex-wrap gap-2" role="group" aria-label={t('plugin_report.chart.colors')}>
          {COLOR_TOKENS.map((tok) => {
            const idx = value.colors.indexOf(tok)
            return (
              <button
                key={tok}
                type="button"
                aria-pressed={idx >= 0}
                aria-label={t(`plugin_report.color.${tok}`)}
                title={t(`plugin_report.color.${tok}`)}
                onClick={() => patch({ colors: idx >= 0 ? value.colors.filter((c) => c !== tok) : [...value.colors, tok] })}
                className={cn('relative size-10 rounded-full border-2 border-border focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:size-8', idx >= 0 && 'ring-2 ring-ring ring-offset-2 ring-offset-background')}
                style={{ backgroundColor: colorVar(tok) }}
              >
                {idx >= 0 ? <span className="absolute -end-1 -top-1 flex size-4 items-center justify-center rounded-full bg-foreground text-[10px] text-background">{idx + 1}</span> : null}
              </button>
            )
          })}
        </div>
        {type === 'bar' ? (
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={!!value.stacked} onCheckedChange={(v) => patch({ stacked: v })} aria-label={t('plugin_report.chart.stacked')} />
            {t('plugin_report.chart.stacked')}
          </label>
        ) : null}
      </section>
    </div>
  )
}
