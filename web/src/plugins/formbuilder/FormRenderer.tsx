import { Star } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DatePicker } from '@/framework/components/DatePicker'
import { DictSelect } from '@/framework/components/DictSelect'
import { Uploader, type UploadedFile } from '@/framework/components/Uploader'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { RadioGroup, RadioGroupItem } from '@/framework/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import { cn } from '@/lib/utils'
import type { FormDefinition, FormField } from './api'
import { visibleValues } from './schema'

interface Props {
  def: FormDefinition
  values: Record<string, unknown>
  onChange: (key: string, value: unknown) => void
  errors?: Record<string, string>
  disabled?: boolean
  /** 设计器预览：字典绑定的字段还没有发布快照，直接用 DictSelect 取当前字典项 */
  preview?: boolean
  idPrefix?: string
}

interface StoredFile {
  name: string
  url: string
  size?: number
  type?: string
}

const toUploaded = (v: unknown): UploadedFile[] =>
  (Array.isArray(v) ? (v as StoredFile[]) : []).map((f) => ({ id: f.url, url: f.url, name: f.name, size: f.size ?? 0, type: f.type ?? '' }))

const fromUploaded = (list: UploadedFile[]): StoredFile[] => list.map((f) => ({ name: f.name, url: f.url, size: f.size, type: f.type }))

export function FormRenderer({ def, values, onChange, errors = {}, disabled, preview, idPrefix = 'fb' }: Props) {
  const { visible } = visibleValues(def, values)
  return (
    <div className="flex flex-col gap-5">
      {def.fields.map((f) =>
        visible.has(f.key) ? (
          <FieldView key={f.key} f={f} id={`${idPrefix}-${f.key}`} value={values[f.key]} error={errors[f.key]} disabled={disabled} preview={preview} onChange={(v) => onChange(f.key, v)} />
        ) : null,
      )}
    </div>
  )
}

interface FieldProps {
  f: FormField
  id: string
  value: unknown
  error?: string
  disabled?: boolean
  preview?: boolean
  onChange: (v: unknown) => void
}

function FieldView({ f, id, value, error, disabled, preview, onChange }: FieldProps) {
  if (f.type === 'section') {
    return (
      <div className="border-b border-border pb-2">
        <h3 className="text-sm font-semibold text-foreground">{f.title}</h3>
        {f.description ? <p className="mt-1 text-xs text-muted-foreground">{f.description}</p> : null}
      </div>
    )
  }
  const describedBy = error ? `${id}-err` : f.description ? `${id}-desc` : undefined
  return (
    <div className="flex flex-col gap-2" data-field={f.key}>
      <Label htmlFor={id} className="flex items-center gap-1">
        <span>{f.title}</span>
        {f.required ? <span className="text-destructive" aria-hidden="true">*</span> : null}
      </Label>
      <Control f={f} id={id} value={value} disabled={disabled} preview={preview} onChange={onChange} describedBy={describedBy} invalid={!!error} />
      {f.description ? (
        <p id={`${id}-desc`} className="text-xs text-muted-foreground">
          {f.description}
        </p>
      ) : null}
      {error ? (
        <p id={`${id}-err`} role="alert" className="text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

interface ControlProps {
  f: FormField
  id: string
  value: unknown
  disabled?: boolean
  preview?: boolean
  onChange: (v: unknown) => void
  describedBy?: string
  invalid: boolean
}

function Control({ f, id, value, disabled, preview, onChange, describedBy, invalid }: ControlProps) {
  const { t } = useTranslation()
  const aria = { 'aria-describedby': describedBy, 'aria-invalid': invalid || undefined }
  const opts = f.options ?? []
  switch (f.type) {
    case 'text':
      return <Input id={id} value={(value as string) ?? ''} disabled={disabled} placeholder={f.placeholder} maxLength={f.maxLen ?? 500} onChange={(e) => onChange(e.target.value)} {...aria} />
    case 'textarea':
      return <Textarea id={id} rows={4} value={(value as string) ?? ''} disabled={disabled} placeholder={f.placeholder} maxLength={f.maxLen ?? 5000} onChange={(e) => onChange(e.target.value)} {...aria} />
    case 'number':
      return (
        <Input
          id={id}
          type="number"
          inputMode="decimal"
          value={typeof value === 'number' ? value : ''}
          disabled={disabled}
          placeholder={f.placeholder}
          min={f.min}
          max={f.max}
          step={f.integer ? 1 : 'any'}
          onChange={(e) => onChange(e.target.value === '' ? undefined : Number(e.target.value))}
          {...aria}
        />
      )
    case 'radio':
      return (
        <RadioGroup id={id} value={(value as string) ?? ''} onValueChange={onChange} disabled={disabled} {...aria}>
          {opts.map((o) => (
            <label key={o.value} className="flex min-h-10 items-center gap-2 text-sm">
              <RadioGroupItem value={o.value} />
              <span>{o.label}</span>
            </label>
          ))}
        </RadioGroup>
      )
    case 'checkbox':
      return <CheckList id={id} opts={opts} value={value} disabled={disabled} onChange={onChange} />
    case 'select':
      if (preview && f.dictType && opts.length === 0) {
        return <DictSelect id={id} dict={f.dictType} multiple={f.multiple} value={value as string | string[] | undefined} onChange={onChange} disabled={disabled} placeholder={f.placeholder} />
      }
      if (f.multiple) return <CheckList id={id} opts={opts} value={value} disabled={disabled} onChange={onChange} />
      return (
        <Select value={(value as string) ?? ''} onValueChange={onChange} disabled={disabled}>
          <SelectTrigger id={id} {...aria}>
            <SelectValue placeholder={f.placeholder || t('common.pleaseSelect')} />
          </SelectTrigger>
          <SelectContent>
            {opts.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )
    case 'date':
      return <DatePicker id={id} value={(value as string) ?? ''} onChange={(v) => onChange(v || undefined)} disabled={disabled} placeholder={f.placeholder} fromDate={f.minDate ? new Date(f.minDate) : undefined} toDate={f.maxDate ? new Date(f.maxDate) : undefined} />
    case 'switch':
      return <Switch id={id} checked={value === true} onCheckedChange={onChange} disabled={disabled} {...aria} />
    case 'rating':
      return <Rating id={id} max={f.max ?? 5} value={typeof value === 'number' ? value : 0} disabled={disabled} onChange={onChange} />
    case 'file':
      return (
        <Uploader
          kind="file"
          multiple
          maxCount={f.max ?? 5}
          value={toUploaded(value)}
          onChange={(list) => onChange(list.length ? fromUploaded(list) : undefined)}
          disabled={disabled}
        />
      )
    default:
      return null
  }
}

function CheckList({ id, opts, value, disabled, onChange }: { id: string; opts: Array<{ label: string; value: string }>; value: unknown; disabled?: boolean; onChange: (v: unknown) => void }) {
  const cur = Array.isArray(value) ? (value as string[]) : []
  return (
    <div id={id} role="group" className="flex flex-col gap-1">
      {opts.map((o) => (
        <label key={o.value} className="flex min-h-10 items-center gap-2 text-sm">
          <Checkbox
            checked={cur.includes(o.value)}
            disabled={disabled}
            onCheckedChange={(c) => {
              const next = c === true ? [...cur, o.value] : cur.filter((x) => x !== o.value)
              onChange(next.length ? next : undefined)
            }}
          />
          <span>{o.label}</span>
        </label>
      ))}
    </div>
  )
}

function Rating({ id, max, value, disabled, onChange }: { id: string; max: number; value: number; disabled?: boolean; onChange: (v: unknown) => void }) {
  const { t } = useTranslation()
  return (
    <div id={id} role="radiogroup" aria-label={t('plugin_formbuilder.types.rating')} className="flex flex-wrap items-center gap-1">
      {Array.from({ length: max }, (_, i) => i + 1).map((n) => (
        <Button
          key={n}
          type="button"
          variant="ghost"
          size="icon"
          role="radio"
          aria-checked={value === n}
          aria-label={t('plugin_formbuilder.stars', { n })}
          disabled={disabled}
          className="size-10 md:size-9"
          onClick={() => onChange(value === n ? undefined : n)}
        >
          <Star className={cn('size-5', n <= value ? 'fill-primary text-primary' : 'text-muted-foreground')} aria-hidden="true" />
        </Button>
      ))}
    </div>
  )
}
