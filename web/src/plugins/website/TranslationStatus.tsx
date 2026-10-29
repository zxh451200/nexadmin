import { useTranslation } from 'react-i18next'
import { REVIEW_KEY } from '@/framework/components/i18n/locale-form'
import { Badge } from '@/framework/ui/badge'
import { useLanguages } from './api'

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
 * 列表里的译文完成度：每种非默认语言一个小标签，填满显示 ✓。
 * 传入 onFocus 时标签可点，直接打开编辑弹窗并定位到那门语言——
 * 语言一多，「哪些内容缺这门语言的译文」要比挨个点弹窗好回答。
 */
export default function TranslationStatus({
  value, fields, onFocus,
}: {
  value: string
  fields: string[]
  onFocus?: (code: string) => void
}) {
  const { t } = useTranslation()
  const { data: languages = [] } = useLanguages()
  const extra = languages.filter((l) => !l.default)
  if (!extra.length) return null
  const data = parse(value)
  const review = (data[REVIEW_KEY] ?? {}) as Record<string, string>
  return (
    <div className="flex flex-wrap items-center gap-1">
      {extra.map((lang) => {
        const done = fields.filter((k) => (data[lang.code]?.[k] ?? '').trim() !== '').length
        const full = done === fields.length
        // 机翻未校对优先于「已填满」：填满但没人看过，不能显示成 ✓。
        const machine = review[lang.code] === 'mt'
        const title = machine
          ? t('plugin_website.i18n.machineHint')
          : t('plugin_website.i18n.statusHint', { done, total: fields.length })
        const cls = 'num text-[10px]'
        const variant = machine ? 'warning' : full ? 'success' : 'outline'
        const label = `${lang.code.split('-')[0].toUpperCase()} ${full ? '✓' : `${done}/${fields.length}`}${machine ? ' · ' + t('plugin_website.i18n.machineShort') : ''}`
        return onFocus ? (
          <button
            key={lang.code}
            type="button"
            className="rounded-md focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => onFocus(lang.code)}
            aria-label={t('plugin_website.i18n.focusLang', { lang: lang.label })}
          >
            <Badge variant={variant} className={cls} title={title}>
              {label}
            </Badge>
          </button>
        ) : (
          <Badge key={lang.code} variant={variant} className={cls} title={title}>
            {label}
          </Badge>
        )
      })}
    </div>
  )
}
