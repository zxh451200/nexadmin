import { useState } from 'react'
import { ArrowDown, ArrowUp, Copy, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import type { Cond, FormField } from './api'
import { CondEditor } from './CondEditor'
import { Field } from './controls'
import { FieldRules } from './FieldRules'
import { MaxTitle } from './limits'
import { hasValue, KEY_RE, usesOptions } from './schema'

const PLACEHOLDER_TYPES = ['text', 'textarea', 'number', 'select', 'date']

interface Props {
  field: FormField
  index: number
  total: number
  /** 可被显隐条件引用的字段（已排除当前字段及其后的字段；字典绑定的选项已展开） */
  refs: FormField[]
  usedKeys: string[]
  onChange: (next: FormField) => void
  onRename: (key: string) => void
  onMove: (delta: -1 | 1) => void
  onDuplicate: () => void
  onRemove: () => void
}

/** 选中字段的属性面板：基础、类型规则、显隐条件 */
export function FieldProps({ field: f, index, total, refs, usedKeys, onChange, onRename, onMove, onDuplicate, onRemove }: Props) {
  const { t } = useTranslation()
  const id = `fb-prop-${index}`
  const [keyText, setKeyText] = useState(f.key)
  const keyBad = !KEY_RE.test(keyText) || (keyText !== f.key && usedKeys.includes(keyText))

  const patch = (p: Partial<FormField>) => {
    const next = { ...f, ...p }
    // 选项变了：默认值里已不存在的取值要去掉，否则服务端会判定默认值不合法
    if ('options' in p && next.default !== undefined && usesOptions(next)) {
      const values = new Set((next.options ?? []).map((o) => o.value))
      if (Array.isArray(next.default)) {
        const kept = next.default.filter((v) => values.has(String(v)))
        next.default = kept.length ? kept : undefined
      } else if (!values.has(String(next.default))) next.default = undefined
    }
    onChange(next)
  }
  const commitKey = () => {
    if (keyText === f.key) return
    if (keyBad) setKeyText(f.key)
    else onRename(keyText)
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2">
        <h3 className="min-w-0 truncate text-sm font-semibold">{t(`plugin_formbuilder.types.${f.type}`)}</h3>
        <div className="flex shrink-0 items-center">
          <Button type="button" variant="ghost" size="icon" disabled={index === 0} aria-label={t('plugin_formbuilder.designer.moveUp')} title={t('plugin_formbuilder.designer.moveUp')} onClick={() => onMove(-1)}>
            <ArrowUp aria-hidden="true" />
          </Button>
          <Button type="button" variant="ghost" size="icon" disabled={index === total - 1} aria-label={t('plugin_formbuilder.designer.moveDown')} title={t('plugin_formbuilder.designer.moveDown')} onClick={() => onMove(1)}>
            <ArrowDown aria-hidden="true" />
          </Button>
          <Button type="button" variant="ghost" size="icon" aria-label={t('plugin_formbuilder.designer.duplicate')} title={t('plugin_formbuilder.designer.duplicate')} onClick={onDuplicate}>
            <Copy aria-hidden="true" />
          </Button>
          <Button type="button" variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} title={t('common.delete')} onClick={onRemove}>
            <Trash2 aria-hidden="true" />
          </Button>
        </div>
      </div>

      <Field id={`${id}-title`} label={t('plugin_formbuilder.prop.title')}>
        <Input id={`${id}-title`} value={f.title} maxLength={MaxTitle} aria-invalid={!f.title.trim() || undefined} onChange={(e) => patch({ title: e.target.value })} />
      </Field>
      <Field id={`${id}-desc`} label={t('plugin_formbuilder.prop.description')}>
        <Textarea id={`${id}-desc`} rows={2} maxLength={1000} value={f.description ?? ''} onChange={(e) => patch({ description: e.target.value || undefined })} />
      </Field>
      {PLACEHOLDER_TYPES.includes(f.type) ? (
        <Field id={`${id}-ph`} label={t('plugin_formbuilder.prop.placeholder')}>
          <Input id={`${id}-ph`} maxLength={200} value={f.placeholder ?? ''} onChange={(e) => patch({ placeholder: e.target.value || undefined })} />
        </Field>
      ) : null}
      {hasValue(f) ? (
        <div className="flex items-center justify-between gap-3">
          <label htmlFor={`${id}-required`} className="text-sm font-medium">
            {f.type === 'switch' ? t('plugin_formbuilder.prop.mustOn') : t('plugin_formbuilder.prop.required')}
          </label>
          <Switch id={`${id}-required`} checked={!!f.required} onCheckedChange={(v) => patch({ required: v || undefined })} />
        </div>
      ) : null}

      {hasValue(f) ? <FieldRules f={f} id={id} patch={patch} /> : null}

      {hasValue(f) ? (
        <Field id={`${id}-key`} label={t('plugin_formbuilder.prop.key')} hint={keyBad ? t('plugin_formbuilder.prop.keyInvalid') : t('plugin_formbuilder.prop.keyHint')}>
          <Input
            id={`${id}-key`}
            className="font-mono"
            maxLength={32}
            value={keyText}
            aria-invalid={keyBad || undefined}
            onChange={(e) => setKeyText(e.target.value)}
            onBlur={commitKey}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                commitKey()
              }
            }}
          />
        </Field>
      ) : null}

      <section className="flex flex-col gap-2 border-t border-border pt-4" aria-label={t('plugin_formbuilder.prop.showIf')}>
        <h4 className="text-sm font-medium">{t('plugin_formbuilder.prop.showIf')}</h4>
        <p className="text-xs text-muted-foreground">{t('plugin_formbuilder.prop.showIfHint')}</p>
        <CondEditor value={f.showIf} refs={refs} onChange={(c: Cond | undefined) => patch({ showIf: c })} />
      </section>
    </div>
  )
}
