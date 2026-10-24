import { useEffect, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { useChartOptions, useDatasetOptions, useSaveReport, type Report, type ReportFilter, type ReportForm } from './api'

const SPANS = [3, 4, 6, 8, 12]
const blank = (): ReportForm => ({ name: '', groupName: '', remark: '', config: { items: [], filters: [] } })

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: Report | null
}

export function ReportDialog({ open, onOpenChange, editing }: Props) {
  const { t } = useTranslation()
  const { data: charts = [] } = useChartOptions('report', open)
  const { data: datasets = [] } = useDatasetOptions('report', open)
  const save = useSaveReport()
  const [form, setForm] = useState<ReportForm>(blank)

  useEffect(() => {
    if (open) setForm(editing ? { name: editing.name, groupName: editing.groupName, remark: editing.remark, config: editing.config } : blank())
  }, [open, editing])

  const cfg = form.config
  const setCfg = (p: Partial<ReportForm['config']>) => setForm({ ...form, config: { ...cfg, ...p } })
  const setItem = (i: number, p: Partial<ReportForm['config']['items'][number]>) => setCfg({ items: cfg.items.map((x, j) => (j === i ? { ...x, ...p } : x)) })
  const setFilter = (i: number, p: Partial<ReportFilter>) => setCfg({ filters: cfg.filters.map((x, j) => (j === i ? { ...x, ...p } : x)) })
  const valid = form.name.trim() !== '' && cfg.items.every((it) => it.chartId) && cfg.filters.every((f) => f.label.trim() && f.datasetId && f.column)
  const fieldsOf = (id: string, timeOnly: boolean) => (datasets.find((d) => d.id === id)?.fields ?? []).filter((c) => !timeOnly || c.type === 'time')

  const submit = async () => {
    if (!valid) return
    const filters = cfg.filters.map((f, i) => ({ ...f, key: `f${i + 1}` })) // 重新编号，保证 key 唯一
    try {
      await save.mutateAsync({ id: editing?.id, data: { ...form, name: form.name.trim(), config: { ...cfg, filters } } })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="xl"
      title={editing ? t('plugin_report.report.editTitle') : t('plugin_report.report.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="report-report-form" disabled={!valid || save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <FormLayout
        id="report-report-form"
        showRequiredHint
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <FormGrid cols={3}>
          <div className="flex flex-col gap-2">
            <Label htmlFor="rp-name">{t('common.name')}</Label>
            <Input id="rp-name" value={form.name} maxLength={100} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="rp-group">{t('plugin_report.report.group')}</Label>
            <Input id="rp-group" value={form.groupName} maxLength={64} onChange={(e) => setForm({ ...form, groupName: e.target.value })} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="rp-remark">{t('common.remark')}</Label>
            <Input id="rp-remark" value={form.remark} maxLength={255} onChange={(e) => setForm({ ...form, remark: e.target.value })} />
          </div>
          <FormGridFull>
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-1 text-sm font-medium">{t('plugin_report.report.items')}</legend>
              {cfg.items.map((it, i) => (
                <div key={i} className="grid grid-cols-1 gap-2 md:grid-cols-[1fr_10rem_7rem_auto]">
                  <Select value={it.chartId} onValueChange={(v) => setItem(i, { chartId: v })}>
                    <SelectTrigger aria-label={t('plugin_report.report.chart')}>
                      <SelectValue placeholder={t('common.pleaseSelect')} />
                    </SelectTrigger>
                    <SelectContent>
                      {charts.map((c) => (
                        <SelectItem key={c.id} value={c.id}>
                          {c.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Input aria-label={t('plugin_report.report.group')} placeholder={t('plugin_report.report.group')} maxLength={64} value={it.group} onChange={(e) => setItem(i, { group: e.target.value })} />
                  <Select value={String(it.span)} onValueChange={(v) => setItem(i, { span: Number(v) })}>
                    <SelectTrigger aria-label={t('plugin_report.report.span')}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {SPANS.map((s) => (
                        <SelectItem key={s} value={String(s)}>
                          {t('plugin_report.report.spanN', { n: s })}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button type="button" variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => setCfg({ items: cfg.items.filter((_, j) => j !== i) })}>
                    <Trash2 aria-hidden="true" />
                  </Button>
                </div>
              ))}
              <Button type="button" variant="outline" size="sm" className="w-fit" disabled={cfg.items.length >= 50} onClick={() => setCfg({ items: [...cfg.items, { chartId: '', group: cfg.items.at(-1)?.group ?? '', span: 6 }] })}>
                <Plus aria-hidden="true" />
                {t('plugin_report.report.addItem')}
              </Button>
            </fieldset>
          </FormGridFull>
          <FormGridFull>
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-1 text-sm font-medium">{t('plugin_report.report.filters')}</legend>
              <p className="text-xs text-muted-foreground">{t('plugin_report.report.filtersHint')}</p>
              {cfg.filters.map((f, i) => (
                <div key={i} className="grid grid-cols-1 gap-2 rounded-md border border-border p-2 md:grid-cols-[1fr_9rem_1fr_1fr_auto]">
                  <Input aria-label={t('plugin_report.report.filterLabel')} placeholder={t('plugin_report.report.filterLabel')} maxLength={50} value={f.label} onChange={(e) => setFilter(i, { label: e.target.value })} />
                  <Select value={f.type} onValueChange={(v) => setFilter(i, { type: v as ReportFilter['type'], column: '' })}>
                    <SelectTrigger aria-label={t('plugin_report.report.filterType')}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="dateRange">{t('plugin_report.report.dateRange')}</SelectItem>
                      <SelectItem value="select">{t('plugin_report.report.select')}</SelectItem>
                    </SelectContent>
                  </Select>
                  <Select value={f.datasetId} onValueChange={(v) => setFilter(i, { datasetId: v, column: '' })}>
                    <SelectTrigger aria-label={t('plugin_report.chart.dataset')}>
                      <SelectValue placeholder={t('plugin_report.chart.dataset')} />
                    </SelectTrigger>
                    <SelectContent>
                      {datasets.map((d) => (
                        <SelectItem key={d.id} value={d.id}>
                          {d.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Select value={f.column} onValueChange={(v) => setFilter(i, { column: v })}>
                    <SelectTrigger aria-label={t('plugin_report.report.filterColumn')}>
                      <SelectValue placeholder={t('plugin_report.report.filterColumn')} />
                    </SelectTrigger>
                    <SelectContent>
                      {fieldsOf(f.datasetId, f.type === 'dateRange').map((c) => (
                        <SelectItem key={c.name} value={c.name}>
                          {t(`plugin_report.field.${c.name}`, { defaultValue: c.label })}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <div className="flex items-center gap-2">
                    {f.type === 'select' ? <Switch checked={!!f.multiple} aria-label={t('plugin_report.report.multiple')} title={t('plugin_report.report.multiple')} onCheckedChange={(v) => setFilter(i, { multiple: v })} /> : null}
                    <Button type="button" variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => setCfg({ filters: cfg.filters.filter((_, j) => j !== i) })}>
                      <Trash2 aria-hidden="true" />
                    </Button>
                  </div>
                </div>
              ))}
              <Button type="button" variant="outline" size="sm" className="w-fit" disabled={cfg.filters.length >= 10} onClick={() => setCfg({ filters: [...cfg.filters, { key: `f${cfg.filters.length + 1}`, label: '', type: 'dateRange', datasetId: '', column: '' }] })}>
                <Plus aria-hidden="true" />
                {t('plugin_report.report.addFilter')}
              </Button>
            </fieldset>
          </FormGridFull>
        </FormGrid>
      </FormLayout>
    </ResponsiveDialog>
  )
}
