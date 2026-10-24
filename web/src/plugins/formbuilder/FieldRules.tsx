import { useTranslation } from 'react-i18next'
import { DatePicker } from '@/framework/components/DatePicker'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import type { FormField } from './api'
import { Field, NumInput } from './controls'
import { MaxFiles, MaxPattern, MaxRatingStars, MaxTextRunes } from './limits'
import { OptionRules } from './OptionRules'
import { usesOptions } from './schema'

export interface RulesProps {
  f: FormField
  id: string
  patch: (p: Partial<FormField>) => void
}

const range = (n: number) => Array.from({ length: n }, (_, i) => i + 1)

/** 按字段类型显示的规则：长度/正则、数值范围、日期范围、星数、文件个数、默认值 */
export function FieldRules({ f, id, patch }: RulesProps) {
  const { t } = useTranslation()
  const P = (k: string) => t(`plugin_formbuilder.prop.${k}`)

  if (f.type === 'text' || f.type === 'textarea') {
    return (
      <>
        <div className="grid grid-cols-2 gap-3">
          <Field id={`${id}-minLen`} label={P('minLen')}>
            <NumInput id={`${id}-minLen`} min={0} max={MaxTextRunes} step={1} value={f.minLen} onChange={(v) => patch({ minLen: v })} />
          </Field>
          <Field id={`${id}-maxLen`} label={P('maxLen')}>
            <NumInput id={`${id}-maxLen`} min={1} max={MaxTextRunes} step={1} value={f.maxLen} onChange={(v) => patch({ maxLen: v })} />
          </Field>
        </div>
        <Field id={`${id}-pattern`} label={P('pattern')} hint={P('patternHint')}>
          <Input id={`${id}-pattern`} maxLength={MaxPattern} className="font-mono" value={f.pattern ?? ''} onChange={(e) => patch({ pattern: e.target.value || undefined })} />
        </Field>
        <Field id={`${id}-default`} label={P('default')}>
          {f.type === 'textarea' ? (
            <Textarea id={`${id}-default`} rows={2} value={(f.default as string) ?? ''} onChange={(e) => patch({ default: e.target.value || undefined })} />
          ) : (
            <Input id={`${id}-default`} value={(f.default as string) ?? ''} onChange={(e) => patch({ default: e.target.value || undefined })} />
          )}
        </Field>
      </>
    )
  }
  if (f.type === 'number') {
    return (
      <>
        <div className="grid grid-cols-2 gap-3">
          <Field id={`${id}-min`} label={P('min')}>
            <NumInput id={`${id}-min`} value={f.min} onChange={(v) => patch({ min: v })} />
          </Field>
          <Field id={`${id}-max`} label={P('max')}>
            <NumInput id={`${id}-max`} value={f.max} onChange={(v) => patch({ max: v })} />
          </Field>
        </div>
        <div className="flex items-center justify-between gap-3">
          <label htmlFor={`${id}-integer`} className="text-sm font-medium">
            {P('integer')}
          </label>
          <Switch id={`${id}-integer`} checked={!!f.integer} onCheckedChange={(v) => patch({ integer: v || undefined })} />
        </div>
        <Field id={`${id}-default`} label={P('default')}>
          <NumInput id={`${id}-default`} value={typeof f.default === 'number' ? f.default : undefined} onChange={(v) => patch({ default: v })} />
        </Field>
      </>
    )
  }
  if (f.type === 'date') {
    return (
      <>
        <div className="grid grid-cols-2 gap-3">
          <Field id={`${id}-minDate`} label={P('minDate')}>
            <DatePicker id={`${id}-minDate`} value={f.minDate ?? ''} onChange={(v) => patch({ minDate: v || undefined })} />
          </Field>
          <Field id={`${id}-maxDate`} label={P('maxDate')}>
            <DatePicker id={`${id}-maxDate`} value={f.maxDate ?? ''} onChange={(v) => patch({ maxDate: v || undefined })} />
          </Field>
        </div>
        <Field id={`${id}-default`} label={P('default')}>
          <DatePicker id={`${id}-default`} value={(f.default as string) ?? ''} onChange={(v) => patch({ default: v || undefined })} />
        </Field>
      </>
    )
  }
  if (f.type === 'switch') {
    return (
      <div className="flex items-center justify-between gap-3">
        <label htmlFor={`${id}-default`} className="text-sm font-medium">
          {P('defaultOn')}
        </label>
        <Switch id={`${id}-default`} checked={f.default === true} onCheckedChange={(v) => patch({ default: v ? true : undefined })} />
      </div>
    )
  }
  if (f.type === 'rating') {
    return (
      <Field id={`${id}-stars`} label={P('stars')}>
        <Select value={String(f.max ?? 5)} onValueChange={(v) => patch({ max: Number(v) })}>
          <SelectTrigger id={`${id}-stars`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {range(MaxRatingStars).map((n) => (
              <SelectItem key={n} value={String(n)}>
                {n}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
    )
  }
  if (f.type === 'file') {
    return (
      <div className="grid grid-cols-2 gap-3">
        <Field id={`${id}-min`} label={P('minFiles')}>
          <NumInput id={`${id}-min`} min={0} max={MaxFiles} step={1} value={f.min} onChange={(v) => patch({ min: v })} />
        </Field>
        <Field id={`${id}-max`} label={P('maxFiles')} hint={P('maxFilesHint')}>
          <NumInput id={`${id}-max`} min={1} max={MaxFiles} step={1} value={f.max} onChange={(v) => patch({ max: v })} />
        </Field>
      </div>
    )
  }
  if (usesOptions(f)) return <OptionRules f={f} id={id} patch={patch} />
  return null
}
