import { useTranslation } from 'react-i18next'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { Checkbox } from '@/framework/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { Field, NumInput } from './controls'
import { OptionsEditor } from './OptionsEditor'
import { isMulti } from './schema'
import type { RulesProps } from './FieldRules'

const NONE = '__none'

/** 选项类字段（单选/多选/下拉）的规则：选项来源（手填或字典）、多选开关、个数范围、默认值 */
export function OptionRules({ f, id, patch }: RulesProps) {
  const { t } = useTranslation()
  const P = (k: string) => t(`plugin_formbuilder.prop.${k}`)
  const fromDict = f.dictType !== undefined
  const multi = isMulti(f)
  const options = f.options ?? []

  return (
    <>
      {f.type === 'select' ? (
        <div className="flex items-center justify-between gap-3">
          <label htmlFor={`${id}-multiple`} className="text-sm font-medium">
            {P('multiple')}
          </label>
          <Switch
            id={`${id}-multiple`}
            checked={!!f.multiple}
            onCheckedChange={(v) => patch({ multiple: v || undefined, default: undefined, min: undefined, max: undefined })}
          />
        </div>
      ) : null}

      <Field id={`${id}-source`} label={P('optionSource')}>
        <Select value={fromDict ? 'dict' : 'manual'} onValueChange={(v) => patch({ dictType: v === 'dict' ? '' : undefined })}>
          <SelectTrigger id={`${id}-source`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="manual">{P('sourceManual')}</SelectItem>
            <SelectItem value="dict">{P('sourceDict')}</SelectItem>
          </SelectContent>
        </Select>
      </Field>

      {fromDict ? (
        <Field id={`${id}-dict`} label={P('dictType')} hint={P('dictHint')}>
          <RemoteSelect
            id={`${id}-dict`}
            url="/dict/types"
            labelKey="name"
            valueKey="code"
            value={f.dictType || null}
            onChange={(v: string | null | undefined) => patch({ dictType: v || '', default: undefined })}
            placeholder={P('dictPlaceholder')}
          />
        </Field>
      ) : (
        <Field id={`${id}-options`} label={P('options')} hint={P('optionsHint')}>
          <OptionsEditor idPrefix={id} options={options} onChange={(o) => patch({ options: o })} />
        </Field>
      )}

      {multi ? (
        <div className="grid grid-cols-2 gap-3">
          <Field id={`${id}-min`} label={P('minCount')}>
            <NumInput id={`${id}-min`} min={0} step={1} value={f.min} onChange={(v) => patch({ min: v })} />
          </Field>
          <Field id={`${id}-max`} label={P('maxCount')}>
            <NumInput id={`${id}-max`} min={1} step={1} value={f.max} onChange={(v) => patch({ max: v })} />
          </Field>
        </div>
      ) : null}

      {!fromDict && options.length > 0 ? <DefaultPicker f={f} id={id} patch={patch} multi={multi} /> : null}
    </>
  )
}

function DefaultPicker({ f, id, patch, multi }: RulesProps & { multi: boolean }) {
  const { t } = useTranslation()
  const options = (f.options ?? []).filter((o) => o.value !== '')
  const label = t('plugin_formbuilder.prop.default')
  if (!multi) {
    return (
      <Field id={`${id}-default`} label={label}>
        <Select value={typeof f.default === 'string' ? f.default : NONE} onValueChange={(v) => patch({ default: v === NONE ? undefined : v })}>
          <SelectTrigger id={`${id}-default`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>{t('plugin_formbuilder.prop.noDefault')}</SelectItem>
            {options.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
    )
  }
  const cur = Array.isArray(f.default) ? (f.default as string[]) : []
  return (
    <fieldset className="flex flex-col gap-1">
      <legend className="mb-1 text-sm font-medium">{label}</legend>
      {options.map((o) => (
        <label key={o.value} className="flex min-h-10 items-center gap-2 text-sm md:min-h-8">
          <Checkbox
            checked={cur.includes(o.value)}
            onCheckedChange={(c) => {
              const next = c === true ? [...cur, o.value] : cur.filter((x) => x !== o.value)
              patch({ default: next.length ? next : undefined })
            }}
          />
          {o.label}
        </label>
      ))}
    </fieldset>
  )
}
