import { useEffect, useMemo, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
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
import { useChartOptions, useRoles, useSaveScreen, type Screen, type ScreenForm, type ScreenItem } from './api'
import { builtinText, revertBuiltin } from './builtinText'
import { canPlace, firstFreeSlot, GRID_COLS, GRID_ROWS } from './chartData'
import { EditorCanvas } from './EditorCanvas'

const REFRESH = [0, 5, 10, 30, 60, 300, 600]
const blank = (): ScreenForm => ({ name: '', remark: '', config: { items: [], refreshSec: 30 }, roleIds: [] })

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: Screen | null
}

export function ScreenDialog({ open, onOpenChange, editing }: Props) {
  const { t } = useTranslation()
  const { data: charts = [] } = useChartOptions('screen', open)
  const { data: roles = [] } = useRoles(open)
  const save = useSaveScreen()
  const [form, setForm] = useState<ScreenForm>(blank)
  const [sel, setSel] = useState(-1)
  const [pick, setPick] = useState('')

  useEffect(() => {
    if (!open) return
    setSel(-1)
    setPick('')
    setForm(editing ? { name: editing.name, remark: editing.remark, config: editing.config, roleIds: editing.roleIds ?? [] } : blank())
  }, [open, editing])

  const items = form.config.items
  const names = useMemo(() => Object.fromEntries(charts.map((c) => [c.id, builtinText(t, c.name)])), [charts, t])
  const setItems = (next: ScreenItem[]) => setForm((f) => ({ ...f, config: { ...f.config, items: next } }))
  const cur = items[sel]
  const setCur = (p: Partial<ScreenItem>) => {
    const next = { ...cur, ...p }
    if (canPlace(items, sel, next)) setItems(items.map((o, i) => (i === sel ? next : o)))
    else toast.warning(t('plugin_report.screen.rejected'))
  }
  const addable = charts.filter((c) => !items.some((i) => i.chartId === c.id))
  const noAddable = addable.length === 0
  const pickPlaceholder = !noAddable
    ? t('common.pleaseSelect')
    : charts.length === 0
      ? t('plugin_report.screen.noCharts')
      : t('plugin_report.screen.allChartsAdded')
  const add = () => {
    const slot = firstFreeSlot(items, 8, 6)
    if (!pick || !slot) return toast.warning(t('plugin_report.screen.noSpace'))
    setItems([...items, { chartId: pick, ...slot, w: 8, h: 6 }])
    setSel(items.length)
    setPick('')
  }
  const valid = form.name.trim() !== ''

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
  const num = (key: 'x' | 'y' | 'w' | 'h', max: number, label: string) => (
    <div className="flex flex-col gap-1">
      <Label htmlFor={`sc-${key}`}>{label}</Label>
      <Input id={`sc-${key}`} type="number" min={key === 'w' || key === 'h' ? 1 : 0} max={max} value={cur?.[key] ?? 0} onChange={(e) => setCur({ [key]: Number(e.target.value) || 0 })} />
    </div>
  )

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="xl"
      title={editing ? t('plugin_report.screen.editTitle') : t('plugin_report.screen.createTitle')}
      description={t('plugin_report.screen.editorHint')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form="report-screen-form" disabled={!valid || save.isPending}>
            {save.isPending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <FormLayout
        id="report-screen-form"
        showRequiredHint
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <FormGrid cols={3}>
          <div className="flex flex-col gap-2">
            <Label htmlFor="sc-name" required>{t('common.name')}</Label>
            <Input id="sc-name" value={builtinText(t, form.name)} maxLength={100} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="sc-title">{t('plugin_report.screen.title')}</Label>
            <Input id="sc-title" value={builtinText(t, form.config.title)} maxLength={100} onChange={(e) => setForm({ ...form, config: { ...form.config, title: e.target.value } })} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="sc-refresh">{t('plugin_report.screen.refresh')}</Label>
            <Select value={String(form.config.refreshSec)} onValueChange={(v) => setForm({ ...form, config: { ...form.config, refreshSec: Number(v) } })}>
              <SelectTrigger id="sc-refresh">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {REFRESH.map((s) => (
                  <SelectItem key={s} value={String(s)}>
                    {s === 0 ? t('plugin_report.screen.noRefresh') : t('plugin_report.screen.everySec', { sec: s })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <FormGridFull>
            <div className="flex flex-wrap items-end gap-2">
              <div className="flex min-w-48 flex-1 flex-col gap-2">
                <Label htmlFor="sc-pick">{t('plugin_report.screen.addChart')}</Label>
                <Select value={pick} onValueChange={setPick} disabled={noAddable}>
                  <SelectTrigger id="sc-pick">
                    <SelectValue placeholder={pickPlaceholder} />
                  </SelectTrigger>
                  <SelectContent>
                    {addable.map((c) => (
                      <SelectItem key={c.id} value={c.id}>
                        {builtinText(t, c.name)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <Button type="button" variant="outline" disabled={!pick} onClick={add}>
                <Plus aria-hidden="true" />
                {t('plugin_report.screen.add')}
              </Button>
            </div>
          </FormGridFull>
          <FormGridFull>
            <EditorCanvas items={items} names={names} selected={sel} onSelect={setSel} onChange={setItems} onReject={() => toast.warning(t('plugin_report.screen.rejected'))} />
            <p className="mt-1 text-xs text-muted-foreground">{t('plugin_report.screen.canvasHint', { cols: GRID_COLS, rows: GRID_ROWS })}</p>
          </FormGridFull>
          {cur ? (
            <FormGridFull>
              <div className="grid grid-cols-2 gap-2 rounded-md bg-muted/40 p-3 md:grid-cols-6">
                <div className="col-span-2 flex flex-col gap-1">
                  <Label htmlFor="sc-item-title">{t('plugin_report.screen.itemTitle')}</Label>
                  <Input id="sc-item-title" value={builtinText(t, cur.title)} maxLength={100} placeholder={names[cur.chartId]} onChange={(e) => setCur({ title: e.target.value })} />
                </div>
                {num('x', GRID_COLS - 1, 'X')}
                {num('y', GRID_ROWS - 1, 'Y')}
                {num('w', GRID_COLS, t('plugin_report.screen.width'))}
                {num('h', GRID_ROWS, t('plugin_report.screen.height'))}
                <div className="col-span-2 flex items-end md:col-span-6">
                  <Button type="button" variant="outline" className="text-destructive" onClick={() => { setItems(items.filter((_, i) => i !== sel)); setSel(-1) }}>
                    <Trash2 aria-hidden="true" />
                    {t('plugin_report.screen.removeItem')}
                  </Button>
                </div>
              </div>
            </FormGridFull>
          ) : null}
          <FormGridFull>
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-1 text-sm font-medium">{t('plugin_report.screen.roles')}</legend>
              <p className="text-xs text-muted-foreground">{t('plugin_report.screen.rolesHint')}</p>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 md:grid-cols-3">
                {roles.map((r) => (
                  <label key={r.id} className="flex min-h-10 items-center gap-2 rounded-md border border-border px-3 text-sm">
                    <Checkbox
                      checked={form.roleIds.includes(r.id)}
                      onCheckedChange={(v) => setForm({ ...form, roleIds: v ? [...form.roleIds, r.id] : form.roleIds.filter((x) => x !== r.id) })}
                    />
                    <span className="truncate">{r.name}</span>
                  </label>
                ))}
              </div>
            </fieldset>
          </FormGridFull>
          <FormGridFull>
            <div className="flex flex-col gap-2">
              <Label htmlFor="sc-remark">{t('common.remark')}</Label>
              <Textarea id="sc-remark" rows={3} value={form.remark} maxLength={255} onChange={(e) => setForm({ ...form, remark: e.target.value })} />
            </div>
          </FormGridFull>
        </FormGrid>
      </FormLayout>
    </ResponsiveDialog>
  )
}
