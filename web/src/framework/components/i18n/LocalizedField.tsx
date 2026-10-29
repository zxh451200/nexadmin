import { useTranslation } from 'react-i18next'
import { ChevronDown } from 'lucide-react'
import { AiWriteButton } from '@/framework/components/AiWriteButton'
import { RichTextEditor } from '@/framework/components/RichTextEditor'
import { RichTextView } from '@/framework/components/RichTextView'
import { Input } from '@/framework/ui/input'
import { Textarea } from '@/framework/ui/textarea'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/framework/ui/collapsible'
import { cn } from '@/lib/utils'
import { useLocaleForm } from './LocaleFormProvider'
import { SourceHint } from './SourceHint'

export interface LocalizedFieldProps {
  /** 必须是实体 schema 里声明过的可翻译字段，否则不渲染 */
  fieldKey: string
  placeholder?: string
  /** 星号只在原文语言下出现：译文留空是合法状态，渲染时回退原文 */
  required?: boolean
  className?: string
}

/** 可翻译字段的唯一入口：标签 + 控件 + 原文对照。切语言换的是数据源，不是表单结构。 */
export function LocalizedField({ fieldKey, placeholder, required, className }: LocalizedFieldProps) {
  const { t } = useTranslation()
  const { spec, field, setField, source, isSource, fieldLabel, active, markMachine } = useLocaleForm()

  const f = spec(fieldKey)
  if (!f) return null

  const value = field(fieldKey)
  const src = source(fieldKey)
  const rich = f.widget === 'rich'
  const inputId = `lf-${active}-${fieldKey}`
  const caption = (
    <span className="inline-flex items-baseline">
      {fieldLabel(fieldKey)}
      {required && isSource ? (
        <span aria-hidden="true" className="ms-0.5 text-destructive">*</span>
      ) : null}
    </span>
  )

  // 逐字段 AI 起草/润色：与「按语言批量翻译填充」（LocaleFormActions）互补——
  // 批量那条要有原文才有活可干，起草空字段只能靠这里。未启用 AI 时组件自己返回 null。
  const ai = (
    <AiWriteButton
      className="h-7"
      target={f.widget === 'text' ? 'title' : 'body'}
      format={rich ? 'html' : 'text'}
      getText={() => field(fieldKey)}
      getBrief={f.widget === 'text' || fieldKey === 'title' ? undefined : () => field('title')}
      onResult={(text) => {
        setField(fieldKey, rich ? text : text.slice(0, f.max ?? text.length))
        // 写到译文页签下的内容一律把该语言降回「机翻未校对」：已经人工校对过的语言被 AI 改写后
        // 必须重新看一眼，否则「已校对」就成了假状态。原文页签不受影响。
        if (!isSource) markMachine()
      }}
    />
  )

  if (rich) {
    // 原文是 HTML，只能按富文本渲染；折叠面板默认收起，不翻正文的人完全感知不到它。
    if (isSource || !src.trim()) {
      return (
        <div className={cn('flex min-w-0 flex-col gap-1 text-sm', className)}>
          <div className="flex min-h-7 items-center justify-between gap-2">
            {caption}
            {ai}
          </div>
          <RichTextEditor value={value} onChange={(next) => setField(fieldKey, next)} />
        </div>
      )
    }
    return (
      <div className={cn('flex min-w-0 flex-col gap-1 text-sm', className)}>
        <Collapsible className="flex flex-col gap-1">
          <div className="flex min-h-7 items-center justify-between gap-2">
            {caption}
            <div className="flex items-center gap-2">
              {ai}
              <CollapsibleTrigger className="inline-flex items-center gap-1 rounded text-xs text-muted-foreground underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring">
                {t('localeForm.source')}
                <ChevronDown className="size-3.5 transition-transform" aria-hidden="true" />
              </CollapsibleTrigger>
            </div>
          </div>
          <CollapsibleContent>
            <div className="rounded-md border border-dashed bg-muted/40">
              <RichTextView html={src} className="px-3 py-2 text-sm" />
            </div>
          </CollapsibleContent>
        </Collapsible>
        <RichTextEditor value={value} onChange={(next) => setField(fieldKey, next)} />
      </div>
    )
  }

  return (
    <div className={cn('flex min-w-0 flex-col gap-1 text-sm', className)}>
      <div className="flex min-h-7 items-center justify-between gap-2">
        <label htmlFor={inputId}>{caption}</label>
        {ai}
      </div>
      {f.widget === 'textarea' ? (
        <Textarea
          id={inputId}
          rows={f.rows ?? 2}
          maxLength={f.max}
          placeholder={placeholder}
          value={value}
          onChange={(e) => setField(fieldKey, e.target.value)}
        />
      ) : (
        <Input
          id={inputId}
          maxLength={f.max}
          placeholder={placeholder}
          value={value}
          onChange={(e) => setField(fieldKey, e.target.value)}
        />
      )}
      {!isSource ? <SourceHint value={src} /> : null}
    </div>
  )
}
