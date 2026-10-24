import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { MaxOptions } from './limits'
import type { FieldOption } from './api'

interface Props {
  idPrefix: string
  options: FieldOption[]
  onChange: (options: FieldOption[]) => void
}

/** 手填选项：value 存库、label 展示，value 在字段内必须唯一 */
export function OptionsEditor({ idPrefix, options, onChange }: Props) {
  const { t } = useTranslation()
  const set = (i: number, p: Partial<FieldOption>) => onChange(options.map((o, j) => (j === i ? { ...o, ...p } : o)))
  const add = () => {
    const used = new Set(options.map((o) => o.value))
    let n = options.length + 1
    while (used.has(String(n))) n++
    onChange([...options, { label: t('plugin_formbuilder.designer.optionN', { n }), value: String(n) }])
  }
  const dup = (v: string) => options.filter((o) => o.value === v).length > 1
  return (
    <div className="flex flex-col gap-2">
      {options.map((o, i) => (
        <div key={i} className="grid grid-cols-[1fr_1fr_auto] items-start gap-2">
          <Input
            aria-label={t('plugin_formbuilder.designer.optionLabel', { n: i + 1 })}
            placeholder={t('plugin_formbuilder.designer.optionLabelPh')}
            maxLength={100}
            value={o.label}
            onChange={(e) => set(i, { label: e.target.value })}
          />
          <Input
            aria-label={t('plugin_formbuilder.designer.optionValue', { n: i + 1 })}
            placeholder={t('plugin_formbuilder.designer.optionValuePh')}
            maxLength={100}
            aria-invalid={o.value === '' || dup(o.value) || undefined}
            value={o.value}
            onChange={(e) => set(i, { value: e.target.value })}
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="text-destructive"
            aria-label={t('plugin_formbuilder.designer.optionRemove', { n: i + 1 })}
            title={t('common.delete')}
            id={`${idPrefix}-rm-${i}`}
            onClick={() => onChange(options.filter((_, j) => j !== i))}
          >
            <Trash2 aria-hidden="true" />
          </Button>
        </div>
      ))}
      <Button type="button" variant="outline" size="sm" className="w-fit" disabled={options.length >= MaxOptions} onClick={add}>
        <Plus aria-hidden="true" />
        {t('plugin_formbuilder.designer.addOption')}
      </Button>
    </div>
  )
}
