import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { CHART_TYPES, previewChart, useDatasetOptions, useSaveChart, type Chart, type ChartForm, type ChartType } from './api'
import { ChartSpecEditor } from './ChartSpecEditor'
import { ChartView } from './ChartView'
import { emptyChartConfig, shapeError } from './chartData'

const blank = (): ChartForm => ({ name: '', datasetId: '', type: 'bar', config: emptyChartConfig(), remark: '' })

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: Chart | null
}

export function ChartDialog({ open, onOpenChange, editing }: Props) {
  const { t } = useTranslation()
  const { data: datasets = [] } = useDatasetOptions('chart', open)
  const save = useSaveChart()
  const [form, setForm] = useState<ChartForm>(blank)
  const [snap, setSnap] = useState<ChartForm | null>(null)

  useEffect(() => {
    if (!open) return
    setSnap(null)
    setForm(editing ? { name: editing.name, datasetId: editing.datasetId, type: editing.type, config: editing.config, remark: editing.remark } : blank())
  }, [open, editing])

  const ds = datasets.find((d) => d.id === form.datasetId)
  const cols = ds?.fields ?? []
  const shape = shapeError(form.type, form.config)
  const valid = form.name.trim() !== '' && !!ds && !shape

  const preview = useQuery({
    queryKey: ['plugin_report', 'chart-preview', snap],
    queryFn: () => previewChart(snap as ChartForm),
    enabled: open && !!snap,
    retry: false,
  })

  const changeType = (type: ChartType) => {
    const c = form.config
    const dimensions = type === 'metric' ? [] : type === 'pie' ? c.dimensions.slice(0, 1) : c.dimensions
    const measures = type === 'metric' || type === 'pie' ? c.measures.slice(0, 1) : c.measures
    setForm({ ...form, type, config: { ...c, dimensions, measures } })
  }
  const submit = async () => {
    if (!valid) return
    try {
      await save.mutateAsync({ id: editing?.id, data: { ...form, name: form.name.trim() } })
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
      title={editing ? t('plugin_report.chart.editTitle') : t('plugin_report.chart.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" variant="secondary" disabled={!valid} onClick={() => setSnap({ ...form, name: form.name.trim() })}>
            {t('common.preview')}
          </Button>
          <Button type="submit" form="report-chart-form" disabled={!valid || save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <FormLayout
        id="report-chart-form"
        showRequiredHint
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <FormGrid cols={3}>
          <div className="flex flex-col gap-2">
            <Label htmlFor="ch-name">{t('common.name')}</Label>
            <Input id="ch-name" value={form.name} maxLength={100} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="ch-ds">{t('plugin_report.chart.dataset')}</Label>
            <Select value={form.datasetId} onValueChange={(v) => setForm({ ...form, datasetId: v, config: { ...emptyChartConfig(), colors: form.config.colors } })}>
              <SelectTrigger id="ch-ds">
                <SelectValue placeholder={t('common.pleaseSelect')} />
              </SelectTrigger>
              <SelectContent>
                {datasets.map((d) => (
                  <SelectItem key={d.id} value={d.id}>
                    {d.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="ch-type">{t('plugin_report.chart.type')}</Label>
            <Select value={form.type} onValueChange={(v) => changeType(v as ChartType)}>
              <SelectTrigger id="ch-type">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {CHART_TYPES.map((c) => (
                  <SelectItem key={c} value={c}>
                    {t(`plugin_report.type.${c}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <FormGridFull>
            {ds ? (
              <ChartSpecEditor type={form.type} columns={cols} value={form.config} onChange={(config) => setForm({ ...form, config })} />
            ) : (
              <p className="text-sm text-muted-foreground">{t('plugin_report.chart.pickDataset')}</p>
            )}
            {ds && shape ? (
              <p role="status" className="mt-2 text-xs text-warning">
                {t(`plugin_report.chart.${shape}`)}
              </p>
            ) : null}
          </FormGridFull>
        </FormGrid>
        {snap ? (
          <div className="rounded-md border border-border p-2">
            <ChartView type={snap.type} result={preview.data} config={snap.config} height={260} loading={preview.isLoading} error={preview.isError} />
          </div>
        ) : null}
      </FormLayout>
    </ResponsiveDialog>
  )
}
