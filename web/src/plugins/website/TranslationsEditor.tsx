import { useTranslation } from 'react-i18next'
import { RichTextEditor } from '@/framework/components/RichTextEditor'
import { Input } from '@/framework/ui/input'
import { Textarea } from '@/framework/ui/textarea'
import { useLanguages } from './api'

export interface TranslationField {
  key: string
  label: string
  multiline?: boolean
  rich?: boolean
  rows?: number
}

type TranslationValue = Record<string, Record<string, string>>

function parse(value: string): TranslationValue {
  if (!value) return {}
  try {
    const parsed = JSON.parse(value)
    return parsed && typeof parsed === 'object' ? (parsed as TranslationValue) : {}
  } catch {
    return {}
  }
}

/**
 * 多语言内容编辑器：按后台配置的语言自动生成输入框，保存为 i18nJson。
 * 新增语言只需在官网 locales/languages.json 加一项，这里无需改代码。
 */
export default function TranslationsEditor({
  value, onChange, fields,
}: {
  value: string
  onChange: (value: string) => void
  fields: TranslationField[]
}) {
  const { t } = useTranslation()
  const { data: languages = [] } = useLanguages()
  const extra = languages.filter((l) => !l.default)
  if (!extra.length) return null

  const data = parse(value)
  const update = (code: string, key: string, next: string) => {
    const merged: TranslationValue = { ...data, [code]: { ...(data[code] ?? {}) } }
    if (next) merged[code][key] = next
    else delete merged[code][key]
    if (!Object.keys(merged[code]).length) delete merged[code]
    onChange(Object.keys(merged).length ? JSON.stringify(merged) : '')
  }

  return (
    <section className="flex flex-col gap-3 rounded-lg border bg-muted/30 p-3">
      <div className="flex flex-col gap-0.5">
        <span className="text-sm font-medium">{t('plugin_website.i18n.title')}</span>
        <span className="text-xs text-muted-foreground">{t('plugin_website.i18n.hint')}</span>
      </div>
      {extra.map((lang) => (
        <div key={lang.code} data-lang={lang.code} className="flex flex-col gap-2 border-t pt-3">
          <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{lang.label}</span>
          {fields.map((field) => (
            <div key={field.key} data-field={field.key} className="flex flex-col gap-1 text-sm">
              <span>{field.label}</span>
              {field.rich ? (
                <RichTextEditor
                  value={data[lang.code]?.[field.key] ?? ''}
                  onChange={(next) => update(lang.code, field.key, next)}
                />
              ) : field.multiline ? (
                <Textarea
                  rows={field.rows ?? 2}
                  value={data[lang.code]?.[field.key] ?? ''}
                  onChange={(e) => update(lang.code, field.key, e.target.value)}
                />
              ) : (
                <Input
                  value={data[lang.code]?.[field.key] ?? ''}
                  onChange={(e) => update(lang.code, field.key, e.target.value)}
                />
              )}
            </div>
          ))}
        </div>
      ))}
    </section>
  )
}
