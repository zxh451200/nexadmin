import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import { previewDataset, useSaveDataset, useSources, type Dataset, type DatasetForm } from './api'
import { builtinText, fieldText, revertBuiltin, sourceText } from './builtinText'
import { ChartView } from './ChartView'
import { emptyChartConfig } from './chartData'
import { FilterEditor } from './FilterEditor'

const empty: DatasetForm = { name: '', source: '', columns: [], filters: [], remark: '' }

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: Dataset | null
}

export function DatasetDialog({ open, onOpenChange, editing }: Props) {
  const { t } = useTranslation()
  const { data: sources = [] } = useSources()
  const save = useSaveDataset()
  const [form, setForm] = useState<DatasetForm>(empty)
  const [snap, setSnap] = useState<DatasetForm | null>(null)

  useEffect(() => {
    if (open) {
      setForm(editing ? { name: editing.name, source: editing.source, columns: editing.columns, filters: editing.filters, remark: editing.remark } : empty)
      setSnap(null)
    }
  }, [open, editing])

  const src = sources.find((s) => s.table === form.source)
  const selectable = (src?.columns ?? []).filter((c) => form.columns.includes(c.name))
  const valid = form.name.trim() !== '' && !!src && form.columns.length > 0

  // 预览只在点击时按当时的表单快照取一次，不随键盘输入反复查询
  const preview = useQuery({
    queryKey: ['plugin_report', 'dataset-preview', snap],
    queryFn: () => previewDataset(snap as DatasetForm),
    enabled: open && !!snap,
    retry: false,
  })

  const submit = async () => {
    if (!valid) return
    try {
      await save.mutateAsync({ id: editing?.id, data: { ...form, name: revertBuiltin(t, editing?.name, form.name.trim()) } })
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
      title={editing ? t('plugin_report.dataset.editTitle') : t('plugin_report.dataset.createTitle')}
      description={t('plugin_report.dataset.safeHint')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" variant="secondary" disabled={!valid} onClick={() => setSnap({ ...form, name: form.name.trim() })}>
            {t('common.preview')}
          </Button>
          <Button type="submit" form="report-dataset-form" disabled={!valid || save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <FormLayout
        id="report-dataset-form"
        showRequiredHint
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <FormGrid>
          <div className="flex flex-col gap-2">
            <Label htmlFor="ds-name" required>{t('common.name')}</Label>
            <Input id="ds-name" value={builtinText(t, form.name)} maxLength={100} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="ds-source" required>{t('plugin_report.dataset.source')}</Label>
            <Select value={form.source} onValueChange={(v) => setForm({ ...form, source: v, columns: [], filters: [] })}>
              <SelectTrigger id="ds-source">
                <SelectValue placeholder={t('common.pleaseSelect')} />
              </SelectTrigger>
              <SelectContent>
                {sources.map((s) => (
                  <SelectItem key={s.table} value={s.table}>
                    {sourceText(t, s.table, s.label)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <FormGridFull>
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-1 text-sm font-medium">
                {t('plugin_report.dataset.columns')}
                <span aria-hidden="true" className="ms-0.5 text-destructive">*</span>
              </legend>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 md:grid-cols-3">
                {(src?.columns ?? []).map((c) => (
                  <label key={c.name} className="flex min-h-10 items-center gap-2 rounded-md border border-border px-3 text-sm">
                    <Checkbox
                      checked={form.columns.includes(c.name)}
                      onCheckedChange={(v) =>
                        setForm({
                          ...form,
                          columns: v ? [...form.columns, c.name] : form.columns.filter((x) => x !== c.name),
                          filters: v ? form.filters : form.filters.filter((f) => f.column !== c.name),
                        })
                      }
                    />
                    <span className="truncate">{fieldText(t, form.source, c.name, c.label)}</span>
                    <span className="ms-auto text-xs text-muted-foreground">{t(`plugin_report.colType.${c.type}`)}</span>
                  </label>
                ))}
                {!src ? <p className="text-sm text-muted-foreground">{t('plugin_report.dataset.pickSource')}</p> : null}
              </div>
            </fieldset>
          </FormGridFull>
          <FormGridFull>
            <div className="flex flex-col gap-2">
              <Label>{t('plugin_report.dataset.fixedFilters')}</Label>
              <p className="text-xs text-muted-foreground">{t('plugin_report.dataset.fixedFiltersHint')}</p>
              <FilterEditor table={form.source} columns={selectable} value={form.filters} onChange={(filters) => setForm({ ...form, filters })} />
            </div>
          </FormGridFull>
          <FormGridFull>
            <div className="flex flex-col gap-2">
              <Label htmlFor="ds-remark">{t('common.remark')}</Label>
              <Textarea id="ds-remark" rows={3} value={form.remark} maxLength={255} onChange={(e) => setForm({ ...form, remark: e.target.value })} />
            </div>
          </FormGridFull>
        </FormGrid>
        {snap ? (
          <div className="rounded-md border border-border p-2">
            <ChartView type="table" result={preview.data} config={emptyChartConfig()} height={240} loading={preview.isLoading} error={preview.isError} />
          </div>
        ) : null}
      </FormLayout>
    </ResponsiveDialog>
  )
}
